package httpapi_test

import (
	"errors"
	"net/http"
	"strings"
	"testing"

	"google.golang.org/genproto/googleapis/rpc/errdetails"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	commonv1 "github.com/example/main-service/internal/platform/genproto/common/v1"
)

func TestFail_GenericError500(t *testing.T) {
	h := newHarness(t)
	boom := errors.New("db unavailable")
	h.cat.listErr = boom
	h.cat.listKPIErr = boom
	h.cat.listClErr = boom
	h.docs.listErr = boom
	h.chats.listErr = boom
	tok := h.token(t, "alice")

	for _, path := range []string{"/documents", "/chats", "/hypotheses", "/kpis", "/clusters"} {
		r := h.do(t, http.MethodGet, path, tok, "")
		if r.code != http.StatusInternalServerError {
			t.Fatalf("%s on error = %d, want 500", path, r.code)
		}
		_ = r.body
	}
}

func TestFail_ResourceExhausted503(t *testing.T) {
	h := newHarness(t)
	h.cat.putHypothesis(&commonv1.Hypothesis{Id: "h1", OwnerId: "alice", Statement: "s"})
	busy, err := status.New(codes.ResourceExhausted, "queue full").WithDetails(&errdetails.RetryInfo{})
	if err != nil {
		t.Fatal(err)
	}
	h.answerer.push(nil, busy.Err())
	r := h.do(t, http.MethodPost, "/hypotheses/h1/verify", h.token(t, "alice"), "")
	if r.code != http.StatusServiceUnavailable {
		t.Fatalf("ResourceExhausted = %d, want 503", r.code)
	}
	_ = r.body
}

func TestEnrichmentAbstentionReturnsSafe422(t *testing.T) {
	h := newHarness(t)
	h.cat.putHypothesis(&commonv1.Hypothesis{Id: "h1", OwnerId: "alice", Statement: "s", Status: "generated"})
	h.answerer.push(&commonv1.RagResponse{
		Answer: "private model response", Trace: &commonv1.RagTrace{AbstainReason: "no_sources"},
	}, nil)
	r := h.do(t, http.MethodPost, "/hypotheses/h1/enrich", h.token(t, "alice"), "")
	if r.code != http.StatusUnprocessableEntity || !strings.Contains(r.body, "недостаточно данных") ||
		strings.Contains(r.body, "private model response") {
		t.Fatalf("unsafe enrichment refusal: status=%d body=%s", r.code, r.body)
	}
}

func TestFailPreservesStatusWithoutExposingStorageDetails(t *testing.T) {
	for _, tc := range []struct {
		code codes.Code
		http int
	}{
		{codes.Aborted, 409}, {codes.Unavailable, 503}, {codes.DeadlineExceeded, 504},
		{codes.Canceled, 408}, {codes.ResourceExhausted, 429}, {codes.Internal, 500},
	} {
		t.Run(tc.code.String(), func(t *testing.T) {
			h := newHarness(t)
			h.cat.listKPIErr = status.Error(tc.code, "private database relation and connection details")
			r := h.do(t, http.MethodGet, "/kpis", h.token(t, "alice"), "")
			if r.code != tc.http || strings.Contains(r.body, "private database") {
				t.Fatalf("unsafe error translation: status=%d body=%s", r.code, r.body)
			}
		})
	}
}

func TestFail_NotFound404(t *testing.T) {
	h := newHarness(t)
	tok := h.token(t, "alice")
	paths := []string{"/documents/missing", "/hypotheses/missing", "/kpis/missing", "/clusters/missing", "/chats/missing"}
	for _, path := range paths {
		r := h.do(t, http.MethodGet, path, tok, "")
		if r.code != http.StatusNotFound {
			t.Fatalf("%s = %d, want 404", path, r.code)
		}
		_ = r.body
	}
}

