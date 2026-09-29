package analysisdoc

import (
	"cmp"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
	"uuid"

	"github.com/example/db-service/internal/platform/indexdoc"
	"github.com/example/db-service/internal/platform/sourcedoc"
)

// Корпус больше MaxDocuments — не ошибка: анализируются новейшие записи, а срез отмечается
// в Input.Limitations.
const (
	MaxDocuments = 2000
	MaxPages     = 20000
	MaxBytes     = 64 << 20
	Version      = "analysis-input-v2"
)

var (
	ErrInvalid  = errors.New("invalid analysis input")
	ErrConflict = errors.New("analysis request conflicts with its frozen input")
	ErrNotFound = errors.New("analysis unavailable")
	// ErrAccessChanged видит только владелец; остальным — ErrNotFound, иначе ответ раскроет запуск.
	ErrAccessChanged = errors.New("analysis sources are no longer readable")
)

const (
	ModeAsOf = "as_of"
	// ModeRetrospective допускает поздние наблюдения и отмечает это в результате: состав текущий,
	// окно историческое.
	ModeRetrospective = "retrospective_current_membership"
)

// Старые имена режимов остаются в снимках, закреплённых хэшем, поэтому переводятся при чтении.
const (
	legacyModeAsOf          = "as_observed"
	legacyModeRetrospective = "reconstruction"
)

// NormalizeMode возвращает "" для неизвестного режима, чтобы вызывающий отказал, а не выбрал
// разрешительный.
func NormalizeMode(mode string) string {
	switch mode {
	case ModeAsOf, legacyModeAsOf:
		return ModeAsOf
	case ModeRetrospective, legacyModeRetrospective:
		return ModeRetrospective
	default:
		return ""
	}
}

const KindBriefV1 = "trend-brief-v1"

// Request получает OwnerID и SharedCorpus от доверенного BFF, а не из браузера.
type Request struct {
	OwnerID   string `json:"owner_id"`
	RequestID string `json:"request_id"`
	Query     string `json:"query"`
	Start     string `json:"start"`
	Cutoff    string `json:"cutoff"`
	Mode      string `json:"mode"`
	// Пустой AnalysisKind — бриф, чтобы старый клиент не получил новую схему.
	AnalysisKind       string   `json:"analysis_kind,omitempty"`
	SharedCorpus       bool     `json:"shared_corpus"`
	TestRunID          string   `json:"test_run_id,omitempty"` // только для оператора, продуктовый эндпоинт его не принимает
	CollectionScopeIDs []string `json:"collection_scope_ids,omitempty"`
	// SearchTerms выводит из Query сам сервис, от клиента они не принимаются. Корпус больше MaxDocuments
	// читается сначала по ним, потом по свежести, чтобы срез сохранил предмет вопроса.
	SearchTerms []string `json:"search_terms,omitempty"`
}

const MaxSearchTerms = 8

var (
	collectionScopeID = regexp.MustCompile(`^[0-9a-f]{64}$`)
	searchTerm        = regexp.MustCompile(`^[a-z0-9][a-z0-9 -]{1,59}$`)
)

// genericTail — слова, которые переводчик дописывает к названию области («artificial intelligence
// technologies»); заголовки называют саму область, поэтому хвост срезается.
func genericTail(word string) bool {
	switch word {
	case "technology", "technologies", "method", "methods", "system", "systems", "approach", "approaches",
		"technique", "techniques", "application", "applications", "solution", "solutions", "tool", "tools":
		return true
	}
	return false
}

// NormalizeTerms отбрасывает негодные фразы, а не отвергает запрос: термины — подсказка сервиса,
// главным остаётся сам запрос.
func NormalizeTerms(terms []string) []string {
	out := make([]string, 0, MaxSearchTerms)
	for _, term := range terms {
		words := strings.Fields(strings.ToLower(term))
		for len(words) > 1 && genericTail(words[len(words)-1]) {
			words = words[:len(words)-1]
		}
		term = strings.Join(words, " ")
		if !searchTerm.MatchString(term) || slices.Contains(out, term) {
			continue
		}
		out = append(out, term)
		if len(out) == MaxSearchTerms {
			break
		}
	}
	return out
}

