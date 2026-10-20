package application

import (
	"bytes"
	"context"
	"encoding/json/v2"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/example/main-service/internal/platform/analysisdoc"
)

const (
	liveMLSourceOff            = "ngrams"
	liveMLSourceOn             = "ml"
	liveMLExtractTimeout       = 300 * time.Second
	liveLimitMLExtractFallback = "ml_extract_fallback"
	liveLimitMLExtractDegraded = "ml_extract_degraded"
	liveMLVersionHeader        = "X-Radar-Classifier-Version"
	liveChannelBytes           = 8 << 20
	// Исходные тексты двухсот записей канала в 8 МиБ не помещаются.
	liveMLChannelBytes = 64 << 20
)

type LiveMLExtractDocument struct {
	DocID      string   `json:"doc_id"`
	Source     string   `json:"source"`
	Title      string   `json:"title"`
	Abstract   string   `json:"abstract"`
	Text       string   `json:"text"`
	Lang       string   `json:"lang"`
	SourceType string   `json:"source_type"`
	TrustLevel string   `json:"trust_level"`
	Date       *string  `json:"date"`
	URL        string   `json:"url"`
	Authors    []string `json:"authors"`
}

type LiveMLExtractRequest struct {
	ObservationDate string                  `json:"observation_date,omitempty"`
	Documents       []LiveMLExtractDocument `json:"documents"`
	Query           string                  `json:"query"`
	ModelVersion    string                  `json:"model_version"`
}

// LiveMLExtractStage только показывается: вердикт выдачи от стадии extractor не зависит.
type LiveMLExtractStage struct {
	Code  int     `json:"code"`
	Label string  `json:"label,omitempty"`
	Quote string  `json:"quote,omitempty"`
	DocID string  `json:"doc_id,omitempty"`
	Prob  float64 `json:"prob,omitempty"`
}

type LiveMLExtractCandidate struct {
	ID          string             `json:"id"`
	Name        string             `json:"name"`
	CanonicalRU string             `json:"canonical_ru"`
	DocIDs      []string           `json:"doc_ids"`
	Stage       LiveMLExtractStage `json:"stage"`
	Trend       struct {
		Code int `json:"code"`
	} `json:"trend"`
	Flags []string `json:"flags,omitempty"`
	Score int      `json:"score"`
	Label string   `json:"label,omitempty"`
	Value float64  `json:"value,omitempty"`
}

type LiveMLExtractCoverage struct {
	DocumentsSkippedLanguage int      `json:"documents_skipped_language,omitempty"`
	DocumentsFullText        int      `json:"documents_full_text,omitempty"`
	DocumentsAbstractOnly    int      `json:"documents_abstract_only,omitempty"`
	DocumentsTextTruncated   int      `json:"documents_text_truncated,omitempty"`
	DocumentsTextDropped     int      `json:"documents_text_dropped,omitempty"`
	DocumentsOmitted         int      `json:"documents_omitted,omitempty"`
	DocumentsIn              int      `json:"documents_in"`
	DocumentsProcessed       int      `json:"documents_processed"`
	DocumentsFailed          int      `json:"documents_failed"`
	SpansTotal               int      `json:"spans_total"`
	DocumentsMultiWindow     *int     `json:"documents_multi_window,omitempty"`
	CandidatesTotal          int      `json:"candidates_total"`
	SentencesScored          int      `json:"sentences_scored"`
	StageConfirmed           int      `json:"stage_confirmed"`
	CacheHits                int      `json:"cache_hits"`
	CacheMisses              int      `json:"cache_misses"`
	Degraded                 bool     `json:"degraded"`
	Notes                    []string `json:"notes,omitempty"`
}

type LiveMLExtractResponse struct {
	Candidates    []LiveMLExtractCandidate `json:"candidates"`
	Coverage      LiveMLExtractCoverage    `json:"coverage"`
	ModelVersions map[string]string        `json:"model_versions"`
}

type LiveMLExtractionRun struct {
	Requested      string                 `json:"requested"`
	Used           string                 `json:"used"`
	Version        string                 `json:"version"`
	FallbackReason string                 `json:"fallback_reason,omitempty"`
	Coverage       *LiveMLExtractCoverage `json:"coverage,omitempty"`
	ModelVersions  map[string]string      `json:"model_versions,omitempty"`
}

type MLCandidateExtractor func(context.Context, LiveMLExtractRequest) (LiveMLExtractResponse, error)

type liveMLSource struct {
	extract MLCandidateExtractor
	version string
}

