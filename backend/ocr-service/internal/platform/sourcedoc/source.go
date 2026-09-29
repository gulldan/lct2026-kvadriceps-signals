package sourcedoc

import (
	"encoding/json/v2"
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"
)

const MaxMetadataBytes = 32 << 10

const (
	kindHypothesis = "hypothesis"
	kindPaper      = "paper"
	kindPatent     = "patent"
	providerGitHub = "github"
	kindRepository = "software_repository"
	providerArXiv  = "arxiv"
	providerManual = "manual"
	precisionYear  = "year"
	precisionMonth = "month"
	precisionDay   = "day"
)

const (
	MaxSections = 256
	MaxAnchors  = 4096
)

// Section адресует часть текста ревизии смещениями в кодовых точках, конец не включается.
// Title хранит заголовок, который отбрасывает каноническое Name.
type Section struct {
	Name  string `json:"name"`
	Title string `json:"title,omitempty"`
	Start int    `json:"start"`
	End   int    `json:"end"`
}

const (
	SectionAbstract         = "abstract"
	SectionIntroduction     = "introduction"
	SectionMethod           = "method"
	SectionExperiments      = "experiments"
	SectionResults          = "results"
	SectionDiscussion       = "discussion"
	SectionLimitations      = "limitations"
	SectionConclusion       = "conclusion"
	SectionReferences       = "references"
	SectionAcknowledgements = "acknowledgements"
	SectionBackground       = "background"
	SectionSummary          = "summary"
	SectionDescription      = "description"
	SectionClaims           = "claims"
)

func CanonicalSections() []string {
	return []string{
		SectionAbstract, SectionIntroduction, SectionMethod, SectionExperiments, SectionResults,
		SectionDiscussion, SectionLimitations, SectionConclusion, SectionReferences, SectionAcknowledgements,
		SectionBackground, SectionSummary, SectionDescription, SectionClaims,
	}
}

// Anchor адресует текст так же, как Section: кодовые точки, конец не включается.
type Anchor struct {
	Path  string `json:"path"`
	Start int    `json:"start"`
	End   int    `json:"end"`
}

func ValidateAnchors(anchors []Anchor, runeLen int) error {
	if len(anchors) > MaxAnchors {
		return fmt.Errorf("a revision declares at most %d anchors", MaxAnchors)
	}
	previous := 0
	for _, anchor := range anchors {
		if anchor.Path == "" || len(anchor.Path) > 512 {
			return errors.New("an anchor names a path of 1..512 characters")
		}
		if anchor.Start < previous || anchor.End <= anchor.Start || anchor.End > runeLen {
			return errors.New("anchors must be ordered, non-empty and inside the stored text")
		}
		previous = anchor.End
	}
	return nil
}

func ValidateSections(sections []Section, runeLen int) error {
	if len(sections) > MaxSections {
		return fmt.Errorf("a revision declares at most %d sections", MaxSections)
	}
	previous := 0
	for _, section := range sections {
		if !slices.Contains(CanonicalSections(), section.Name) {
			return fmt.Errorf("unknown section name %q", section.Name)
		}
		if len(section.Title) > 512 {
			return errors.New("a section title holds at most 512 characters")
		}
		if section.Start < previous || section.End <= section.Start || section.End > runeLen {
			return errors.New("sections must be ordered, non-empty and inside the stored text")
		}
		previous = section.End
	}
	return nil
}

type Date struct {
	Value     string `json:"value"`
	Precision string `json:"precision"`
	Basis     string `json:"basis"` // поле провайдера или локатор в источнике
}

func (d Date) Validate() error {
	layout := map[string]string{precisionYear: "2006", precisionMonth: "2006-01", precisionDay: time.DateOnly}[d.Precision]
	parsed, err := time.Parse(layout, d.Value)
	if layout == "" || err != nil || parsed.Year() < 1900 || strings.TrimSpace(d.Basis) == "" {
		return errors.New("publication date requires a valid year/month/day since 1900 and its basis")
	}
	return nil
}

