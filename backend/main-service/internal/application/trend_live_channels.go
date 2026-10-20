package application

import (
	"bytes"
	"cmp"
	"context"
	"encoding/json/v2"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"slices"
	"strings"
	"sync"
	"time"
)

// LiveChannel с nil в рядах — история не собиралась; частичные счёты — нижняя граница, а не оценка всего веба.
type LiveChannel struct {
	SourceSet  string               `json:"source_set"`
	Coverage   []LiveSourceCoverage `json:"coverage,omitempty"`
	Source     string               `json:"source"`
	ObservedAt string               `json:"observed_at"`
	Counts     []*int               `json:"counts"`
	Total      int                  `json:"total"`
	Partial    bool                 `json:"partial"`
	Limits     []string             `json:"limits"`
	Records    []LiveSourceRecord   `json:"records"`
}

type LiveSourceCoverage struct {
	Source        string                 `json:"source"`
	From          string                 `json:"from"`
	Through       string                 `json:"through"`
	Received      int                    `json:"received"`
	Titles        int                    `json:"titles"`
	Content       int                    `json:"content"`
	MissingTitles int                    `json:"missing_titles"`
	Families      int                    `json:"families"`
	ProviderTotal *int                   `json:"provider_total"`
	NextCursor    string                 `json:"next_cursor"`
	StopReason    string                 `json:"stop_reason"`
	Exhausted     bool                   `json:"exhausted"`
	ObservedAt    string                 `json:"observed_at"`
	Pages         []LiveSourceProvenance `json:"pages"`
}

type LiveSourceProvenance struct {
	URL    string `json:"url"`
	SHA256 string `json:"sha256"`
}

type LiveSourceRecord struct {
	DOI           string   `json:"doi,omitempty"`
	DOIAliases    []string `json:"doi_aliases,omitempty"`
	Authors       []string `json:"authors,omitempty"`
	ID            string   `json:"id"`
	Title         string   `json:"title"`
	Date          string   `json:"date"`
	URL           string   `json:"url"`
	Source        string   `json:"source"`
	Language      string   `json:"language,omitempty"`
	LanguageBasis string   `json:"language_basis,omitempty"`
	PublisherType string   `json:"publisher_type,omitempty"`
	// Genre — что это за материал; издатель отвечает на «кто опубликовал».
	Genre         string                 `json:"genre,omitempty"`
	Trust         string                 `json:"trust,omitempty"`
	TrustBasis    string                 `json:"trust_basis,omitempty"`
	PublishedAt   string                 `json:"published_at,omitempty"`
	AvailableAt   string                 `json:"available_at,omitempty"`
	ObservedAt    string                 `json:"observed_at,omitempty"`
	TextStatus    string                 `json:"text_status,omitempty"`
	Text          string                 `json:"text,omitempty"` // только ?full_text=1 режима ml
	TitleBasis    string                 `json:"title_basis,omitempty"`
	DateBasis     string                 `json:"date_basis,omitempty"`
	Filing        string                 `json:"filing,omitempty"`
	Priority      string                 `json:"priority,omitempty"`
	Application   string                 `json:"application,omitempty"`
	FamilyID      string                 `json:"family_id,omitempty"`
	FamilyBasis   string                 `json:"family_basis,omitempty"`
	CanonicalURL  string                 `json:"canonical_url,omitempty"`
	OriginalURL   string                 `json:"original_url,omitempty"`
	OriginalBasis string                 `json:"original_basis,omitempty"`
	ContentHash   string                 `json:"content_hash,omitempty"`
	Provenance    []LiveSourceProvenance `json:"provenance,omitempty"`
	// Excerpt сборщик отдаёт только на запрос по кандидату; по нему карточка пишет кейс из рынка.
	Excerpt string `json:"excerpt,omitempty"`
}

