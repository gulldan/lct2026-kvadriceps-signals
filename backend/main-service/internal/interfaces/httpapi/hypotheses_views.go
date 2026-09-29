package httpapi

import (
	"encoding/json"
	"regexp"
	"strings"

	"github.com/example/main-service/internal/application"

	commonv1 "github.com/example/main-service/internal/platform/genproto/common/v1"
)

// JSONB-колонки идут в proto строками и отдаются настоящим JSON; оценки — указатели, чтобы гипотеза
// без оценки давала null.

func rawJSON(s string) json.RawMessage {
	if s == "" || !json.Valid([]byte(s)) {
		return json.RawMessage("null")
	}
	return json.RawMessage(s)
}

func jsonStr(m json.RawMessage) string {
	if len(m) == 0 {
		return ""
	}
	return string(m)
}

func keepJSONKeys(s string, keys ...string) json.RawMessage {
	var m map[string]any
	if err := json.Unmarshal([]byte(s), &m); err != nil || m == nil {
		return rawJSON(s)
	}
	kept := make(map[string]any, len(keys))
	for _, key := range keys {
		if v, ok := m[key]; ok {
			kept[key] = v
		}
	}
	out, err := json.Marshal(kept)
	if err != nil {
		return rawJSON(s)
	}
	return out
}

// generationRef оставляет от generation только скаляры, нужные спискам (вкладки, роутинг карточек,
// бейдж происхождения).
func generationRef(s string) json.RawMessage {
	return keepJSONKeys(s, "kind", "semantic_kind", "model", "fallback_reason")
}

func orEmptyStrings(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}

// nilIfEmpty превращает пустой kpi_id / cluster_id / document_id в SQL NULL: пустая строка сломала бы FK.
func nilIfEmpty(s *string) *string {
	if s == nil || *s == "" {
		return nil
	}
	return s
}

type evidenceView struct {
	ID             string   `json:"id"`
	DocumentID     string   `json:"document_id,omitempty"`
	ChunkID        string   `json:"chunk_id"`
	Filename       string   `json:"filename"`
	Snippet        string   `json:"snippet"`
	Stance         string   `json:"stance"`
	Score          *float64 `json:"score"`
	Ord            int32    `json:"ord"`
	Relation       string   `json:"relation,omitempty"`
	NumericSignals []string `json:"numeric_signals,omitempty"`
	PageStart      int32    `json:"page_start,omitempty"`
	PageEnd        int32    `json:"page_end,omitempty"`
	SectionHeading string   `json:"section_heading,omitempty"`
	Origin         string   `json:"origin,omitempty"`
}

// newEvidenceView берёт связь фрагмента от модели, а без неё — детерминированный шаблон, так что
// комментарий к связи есть всегда.
func newEvidenceView(e *commonv1.HypothesisEvidence) evidenceView {
	signals := evidenceNumericSignals(e.GetSnippet())
	relation := strings.TrimSpace(e.GetRelation())
	if relation == "" {
		relation = evidenceRelation(e.GetStance(), signals)
	}
	return evidenceView{
		ID: e.GetId(), DocumentID: e.GetDocumentId(), ChunkID: e.GetChunkId(), Filename: e.GetFilename(),
		Snippet: e.GetSnippet(), Stance: e.GetStance(), Score: e.Score, Ord: e.GetOrd(),
		Relation: relation, NumericSignals: signals,
		PageStart: e.GetPageStart(), PageEnd: e.GetPageEnd(), SectionHeading: e.GetSectionHeading(),
		Origin: e.GetOrigin(),
	}
}

var evidenceNumberRE = regexp.MustCompile(`(?i)[+-]?[0-9]+(?:[.,][0-9]+)?\s?` +
	`(?:%|°C|K|МПа|ГПа|MPa|GPa|mAh/g|мА·ч/г|мм/год|mm/year|мкм|μm|nm|нм|h|ч|мин|циклов?)`)

func evidenceNumericSignals(snippet string) []string {
	matches := evidenceNumberRE.FindAllString(snippet, -1)
	if len(matches) == 0 {
		return nil
	}
	out := make([]string, 0, min(len(matches), 3))
	seen := map[string]struct{}{}
	for _, raw := range matches {
		v := strings.TrimSpace(raw)
		key := strings.ToLower(v)
		if v == "" {
			continue
		}
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, v)
		if len(out) == 3 {
			break
		}
	}
	return out
}