func (d Date) Range() (time.Time, time.Time) {
	switch d.Precision {
	case precisionYear:
		at, err := time.Parse("2006", d.Value)
		if err != nil {
			return time.Time{}, time.Time{}
		}
		return at.UTC(), at.UTC().AddDate(1, 0, 0)
	case precisionMonth:
		at, err := time.Parse("2006-01", d.Value)
		if err != nil {
			return time.Time{}, time.Time{}
		}
		return at.UTC(), at.UTC().AddDate(0, 1, 0)
	case precisionDay:
		at, err := time.Parse(time.DateOnly, d.Value)
		if err != nil {
			return time.Time{}, time.Time{}
		}
		return at.UTC(), at.UTC().AddDate(0, 0, 1)
	}
	return time.Time{}, time.Time{}
}

// Quarter возвращает квартал, только если дата целиком в нём: год не размазывается на четыре.
func (d Date) Quarter() (string, bool) {
	start, end := d.Range()
	if start.IsZero() {
		return "", false
	}
	quarter := (int(start.Month()) - 1) / 3
	first := time.Date(start.Year(), time.Month(quarter*3+1), 1, 0, 0, 0, 0, time.UTC)
	if end.After(first.AddDate(0, 3, 0)) {
		return "", false
	}
	return fmt.Sprintf("%d-Q%d", start.Year(), quarter+1), true
}

type ArXiv struct {
	BaseID         string    `json:"base_id"`
	Version        int       `json:"version"`
	FirstPublished time.Time `json:"first_published"`
	VersionUpdated time.Time `json:"version_updated"`
	DOI            string    `json:"doi,omitempty"`
	JournalRef     string    `json:"journal_ref,omitempty"`
}

type GitHub struct {
	RepositoryID int64      `json:"repository_id"`
	CommitSHA    string     `json:"commit_sha"`
	BlobSHA      string     `json:"blob_sha,omitempty"`
	ReadmePath   string     `json:"readme_path,omitempty"`
	Fork         bool       `json:"fork"`
	RootID       *int64     `json:"root_id"`
	Archived     bool       `json:"archived"`
	License      string     `json:"license,omitempty"`
	CreatedAt    time.Time  `json:"created_at"`
	UpdatedAt    time.Time  `json:"updated_at"`
	PushedAt     *time.Time `json:"pushed_at"`
	ReleaseID    *int64     `json:"release_id"`
	ReleasedAt   *time.Time `json:"released_at"`
	Stars        *int64     `json:"stars"` // снимок на момент Source.ObservedAt
	Forks        *int64     `json:"forks"`
}

// Patent: члены семейства хранятся отдельно и не умножают число изобретений, поэтому FamilyID
// несёт основание; перевод не самостоятельный источник и наследует идентичность оригинала.
type Patent struct {
	PublicationNumber string `json:"publication_number"`
	ApplicationNumber string `json:"application_number,omitempty"`
	KindCode          string `json:"kind_code,omitempty"`
	Jurisdiction      string `json:"jurisdiction"`
	FamilyID          string `json:"family_id,omitempty"`
	FamilyBasis       string `json:"family_basis,omitempty"`
	TranslationOf     string `json:"translation_of,omitempty"`
}

func (p Patent) Validate() error {
	if !jurisdictionCode.MatchString(p.Jurisdiction) || !patentNumber.MatchString(p.PublicationNumber) ||
		(p.ApplicationNumber != "" && len(p.ApplicationNumber) > 64) ||
		(p.KindCode != "" && !patentKindCode.MatchString(p.KindCode)) ||
		(p.TranslationOf != "" && !patentNumber.MatchString(p.TranslationOf)) ||
		(p.FamilyID == "") != (p.FamilyBasis == "") || len(p.FamilyBasis) > 512 {
		return errors.New("patent requires a country code, a publication number and a family basis for any family ID")
	}
	if p.TranslationOf != "" && p.TranslationOf == p.PublicationNumber {
		return errors.New("a publication cannot be a translation of itself")
	}
	return nil
}

