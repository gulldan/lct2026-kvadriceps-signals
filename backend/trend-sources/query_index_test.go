package main

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestQueryIndexKeepsAnswer(t *testing.T) {
	rows := []record{
		{ID: "1", Title: "Твердотельные аккумуляторы для дронов", Date: "2026-09-02"},
		{ID: "2", Title: "Solid-state battery pack", Text: "Pack. A solid-state battery with sulfide electrolyte.", Date: "2026-08-15"},
		{ID: "3", Title: "Ёмкостный накопитель", Text: "Твердотельный аккумулятор ёмкостью 5 Вт·ч.", Date: "2026-09-05"},
		{ID: "4", Title: "Solid-state battery recall", ExcludedReason: "duplicate", Date: "2026-09-01"},
		{ID: "5", Text: "solid-state battery", Date: "2026-09-01"},
		{ID: "6", Title: "Quantum dots display", Text: "Solid state lighting.", Date: "2026-09-03"},
		{ID: "7", Title: "Old solid state battery", Date: "2025-01-01"},
	}
	plain := &service{data: &snapshot{Source: "test", Records: rows}}
	fast := &service{data: &snapshot{Source: "test", Records: rows, index: newStemIndex(rows)}}
	periods := `"periods":[{"start":"2026-08-01","end_exclusive":"2026-09-01"},` +
		`{"start":"2026-09-01","end_exclusive":"2026-10-01"}]`
	for _, terms := range []string{`["твердотельный аккумулятор"]`, `["solid-state battery","емкостный накопитель"]`,
		`["quantum*"]`, `["solid-state batt*"]`, `["state lighting","дрон"]`} {
		for _, mode := range []string{"", `,"titles":true`, `,"excerpts":true`} {
			body := `{"terms":` + terms + `,` + periods + mode + `}`
			answer := func(s *service) []byte {
				w := httptest.NewRecorder()
				s.query(w, httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/v1/query", strings.NewReader(body)))
				return w.Body.Bytes()
			}
			if want, got := answer(plain), answer(fast); !bytes.Equal(want, got) || !bytes.Contains(got, []byte(`"id"`)) {
				t.Fatalf("%s:\nперебор %s\nиндекс  %s", body, want, got)
			}
		}
	}
}
