package analysisdoc

import (
	"cmp"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"math"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/example/db-service/internal/platform/indexdoc"
)

// Evidence адресует текст закреплённого источника в кодовых точках, конец не включается.
type Evidence struct {
	ID             string `json:"id"`
	Field          string `json:"field"`
	WorkID         string `json:"work_id"`
	DocumentID     string `json:"document_id"`
	SnapshotID     string `json:"snapshot_id"`
	SourceRevision string `json:"source_revision"`
	TextSHA256     string `json:"text_sha256"`
	Start          int    `json:"start"`
	End            int    `json:"end"`
	Quote          string `json:"quote"`
	ClaimType      string `json:"claim_type"`
}

type BriefField struct {
	Text        string   `json:"text"`
	EvidenceIDs []string `json:"evidence_ids"`
}

type Brief struct {
	Problem           *BriefField `json:"problem"`
	Advantage         *BriefField `json:"advantage"`
	Example           *BriefField `json:"example"`
	DescriptionStatus string      `json:"description_status"`
	EvidenceSHA256    string      `json:"evidence_sha256"`
}

type Candidate struct {
	ID               string         `json:"id"`
	Title            string         `json:"title"`
	WorkIDs          []string       `json:"work_ids"`
	Measurements     jsontext.Value `json:"measurements"`
	Assessment       jsontext.Value `json:"assessment"`
	Brief            Brief          `json:"brief"`
	Evidence         []Evidence     `json:"evidence"`
	CompleteForBrief bool           `json:"complete_for_brief"`
	MissingFields    []string       `json:"missing_fields"`
	Relevance        Relevance      `json:"relevance"`
}

// Relevance зависит от запроса и никогда не становится временным признаком появления.
type Relevance struct {
	Version            string   `json:"version"`
	Relevant           bool     `json:"relevant"`
	SemanticSimilarity *float64 `json:"semantic_similarity"`
	LexicalOverlap     float64  `json:"lexical_overlap"`
	Mode               string   `json:"mode"`
}

type Ranking struct {
	OrderedIDs   []string   `json:"ordered_ids"`
	Completeness string     `json:"completeness"`
	Reasons      []string   `json:"reasons"`
	Diversity    *Diversity `json:"diversity,omitzero"`
}

// Diversity: порядок MMR проверяется независимо по векторам неизменяемого снимка; сходства от воркера
// не используются.
type Diversity struct {
	Version            string   `json:"version"`
	Lambda             float64  `json:"lambda"`
	DuplicateThreshold float64  `json:"duplicate_threshold"`
	InputSHA256        string   `json:"input_sha256"`
	SuppressedIDs      []string `json:"suppressed_ids"`
}

type Work struct {
	ID                 string   `json:"id"`
	Kind               string   `json:"kind"`
	DocumentIDs        []string `json:"document_ids"`
	Aliases            []string `json:"aliases"`
	ImplementationRoot *string  `json:"implementation_root"`
	PatentFamily       *string  `json:"patent_family"`
	Limitations        []string `json:"limitations"`
}

// Result — полная проекция; векторам, владельцу и данным живого индекса здесь не место.
type Result struct {
	Version         string                `json:"version"`
	JobID           string                `json:"job_id"`
	InputRunID      string                `json:"input_run_id"`
	InputSHA256     string                `json:"input_sha256"`
	MethodSHA256    string                `json:"method_sha256"`
	Query           string                `json:"query"`
	Cutoff          string                `json:"cutoff"`
	Candidates      []Candidate           `json:"candidates"`
	Unassigned      []string              `json:"unassigned"`
	Works           []Work                `json:"works"`
	Relations       []Relation            `json:"relations"`
	Coverage        jsontext.Value        `json:"coverage"`
	Search          jsontext.Value        `json:"search"`
	Limitations     []string              `json:"limitations"`
	Ranking         Ranking               `json:"ranking"`
	Technologies    []ResultTechnology    `json:"technologies,omitempty"`
	Directions      []ResultDirection     `json:"directions,omitempty"`
	TechnologyOrder ResultTechnologyOrder `json:"technology_order,omitzero"`
}

