package extractdoc

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json/v2"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/example/main-service/internal/platform/indexdoc"
	"github.com/example/main-service/internal/platform/sourcedoc"
)

// LeaseDuration совпадает с арендой source-index, чтобы оператору хватало одного числа.
const LeaseDuration = 2 * time.Minute

const MaxAttempts = 3

var (
	ErrInvalid  = errors.New("invalid extraction job")
	ErrConflict = errors.New("extraction job lease conflict")
)

// Живую аренду держит только Running, и только живая аренда публикует итог.
const (
	Queued    = "queued"
	Running   = "running"
	Completed = "completed"
	Partial   = "partial"
	Failed    = "failed"
	Cancelled = "cancelled"
)

func States() []string { return []string{Queued, Running, Completed, Partial, Failed, Cancelled} }

func Terminal(state string) bool {
	return state == Completed || state == Partial || state == Failed || state == Cancelled
}

// Key содержит только то, что меняет прочитанное моделью или форму её ответа:
// лишнее поле заставит платить за неизменный корпус дважды.
type Key struct {
	RevisionHash     string `json:"source_revision_hash"`
	ParserVersion    string `json:"parser_version"`
	ExtractorVersion string `json:"extractor_version"`
	SchemaVersion    string `json:"schema_version"`
}

func (k Key) ID() string {
	sum := sha256.Sum256([]byte(strings.Join(
		[]string{k.RevisionHash, k.ParserVersion, k.ExtractorVersion, k.SchemaVersion}, "\x00")))
	return hex.EncodeToString(sum[:])
}

func (k Key) Validate() error {
	if !indexdoc.IsHash(k.RevisionHash) || !bounded(k.ParserVersion, 512) ||
		!bounded(k.ExtractorVersion, 128) || !bounded(k.SchemaVersion, 128) {
		return ErrInvalid
	}
	return nil
}

func bounded(value string, most int) bool {
	return value != "" && len(value) <= most && strings.TrimSpace(value) == value
}

// RevisionHash намеренно не зависит от времени наблюдения. Хэш текста входит в него:
// перепарсинг меняет то, что читает модель, даже при тех же исходных байтах.
func RevisionHash(source sourcedoc.Source, textHash string) string {
	sum := sha256.Sum256([]byte(strings.Join([]string{
		source.Provider, source.NativeID, source.Revision, source.ContentHash, textHash,
	}, "\x00")))
	return hex.EncodeToString(sum[:])
}

// Lease фенсится токеном: воркер, потерявший аренду, держит токен, которого у задания
// уже нет, и ничего не публикует.
type Lease struct {
	JobID   string `json:"job_id"`
	OwnerID string `json:"owner_id"`
	Token   string `json:"lease_token"`
}

// Artifact хранит хэш в ключе, чтобы сохранённый объект нельзя было тихо подменить.
type Artifact struct {
	Key    string `json:"key,omitempty"`
	SHA256 string `json:"sha256,omitempty"`
}

func (a Artifact) Present() bool { return a.Key != "" && a.SHA256 != "" }

func (a Artifact) Validate() error {
	switch {
	case a.Key == "" && a.SHA256 == "":
		return nil
	case !indexdoc.IsHash(a.SHA256) || !strings.Contains(a.Key, a.SHA256) || len(a.Key) > 2048:
		return ErrInvalid
	}
	return nil
}

type Job struct {
	ID               string     `json:"id"`
	Key              Key        `json:"key"`
	SourceRevisionID string     `json:"source_revision_id"`
	OwnerID          string     `json:"owner_id"`
	State            string     `json:"state"`
	Attempt          int        `json:"attempt"`
	ReasonCode       string     `json:"reason_code,omitempty"`
	Input            Artifact   `json:"input"`
	Intermediate     Artifact   `json:"intermediate"`
	Output           Artifact   `json:"output"`
	LeasedUntil      *time.Time `json:"leased_until,omitempty"`
	CreatedAt        time.Time  `json:"created_at"`
	UpdatedAt        time.Time  `json:"updated_at"`
}

// Candidate несёт конверты в том виде, в каком они лежат в базе: ключ строится ровно из её байтов.
type Candidate struct {
	DocumentID     string `json:"document_id"`
	OwnerID        string `json:"owner_id"`
	SourceMetadata string `json:"source_metadata"`
	Manifest       string `json:"manifest"`
}

type Result struct {
	Lease        Lease    `json:"lease"`
	State        string   `json:"state"`
	ReasonCode   string   `json:"reason_code,omitempty"`
	Intermediate Artifact `json:"intermediate"`
	Output       Artifact `json:"output"`
}

func (r Result) Validate() error {
	if !Terminal(r.State) || r.State == Cancelled {
		return ErrInvalid
	}
	if (r.State == Completed || r.State == Partial) && !r.Output.Present() {
		return ErrInvalid
	}
	if r.State == Failed && r.ReasonCode == "" {
		return ErrInvalid
	}
	if len(r.ReasonCode) > 128 {
		return ErrInvalid
	}
	return errors.Join(r.Output.Validate(), r.Intermediate.Validate())
}

