package httpapi_test

import (
	"net/http"
	"strings"
	"testing"

	commonv1 "github.com/example/main-service/internal/platform/genproto/common/v1"
)

const supportedVerdict = `{"verdict":"supported","confidence":0.9,"rationale":"ok",` +
	`"supporting":["alpha beta gamma delta"],"contradicting":[]}`

func TestHypotheses_AdminSeesAllOwners(t *testing.T) {
	h := newHarness(t)
	h.cat.putHypothesis(&commonv1.Hypothesis{Id: "h-a", OwnerId: "alice", Title: "A", Statement: "s"})
	h.cat.putHypothesis(&commonv1.Hypothesis{Id: "h-b", OwnerId: "bob", Title: "B", Statement: "s"})
	user := h.token(t, "alice", "user")
	admin := h.token(t, "root", "admin")

	if r := h.do(t, http.MethodGet, "/hypotheses", user, ""); r.code != http.StatusOK ||
		strings.Contains(r.body, "h-b") {
		t.Fatalf("user list = %d, must not contain foreign rows: %s", r.code, r.body)
	}
	if r := h.do(t, http.MethodGet, "/hypotheses", admin, ""); r.code != http.StatusOK ||
		!strings.Contains(r.body, "h-a") || !strings.Contains(r.body, "h-b") {
		t.Fatalf("admin list = %d: %s", r.code, r.body)
	}
	if r := h.do(t, http.MethodGet, "/hypotheses/h-b", admin, ""); r.code != http.StatusOK ||
		!strings.Contains(r.body, `"owner_id":"bob"`) {
		t.Fatalf("admin foreign detail = %d: %s", r.code, r.body)
	}
	if r := h.do(t, http.MethodGet, "/hypotheses/h-b", user, ""); r.code != http.StatusNotFound {
		t.Fatalf("user foreign detail = %d, want 404", r.code)
	}
	if r := h.do(t, http.MethodPut, "/hypotheses/h-b", admin, `{"title":"B2","statement":"s2"}`); r.code != http.StatusOK ||
		!strings.Contains(r.body, `"owner_id":"bob"`) || !strings.Contains(r.body, `"title":"B2"`) {
		t.Fatalf("admin foreign update = %d, owner must stay bob: %s", r.code, r.body)
	}
	if r := h.do(t, http.MethodPut, "/hypotheses/h-a", user, `{"title":"A2","statement":"s2"}`); r.code != http.StatusOK {
		t.Fatalf("own update = %d: %s", r.code, r.body)
	}
}

func TestHypotheses_Create(t *testing.T) {
	h := newHarness(t)
	tok := h.token(t, "alice")
	if r := h.do(t, http.MethodPost, "/hypotheses", tok, `{"title":"","statement":""}`); r.code != http.StatusBadRequest {
		t.Fatalf("missing fields = %d, want 400", r.code)
	}
	r := h.do(t, http.MethodPost, "/hypotheses", tok, `{"title":"H","statement":"S","value_score":0.7}`)
	if r.code != http.StatusCreated {
		t.Fatalf("create = %d: %s", r.code, r.body)
	}
	if !strings.Contains(r.body, "composite_score") {
		t.Fatal("response must include a composite score")
	}
}

func TestHypotheses_ListAndBoard(t *testing.T) {
	h := newHarness(t)
	docID := "doc-1"
	h.cat.putHypothesis(&commonv1.Hypothesis{Id: "h1", OwnerId: "alice", Title: "Coating idea", Status: "generated"})
	h.cat.evidence["h1"] = []*commonv1.HypothesisEvidence{
		{Id: "ev-1", HypothesisId: "h1", DocumentId: &docID, Stance: "supports"},
	}
	h.cat.putHypothesis(&commonv1.Hypothesis{Id: "h2", OwnerId: "bob", Title: "Other"})
	tok := h.token(t, "alice")

	list := h.do(t, http.MethodGet, "/hypotheses?status=generated&tags=a,b", tok, "").body
	if !strings.Contains(list, "Coating idea") || strings.Contains(list, "Other") {
		t.Fatalf("list must be owner-scoped, got %s", list)
	}
	refs := h.do(t, http.MethodGet, "/hypotheses?view=ref", tok, "").body
	if !strings.Contains(refs, "Coating idea") || strings.Contains(refs, "statement") {
		t.Fatalf("ref view must carry only id/kpi_id/status/title/generation, got %s", refs)
	}
	board := h.do(t, http.MethodGet, "/hypotheses/board?q=coating&queue=needs_verify&limit=10", tok, "").body
	if !strings.Contains(board, "queue_counts") || !strings.Contains(board, "facets") {
		t.Fatalf("board must carry aggregates, got %s", board)
	}
	if !strings.Contains(board, `"evidence_count":1`) || !strings.Contains(board, `"evidence_document_count":1`) {
		t.Fatalf("board items must carry evidence counters, got %s", board)
	}
}

