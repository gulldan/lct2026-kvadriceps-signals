package provider

import (
	"encoding/json/v2"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
)

func TestCollectorRoutesCNToIndependentEndpoint(t *testing.T) {
	for _, tc := range []struct {
		name, kind, query string
		configured, cn    bool
	}{
		{"CN scope", providerPatent, "country:CN", true, true},
		{"general patents", providerPatent, "hydrogen", true, false},
		{"legacy fallback", providerPatent, "country:CN", false, false},
		{"web keeps its endpoint", "web", "country:CN", true, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var cnCalls, defaultCalls atomic.Int32
			endpoint := func(population string, calls *atomic.Int32) *httptest.Server {
				return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
					calls.Add(1)
					q := req.URL.Query()
					if req.Method != http.MethodGet || req.URL.Path != "/v1/records" ||
						q.Get("query") != tc.query || q.Get("from") != "2026-05-01" ||
						q.Get("through") != "2026-05-31" || q.Get("limit") != "1" || q.Get("cursor") != "checkpoint:1" {
						t.Errorf("collector lost the pinned query or cursor: %s %s", req.Method, req.URL)
					}
					body, err := json.Marshal(map[string]any{
						"records": []collectorRecord{{ID: "CN122065940A", Title: "知识抽取方法", Text: "原始中文内容",
							Date: "2026-05-19", DateBasis: "Google Patents publicationDate", KindCode: "A",
							URL: "https://patents.google.com/patent/CN122065940A/zh", Language: "zh", TextStatus: "full_text"}},
						"exhausted": true, "total": 1, "source_set": population,
					})
					if err != nil {
						t.Error(err)
						http.Error(w, "encode response", http.StatusInternalServerError)
						return
					}
					w.Header().Set("Content-Type", "application/json")
					_, _ = w.Write(body)
				}))
			}
			cn := endpoint("cn-snapshot", &cnCalls)
			defer cn.Close()
			other := endpoint("default-snapshot", &defaultCalls)
			defer other.Close()
			t.Setenv("PATENT_COLLECTOR_URL", other.URL)
			t.Setenv("WEB_COLLECTOR_URL", other.URL)
			cnURL := ""
			if tc.configured {
				cnURL = cn.URL + "/"
			}
			t.Setenv("CN_PATENT_COLLECTOR_URL", cnURL)
			collector, err := NewCollector(tc.kind)
			if err != nil {
				t.Fatal(err)
			}
			page, err := collector.Fetch(t.Context(), Scope{Query: tc.query, From: "2026-05-01",
				Through: "2026-05-31", PageSize: 1}, "checkpoint:1")
			if err != nil || len(page.Records) != 1 {
				t.Fatalf("configured snapshot was not ingested: %d records, %v", len(page.Records), err)
			}
			wantCN, wantDefault, population := int32(0), int32(1), "default-snapshot"
			if tc.cn {
				wantCN, wantDefault, population = 1, 0, "cn-snapshot"
			}
			if cnCalls.Load() != wantCN || defaultCalls.Load() != wantDefault || collector.PopulationVersion() != population {
				t.Fatalf("wrong collector routed: cn=%d default=%d population=%s",
					cnCalls.Load(), defaultCalls.Load(), collector.PopulationVersion())
			}
			if page.Records[0].Source.Language != "zh" || page.Records[0].Source.NativeID != "CN122065940A" {
				t.Fatal("routing lost original patent identity or language")
			}
		})
	}
}