func evidenceRelation(stance string, signals []string) string {
	var base string
	switch stance {
	case "supports":
		base = "Фрагмент содержит результат или условие, которое работает в пользу заявленного эффекта."
	case "contradicts":
		base = "Фрагмент указывает на ограничение, отрицательный результат или условие, которое спорит с заявленным эффектом."
	default:
		base = "Фрагмент даёт условия, методику или материал для проверки, но сам по себе не доказывает эффект."
	}
	if len(signals) == 0 {
		return base
	}
	return base + " Численные сигналы: " + strings.Join(signals, ", ") + "."
}

func newEvidenceViews(es []*commonv1.HypothesisEvidence) []evidenceView {
	out := make([]evidenceView, 0, len(es))
	seen := map[string]struct{}{}
	for _, e := range es {
		key := strings.Join(strings.Fields(strings.ToLower(e.GetSnippet())), " ")
		if key != "" {
			if _, dup := seen[key]; dup {
				continue
			}
			seen[key] = struct{}{}
		}
		out = append(out, newEvidenceView(e))
	}
	return out
}

type evidenceInput struct {
	DocumentID *string  `json:"document_id"`
	ChunkID    string   `json:"chunk_id"`
	Filename   string   `json:"filename"`
	Snippet    string   `json:"snippet"`
	Stance     string   `json:"stance"`
	Score      *float64 `json:"score"`
	Ord        int32    `json:"ord"`
}

type revisionView struct {
	ID           string          `json:"id"`
	HypothesisID string          `json:"hypothesis_id"`
	RevisionNo   int32           `json:"revision_no"`
	EditorID     string          `json:"editor_id"`
	Action       string          `json:"action"`
	Summary      string          `json:"summary"`
	Patch        json.RawMessage `json:"patch"`
	CreatedAt    string          `json:"created_at"`
}

func newRevisionView(r *commonv1.HypothesisRevision) revisionView {
	return revisionView{
		ID: r.GetId(), HypothesisID: r.GetHypothesisId(), RevisionNo: r.GetRevisionNo(),
		EditorID: r.GetEditorId(), Action: r.GetAction(), Summary: r.GetSummary(),
		Patch: rawJSON(r.GetPatch()), CreatedAt: r.GetCreatedAt(),
	}
}

func newRevisionViews(rs []*commonv1.HypothesisRevision) []revisionView {
	out := make([]revisionView, 0, len(rs))
	for _, r := range rs {
		out = append(out, newRevisionView(r))
	}
	return out
}

type revisionInput struct {
	Action   string          `json:"action"`
	Summary  string          `json:"summary"`
	EditorID string          `json:"editor_id"`
	Patch    json.RawMessage `json:"patch"`
}

func (in *revisionInput) toProto(hypothesisID string) *commonv1.HypothesisRevision {
	if in == nil {
		return nil
	}
	return &commonv1.HypothesisRevision{
		HypothesisId: hypothesisID, Action: in.Action, Summary: in.Summary,
		EditorId: in.EditorID, Patch: jsonStr(in.Patch),
	}
}

type hypothesisView struct {
	Version          int64           `json:"version"`
	ID               string          `json:"id"`
	OwnerID          string          `json:"owner_id"`
	RunID            string          `json:"run_id"`
	Title            string          `json:"title"`
	Statement        string          `json:"statement"`
	Rationale        string          `json:"rationale"`
	Method           string          `json:"method"`
	Status           string          `json:"status"`
	KPIID            string          `json:"kpi_id,omitempty"`
	PrimaryClusterID string          `json:"primary_cluster_id,omitempty"`
	TRL              *int32          `json:"trl"`
	NoveltyScore     *float64        `json:"novelty_score"`
	RiskScore        *float64        `json:"risk_score"`
	ValueScore       *float64        `json:"value_score"`
	ConfidenceScore  *float64        `json:"confidence_score"`
	CompositeScore   *float64        `json:"composite_score"`
	Measurable       bool            `json:"measurable"`
	Organization     string          `json:"organization"`
	FunctionArea     string          `json:"function_area"`
	SourceType       string          `json:"source_type"`
	Location         string          `json:"location"`
	Tags             []string        `json:"tags"`
	Assessment       json.RawMessage `json:"assessment"`
	Detail           json.RawMessage `json:"detail"`
	Generation       json.RawMessage `json:"generation"`
	CreatedAt        string          `json:"created_at"`
	UpdatedAt        string          `json:"updated_at"`
	EvidenceCount    int             `json:"evidence_count"`
	EvidenceDocs     int             `json:"evidence_document_count"`
	EvidenceSupports int             `json:"evidence_support_count"`
	EvidenceAgainst  int             `json:"evidence_contradict_count"`
	Evidence         []evidenceView  `json:"evidence"`
}