func TestListResponsesArePruned(t *testing.T) {
	h := newHarness(t)
	tok := h.token(t, "alice")
	h.cat.putHypothesis(&commonv1.Hypothesis{
		Id: "h1", OwnerId: "alice", Title: "H", Status: "generated",
		Assessment: `{"trl":{"level":4,"levels":[{"note":"big"}]},` +
			`"check":{"verdict":"supported","supporting":["x"],"refuting":["y"]},` +
			`"ranking":{"score":71,"factors":[{"note":"f"}]}}`,
		Detail: `{"plan":"big"}`, Generation: `{"prompt":"big","kind":"bridge"}`,
	})
	h.cat.clusters["c1"] = &commonv1.Cluster{
		Id: "c1", OwnerId: "alice", Label: "C",
		Representatives: `[{"snippet":"long"}]`,
		Params: `{"members":[1,2],"fingerprint":"fp1","metrics":{"size":2},` +
			`"itc":{"score":6.5,"techscore":0.42,"components":{"a":1},"signals":{"pub_count":2,"years":[2025]}}}`,
	}

	board := h.do(t, http.MethodGet, "/hypotheses/board", tok, "").body
	if strings.Contains(board, `"levels"`) || strings.Contains(board, `"supporting"`) ||
		strings.Contains(board, `"refuting"`) || strings.Contains(board, `"factors"`) {
		t.Fatalf("board assessment must be pruned, got %s", board)
	}
	if !strings.Contains(board, `"verdict":"supported"`) || !strings.Contains(board, `"score":71`) {
		t.Fatalf("board must keep assessment scalars, got %s", board)
	}
	if !strings.Contains(board, `"detail":null`) || strings.Contains(board, `"prompt"`) ||
		!strings.Contains(board, `"generation":{"kind":"bridge"}`) {
		t.Fatalf("board must drop detail and keep only generation scalars, got %s", board)
	}

	refs := h.do(t, http.MethodGet, "/hypotheses?view=ref", tok, "").body
	if !strings.Contains(refs, `"generation":{"kind":"bridge"}`) || strings.Contains(refs, `"prompt"`) {
		t.Fatalf("ref view must carry truncated generation, got %s", refs)
	}

	list := h.do(t, http.MethodGet, "/clusters", tok, "").body
	if strings.Contains(list, `"members"`) || strings.Contains(list, `"components"`) ||
		strings.Contains(list, `"metrics"`) || strings.Contains(list, `"years"`) ||
		strings.Contains(list, `"score":6.5`) || strings.Contains(list, "snippet") {
		t.Fatalf("cluster list must keep only the projected params, got %s", list)
	}
	if !strings.Contains(list, `"representatives":[]`) || !strings.Contains(list, `"techscore":0.42`) ||
		!strings.Contains(list, `"pub_count":2`) || !strings.Contains(list, `"fingerprint":"fp1"`) {
		t.Fatalf("cluster list must keep itc scalars, got %s", list)
	}
	full := h.do(t, http.MethodGet, "/clusters/c1", tok, "").body
	if !strings.Contains(full, "snippet") || !strings.Contains(full, `"members"`) {
		t.Fatalf("cluster detail must stay full, got %s", full)
	}
}

