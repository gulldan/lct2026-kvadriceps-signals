package application

import (
	"fmt"
	"math"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

// Окна среза 2026 года (до 2020, 2020–2023, 2024–2025) кодируются в ответе числом 1–3, истории +10; «broken» роняет запрос.
func TestHNCounts(t *testing.T) {
	windows := map[string][2]string{"0": {"1577836800", "1"}, "1577836800": {"1704067200", "2"},
		"1704067200": {"1767225600", "3"}}
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		q := r.URL.Query()
		from, to, _ := strings.Cut(strings.TrimPrefix(q.Get("numericFilters"), "created_at_i>="), ",created_at_i<")
		window, ok := windows[from]
		if r.URL.Path != "/search" || q.Get("hitsPerPage") != "0" || !ok || window[0] != to {
			t.Errorf("не тот запрос: %s", r.URL.RawQuery)
		}
		if query := q.Get("query"); query != `"say hi"` && query != `"broken"` {
			t.Errorf("фраза не точная: %q", query)
		}
		story := q.Get("tags") == "story"
		if q.Get("query") == `"broken"` && story && window[1] == "3" {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		hits := window[1]
		if story {
			hits = "1" + hits
		}
		_, _ = fmt.Fprintf(w, `{"nbHits":%s,"hits":[]}`, hits)
	}))
	t.Cleanup(server.Close)

	phrases := []string{`say "hi"`, "broken"}
	counts, err := HackerNewsCounts(t.Context(), server.URL, phrases, "2026-09")
	if err == nil || counts["broken"] != nil {
		t.Fatalf("сбой одного запроса должен убрать фразу из ответа: %v %v", counts, err)
	}
	want := LiveHNCounts{Before: 1, BeforeStory: 11, Mid: 2, MidStory: 12, Recent: 3, RecentStory: 13}
	if got := counts[`say "hi"`]; got == nil || *got != want {
		t.Fatalf("счёт по окнам: %+v", got)
	}
	inputs := LiveHNInputs(counts[`say "hi"`])
	expected := []float64{math.Log1p(6), 1.0 / 6, math.Log(2.5) - math.Log(1.5), math.Log1p(3), math.Log1p(13),
		math.Log(7.5) - math.Log(4)}
	for i := range expected {
		if math.Abs(inputs[i]-expected[i]) > 1e-12 {
			t.Fatalf("вход %d: %v, ждали %v", i, inputs[i], expected[i])
		}
	}
	if missing := LiveHNInputs(nil); len(missing) != 6 || !math.IsNaN(missing[5]) {
		t.Fatalf("без ответа входы неизмерены: %v", missing)
	}

	// Кеш месяца: удача не переспрашивается, сбой спрашивается снова.
	cache := newLiveHNCache(server.URL)
	requests.Store(0)
	cache.counts(t.Context(), phrases, "2026-09")
	if got := cache.counts(t.Context(), phrases, "2026-09"); got[`say "hi"`] == nil || requests.Load() != 18 {
		t.Fatalf("кеш: %d запросов, ждали 12 + 6", requests.Load())
	}
}