func newHypothesisView(h *commonv1.Hypothesis) hypothesisView {
	return newHypothesisViewWithEvidence(h, true)
}

func newHypothesisViewWithEvidence(h *commonv1.Hypothesis, includeEvidence bool) hypothesisView {
	evidence := h.GetEvidence()
	evidenceViews := []evidenceView{}
	if includeEvidence {
		evidenceViews = newEvidenceViews(evidence)
	}
	total, docs, supports, contradicts := hypothesisEvidenceStats(evidence)
	return hypothesisView{
		Version: h.GetVersion(),
		ID:      h.GetId(), OwnerID: h.GetOwnerId(), RunID: h.GetRunId(), Title: h.GetTitle(),
		Statement: h.GetStatement(), Rationale: h.GetRationale(), Method: h.GetMethod(), Status: h.GetStatus(),
		KPIID: h.GetKpiId(), PrimaryClusterID: h.GetPrimaryClusterId(), TRL: h.Trl,
		NoveltyScore: h.NoveltyScore, RiskScore: h.RiskScore, ValueScore: h.ValueScore,
		ConfidenceScore: h.ConfidenceScore, CompositeScore: h.CompositeScore, Measurable: h.GetMeasurable(),
		Organization: h.GetOrganization(), FunctionArea: h.GetFunctionArea(), SourceType: h.GetSourceType(),
		Location: h.GetLocation(), Tags: orEmptyStrings(h.GetTags()), Assessment: rawJSON(h.GetAssessment()),
		Detail: rawJSON(h.GetDetail()), Generation: rawJSON(h.GetGeneration()),
		CreatedAt: h.GetCreatedAt(), UpdatedAt: h.GetUpdatedAt(),
		EvidenceCount: total, EvidenceDocs: docs, EvidenceSupports: supports, EvidenceAgainst: contradicts,
		Evidence: evidenceViews,
	}
}

func newHypothesisViews(hs []*commonv1.Hypothesis) []hypothesisView {
	out := make([]hypothesisView, 0, len(hs))
	for _, h := range hs {
		out = append(out, newHypothesisView(h))
	}
	return out
}

type hypothesisRefView struct {
	ID         string          `json:"id"`
	KPIID      string          `json:"kpi_id,omitempty"`
	Status     string          `json:"status"`
	Title      string          `json:"title"`
	Generation json.RawMessage `json:"generation"`
}

func newHypothesisRefViews(hs []*commonv1.Hypothesis) []hypothesisRefView {
	out := make([]hypothesisRefView, 0, len(hs))
	for _, h := range hs {
		out = append(out, hypothesisRefView{
			ID: h.GetId(), KPIID: h.GetKpiId(), Status: h.GetStatus(), Title: h.GetTitle(),
			Generation: generationRef(h.GetGeneration()),
		})
	}
	return out
}

func hypothesisEvidenceStats(evidence []*commonv1.HypothesisEvidence) (total, docs, supports, contradicts int) {
	docSet := map[string]bool{}
	for _, e := range evidence {
		if e == nil {
			continue
		}
		total++
		switch strings.ToLower(strings.TrimSpace(e.GetStance())) {
		case "supports":
			supports++
		case "contradicts":
			contradicts++
		}
		docKey := strings.TrimSpace(e.GetDocumentId())
		if docKey == "" {
			docKey = strings.TrimSpace(e.GetFilename())
		}
		if docKey != "" {
			docSet[docKey] = true
		}
	}
	return total, len(docSet), supports, contradicts
}

