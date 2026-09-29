package httpapi_test

import (
	"encoding/json/v2"
	"net/http"
	"strings"
	"testing"

	commonv1 "github.com/example/main-service/internal/platform/genproto/common/v1"
)

func TestClusterITCPreservesSourceData(t *testing.T) {
	h := newHarness(t)
	h.cat.clusters["theme"] = &commonv1.Cluster{
		Id: "theme", OwnerId: "alice", Label: "Materials", Summary: "Source summary",
		DocumentCount: 2, ChunkCount: 12,
		Representatives: `[{"document_id":"doc-1","snippet":"Source evidence"}]`,
		Params: `{"members":["doc-1","doc-2"],"fingerprint":"stable","itc":{"score":3,` +
			`"signals":{"pub_count":0,"freshness_available":false,"orgs":["Private sample"]},` +
			`"components":{"SM":{"norm":0.5}}}}`,
	}
	token := h.token(t, "alice")
	compact := h.do(t, http.MethodGet, "/clusters", token, "")
	full := h.do(t, http.MethodGet, "/clusters?view=full", token, "")
	// Описание темы едет только полным видом: сборщики читают его, списочные
	// экраны — нет.
	if compact.code != http.StatusOK || strings.Contains(compact.body, `"members"`) ||
		strings.Contains(compact.body, "Source summary") || !strings.Contains(full.body, "Source summary") ||
		full.code != http.StatusOK || !strings.Contains(full.body, `"members"`) || !strings.Contains(full.body, "Source evidence") {
		t.Fatalf("cluster projections: compact %d %s; full %d %s", compact.code, compact.body, full.code, full.body)
	}
	if !strings.Contains(compact.body, `"pub_count":0`) || !strings.Contains(compact.body, `"freshness_available":false`) ||
		strings.Contains(compact.body, "Private sample") || strings.Contains(compact.body, `"components"`) {
		t.Fatalf("compact list must preserve missing-data signals without heavy details: %s", compact.body)
	}
	for _, request := range []struct {
		owner string
		body  string
		code  int
	}{
		{"bob", `{"itc":{"score":7}}`, http.StatusNotFound},
		{"alice", `{"itc":{"score":0}}`, http.StatusBadRequest},
		{"alice", `{"itc":null}`, http.StatusBadRequest},
		{"alice", `{"itc":{"score":7,"axes":{"novelty":0.8}}}`, http.StatusOK},
	} {
		r := h.do(t, http.MethodPost, "/clusters/theme/itc", h.token(t, request.owner), request.body)
		if r.code != request.code {
			t.Fatalf("store cluster ITC = %d: %s; want %d", r.code, r.body, request.code)
		}
	}
	updated := h.cat.clusters["theme"]
	var params struct {
		Members     []string `json:"members"`
		Fingerprint string   `json:"fingerprint"`
		ITC         struct {
			Score int `json:"score"`
		} `json:"itc"`
	}
	if err := json.Unmarshal([]byte(updated.GetParams()), &params); err != nil {
		t.Fatal(err)
	}
	if updated.GetLabel() != "Materials" || updated.GetSummary() != "Source summary" || updated.GetDocumentCount() != 2 ||
		!strings.Contains(updated.GetRepresentatives(), "Source evidence") ||
		params.Fingerprint != "stable" || len(params.Members) != 2 || params.ITC.Score != 7 {
		t.Fatalf("ITC update changed source data: %v", updated)
	}
}

// Витрине поиска нужна только верхушка списка: ?limit= отдаёт самые крупные
// темы, а не девятьсот записей ради четырёх строк.
func TestClusterListLimitKeepsBiggestThemes(t *testing.T) {
	h := newHarness(t)
	for id, docs := range map[string]int32{"small": 1, "big": 30, "mid": 10} {
		h.cat.clusters[id] = &commonv1.Cluster{Id: id, OwnerId: "alice", Label: id, DocumentCount: docs}
	}
	limited := h.do(t, http.MethodGet, "/clusters?limit=2", h.token(t, "alice"), "")
	var got []struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal([]byte(limited.body), &got); err != nil {
		t.Fatal(err)
	}
	if limited.code != http.StatusOK || len(got) != 2 || got[0].ID != "big" || got[1].ID != "mid" {
		t.Fatalf("limit must keep the biggest themes: %d %s", limited.code, limited.body)
	}
}