func (p Patent) CanonicalID() string {
	if p.TranslationOf != "" {
		return "patent:" + p.TranslationOf
	}
	return "patent:" + p.PublicationNumber
}

// Source неизменен в пределах ревизии: новая ревизия — другой документ, а canonical_id связывает
// работы, а не их чанки или наблюдения.
type Source struct {
	TextStatus  string `json:"text_status,omitempty"`
	Version     int    `json:"version"`
	Kind        string `json:"source_kind"`
	Provider    string `json:"provider"`
	NativeID    string `json:"native_id"`
	URL         string `json:"url"`
	CanonicalID string `json:"canonical_id"`
	Publication *Date  `json:"publication"`
	// RevisionPublication датирует ревизию, Publication — работу: технологию из v2 нельзя датировать v1.
	RevisionPublication *Date      `json:"revision_publication,omitempty"`
	ObservedAt          time.Time  `json:"observed_at"`
	AvailableAt         *time.Time `json:"available_at"`
	AvailableBasis      string     `json:"available_basis,omitempty"`
	Revision            string     `json:"revision"`
	ContentHash         string     `json:"content_hash"`
	HashAlgorithm       string     `json:"hash_algorithm"`
	AccessScope         string     `json:"access_scope"`
	TestRunID           string     `json:"test_run_id,omitempty"`
	Patent              *Patent    `json:"patent,omitempty"`
	Priority            *Date      `json:"priority"`
	Filing              *Date      `json:"filing"`
	Grant               *Date      `json:"grant,omitempty"`
	ArXiv               *ArXiv     `json:"arxiv"`
	GitHub              *GitHub    `json:"github"`
	// Пустой Sections означает, что структуру выводят дальше по конвейеру.
	Sections []Section `json:"sections,omitempty"`
	Anchors  []Anchor  `json:"anchors,omitempty"`
	// Language — происхождение (кто решил, в LanguageBasis), а не фильтр приёма или извлечения.
	Language      string `json:"language,omitempty"`
	LanguageBasis string `json:"language_basis,omitempty"`
}

var (
	hex256           = regexp.MustCompile(`^[0-9a-f]{64}$`)
	jurisdictionCode = regexp.MustCompile(`^[A-Z]{2}$`)
	patentNumber     = regexp.MustCompile(`^[A-Z]{0,2}[A-Z0-9/.-]{1,32}$`)
	patentKindCode   = regexp.MustCompile(`^[A-Z][0-9]?$`)
	gitSHA           = regexp.MustCompile(`^(?:[0-9a-f]{40}|[0-9a-f]{64})$`)
	arxivID          = regexp.MustCompile(`^(?:[0-9]{4}\.[0-9]{4,5}|[a-z-]+(?:\.[A-Z]{2})?/[0-9]{7})$`)
	languageCode     = regexp.MustCompile(`^[a-z]{2}$`)
)

func Decode(raw []byte) (*Source, error) {
	if len(raw) == 0 || len(raw) > MaxMetadataBytes {
		return nil, errors.New("source metadata must contain 1..32768 bytes")
	}
	var s Source
	if err := json.Unmarshal(raw, &s, json.RejectUnknownMembers(true)); err != nil {
		return nil, fmt.Errorf("decode source: %w", err)
	}
	if err := s.Validate(); err != nil {
		return nil, err
	}
	return &s, nil
}

func (s Source) Validate() error {
	return errors.Join(s.validateIdentity(), s.validateDates(), s.validateProvider())
}