func TestHypotheses_BoardQueuePartition(t *testing.T) {
	h := newHarness(t)
	tok := h.token(t, "alice")
	// supported ⇒ «Готовые» даже при низких TRL и оценке и высоком риске, но не «Противоречия».
	h.cat.putHypothesis(&commonv1.Hypothesis{
		Id: "supported", OwnerId: "alice", Title: "Supported", Status: "generated",
		Assessment: `{"check":{"verdict":"supported"}}`, Trl: i32(2), CompositeScore: f64(0.20), RiskScore: f64(0.95),
	})
	h.cat.putHypothesis(&commonv1.Hypothesis{
		Id: "mixed", OwnerId: "alice", Title: "Mixed", Status: "generated",
		Assessment: `{"check":{"verdict":"mixed"}}`,
	})
	// insufficient ⇒ «Недостаточно данных» даже при высоком риске, но не «Противоречия».
	h.cat.putHypothesis(&commonv1.Hypothesis{
		Id: "insufficient", OwnerId: "alice", Title: "Insufficient", Status: "generated",
		Assessment: `{"check":{"verdict":"insufficient"}}`, RiskScore: f64(0.95),
	})
	h.cat.putHypothesis(&commonv1.Hypothesis{
		Id: "pending", OwnerId: "alice", Title: "Pending", Status: "generated",
	})

	body := h.do(t, http.MethodGet, "/hypotheses/board", tok, "").body
	for _, want := range []string{`"all":4`, `"ready":1`, `"risk":1`, `"insufficient":1`, `"needs_verify":1`} {
		if !strings.Contains(body, want) {
			t.Fatalf("queue must partition on verdict (want %s), got %s", want, body)
		}
	}
}

func TestHypotheses_GetUpdateRevisionsEvidence(t *testing.T) {
	h := newHarness(t)
	docID := "d1"
	h.cat.putHypothesis(&commonv1.Hypothesis{
		Id: "h1", OwnerId: "alice", Title: "H", Statement: "S",
		Evidence: []*commonv1.HypothesisEvidence{{Id: "e1", DocumentId: &docID, Stance: "supports"}},
	})
	h.cat.evidence["h1"] = []*commonv1.HypothesisEvidence{{Id: "e1", DocumentId: &docID, Stance: "supports"}}
	tok := h.token(t, "alice")

	if r := h.do(t, http.MethodGet, "/hypotheses/h1", tok, ""); r.code != http.StatusOK {
		t.Fatalf("get = %d", r.code)
	} else {
		_ = r.body
	}
	if r := h.do(t, http.MethodPut, "/hypotheses/h1", tok,
		`{"title":"H2","statement":"S2","value_score":0.5}`); r.code != http.StatusOK {
		t.Fatalf("update = %d: %s", r.code, r.body)
	} else {
		_ = r.body
	}
	if r := h.do(t, http.MethodGet, "/hypotheses/h1/revisions", tok, ""); r.code != http.StatusOK {
		t.Fatalf("list revisions = %d", r.code)
	} else {
		_ = r.body
	}
	if r := h.do(t, http.MethodPost, "/hypotheses/h1/revisions", tok,
		`{"action":"reviewed","summary":"ok"}`); r.code != http.StatusCreated {
		t.Fatalf("add revision = %d: %s", r.code, r.body)
	} else {
		_ = r.body
	}
	if r := h.do(t, http.MethodPost, "/hypotheses/h1/revisions", tok, `{"summary":"x"}`); r.code != http.StatusBadRequest {
		t.Fatalf("missing action = %d, want 400", r.code)
	}
	if r := h.do(t, http.MethodGet, "/hypotheses/h1/evidence", tok, ""); r.code != http.StatusOK {
		t.Fatalf("list evidence = %d", r.code)
	} else {
		_ = r.body
	}
}