// SetMLCandidateSource вызывается до запуска и после SetSearchStore: он оборачивает уже заданное хранилище.
func (s *TrendLiveService) SetMLCandidateSource(source string, extract MLCandidateExtractor, version string) {
	switch source = strings.TrimSpace(source); source {
	case "", liveMLSourceOff:
		s.ml = nil
	case liveMLSourceOn:
		s.ml = &liveMLSource{extract: extract, version: strings.TrimSpace(version)}
		if extract == nil || s.ml.version == "" {
			slog.Error("radar candidate source ml is not configured; every search falls back to ngrams",
				"extract_url_set", extract != nil, "version_set", s.ml.version != "")
		}
	default:
		s.ml = nil
		slog.Error("radar candidate source is unknown; using ngrams", "source", source)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	store := s.searchStore
	if view, ok := store.(liveMLSearchStore); ok {
		store = view.LiveSearchStore
	}
	if s.ml != nil && store != nil {
		store = liveMLSearchStore{store}
	}
	s.searchStore = store
}

func NewMLCandidateExtractor(endpoint string, timeout time.Duration, requestBytes, responseBytes int) MLCandidateExtractor {
	return newMLCandidateExtractor(endpoint, timeout, nil, requestBytes, responseBytes)
}

func newMLCandidateExtractor(endpoint string, timeout time.Duration, transport http.RoundTripper,
	requestBytes, responseBytes int,
) MLCandidateExtractor {
	if endpoint = strings.TrimSpace(endpoint); endpoint == "" {
		return nil
	}
	if timeout <= 0 {
		timeout = liveMLExtractTimeout
	}
	if requestBytes <= 0 {
		requestBytes = liveClassifierInputBytes
	}
	if responseBytes <= 0 {
		responseBytes = liveClassifierBytes
	}
	client := &http.Client{Timeout: timeout, Transport: transport, CheckRedirect: func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	}}
	return func(ctx context.Context, input LiveMLExtractRequest) (LiveMLExtractResponse, error) {
		var output LiveMLExtractResponse
		request, err := liveMLExtractHTTPRequest(ctx, endpoint, input, requestBytes)
		if err != nil {
			return output, err
		}
		defer func() { _ = request.Body.Close() }()
		response, err := client.Do(request)
		if err != nil {
			return output, errors.New("сервис извлечения не ответил")
		}
		defer func() { _ = response.Body.Close() }()
		if response.StatusCode != http.StatusOK {
			return output, fmt.Errorf("сервис извлечения ответил HTTP %d", response.StatusCode)
		}
		if response.Header.Get(liveMLVersionHeader) != input.ModelVersion {
			return output, errors.New("версия извлечения в заголовке не совпадает с запрошенной")
		}
		raw, err := io.ReadAll(io.LimitReader(response.Body, int64(responseBytes)+1))
		if err != nil || len(raw) > responseBytes {
			return output, fmt.Errorf("ответ извлечения не прочитан или превышает предел %d байт", responseBytes)
		}
		if err := json.Unmarshal(raw, &output); err != nil {
			return output, errors.New("неверный формат ответа извлечения")
		}
		if output.Candidates == nil || output.ModelVersions["service"] != input.ModelVersion {
			return output, errors.New("в ответе извлечения отсутствует список кандидатов или согласованная версия")
		}
		return output, nil
	}
}

func liveMLExtractHTTPRequest(
	ctx context.Context, endpoint string, input LiveMLExtractRequest, maxBytes int,
) (*http.Request, error) {
	for _, document := range input.Documents {
		if document.Lang != "ru" && document.Lang != "en" {
			return nil, fmt.Errorf("язык документа %q не поддерживается извлечением: %q", document.DocID, document.Lang)
		}
	}
	if !liveServiceURL(endpoint) {
		return nil, errors.New("неверный адрес извлечения")
	}
	if input.ModelVersion == "" {
		return nil, errors.New("не задана версия извлечения")
	}
	body, err := json.Marshal(input)
	if err != nil {
		return nil, fmt.Errorf("запрос извлечения: %w", err)
	}
	if len(body) > maxBytes {
		return nil, &liveMLExtractTooLargeError{limit: maxBytes}
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("запрос извлечения: %w", err)
	}
	request.Header.Set("Content-Type", "application/json")
	return request, nil
}

// liveMLExtractTooLargeError: пакет не отправлялся, поэтому поиск урезает его и повторяет запрос.
type liveMLExtractTooLargeError struct{ limit int }

func (e *liveMLExtractTooLargeError) Error() string {
	return fmt.Sprintf("вход модели превышает предел %d байт", e.limit)
}

type liveMLSearchKey struct{}

// liveMLContext даёт /extract и /predict одну дату наблюдения, а тексты каналов — только разовому поиску ml.
func (s *TrendLiveService) liveMLContext(ctx context.Context) context.Context {
	if s.ml == nil {
		return ctx
	}
	return context.WithValue(ctx, liveMLSearchKey{}, s.now().UTC())
}