func TestUpdateOwned_Errors(t *testing.T) {
	h := newHarness(t)
	tok := h.token(t, "alice")
	if r := h.do(t, http.MethodPut, "/kpis/x", tok, `{not json`); r.code != http.StatusBadRequest {
		t.Fatalf("bad PUT body = %d, want 400", r.code)
	}
	if r := h.do(t, http.MethodPut, "/kpis/missing", tok, `{"title":"x"}`); r.code != http.StatusNotFound {
		t.Fatalf("update missing kpi = %d, want 404", r.code)
	}
}

func TestJobs_Unconfigured503(t *testing.T) {
	h := newHarness(t, withoutJobs())
	tok := h.token(t, "alice")
	enqueue := h.do(t, http.MethodPost, "/hypothesis-jobs", tok, `{"kind":"verify","hypothesis_id":"h"}`)
	if enqueue.code != http.StatusServiceUnavailable {
		t.Fatalf("enqueue unconfigured = %d, want 503", enqueue.code)
	}
	if r := h.do(t, http.MethodGet, "/hypothesis-jobs", tok, ""); r.code != http.StatusServiceUnavailable {
		t.Fatalf("list unconfigured = %d, want 503", r.code)
	}
	if r := h.do(t, http.MethodGet, "/hypothesis-jobs/x", tok, ""); r.code != http.StatusServiceUnavailable {
		t.Fatalf("get unconfigured = %d, want 503", r.code)
	}
}

func TestReplaceClustersDoesNotRequireMetricsStore(t *testing.T) {
	h := newHarness(t, withoutMetrics())
	r := h.do(
		t,
		http.MethodPost,
		"/clusters/replace",
		h.token(
			t,
			"alice",
		),
		`{"request_id":"91f5a038-dbd2-4d23-997a-beb654f5a100","clusters":[{"label":"A"}]}`,
	)
	if r.code != http.StatusCreated {
		t.Fatalf("replace without metrics = %d, want 201", r.code)
	}
	_ = r.body
}

func TestListClusters_NilMetricsStore(t *testing.T) {
	h := newHarness(t, withoutMetrics())
	h.cat.clusters["c1"] = &commonv1.Cluster{Id: "c1", OwnerId: "alice", Label: "L"}
	r := h.do(t, http.MethodGet, "/clusters", h.token(t, "alice"), "")
	if r.code != http.StatusOK {
		t.Fatalf("list with nil store = %d, want 200", r.code)
	}
	_ = r.body
}

func TestListClustersSurvivesMetricsFailure(t *testing.T) {
	h := newHarness(t)
	h.metrics.err = errors.New("valkey down")
	r := h.do(t, http.MethodGet, "/clusters", h.token(t, "alice"), "")
	if r.code != http.StatusOK {
		t.Fatalf("list with unavailable metrics = %d, want 200", r.code)
	}
	_ = r.body
}

func TestDecodeResource_BadBody(t *testing.T) {
	h := newHarness(t)
	tok := h.token(t, "alice")
	for _, path := range []string{"/hypotheses", "/kpis", "/clusters", "/hypotheses/generate"} {
		r := h.do(t, http.MethodPost, path, tok, `{bad`)
		if r.code != http.StatusBadRequest {
			t.Fatalf("%s bad body = %d, want 400", path, r.code)
		}
		_ = r.body
	}
}

func TestTriggerWorker_StoreError(t *testing.T) {
	h := newHarness(t)
	h.metrics.err = errors.New("valkey down")
	r := h.do(t, http.MethodPost, "/admin/metrics/run", h.token(t, "root", "admin"), "")
	if r.code != http.StatusInternalServerError {
		t.Fatalf("trigger with store error = %d, want 500", r.code)
	}
	_ = r.body
}

func TestPostMetrics_StoreError(t *testing.T) {
	h := newHarness(t)
	h.metrics.err = errors.New("valkey down")
	r := h.do(t, http.MethodPost, "/admin/metrics", h.token(t, "root", "admin"), `{"x":1}`)
	if r.code != http.StatusInternalServerError {
		t.Fatalf("post metrics store error = %d, want 500", r.code)
	}
	_ = r.body
}