func liveChannels(ctx context.Context, src *liveSources, topic *LiveTopic, periods []LivePeriod) {
	for _, family := range liveFamilies() {
		if family.id == topic.ID {
			topic.Query.Terms = family.query.Terms
			break
		}
	}
	terms := liveTopicTerms(topic)
	for _, target := range []struct {
		env  string
		into **LiveChannel
	}{
		{"PATENT_COLLECTOR_URL", &topic.Patents}, {"WEB_COLLECTOR_URL", &topic.Web},
		// Китайские патенты нужны и живому поиску: иначе крупнейшее патентное ведомство мира невидимо.
		{"CN_PATENT_COLLECTOR_URL", &topic.PatentsCN},
	} {
		base := strings.TrimRight(os.Getenv(target.env), "/")
		if base == "" {
			continue
		}
		src.channelMu.Lock()
		failed := src.channelErrors[target.env]
		src.channelMu.Unlock()
		if failed != nil {
			topic.Limits = append(topic.Limits, liveLimitFail(liveLimitChannelOff, target.env, failed))
			continue
		}
		channelTerms, web := terms, target.env == "WEB_COLLECTOR_URL"
		if web {
			channelTerms = liveWebTerms(terms, topic.Name)
			src.step(liveStepWeb, 0)
		} else {
			src.step(liveStepPatents, 0)
		}
		channel, err := src.queryChannelTerms(ctx, base, channelTerms, periods, web && src.excerpts)
		if err == nil {
			*target.into = channel
			continue
		}
		// Канал выключается до конца прохода только отказом самого сборщика: запрос одной темы, отвергнутый
		// им, раньше оставлял без патентов и веба все следующие темы.
		if !errors.Is(err, errLiveChannelQuery) {
			src.channelMu.Lock()
			if src.channelErrors == nil {
				src.channelErrors = map[string]error{}
			}
			src.channelErrors[target.env] = err
			src.channelMu.Unlock()
		}
		topic.Limits = append(topic.Limits, liveLimitFail(liveLimitChannelOff, target.env, err))
	}
}

// errLiveChannelQuery — сборщик отверг запрос одной темы (4xx, кроме 429): сам канал исправен.
var errLiveChannelQuery = errors.New("source rejected the topic query")

// liveChannelTerms — предел сборщика: запрос больше восьми терминов он отвергает
// целиком, и канал терялся бы весь, а не одной фразой.
const liveChannelTerms = 8

// Русские ленты получают исходную формулировку вместе со всем английским планом; пакеты ограничивает клиент.
func liveWebTerms(terms []string, name string) []string {
	russian := slices.DeleteFunc(liveRussianTerms(name), func(term string) bool { return slices.Contains(terms, term) })
	if len(russian) == 0 {
		return terms
	}
	return append(slices.Clone(terms), russian...)
}

// К двенадцати переведённым фразам добавляется не больше четырёх исходных: всего два пакета сборщика.
const liveRussianMaxTerms = 4

// Переводчик отдаёт только английские термины, а отдельное русское слово совпало бы с любой новостью.
func liveRussianTerms(query string) []string {
	if !liveNeedsTranslation(query) {
		return nil
	}
	terms := []string{}
	for _, part := range liveRussianParts(query) {
		if len(terms) >= liveRussianMaxTerms {
			break
		}
		terms = append(terms, strings.Join(part, " "))
		// В русской именной группе главное слово последнее, поэтому хвост из двух слов называет предмет.
		if len(part) > 2 && len(terms) < liveRussianMaxTerms {
			terms = append(terms, strings.Join(part[len(part)-2:], " "))
		}
	}
	return terms
}

// Предлог уточняет предмет: разрез по нему оставил бы широкое одинокое слово.
func liveRussianParts(query string) [][]string {
	parts, current := [][]string{}, []string{}
	cut := func() {
		// «слабые сигналы в области кибербезопасности» ищется как «кибербезопасности».
		for len(current) > 0 && liveRussianFraming(current[0]) {
			current = current[1:]
		}
		if len(current) > 0 {
			parts = append(parts, current)
		}
		current = nil
	}
	// liveWords стирает запятые вместе с остальными знаками, поэтому запрос сначала делится по ним.
	for _, segment := range strings.FieldsFunc(query, func(r rune) bool { return r == ',' || r == ';' }) {
		for _, word := range liveWords(segment) {
			if liveRussianConjunction(word) {
				cut()
				continue
			}
			current = append(current, word)
		}
		cut()
	}
	return parts
}

func liveRussianConjunction(word string) bool {
	switch word {
	case "и", "или", "либо", "а", "также":
		return true
	}
	return false
}

// Тот же список, что снимает переводчик, только по-русски.
func liveRussianFraming(word string) bool {
	switch word {
	case "зарождающиеся", "зарождающихся", "зарождающаяся", "новые", "новых", "новейшие",
		"перспективные", "перспективных", "слабые", "слабых", "сигналы", "сигналов", "сигнал",
		"тренды", "трендов", "тренд", "технологии", "технологий", "технология",
		"решения", "решений", "направления", "направлений", "разработки", "разработок",
		"области", "область", "сфере", "сфера", "отрасли", "отрасль", "рынке", "рынка", "рынок",
		"в", "во", "на", "для", "по", "о", "об", "про":
		return true
	}
	return false
}