func TestHypotheses_LLMEndpoints(t *testing.T) {
	cases := []struct {
		path   string
		answer string
	}{
		{"/hypotheses/h1/verify", supportedVerdict},
		{"/hypotheses/h1/assess-trl", `{"levels":[{"level":1,"met":false}]}`},
		{"/hypotheses/h1/competitors", `{"summary":"s","competitors":[]}`},
		{"/hypotheses/h1/refine", supportedVerdict},
		{"/hypotheses/h1/experiment", `{"materials":["m"],"success_criteria":["c"]}`},
		{"/hypotheses/h1/tag", `{"research_type":"практическое","grnti":[],"vak":[],"asjc":[]}`},
	}
	for _, tc := range cases {
		t.Run(tc.path, func(t *testing.T) {
			h := newHarness(t)
			h.answerer.push(&commonv1.RagResponse{
				Answer: tc.answer, Model: "m",
				Sources: []*commonv1.Source{{ChunkId: "c1", Snippet: "alpha beta gamma delta epsilon"}},
			}, nil)
			h.cat.putHypothesis(&commonv1.Hypothesis{
				Id: "h1", OwnerId: "alice", Title: "H", Statement: "alpha beta gamma delta",
			})
			r := h.do(t, http.MethodPost, tc.path, h.token(t, "alice"), "")
			if r.code != http.StatusOK {
				t.Fatalf("%s = %d: %s", tc.path, r.code, r.body)
			}
			_ = r.body
		})
	}
}

// Чужой владелец получает 404 на LLM-ручках: владение проверяется до вызова модели.
func TestHypotheses_VerifyForeign(t *testing.T) {
	h := newHarness(t)
	h.cat.putHypothesis(&commonv1.Hypothesis{Id: "h1", OwnerId: "alice", Statement: "s"})
	r := h.do(t, http.MethodPost, "/hypotheses/h1/verify", h.token(t, "bob"), "")
	if r.code != http.StatusNotFound {
		t.Fatalf("foreign verify = %d, want 404", r.code)
	}
	_ = r.body
}

func TestHypotheses_StoreITC(t *testing.T) {
	h := newHarness(t)
	h.cat.putHypothesis(&commonv1.Hypothesis{Id: "h1", OwnerId: "alice"})
	body := `{"cluster_id":"cl1","itc":{"score":7},"composite_score":0.7}`
	r := h.do(t, http.MethodPost, "/hypotheses/h1/itc", h.token(t, "alice"), body)
	if r.code != http.StatusOK {
		t.Fatalf("store itc = %d: %s", r.code, r.body)
	}
	_ = r.body
}

func TestRubrics(t *testing.T) {
	h := newHarness(t)
	tok := h.token(t, "alice")
	if r := h.do(t, http.MethodGet, "/trl/rubric", tok, ""); r.code != http.StatusOK || len(r.body) == 0 {
		t.Fatal("trl rubric must be non-empty JSON")
	}
	if r := h.do(t, http.MethodGet, "/itc/rubric", tok, ""); r.code != http.StatusOK || len(r.body) == 0 {
		t.Fatal("itc rubric must be non-empty JSON")
	}
}

func TestHypotheses_Generate(t *testing.T) {
	h := newHarness(t)
	h.answerer.push(&commonv1.RagResponse{
		Answer:  `[{"title":"A","statement":"sa","rationale":"ra"}]`,
		Model:   "m",
		Sources: []*commonv1.Source{{DocumentId: "d1", Filename: "p.pdf", Snippet: "grounded fact"}},
	}, nil)
	h.answerer.push(&commonv1.RagResponse{Answer: "[]", Model: "m"}, nil)
	r := h.do(t, http.MethodPost, "/hypotheses/generate", h.token(t, "alice"), `{"kpi_title":"Hardness","count":1}`)
	if r.code != http.StatusCreated {
		t.Fatalf("generate = %d: %s", r.code, r.body)
	}
	_ = r.body
}

func TestHypotheses_GenerateValidation(t *testing.T) {
	h := newHarness(t)
	r := h.do(t, http.MethodPost, "/hypotheses/generate", h.token(t, "alice"), `{"count":1}`)
	if r.code != http.StatusBadRequest {
		t.Fatalf("missing kpi = %d, want 400", r.code)
	}
	_ = r.body
}

func TestHypotheses_Graph(t *testing.T) {
	h := newHarness(t)
	h.cat.putHypothesis(&commonv1.Hypothesis{Id: "h1", OwnerId: "alice", Title: "H"})
	r := h.do(t, http.MethodGet, "/hypotheses/graph", h.token(t, "alice"), "")
	if r.code != http.StatusOK || !strings.Contains(r.body, "nodes") {
		t.Fatal("graph must return nodes/edges")
	}
}