func ValidateResult(raw string, job Job, input Input) error {
	if len(raw) == 0 || len(raw) > MaxBytes {
		return ErrInvalid
	}
	var finite any
	if json.Unmarshal([]byte(raw), &finite) != nil {
		return ErrInvalid
	}
	r, err := Decode[Result](raw)
	if err != nil || len(raw) > MaxBytes || r.Version != ResultVersion || r.JobID != job.ID ||
		r.InputRunID != input.RunID || r.InputSHA256 != job.InputSHA256 || r.MethodSHA256 != job.MethodSHA256 ||
		r.Query != input.Request.Query || r.Cutoff != input.Request.Cutoff || len(r.Candidates) > MaxDocuments ||
		len(r.Works) > MaxDocuments || len(r.Ranking.OrderedIDs) > 15 || len(r.Coverage) < 2 {
		return ErrInvalid
	}
	docs := make(map[string]Record, len(input.Records))
	for _, d := range input.Records {
		docs[d.ID] = d
	}
	works, err := validateWorks(r.Works, docs)
	if err != nil {
		return err
	}
	if r.Ranking.Diversity != nil && r.Ranking.Diversity.InputSHA256 != r.InputSHA256 {
		return ErrInvalid
	}
	if job.MethodJSON != "" {
		var expected struct {
			Parameters struct {
				RankingVersion string `json:"ranking_version"`
			} `json:"parameters"`
		}
		if json.Unmarshal([]byte(job.MethodJSON), &expected) != nil ||
			!matchesDiversityMethod(expected.Parameters.RankingVersion, r.Ranking.Diversity) {
			return ErrInvalid
		}
	}
	if err = validateCandidates(r, works, docs); err != nil {
		return err
	}
	return validateTechnologyContent(r, input)
}

func validateCandidates(r Result, works map[string]bool, docs map[string]Record) error {
	seen, members := map[string]Candidate{}, map[string]bool{}
	for _, c := range r.Candidates {
		if _, exists := seen[c.ID]; exists || c.ID == "" || len(c.WorkIDs) < 2 || c.Title == "" ||
			len(c.Measurements) < 2 || len(c.Assessment) < 2 || validateEvidence(c, docs) != nil ||
			!validRelevance(c.Relevance) {
			return ErrInvalid
		}
		seen[c.ID] = c
		for _, id := range c.WorkIDs {
			if !works[id] || members[id] {
				return ErrInvalid
			}
			members[id] = true
		}
	}
	for _, id := range r.Unassigned {
		if !works[id] || members[id] {
			return ErrInvalid
		}
		members[id] = true
	}
	if len(members) != len(works) {
		return ErrInvalid
	}
	return validateRanking(r.Ranking, seen)
}

func validateWorks(all []Work, docs map[string]Record) (map[string]bool, error) {
	works := make(map[string]bool, len(all))
	docSeen := make(map[string]bool, len(docs))
	for _, work := range all {
		if work.ID == "" || works[work.ID] || len(work.DocumentIDs) == 0 {
			return nil, ErrInvalid
		}
		for _, id := range work.DocumentIDs {
			if _, ok := docs[id]; !ok || docSeen[id] {
				return nil, ErrInvalid
			}
			docSeen[id] = true
		}
		works[work.ID] = true
	}
	if len(docSeen) != len(docs) {
		return nil, ErrInvalid
	}
	return works, nil
}

func validateEvidence(c Candidate, docs map[string]Record) error {
	if len(c.Evidence) > 12 {
		return ErrInvalid
	}
	seen := map[string]Evidence{}
	for _, e := range c.Evidence {
		d, ok := docs[e.DocumentID]
		if _, exists := seen[e.ID]; exists || !ok || e.ID == "" || !slices.Contains(c.WorkIDs, e.WorkID) ||
			e.SnapshotID != d.Index.SnapshotID || e.SourceRevision != d.Index.SourceRevision ||
			e.TextSHA256 != d.Index.Text.SHA256 || e.Start < 0 || e.End <= e.Start ||
			utf8.RuneCountInString(e.Quote) != e.End-e.Start || e.End-e.Start > 3000 ||
			!validBriefFieldName(e.Field) ||
			!slices.Contains([]string{"research_claim", "company_claim"}, e.ClaimType) {
			return ErrInvalid
		}
		seen[e.ID] = e
	}
	complete := eligibleOrder(c.Assessment)
	for name, field := range (DescriptionFields{c.Brief.Problem, c.Brief.Advantage, c.Brief.Example}).byName() {
		if field == nil {
			complete = false
			continue
		}
		if validateBriefField(name, field, seen, c.Brief.DescriptionStatus) != nil {
			return ErrInvalid
		}
	}
	if c.CompleteForBrief != complete || !ValidHash(c.Brief.EvidenceSHA256) ||
		c.Brief.DescriptionStatus != DescriptionTemplate {
		return ErrInvalid
	}
	return nil
}

