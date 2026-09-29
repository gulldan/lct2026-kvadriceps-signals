package main

import (
	"encoding/json/v2"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestStatusCountsTitlesWithoutChangingSnapshot(t *testing.T) {
	s := &service{data: &snapshot{Records: []record{
		{ID: "first", Title: "Hydrogen process"}, {ID: "missing"}, {ID: "last", Title: "Battery materials"},
	}}}
	w := httptest.NewRecorder()
	s.status(w, httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/v1/status", nil))
	var body struct {
		Records int `json:"records"`
		Titles  int `json:"dated_titles"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if w.Code != http.StatusOK || body.Records != 3 || body.Titles != 2 {
		t.Fatalf("status counts changed: %s", w.Body.String())
	}
	if len(s.data.Records) != 3 || s.data.Records[1].ID != "missing" || s.data.Records[2].ID != "last" {
		t.Fatal("reading status modified the source snapshot")
	}
}

func BenchmarkStatusLargeSnapshot(b *testing.B) {
	rows := make([]record, 50000)
	for i := range rows {
		if i%2 == 0 {
			rows[i].Title = "Verified title"
		}
	}
	s := &service{data: &snapshot{Records: rows}}
	r := httptest.NewRequestWithContext(b.Context(), http.MethodGet, "/v1/status", nil)
	b.ReportAllocs()
	for b.Loop() {
		s.status(httptest.NewRecorder(), r)
	}
}
