package httpapi_test

import (
	"net/http"
	"strings"
	"testing"

	commonv1 "github.com/example/main-service/internal/platform/genproto/common/v1"
)

func TestScoringWeights(t *testing.T) {
	h := newHarness(t)
	tok := h.token(t, "alice")
	if r := h.do(t, http.MethodGet, "/hypotheses/scoring-weights", tok, ""); r.code != http.StatusOK {
		t.Fatalf("get weights = %d", r.code)
	} else if !strings.Contains(r.body, "kpi_fit") {
		t.Fatal("weights must include kpi_fit")
	}
	if r := h.do(t, http.MethodPut, "/hypotheses/scoring-weights", tok,
		`{"kpi_fit":0.5,"evidence":0.5,"novelty":0,"value":0,"risk_inv":0,"trl_fit":0}`); r.code != http.StatusOK {
		t.Fatalf("update weights = %d: %s", r.code, r.body)
	} else {
		_ = r.body
	}
	if r := h.do(t, http.MethodPut, "/hypotheses/scoring-weights", tok,
		`{"kpi_fit":2,"evidence":0,"novelty":0,"value":0,"risk_inv":0,"trl_fit":0}`); r.code != http.StatusBadRequest {
		t.Fatalf("out-of-range = %d, want 400", r.code)
	}
	if r := h.do(t, http.MethodPut, "/hypotheses/scoring-weights", tok,
		`{"kpi_fit":0,"evidence":0,"novelty":0,"value":0,"risk_inv":0,"trl_fit":0}`); r.code != http.StatusBadRequest {
		t.Fatalf("all-zero = %d, want 400", r.code)
	}
}

func TestHypothesisRuntimeSettings(t *testing.T) {
	h := newHarness(t)
	tok := h.token(t, "alice")
	if r := h.do(t, http.MethodGet, "/hypotheses/runtime-settings", tok, ""); r.code != http.StatusOK {
		t.Fatalf("get settings = %d", r.code)
	} else if !strings.Contains(r.body, "default_generate_count") {
		t.Fatalf("settings must include generation count, got %s", r.body)
	}
	body := `{"default_generate_count":25,"cluster_generate_count":4,"direction_generate_count":4,` +
		`"generation_timeout_sec":900,"ready_trl_min":5,"ready_score_min":65,` +
		`"risk_score_min":75,"graph_direction_limit":30,"deep_postprocess_enabled":true}`
	if r := h.do(t, http.MethodPut, "/hypotheses/runtime-settings", tok, body); r.code != http.StatusOK {
		t.Fatalf("update settings = %d: %s", r.code, r.body)
	} else if !strings.Contains(r.body, `"default_generate_count":10`) ||
		!strings.Contains(r.body, `"graph_direction_limit":20`) ||
		!strings.Contains(r.body, `"generation_timeout_sec":600`) {
		t.Fatalf("settings must be clamped, got %s", r.body)
	}
	if r := h.do(t, http.MethodGet, "/hypotheses/runtime-settings", tok, ""); r.code != http.StatusOK {
		t.Fatalf("get saved settings = %d", r.code)
	} else if !strings.Contains(r.body, `"cluster_generate_count":4`) ||
		!strings.Contains(r.body, `"deep_postprocess_enabled":true`) {
		t.Fatalf("saved settings missing custom value, got %s", r.body)
	}
}

func TestHypothesisJobs(t *testing.T) {
	h := newHarness(t)
	h.cat.putHypothesis(&commonv1.Hypothesis{Id: "h1", OwnerId: "alice", Statement: "alpha beta gamma delta"})
	h.answerer.push(&commonv1.RagResponse{
		Answer: supportedVerdict, Model: "m",
		Sources: []*commonv1.Source{{ChunkId: "c1", Snippet: "alpha beta gamma delta x"}},
	}, nil)
	// Раннер держится до отрисовки ответа: он и обработчик делят *HypothesisJob, и ранний старт дал бы
	// гонку чтения и записи.
	gate := make(chan struct{})
	h.jobStore.claimGate = gate
	tok := h.token(t, "alice")

	r := h.do(t, http.MethodPost, "/hypothesis-jobs", tok, `{"kind":"verify","hypothesis_id":"h1"}`)
	if r.code != http.StatusAccepted {
		t.Fatalf("enqueue = %d: %s", r.code, r.body)
	}
	id := extractJSONString(t, r.body, "id")
	close(gate)

	if r := h.do(t, http.MethodGet, "/hypothesis-jobs", tok, ""); r.code != http.StatusOK {
		t.Fatalf("list jobs = %d", r.code)
	} else {
		_ = r.body
	}
	if r := h.do(t, http.MethodGet, "/hypothesis-jobs/"+id, tok, ""); r.code != http.StatusOK {
		t.Fatalf("get job = %d", r.code)
	} else {
		_ = r.body
	}
	if r := h.do(t, http.MethodPost, "/hypothesis-jobs", tok, `{"kind":"bogus"}`); r.code != http.StatusBadRequest {
		t.Fatalf("bad kind = %d, want 400", r.code)
	}
}
