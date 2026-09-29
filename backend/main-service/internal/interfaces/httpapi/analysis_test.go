package httpapi

import (
	"encoding/json/v2"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"

	"github.com/example/main-service/internal/platform/analysisdoc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestAnalysisViewDoesNotEncodeProjectionAsBase64(t *testing.T) {
	job := analysisdoc.Job{ID: "job", Status: analysisdoc.Published, MethodJSON: analysisdoc.DevelopmentMethod,
		ResultJSON: `{"version":"analysis-result-v1","ranking":{"ordered_ids":[]}}`}
	for _, state := range []string{analysisdoc.Prepared, analysisdoc.Validated, analysisdoc.Published, analysisdoc.Failed} {
		job.Status = state
		view, err := jobView(job, &resultTrimCache{})
		if err != nil {
			t.Fatal(err)
		}
		w := httptest.NewRecorder()
		analysisJSON(w, http.StatusOK, view)
		var got map[string]any
		if err = json.Unmarshal(w.Body.Bytes(), &got); err != nil {
			t.Fatal(err)
		}
		if state == analysisdoc.Published {
			if _, ok := got["result"].(map[string]any); !ok {
				t.Fatal("published projection is not an object")
			}
		} else if got["result"] != nil {
			t.Fatal("partial projection escaped into response")
		}
		method, ok := got["method"].(map[string]any)
		if !ok {
			t.Fatal("method is not an object")
		}
		if _, ok = method["parameters"].(map[string]any); !ok {
			t.Fatal("method parameters are not an object")
		}
	}
}

func TestAnalysisErrorsPreserveScopeAndIdempotencySemantics(t *testing.T) {
	cases := []struct {
		code codes.Code
		want int
	}{{codes.NotFound, http.StatusNotFound},
		{codes.Aborted, http.StatusConflict}, {codes.InvalidArgument, http.StatusBadRequest},
		{codes.ResourceExhausted, http.StatusTooManyRequests}, {codes.FailedPrecondition, http.StatusInsufficientStorage},
		{codes.Unavailable, http.StatusServiceUnavailable}}
	a := &API{}
	for _, c := range cases {
		w := httptest.NewRecorder()
		a.analysisError(w, status.Error(c.code, "private backend detail"))
		if w.Code != c.want {
			t.Fatal(c.code, w.Code)
		}
		if c.code == codes.ResourceExhausted && w.Header().Get("Retry-After") != "10" {
			t.Fatal("queue backpressure needs a retry delay")
		}
		var got map[string]string
		if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil || got["error"] == "private backend detail" {
			t.Fatal("error leaked", err)
		}
	}
}

func TestAnalysisViewPreservesSelectedCorpus(t *testing.T) {
	scopes := []string{strings.Repeat("a", 64)}
	job := analysisdoc.Job{MethodJSON: analysisdoc.DevelopmentMethod,
		Request: &analysisdoc.Request{OwnerID: "private-owner", CollectionScopeIDs: scopes}}
	view, err := jobView(job, &resultTrimCache{})
	if err != nil || view.Request == nil || !slices.Equal(view.Request.CollectionScopeIDs, scopes) {
		t.Fatal("selected corpus was lost in the public request view", err)
	}
	raw, err := json.Marshal(view.Request)
	if err != nil || strings.Contains(string(raw), "owner") {
		t.Fatal("public corpus request exposed authority fields", err)
	}
	var decoded analysisRequestView
	if err := json.Unmarshal(raw, &decoded, json.RejectUnknownMembers(true)); err != nil ||
		!slices.Equal(decoded.CollectionScopeIDs, scopes) {
		t.Fatal("the returned corpus request cannot be submitted again", err)
	}
}