func ArtifactKey(jobID, stage, hash string) string {
	return "technology-extraction/v1/" + jobID + "/" + stage + "/" + hash + ".json"
}

// ReadRevision с пустыми версиями экстрактора и схемы читает последнее извлечение ревизии,
// чтобы ответ не смешивал версии.
type ReadRevision struct {
	ReaderID         string `json:"reader_id"`
	SourceRevisionID string `json:"source_revision_id"`
	ExtractorVersion string `json:"extractor_version,omitempty"`
	SchemaVersion    string `json:"schema_version,omitempty"`
	SharedCorpus     bool   `json:"shared_corpus,omitempty"`
}

type Coverage struct {
	Status            string `json:"status"`
	ReasonCode        string `json:"reason_code,omitempty"`
	ProcessedSections int    `json:"processed_sections"`
	TotalSections     int    `json:"total_sections"`
	ExtractedAt       string `json:"extracted_at,omitempty"`
}

type Quote struct {
	EvidenceID string  `json:"evidence_id"`
	Locator    Locator `json:"locator"`
	Quote      string  `json:"quote"`
}

type Locator struct {
	Section        string `json:"section"`
	Page           int    `json:"page,omitzero"`
	StartChar      int    `json:"start_char"`
	EndChar        int    `json:"end_char"`
	ElementPath    string `json:"element_path,omitempty"`
	SnapshotSHA256 string `json:"snapshot_sha256,omitempty"`
}

type Mention struct {
	MentionID         string              `json:"mention_id"`
	TechnologyID      string              `json:"technology_id"`
	Name              string              `json:"name"`
	Description       string              `json:"description,omitempty"`
	Role              string              `json:"role"`
	Problem           string              `json:"problem,omitempty"`
	Mechanism         string              `json:"mechanism,omitempty"`
	Inputs            []string            `json:"inputs,omitempty"`
	Outputs           []string            `json:"outputs,omitempty"`
	ExtractionStatus  string              `json:"extraction_status"`
	SourceRevisionID  string              `json:"source_revision_id,omitempty"`
	SourceTitle       string              `json:"source_title,omitempty"`
	SourcePublishedAt string              `json:"source_published_at,omitempty"`
	Evidence          []Quote             `json:"evidence"`
	FieldEvidence     map[string][]string `json:"field_evidence,omitempty"`
}

// RevisionTechnologies: пустой список осмыслен только вместе с Coverage — no_technology
// это находка, а failed ничего не говорит о материале.
type RevisionTechnologies struct {
	SourceRevisionID string    `json:"source_revision_id"`
	ExtractorVersion string    `json:"extractor_version"`
	SchemaVersion    string    `json:"schema_version"`
	Coverage         Coverage  `json:"coverage"`
	Technologies     []Mention `json:"technologies"`
}

// ErrNotFound одинаков для недоступной и отсутствующей ревизии, чтобы ответ не подтверждал
// существование приватного документа.
var ErrNotFound = errors.New("revision not available")

func (r ReadRevision) Validate() error {
	if r.ReaderID == "" || r.SourceRevisionID == "" ||
		len(r.ExtractorVersion) > 128 || len(r.SchemaVersion) > 128 {
		return ErrInvalid
	}
	return nil
}

func Decode[T any](raw string) (T, error) {
	var value T
	if len(raw) == 0 || len(raw) > 8<<20 {
		return value, ErrInvalid
	}
	if err := json.Unmarshal([]byte(raw), &value, json.RejectUnknownMembers(true)); err != nil {
		return value, fmt.Errorf("decode technology request: %w: %w", ErrInvalid, err)
	}
	return value, nil
}

const (
	OpRename        = "rename"
	OpDescribe      = "describe"
	OpSetRole       = "set_role"
	OpSetDependency = "set_dependency"
	OpConfirm       = "confirm"
	OpMerge         = "merge"
	OpSplit         = "split"
	OpRevert        = "revert"
)

func ReviewOperations() []string {
	return []string{OpRename, OpDescribe, OpSetRole, OpSetDependency, OpConfirm, OpMerge, OpSplit, OpRevert}
}

type Review struct {
	ID            string         `json:"id"`
	TechnologyID  string         `json:"technology_id"`
	AuthorID      string         `json:"author_id"`
	Operation     string         `json:"operation"`
	Reason        string         `json:"reason"`
	PreviousValue map[string]any `json:"previous_value"`
	NewValue      map[string]any `json:"new_value"`
	RevertsID     string         `json:"reverts_id,omitempty"`
	CreatedAt     time.Time      `json:"created_at"`
}