func (src *liveSources) queryChannelAt(ctx context.Context, endpoint string, limit int64, body []byte, periodCount int,
) (*LiveChannel, error) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("create source query: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	response, err := src.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("query source channel: %w", err)
	}
	data, err := io.ReadAll(io.LimitReader(response.Body, limit))
	_ = response.Body.Close()
	if err != nil {
		return nil, fmt.Errorf("read source channel: %w", err)
	}
	if response.StatusCode >= 400 && response.StatusCode < 500 && response.StatusCode != http.StatusTooManyRequests {
		return nil, fmt.Errorf("%w: HTTP %d", errLiveChannelQuery, response.StatusCode)
	}
	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d", response.StatusCode)
	}
	var channel LiveChannel
	if err := json.Unmarshal(data, &channel); err != nil {
		return nil, fmt.Errorf("decode source channel: %w", err)
	}
	if len(channel.Counts) != periodCount {
		return nil, errors.New("invalid calendar")
	}
	stampPassports(channel.Records)
	return &channel, nil
}

// Реестр задаёт доверие к издателю; явный тип публикации из метаданных точнее эвристики заголовка.
func stampPassports(records []LiveSourceRecord) {
	for i := range records {
		address := records[i].URL
		if address == "" {
			address = records[i].CanonicalURL
		}
		passport, genre := liveApplyRecordGenre(ClassifySource(address), address, records[i].Title, records[i].Genre)
		records[i].PublisherType = passport.Publisher
		records[i].Trust = passport.Trust
		records[i].TrustBasis = passport.TrustBasis
		records[i].Genre = cmp.Or(strings.TrimSpace(records[i].Genre), genre)
	}
}

// liveSourceRefreshWorkers согласован с cpus сборщиков в docker-compose.yml.
const liveSourceRefreshWorkers = 4

// RefreshSources сохраняет научные наблюдения и даты; публикацию защищает та же аренда писателя, что и полный сбор.
func (s *TrendLiveService) RefreshSources(ctx context.Context) error {
	ctx, finish, err := s.beginWrite(ctx)
	if err != nil {
		return err
	}
	defer finish()
	// prepareWrite уже загрузил версию, которую защищает эта публикация.
	s.mu.RLock()
	if s.snapshot == nil {
		s.mu.RUnlock()
		return ErrLiveNotReady
	}
	snapshot := *s.snapshot
	snapshot.Topics = slices.Clone(snapshot.Topics)
	snapshot.Discovered = slices.Clone(snapshot.Discovered)
	s.mu.RUnlock()
	src := s.newSources()
	// 18.10 двести направлений подряд опрашивались около 50 минут при сроке 20: добор не публиковался
	// с 14.10 и шёл по кругу. Каждый сборщик держит запрос на одном ядре, поэтому потоков не больше их ядер.
	slots := make(chan struct{}, liveSourceRefreshWorkers)
	var wg sync.WaitGroup
	for _, topics := range [][]LiveTopic{snapshot.Topics, snapshot.Discovered} {
		for i := range topics {
			topic := &topics[i]
			wg.Go(func() {
				slots <- struct{}{}
				defer func() { <-slots }()
				// Прошлые отказы снимаются перед опросом, иначе отказ неучтённого сборщика прилипал бы к теме навсегда.
				topic.Limits = slices.DeleteFunc(slices.Clone(topic.Limits), func(limit LiveLimit) bool {
					name, _, ok := strings.Cut(limit.Text, ":")
					return ok && strings.HasSuffix(name, "_COLLECTOR_URL")
				})
				liveChannels(ctx, src, topic, snapshot.Periods)
			})
		}
	}
	wg.Wait()
	// Пересчёт свидетельств получает половину оставшегося срока, чтобы публикация успела при любом его исходе;
	// при отказе прежние свидетельства остаются.
	evidenceCtx, cancel := ctx, context.CancelFunc(func() {})
	if deadline, ok := ctx.Deadline(); ok {
		evidenceCtx, cancel = context.WithTimeout(ctx, time.Until(deadline)/2)
	}
	err = s.enrichSourceEvidence(evidenceCtx, &snapshot)
	cancel()
	if err != nil {
		slog.Warn("radar source evidence unavailable", "error", err)
		snapshot.Limits = append(slices.Clone(snapshot.Limits), liveLimitFail(liveLimitAnalysisOff, "source evidence", err))
	}
	if err = s.publish(ctx, &snapshot); err != nil {
		return err
	}
	failures := []error{}
	for source, failure := range src.channelErrors {
		failures = append(failures, fmt.Errorf("%s: %w", source, failure))
	}
	return errors.Join(failures...)
}