func (r Request) Normalize(now time.Time) (Request, error) {
	r.Query = strings.Join(strings.Fields(r.Query), " ")
	if len(r.CollectionScopeIDs) > 16 {
		return r, ErrInvalid
	}
	r.CollectionScopeIDs = slices.Clone(r.CollectionScopeIDs)
	slices.Sort(r.CollectionScopeIDs)
	r.SearchTerms = NormalizeTerms(r.SearchTerms)
	for i, id := range r.CollectionScopeIDs {
		if !collectionScopeID.MatchString(id) || (i > 0 && id == r.CollectionScopeIDs[i-1]) {
			return r, ErrInvalid
		}
	}
	_, ownerErr := uuid.Parse(r.OwnerID)
	_, requestErr := uuid.Parse(r.RequestID)
	start, startErr := time.Parse(time.DateOnly, r.Start)
	cutoff, cutoffErr := time.Parse(time.RFC3339, r.Cutoff)
	if r.TestRunID != "" {
		if _, err := uuid.Parse(strings.TrimPrefix(r.TestRunID, "tr-test-")); err != nil ||
			!strings.HasPrefix(r.TestRunID, "tr-test-") {
			return r, ErrInvalid
		}
	}
	// Клиент ставит cutoff «сейчас» по своим часам: небольшое будущее из-за расхождения часов
	// прижимается к серверному «сейчас», далёкое — ошибка.
	if cutoffErr == nil && cutoff.After(now) && !cutoff.After(now.Add(5*time.Minute)) {
		cutoff = now
	}
	if ownerErr != nil || requestErr != nil || startErr != nil || cutoffErr != nil ||
		start.Year() < 1900 || start.After(cutoff) || cutoff.After(now) ||
		utf8.RuneCountInString(r.Query) < 2 || utf8.RuneCountInString(r.Query) > 300 ||
		strings.ContainsFunc(r.Query, unicode.IsControl) || NormalizeMode(r.Mode) == "" ||
		cmp.Or(r.AnalysisKind, KindBriefV1) != KindBriefV1 {
		return r, ErrInvalid
	}
	r.Mode = NormalizeMode(r.Mode)
	r.AnalysisKind = cmp.Or(r.AnalysisKind, KindBriefV1)
	r.Cutoff = cutoff.UTC().Format(time.RFC3339)
	return r, nil
}

// Record: Source может опускать исторически недоказанные пометки, а SourceHash закрепляет исходные
// метаданные целиком.
type Record struct {
	ID             string            `json:"id"`
	Title          string            `json:"title"`
	MIMEType       string            `json:"mime_type"`
	Representation string            `json:"representation"`
	Source         sourcedoc.Source  `json:"source"`
	SourceHash     string            `json:"source_hash"`
	Index          indexdoc.Manifest `json:"index"`
	Limitations    []string          `json:"limitations"`
	// Заголовок содержит поисковый термин: это корпус самого вопроса, относительно которого меряется
	// рост, когда проверенного прохода провайдера нет.
	QueryMatched bool `json:"query_matched,omitempty"`
}

// Exclusion называет только записи, которые аутентифицированный читатель и так видел.
type Exclusion struct {
	DocumentID string `json:"document_id"`
	Reason     string `json:"reason"`
}

// Page: исчерпанная пагинация сама по себе не доказывает полного мирового покрытия.
type Page struct {
	ScopeID     string          `json:"scope_id"`
	RunID       string          `json:"run_id"`
	Number      int             `json:"number"`
	Provider    string          `json:"provider"`
	Scope       jsontext.Value  `json:"scope"`
	Raw         indexdoc.Object `json:"raw"`
	Manifest    indexdoc.Object `json:"manifest"`
	Received    int             `json:"received"`
	Rejected    int             `json:"rejected"`
	Exhausted   bool            `json:"exhausted"`
	Limitations jsontext.Value  `json:"limitations"`
	CapturedAt  time.Time       `json:"captured_at"`
}

type Input struct {
	Version     string          `json:"version"`
	RunID       string          `json:"run_id"`
	Request     Request         `json:"request"`
	CreatedAt   time.Time       `json:"created_at"`
	AccessEpoch int64           `json:"access_epoch"`
	Model       *indexdoc.Model `json:"model"`
	Records     []Record        `json:"records"`
	Excluded    []Exclusion     `json:"excluded"`
	Pages       []Page          `json:"pages"`
	Limitations []string        `json:"limitations"`
}

// Frozen передаёт ссылку на неизменяемый объект; его содержимое не входит в RPC.
type Frozen struct {
	RunID        string `json:"run_id"`
	SHA256       string `json:"sha256"`
	JSON         string `json:"-"`
	Status       string `json:"status"`
	Artifact     string `json:"artifact"`
	Bytes        int    `json:"bytes"`
	ReaderID     string `json:"reader_id"`
	SharedCorpus bool   `json:"shared_corpus"`
}

func (f Frozen) Validate() error {
	if _, err := uuid.Parse(f.RunID); err != nil || !ValidHash(f.SHA256) ||
		f.Artifact != ArtifactKey(f.RunID, f.SHA256) || f.Bytes < 1 || f.Bytes > MaxBytes || f.ReaderID == "" {
		return ErrInvalid
	}
	return nil
}

// Read никогда не даёт администратору доступ к чужому анализу.
type Read struct {
	RunID        string `json:"run_id"`
	ReaderID     string `json:"reader_id"`
	SharedCorpus bool   `json:"shared_corpus"`
}

func Decode[T any](raw string) (T, error) {
	var result T
	if len(raw) == 0 || len(raw) > MaxBytes*2 {
		return result, ErrInvalid
	}
	if err := json.Unmarshal([]byte(raw), &result, json.RejectUnknownMembers(true)); err != nil {
		return result, fmt.Errorf("decode analysis: %w: %w", ErrInvalid, err)
	}
	return result, nil
}

func ArtifactKey(runID, hash string) string {
	return "signalgraph/v1/" + runID + "/inputs/" + hash + ".json"
}
