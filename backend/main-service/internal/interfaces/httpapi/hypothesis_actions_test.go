package httpapi_test

import (
	"net/http"
	"strings"
	"testing"

	commonv1 "github.com/example/main-service/internal/platform/genproto/common/v1"
)

// Общие действия прячут чужие строки, а админ действует от имени настоящего владельца, включая чтение
// и запись ниже по стеку.
func TestHypothesisActions_Ownership(t *testing.T) {
	for _, tc := range []struct {
		action string
		answer string
	}{
		{"enrich", `{}`},
		{"assess-trl", `{"levels":[{"level":1,"met":false}]}`},
		{"competitors", `{"summary":"s","competitors":[]}`},
		{"tag", `{"research_type":"практическое","grnti":[],"vak":[],"asjc":[]}`},
		{"experiment", `{"materials":["m"],"success_criteria":["c"]}`},
		{"refine", supportedVerdict},
	} {
		t.Run(tc.action, func(t *testing.T) {
			h := newHarness(t, withoutJobs())
			h.cat.putHypothesis(&commonv1.Hypothesis{
				Id: "owned", OwnerId: "alice", Title: "H", Statement: "alpha beta gamma delta",
			})
			h.answerer.push(&commonv1.RagResponse{
				Answer: tc.answer, Model: "m",
				Sources: []*commonv1.Source{{ChunkId: "c1", Snippet: "alpha beta gamma delta epsilon"}},
			}, nil)
			path := "/hypotheses/owned/" + tc.action
			if r := h.do(t, http.MethodPost, path, h.token(t, "bob"), ""); r.code != http.StatusNotFound {
				t.Fatalf("foreign action = %d: %s", r.code, r.body)
			}
			admin := h.token(t, "root", "admin")
			if r := h.do(t, http.MethodPost, path, admin, ""); r.code != http.StatusOK ||
				!strings.Contains(r.body, `"owner_id":"alice"`) {
				t.Fatalf("admin action must preserve alice's ownership = %d: %s", r.code, r.body)
			}
			if r := h.do(t, http.MethodPost, "/hypotheses/missing/"+tc.action, admin, ""); r.code != http.StatusNotFound {
				t.Fatalf("missing admin target = %d: %s", r.code, r.body)
			}
		})
	}
}