// queryChannel переспрашивает без текста ответ, не уложившийся в предел: потерять канал хуже, чем тексты.
func (src *liveSources) queryChannel(ctx context.Context, base string, body []byte, periodCount int) (*LiveChannel, error) {
	endpoint := base + "/v1/query"
	if _, ml := ctx.Value(liveMLSearchKey{}).(time.Time); !ml {
		return src.queryChannelAt(ctx, endpoint, liveChannelBytes, body, periodCount)
	}
	channel, err := src.queryChannelAt(ctx, endpoint+"?full_text=1", liveMLChannelBytes, body, periodCount)
	if err == nil || ctx.Err() != nil || !liveMLChannelRetryable(err) {
		return channel, err
	}
	slog.Warn("radar channel full text failed; retrying without text", "endpoint", endpoint, "error", err)
	return src.queryChannelAt(ctx, endpoint, liveChannelBytes, body, periodCount)
}

// Обрезанный пределом ответ даёт ErrUnexpectedEOF, долгий — DeadlineExceeded; прочие отказы от текста не зависят.
func liveMLChannelRetryable(err error) bool {
	return errors.Is(err, io.ErrUnexpectedEOF) || errors.Is(err, context.DeadlineExceeded)
}

func (s *TrendLiveService) mlObservationDate(ctx context.Context) time.Time {
	if observed, ok := ctx.Value(liveMLSearchKey{}).(time.Time); ok {
		return observed
	}
	if s.now != nil {
		return s.now().UTC()
	}
	return time.Now().UTC()
}

// Откат и неполное извлечение — итог одной попытки, а не ответ на сутки.
func (s *TrendLiveService) reusableMLExtraction(run *LiveMLExtractionRun) bool {
	if s.ml == nil {
		return run == nil
	}
	return run != nil && run.Requested == liveMLSourceOn && run.Used == liveMLSourceOn &&
		run.Version == s.ml.version && run.FallbackReason == "" && run.Coverage != nil &&
		!run.Coverage.Degraded && run.Coverage.DocumentsFailed == 0
}

// liveMLStoreMark разводит режимы в хранилище: под одним ключом они затирали бы ответы друг друга.
const liveMLStoreMark = "+ml"

type liveMLSearchStore struct{ LiveSearchStore }

func liveMLStoreKey(key string) string {
	contract, rest, ok := strings.Cut(key, "|")
	if !ok {
		return key
	}
	return contract + liveMLStoreMark + "|" + rest
}

// liveMLPlainKey снимает отметку режима; ключ другого режима даёт пустую строку.
func liveMLPlainKey(key string) string {
	marked, rest, ok := strings.Cut(key, "|")
	contract, ml := strings.CutSuffix(marked, liveMLStoreMark)
	if !ok || !ml {
		return ""
	}
	return contract + "|" + rest
}

func (st liveMLSearchStore) ReadLiveSearch(ctx context.Context, req analysisdoc.LiveSearchRead,
) (analysisdoc.LiveSearchStored, error) {
	req.Query, req.TermsKey = liveMLStoreKey(req.Query), liveMLStoreKey(req.TermsKey)
	stored, err := st.LiveSearchStore.ReadLiveSearch(ctx, req)
	if stored.Found {
		stored.Query, stored.TermsKey = liveMLPlainKey(stored.Query), liveMLPlainKey(stored.TermsKey)
	}
	return stored, err
}

func (st liveMLSearchStore) SaveLiveSearch(ctx context.Context, req analysisdoc.LiveSearchSave,
) (analysisdoc.LiveSearchStored, error) {
	req.Query, req.TermsKey = liveMLStoreKey(req.Query), liveMLStoreKey(req.TermsKey)
	return st.LiveSearchStore.SaveLiveSearch(ctx, req)
}

func (st liveMLSearchStore) ListLiveSearches(ctx context.Context, req analysisdoc.LiveSearchList,
) (analysisdoc.LiveSearchHistory, error) {
	history, err := st.LiveSearchStore.ListLiveSearches(ctx, req)
	kept := history.Entries[:0]
	for _, entry := range history.Entries {
		if entry.Query = liveMLPlainKey(entry.Query); entry.Query != "" {
			kept = append(kept, entry)
		}
	}
	history.Entries = kept
	return history, err
}

// Цитаты ML-кандидата бывают из документов владельца: снятие Corpus перед общим хранилищем их не удаляет.
func liveMLOwnerEvidence(detail LiveDetail) bool {
	return detail.Extraction != nil && detail.Topic.Corpus != nil && len(detail.Topic.Corpus.Records) > 0
}

// liveMLDropTexts: исходные тексты нужны только входу /extract и не уходят в ответ, хранилище и карточки.
func liveMLDropTexts(topic *LiveTopic) {
	for _, channel := range []*LiveChannel{topic.Patents, topic.PatentsCN, topic.Registry, topic.Web, topic.Corpus, topic.Research} {
		for i := range liveChannelRecords(channel) {
			channel.Records[i].Text = ""
		}
	}
}
