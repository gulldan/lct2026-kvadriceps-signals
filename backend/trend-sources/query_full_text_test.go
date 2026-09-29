package main

import (
	"encoding/json/v2"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestQueryFullTextOnlyOnRequest(t *testing.T) {
	text := "Carbon capture method. Полный текст: прототип прошёл пилотные испытания."
	s := &service{data: &snapshot{Source: "USPTO", ObservedAt: "2026-09-10T12:00:00Z", Records: []record{{ID: "1",
		Title: "Carbon capture method", Date: "2026-09-08", URL: "https://example.org/1", Text: text,
		TextStatus: "full_text", FullText: &patentFullTextFacts{GUID: "test"}}}}}
	for _, tc := range []struct {
		target, want string
	}{{"/v1/query", ""}, {"/v1/query?full_text=1", text}} {
		r := httptest.NewRequestWithContext(t.Context(), http.MethodPost, tc.target, strings.NewReader(
			`{"terms":["carbon capture"],"periods":[{"start":"2026-09-01","end_exclusive":"2026-09-11"}]}`))
		w := httptest.NewRecorder()
		s.query(w, r)
		var response struct {
			Records []record `json:"records"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
			t.Fatal(err)
		}
		if len(response.Records) != 1 || response.Records[0].Text != tc.want || response.Records[0].FullText != nil {
			t.Fatalf("%s: неверный текст или разметка: %+v", tc.target, response.Records)
		}
	}
	if s.data.Records[0].FullText == nil || s.data.Records[0].Text != text {
		t.Fatal("запрос изменил сохранённый документ")
	}
}