// Fingerprint схлопывает повтор той же правки тем же автором в одну.
func (r Review) Fingerprint() string {
	raw, _ := json.Marshal(struct {
		Operation string         `json:"operation"`
		Author    string         `json:"author"`
		Previous  map[string]any `json:"previous"`
		New       map[string]any `json:"new"`
		Reverts   string         `json:"reverts"`
	}{r.Operation, r.AuthorID, r.PreviousValue, r.NewValue, r.RevertsID}, json.Deterministic(true))
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}

func (r Review) Validate() error {
	if r.TechnologyID == "" || r.AuthorID == "" || !slices.Contains(ReviewOperations(), r.Operation) {
		return ErrInvalid
	}
	if reason := strings.TrimSpace(r.Reason); reason == "" || len(reason) > 2048 {
		return ErrInvalid
	}
	if (r.Operation == OpRevert) != (r.RevertsID != "") {
		return ErrInvalid
	}
	if r.NewValue == nil || r.PreviousValue == nil {
		return ErrInvalid
	}
	return nil
}

type ReadReviews struct {
	ReaderID     string `json:"reader_id"`
	TechnologyID string `json:"technology_id"`
	SharedCorpus bool   `json:"shared_corpus,omitempty"`
}

type ReadTechnology struct {
	ReaderID     string `json:"reader_id"`
	TechnologyID string `json:"technology_id"`
	SharedCorpus bool   `json:"shared_corpus,omitempty"`
}

// Technology считает только доступное читателю, чтобы приватный документ не сдвигал чужие цифры.
// RestrictedRevisions — лишь число: перечисление ревизий раскрыло бы скрытое.
type Technology struct {
	TechnologyID        string    `json:"technology_id"`
	CanonicalName       string    `json:"canonical_name"`
	NameKind            string    `json:"name_kind"`
	Description         string    `json:"description"`
	ValidationStatus    string    `json:"validation_status"`
	MethodVersion       string    `json:"method_version"`
	Aliases             []Alias   `json:"aliases"`
	Mentions            []Mention `json:"mentions"`
	ReadableRevisions   int       `json:"readable_revisions"`
	RestrictedRevisions int       `json:"restricted_revisions"`
}

type Alias struct {
	Value    string `json:"value"`
	Language string `json:"language"`
	Basis    string `json:"basis"`
}

func (r ReadTechnology) Validate() error {
	if r.ReaderID == "" || r.TechnologyID == "" {
		return ErrInvalid
	}
	return nil
}

// SearchTechnologies: IncludeAll — явный флаг, чтобы пустой запрос не выгрузил весь каталог случайно.
type SearchTechnologies struct {
	ReaderID     string   `json:"reader_id"`
	Query        string   `json:"q"`
	Languages    []string `json:"languages,omitempty"`
	Roles        []string `json:"roles,omitempty"`
	Stages       []string `json:"stages,omitempty"`
	Limit        int      `json:"limit"`
	Cursor       string   `json:"cursor,omitempty"`
	IncludeAll   bool     `json:"all,omitempty"`
	SharedCorpus bool     `json:"shared_corpus,omitempty"`
}

const (
	MatchName        = "name"
	MatchAlias       = "alias"
	MatchDescription = "description"
	MatchMechanism   = "mechanism"
	MatchProblem     = "problem"
	MatchApplication = "application"
)

const (
	ReasonQueryRequired  = "query_required"
	ReasonNoMatch        = "below_relevance_floor"
	ReasonEmptyCatalogue = "catalogue_empty"
)

type TechnologyHit struct {
	TechnologyID     string   `json:"technology_id"`
	CanonicalName    string   `json:"canonical_name"`
	Description      string   `json:"description"`
	ValidationStatus string   `json:"validation_status"`
	MatchReasons     []string `json:"match_reasons"`
	MatchedText      string   `json:"matched_text,omitempty"`
}

// TechnologyPage: CatalogueVersion привязывает курсор, поэтому страница, выданная до пересчёта,
// отклоняется, а не смешивается с новой.
type TechnologyPage struct {
	Technologies     []TechnologyHit `json:"technologies"`
	NextCursor       string          `json:"next_cursor,omitempty"`
	CatalogueVersion string          `json:"catalogue_version"`
	Reason           string          `json:"reason,omitempty"`
	SchemaVersion    string          `json:"schema_version"`
}

const CatalogueSchemaVersion = "technology-catalogue-1"

func (s SearchTechnologies) Validate() error {
	if s.ReaderID == "" || s.Limit < 1 || s.Limit > 100 || len(s.Query) > 512 {
		return ErrInvalid
	}
	if len(s.Languages) > 8 || len(s.Roles) > 16 {
		return ErrInvalid
	}
	// Принять и молча проигнорировать фильтр стадии нельзя: ответ оказался бы шире вопроса.
	if len(s.Stages) > 0 {
		return fmt.Errorf("%w: the stage of a direction is not computed yet, so it cannot be filtered on", ErrInvalid)
	}
	return nil
}