func (s Source) validateIdentity() error {
	if s.TextStatus != "" && !slices.Contains([]string{
		"metadata_only", "article_text", "representative_claim", "full_text", "title_only", "title_and_abstract",
		"pdf", "repository_metadata_only", "pinned_readme", "abstract_only",
	}, s.TextStatus) {
		return errors.New("unsupported source text status")
	}
	if s.Version != 1 ||
		!slices.Contains([]string{kindPaper, kindPatent, "company_material", "analysis", kindRepository, kindHypothesis}, s.Kind) ||
		!slices.Contains([]string{
			providerArXiv, providerGitHub, "openalex", "crossref", providerManual, "generated", kindPatent, "web",
		}, s.Provider) {
		return errors.New("unsupported source version, kind or provider")
	}
	for _, v := range []string{s.NativeID, s.CanonicalID, s.Revision} {
		if v == "" || strings.TrimSpace(v) != v || len(v) > 512 {
			return errors.New("native_id, canonical_id and revision must contain 1..512 characters without surrounding whitespace")
		}
	}
	u, err := url.Parse(s.URL)
	if err != nil || u.Hostname() == "" || u.User != nil || (u.Scheme != "https" && u.Scheme != "http") {
		return errors.New("source URL must be an absolute HTTP(S) URL without credentials")
	}
	if !slices.Contains([]string{"public", "shared", "owner"}, s.AccessScope) ||
		!slices.Contains([]string{"sha256", "blake3"}, s.HashAlgorithm) || !hex256.MatchString(s.ContentHash) || s.ObservedAt.IsZero() {
		return errors.New("source requires access scope, observed_at and a sha256/blake3 content hash")
	}
	if s.TestRunID != "" && !strings.HasPrefix(s.TestRunID, "tr-test-") {
		return errors.New("test_run_id must use the tr-test- namespace")
	}
	if (s.Language == "") != (s.LanguageBasis == "") || len(s.LanguageBasis) > 256 ||
		(s.Language != "" && !languageCode.MatchString(s.Language)) {
		return errors.New("a language must be a two-letter code and name who decided it")
	}
	return nil
}

func (s Source) KnownNoLaterThan() time.Time {
	if s.AvailableAt != nil {
		return *s.AvailableAt
	}
	return s.ObservedAt
}

func (s Source) validateDates() error {
	for _, d := range []*Date{s.Publication, s.RevisionPublication, s.Priority, s.Filing, s.Grant} {
		if d == nil {
			continue
		}
		if err := d.Validate(); err != nil {
			return err
		}
		// Дата позже наблюдения — ошибка разбора, а не открытие.
		if start, _ := d.Range(); start.After(s.ObservedAt) {
			return errors.New("source date cannot be later than the observation that found it")
		}
	}
	if s.Kind != kindPatent && (s.Priority != nil || s.Filing != nil || s.Grant != nil) {
		return errors.New("priority, filing and grant dates belong only to patents")
	}
	return errors.Join(s.validateRevisionDate(), s.validateAvailability())
}

func (s Source) validateRevisionDate() error {
	if s.RevisionPublication == nil {
		return nil
	}
	if s.Publication == nil {
		return errors.New("a revision publication date requires the first publication date")
	}
	first, _ := s.Publication.Range()
	if revision, _ := s.RevisionPublication.Range(); revision.Before(first) {
		return errors.New("a revision cannot be published before the work it revises")
	}
	return nil
}

func (s Source) validateAvailability() error {
	if (s.AvailableAt == nil) != (s.AvailableBasis == "") ||
		(s.AvailableAt != nil && (s.AvailableAt.IsZero() || s.AvailableAt.After(s.ObservedAt))) {
		return errors.New("available_at requires a basis and cannot exceed observed_at")
	}
	if s.AvailableAt == nil || s.Publication == nil {
		return nil
	}
	if start, _ := s.Publication.Range(); s.AvailableAt.Before(start) {
		return errors.New("available_at cannot precede the publication it makes available")
	}
	return nil
}

