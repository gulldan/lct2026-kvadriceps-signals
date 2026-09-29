package openalex

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"
)

func TestClientAddsCredentialsPacesAndHonoursBudget(t *testing.T) {
	t.Parallel()
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.URL.Query().Get("api_key") != "key" || r.URL.Query().Get("mailto") != "dev@example.com" ||
			r.Header.Get("User-Agent") != "rag-test-test/1.0" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		switch {
		case r.URL.Path == "/works/doi:10.1/missing":
			w.WriteHeader(http.StatusNotFound)
		case r.URL.Path == "/works/doi:10.1/spent":
			w.Header().Set("Retry-After", "8246")
			w.WriteHeader(http.StatusTooManyRequests)
		case calls == 1:
			w.Header().Set("Retry-After", "2")
			w.WriteHeader(http.StatusTooManyRequests)
		default:
			w.Header().Set("X-Ratelimit-Limit", "10000")
			w.Header().Set("X-Ratelimit-Remaining", "9990")
			w.Header().Set("X-Ratelimit-Reset", "3600")
			_, _ = w.Write([]byte(`{"meta":{"count":1,"per_page":100},"results":[{"id":"https://openalex.org/W1",` +
				`"doi":"https://doi.org/10.1/ABC","title":"T","publication_year":2026,"cited_by_count":3,` +
				`"type":"preprint","language":"en",` +
				`"primary_location":{"landing_page_url":"https://arxiv.org/abs/2601.04227",` +
				`"source":{"display_name":"Venue"}},"abstract_inverted_index":{"world":[1],"hello":[0]}}]}`))
		}
	}))
	defer server.Close()
	client, err := New(Config{BaseURL: server.URL, APIKey: "key", Mailto: "dev@example.com", UserAgent: "rag-test-test/1.0"})
	if err != nil {
		t.Fatal(err)
	}
	waits := []time.Duration{}
	client.Sleep = func(_ context.Context, d time.Duration) error { waits = append(waits, d); return nil }
	page, err := client.Works(t.Context(), url.Values{"filter": {"doi:10.1/abc"}, "per-page": {"100"}})
	if err != nil || page.Meta.Count != 1 || len(page.Results) != 1 || calls != 2 || len(waits) == 0 || waits[0] != 2*time.Second {
		t.Fatalf("works: page=%+v err=%v calls=%d waits=%v", page, err, calls, waits)
	}
	if budget, ok := client.Budget(); !ok || budget.Limit != 10000 || budget.Remaining != 9990 {
		t.Fatalf("budget headers not recorded: %+v %v", budget, ok)
	}
	if _, ok := client.With("", "").Budget(); ok {
		t.Fatal("the address budget is not the key's budget")
	}
	work := page.Results[0]
	if work.PlainDOI() != "10.1/abc" || work.Venue() != "Venue" || work.Abstract() != "hello world" || work.CitedByCount != 3 {
		t.Fatalf("work: %+v", work)
	}
	if work.Type != "preprint" || work.Language != "en" || work.PrimaryLocation == nil ||
		work.PrimaryLocation.LandingPageURL != "https://arxiv.org/abs/2601.04227" {
		t.Fatalf("source metadata lost: %+v", work)
	}
	if _, _, err = client.WorkByDOI(t.Context(), "https://doi.org/10.1/missing"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("404 must be ErrNotFound: %v", err)
	}
	before := calls
	_, _, err = client.WorkByDOI(t.Context(), "10.1/spent", "id")
	var budget *BudgetError
	if !errors.As(err, &budget) || budget.ResetIn != 8246*time.Second || calls != before+1 {
		t.Fatalf("spent budget must fail at once: err=%v calls=%d", err, calls-before)
	}
	if spent, ok := client.Budget(); !ok || spent.Remaining != 0 || spent.Limit != 10000 {
		t.Fatalf("spent budget must read as zero remaining: %+v %v", spent, ok)
	}
	if other := client.With("", ""); other.Configured() || !client.Configured() {
		t.Fatal("With must not change the original credentials")
	}
	if _, err = New(Config{Proxy: "ftp://x"}); err == nil {
		t.Fatal("invalid proxy must be refused")
	}
}