func validateBriefField(name string, field *BriefField, seen map[string]Evidence, descriptionStatus string) error {
	if field.Text == "" || len(field.EvidenceIDs) == 0 || len(field.Text) > 12000 {
		return ErrInvalid
	}
	quotes := make([]string, 0, len(field.EvidenceIDs))
	for _, id := range field.EvidenceIDs {
		if e, ok := seen[id]; !ok || e.Field != name {
			return ErrInvalid
		}
		quotes = append(quotes, seen[id].Quote)
	}
	if descriptionStatus == DescriptionTemplate && field.Text != strings.Join(quotes, "\n\n") {
		return ErrInvalid
	}
	return nil
}

func validBriefFieldName(name string) bool {
	return name == "problem" || name == "advantage" || name == "example"
}

func validateRanking(r Ranking, candidates map[string]Candidate) error {
	if (len(r.OrderedIDs) == 15 && r.Completeness != "complete") ||
		(len(r.OrderedIDs) < 15 && (r.Completeness != "insufficient_coverage" || len(r.Reasons) == 0)) {
		return ErrInvalid
	}
	seen := map[string]bool{}
	for _, id := range r.OrderedIDs {
		c, ok := candidates[id]
		if !ok || seen[id] || !rankable(c) {
			return ErrInvalid
		}
		seen[id] = true
	}
	if d := r.Diversity; d != nil {
		// Проверка по источникам восстанавливает центроиды и сверяет точный порядок MMR.
		return validateSuppressed(d, candidates, seen)
	}
	want := rankedCandidates(candidates)
	if !slices.Equal(want, r.OrderedIDs) {
		return ErrInvalid
	}
	return nil
}

func validateSuppressed(d *Diversity, candidates map[string]Candidate, seen map[string]bool) error {
	if !validDiversityVersion(d.Version) || d.Lambda != .75 || d.DuplicateThreshold != .92 ||
		!ValidHash(d.InputSHA256) || len(d.SuppressedIDs) > len(candidates) || !slices.IsSorted(d.SuppressedIDs) {
		return ErrInvalid
	}
	for _, id := range d.SuppressedIDs {
		c, exists := candidates[id]
		if !exists || seen[id] || !c.CompleteForBrief || !c.Relevance.Relevant {
			return ErrInvalid
		}
		seen[id] = true
	}
	return nil
}

func validRelevance(r Relevance) bool {
	if r.Version == "" || r.LexicalOverlap < 0 || r.LexicalOverlap > 1 ||
		!slices.Contains([]string{"semantic_and_lexical", "lexical_fallback"}, r.Mode) {
		return false
	}
	if r.SemanticSimilarity != nil && (*r.SemanticSimilarity < -1.000001 || *r.SemanticSimilarity > 1.000001) {
		return false
	}
	return r.Mode != "lexical_fallback" || r.SemanticSimilarity == nil
}

func rankedCandidates(candidates map[string]Candidate) []string {
	ids := orderedCandidates(candidates)
	return ids[:min(15, len(ids))]
}

func orderedCandidates(candidates map[string]Candidate) []string {
	type item struct {
		id    string
		order []float64
	}
	items := []item{}
	for _, c := range candidates {
		if !rankable(c) {
			continue
		}
		var assessment struct {
			Order []float64 `json:"order"`
		}
		if json.Unmarshal(c.Assessment, &assessment) != nil || !eligibleOrder(c.Assessment) {
			continue
		}
		items = append(items, item{c.ID, assessment.Order})
	}
	slices.SortFunc(items, func(a, b item) int {
		for i := range a.order {
			if n := cmp.Compare(b.order[i], a.order[i]); n != 0 {
				return n
			}
		}
		return cmp.Compare(a.id, b.id)
	})
	ids := make([]string, 0, len(items))
	for _, item := range items {
		ids = append(ids, item.id)
	}
	return ids
}

// MinBriefFields — сколько из problem, advantage и example должна назвать тема в рейтинге;
// недостающее показывается как не найденное и никогда не дописывается.
const MinBriefFields = 2

func rankable(c Candidate) bool {
	fields := 0
	for _, f := range []*BriefField{c.Brief.Problem, c.Brief.Advantage, c.Brief.Example} {
		if f != nil {
			fields++
		}
	}
	return c.Relevance.Relevant && fields >= MinBriefFields && eligibleOrder(c.Assessment)
}

func eligibleOrder(raw jsontext.Value) bool {
	var a struct {
		Eligible bool      `json:"eligible"`
		Order    []float64 `json:"order"`
	}
	if json.Unmarshal(raw, &a) != nil || !a.Eligible || len(a.Order) != 4 {
		return false
	}
	return !slices.ContainsFunc(a.Order, func(n float64) bool { return math.IsNaN(n) || math.IsInf(n, 0) })
}

func ResultHash(raw string) string { return indexdoc.Hash([]byte(raw)) }
