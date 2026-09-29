package httpapi_test

import (
	"net/http"
	"strings"
	"testing"

	commonv1 "github.com/example/main-service/internal/platform/genproto/common/v1"
)

type route struct {
	method string
	path   string
}

// Токен с пустым user id проходит middleware (она проверяет только подпись), поэтому каждая ручка
// владельца обязана отказать сама.
func TestMissingClaims_Sweep(t *testing.T) {
	h := newHarness(t)
	tok := h.token(t, "")
	routes := []route{
		{http.MethodGet, "/documents"},
		{http.MethodGet, "/documents/x"},
		{http.MethodGet, "/documents/x/chunks"},
		{http.MethodGet, "/documents/x/content"},
		{http.MethodPost, "/documents"},
		{http.MethodPost, "/chats"},
		{http.MethodGet, "/chats"},
		{http.MethodGet, "/chats/x"},
		{http.MethodGet, "/chats/x/messages"},
		{http.MethodPost, "/chats/x/messages"},
		{http.MethodPost, "/uploads"},
		{http.MethodGet, "/uploads/x/parts"},
		{http.MethodPost, "/uploads/x/complete"},
		{http.MethodDelete, "/uploads/x"},
		{http.MethodGet, "/hypotheses"},
		{http.MethodGet, "/hypotheses/board"},
		{http.MethodPost, "/hypotheses"},
		{http.MethodPost, "/hypotheses/generate"},
		{http.MethodGet, "/hypotheses/graph"},
		{http.MethodGet, "/hypotheses/x"},
		{http.MethodPost, "/hypotheses/x/verify"},
		{http.MethodPost, "/hypotheses/x/assess-trl"},
		{http.MethodPost, "/hypotheses/x/competitors"},
		{http.MethodPost, "/hypotheses/x/refine"},
		{http.MethodPost, "/hypotheses/x/experiment"},
		{http.MethodPost, "/hypotheses/x/tag"},
		{http.MethodPost, "/hypotheses/x/itc"},
		{http.MethodGet, "/hypotheses/scoring-weights"},
		{http.MethodPut, "/hypotheses/scoring-weights"},
		{http.MethodGet, "/hypotheses/runtime-settings"},
		{http.MethodPut, "/hypotheses/runtime-settings"},
		{http.MethodPut, "/hypotheses/x"},
		{http.MethodGet, "/hypotheses/x/revisions"},
		{http.MethodPost, "/hypotheses/x/revisions"},
		{http.MethodGet, "/hypotheses/x/evidence"},
		{http.MethodPost, "/hypothesis-jobs"},
		{http.MethodGet, "/hypothesis-jobs"},
		{http.MethodGet, "/hypothesis-jobs/x"},
		{http.MethodGet, "/kpis"},
		{http.MethodPost, "/kpis"},
		{http.MethodPost, "/kpis/x/graph-hypotheses"},
		{http.MethodGet, "/kpis/x"},
		{http.MethodPut, "/kpis/x"},
		{http.MethodGet, "/clusters"},
		{http.MethodPost, "/clusters/replace"},
		{http.MethodPost, "/clusters"},
		{http.MethodDelete, "/clusters"},
		{http.MethodGet, "/clusters/x"},
		{http.MethodPut, "/clusters/x"},
	}
	for _, rt := range routes {
		resp := h.do(t, rt.method, rt.path, tok, "{}")
		if resp.code != http.StatusUnauthorized {
			t.Errorf("%s %s with empty claims = %d, want 401", rt.method, rt.path, resp.code)
		}
		_ = resp.body
	}
}

func TestAdmin_MissingClaims(t *testing.T) {
	h := newHarness(t)
	tok := h.token(t, "")
	for _, path := range []string{"/admin/users", "/admin/documents", "/admin/metrics", "/admin/metrics/run", "/admin/itc/run"} {
		method := http.MethodGet
		if path == "/admin/metrics/run" || path == "/admin/itc/run" {
			method = http.MethodPost
		}
		r := h.do(t, method, path, tok, "")
		// Без ролей — 403, без claims — 401; в любом случае отказ.
		if r.code != http.StatusForbidden && r.code != http.StatusUnauthorized {
			t.Fatalf("%s with empty claims = %d, want 401/403", path, r.code)
		}
		_ = r.body
	}
}

func TestPostMessage_RendersSources(t *testing.T) {
	h := newHarness(t)
	h.chats.chats["c1"] = &commonv1.Chat{Id: "c1", OwnerId: "alice"}
	h.answerer.push(&commonv1.RagResponse{
		Answer:  "grounded",
		Model:   "m",
		Sources: []*commonv1.Source{{DocumentId: "d1", Filename: "p.pdf", ChunkId: "c", Snippet: "frag", Score: 0.5}},
	}, nil)
	r := h.do(t, http.MethodPost, "/chats/c1/messages", h.token(t, "alice"), `{"content":"q","request_id":"turn-1"}`)
	if r.code != http.StatusCreated {
		t.Fatalf("post message = %d: %s", r.code, r.body)
	}
	if body := r.body; !strings.Contains(body, "p.pdf") {
		t.Fatalf("message view must render its source citations, got %s", body)
	}
}

func TestBoard_VerdictParsing(t *testing.T) {
	h := newHarness(t)
	h.cat.putHypothesis(&commonv1.Hypothesis{
		Id: "h1", OwnerId: "alice", Title: "Refuted one", Status: "generated",
		Assessment: `{"check":{"verdict":"refuted"}}`, RiskScore: f64(0.9),
	})
	h.cat.putHypothesis(&commonv1.Hypothesis{
		Id: "h2", OwnerId: "alice", Title: "Supported ready", Status: "approved",
		Assessment: `{"check":{"verdict":"supported"}}`, Trl: i32(6), CompositeScore: f64(0.8),
	})
	r := h.do(t, http.MethodGet, "/hypotheses/board?queue=risk", h.token(t, "alice"), "")
	if r.code != http.StatusOK {
		t.Fatalf("board = %d", r.code)
	}
	if body := r.body; !strings.Contains(body, "Refuted one") {
		t.Fatalf("risk queue must include the refuted hypothesis, got %s", body)
	}
}

func f64(v float64) *float64 { return &v }
func i32(v int32) *int32     { return &v }