type hypothesisInput struct {
	Version          int64           `json:"version"`
	RunID            string          `json:"run_id"`
	Title            string          `json:"title"`
	Statement        string          `json:"statement"`
	Rationale        string          `json:"rationale"`
	Method           string          `json:"method"`
	Status           string          `json:"status"`
	KPIID            *string         `json:"kpi_id"`
	PrimaryClusterID *string         `json:"primary_cluster_id"`
	TRL              *int32          `json:"trl"`
	NoveltyScore     *float64        `json:"novelty_score"`
	RiskScore        *float64        `json:"risk_score"`
	ValueScore       *float64        `json:"value_score"`
	ConfidenceScore  *float64        `json:"confidence_score"`
	CompositeScore   *float64        `json:"composite_score"`
	Measurable       bool            `json:"measurable"`
	Organization     string          `json:"organization"`
	FunctionArea     string          `json:"function_area"`
	SourceType       string          `json:"source_type"`
	Location         string          `json:"location"`
	Tags             []string        `json:"tags"`
	Assessment       json.RawMessage `json:"assessment"`
	Detail           json.RawMessage `json:"detail"`
	Generation       json.RawMessage `json:"generation"`
	Evidence         []evidenceInput `json:"evidence"`
	Revision         *revisionInput  `json:"revision"`
}

func (in hypothesisInput) toProto(id string) *commonv1.Hypothesis {
	h := &commonv1.Hypothesis{
		Version: in.Version,
		Id:      id, RunId: in.RunID, Title: in.Title, Statement: in.Statement, Rationale: in.Rationale,
		Method: in.Method, Status: in.Status, KpiId: nilIfEmpty(in.KPIID), PrimaryClusterId: nilIfEmpty(in.PrimaryClusterID),
		Trl: in.TRL, NoveltyScore: in.NoveltyScore, RiskScore: in.RiskScore, ValueScore: in.ValueScore,
		ConfidenceScore: in.ConfidenceScore, CompositeScore: in.CompositeScore, Measurable: in.Measurable,
		Organization: in.Organization, FunctionArea: in.FunctionArea, SourceType: in.SourceType,
		Location: in.Location, Tags: in.Tags, Assessment: jsonStr(in.Assessment),
		Detail: jsonStr(in.Detail), Generation: jsonStr(in.Generation),
	}
	for _, e := range in.Evidence {
		h.Evidence = append(h.Evidence, &commonv1.HypothesisEvidence{
			DocumentId: nilIfEmpty(e.DocumentID), ChunkId: e.ChunkID, Filename: e.Filename, Snippet: e.Snippet,
			Stance: e.Stance, Score: e.Score, Ord: e.Ord,
		})
	}
	return h
}

type graphNodeView struct {
	ID      string `json:"id"`
	Kind    string `json:"kind"`
	Label   string `json:"label"`
	Status  string `json:"status,omitempty"`
	TRL     *int32 `json:"trl,omitempty"`
	Verdict string `json:"verdict,omitempty"`
	Degree  int    `json:"degree,omitempty"`
	KPIID   string `json:"kpi_id,omitempty"`
}

type graphEdgeView struct {
	Source  string   `json:"source"`
	Target  string   `json:"target"`
	Class   string   `json:"class"`
	Score   *float64 `json:"score,omitempty"`
	ChunkID string   `json:"chunk_id,omitempty"`
}

type graphView struct {
	Nodes []graphNodeView `json:"nodes"`
	Edges []graphEdgeView `json:"edges"`
}

func newGraphView(g *application.HypothesisGraph) graphView {
	v := graphView{
		Nodes: make([]graphNodeView, 0, len(g.Nodes)),
		Edges: make([]graphEdgeView, 0, len(g.Edges)),
	}
	for _, n := range g.Nodes {
		v.Nodes = append(v.Nodes, graphNodeView{
			ID: n.ID, Kind: n.Kind, Label: n.Label, Status: n.Status,
			TRL: n.TRL, Verdict: n.Verdict, Degree: n.Degree, KPIID: n.KPIID,
		})
	}
	for _, e := range g.Edges {
		v.Edges = append(v.Edges, graphEdgeView{
			Source: e.Source, Target: e.Target, Class: e.Class, Score: e.Score, ChunkID: e.ChunkID,
		})
	}
	return v
}