func (s Source) validateProvider() error {
	if (s.Provider == kindPatent && s.Kind != kindPatent) || (s.Provider == "web" && s.Kind != "analysis") {
		return errors.New("patent/web collector kind does not match provider")
	}
	if err := s.validatePatent(); err != nil {
		return err
	}
	if (s.Kind == kindHypothesis) != (s.Provider == "generated") || (s.Kind == kindHypothesis && s.Publication != nil) {
		return errors.New("generated hypotheses have separate provenance, not a publication date")
	}
	if (s.Provider == providerArXiv) != (s.ArXiv != nil) || (s.Provider == providerGitHub) != (s.GitHub != nil) {
		return errors.New("provider-specific metadata must match provider")
	}

	if s.Kind == kindRepository && s.Publication != nil {
		return errors.New("repository timestamps are not scientific publication dates")
	}
	if s.Provider == providerArXiv {
		return s.validateArXiv()
	}
	if s.Provider == providerGitHub {
		return s.validateGitHub()
	}
	if (s.Provider == "openalex" || s.Provider == "crossref") && s.Kind != kindPaper {
		return errors.New("scientific provider requires paper source kind")
	}

	return nil
}

func (s Source) validatePatent() error {
	if s.Patent == nil {
		// Источники, сохранённые до появления идентичности патента, должны оставаться читаемыми.
		return nil
	}
	if s.Kind != kindPatent {
		return errors.New("patent identity belongs only to a patent source")
	}
	if err := s.Patent.Validate(); err != nil {
		return err
	}
	if s.Provider == providerManual &&
		(s.NativeID != s.Patent.PublicationNumber || s.CanonicalID != s.Patent.CanonicalID()) {
		return errors.New("an imported patent must be identified by its publication number")
	}
	return nil
}

func (s Source) validateArXiv() error {
	a := s.ArXiv
	if s.Kind != kindPaper || !arxivID.MatchString(a.BaseID) || a.Version < 1 || s.NativeID != a.BaseID ||
		s.Revision != a.BaseID+"v"+strconv.Itoa(a.Version) || a.FirstPublished.IsZero() ||
		a.VersionUpdated.Before(a.FirstPublished) || a.VersionUpdated.After(s.ObservedAt) ||
		s.Publication == nil || s.Publication.Precision != precisionDay ||
		s.Publication.Value != a.FirstPublished.UTC().Format(time.DateOnly) {
		return errors.New("arxiv requires a base ID, version ID, first/version dates and the matching first publication day")
	}
	if s.RevisionPublication != nil && (s.RevisionPublication.Precision != precisionDay ||
		s.RevisionPublication.Value != a.VersionUpdated.UTC().Format(time.DateOnly)) {
		return errors.New("arxiv revision publication must be the day this version was updated")
	}
	return nil
}

func (s Source) validateGitHub() error {
	g := s.GitHub
	if s.Kind != kindRepository || g.RepositoryID <= 0 || s.NativeID != strconv.FormatInt(g.RepositoryID, 10) ||
		!gitSHA.MatchString(g.CommitSHA) || s.Revision != g.CommitSHA+"@"+s.ObservedAt.UTC().Format(time.RFC3339Nano) ||
		(g.BlobSHA != "" && !gitSHA.MatchString(g.BlobSHA)) ||
		g.CreatedAt.IsZero() || g.UpdatedAt.Before(g.CreatedAt) || g.UpdatedAt.After(s.ObservedAt) {
		return errors.New("github requires stable repository ID, commit revision and typed timestamps")
	}
	return s.validateGitHubEvents()
}

func (s Source) validateGitHubEvents() error {
	g := s.GitHub
	if g.RootID != nil && (*g.RootID <= 0 || *g.RootID == g.RepositoryID || !g.Fork) {
		return errors.New("fork root must reference a different repository")
	}
	if (g.ReleasedAt == nil) != (g.ReleaseID == nil) || (g.ReleaseID != nil && *g.ReleaseID <= 0) {
		return errors.New("release requires ID and published timestamp together")
	}
	for _, at := range []*time.Time{g.PushedAt, g.ReleasedAt} {
		if at != nil && (at.IsZero() || at.After(s.ObservedAt)) {
			return errors.New("repository event exceeds observation")
		}
	}
	if (g.Stars != nil && *g.Stars < 0) || (g.Forks != nil && *g.Forks < 0) {
		return errors.New("repository observations cannot be negative")
	}
	return nil
}
