package provider

import (
	"context"
	"encoding/json/v2"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"
)

func testScope() Scope {
	return Scope{Query: "retrieval augmented generation", From: "2023-01-01", Through: "2026-08-31",
		PageSize: 2, WorkType: "journal-article"}
}
func testObserved() time.Time { return time.Date(2026, 9, 8, 0, 0, 0, 0, time.UTC) }
func crItem(id, date string) string {
	return fmt.Sprintf(`{"DOI":"10.1234/%s","title":["Retrieval augmented generation %s"],`+
		`"type":"journal-article","published":{"date-parts":%s}}`, id, id, date)
}
func crPage(next string, items ...string) string {
	return fmt.Sprintf(`{"status":"ok","message":{"total-results":12,"next-cursor":%q,"items":[%s]}}`,
		next, strings.Join(items, ","))
}

func fakeClient(t *testing.T, handler http.HandlerFunc) *Crossref {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	c := NewCrossref("")
	c.papers = nil
	c.base = server.URL
	now := testObserved()
	c.fetcher.now = func() time.Time { return now }
	c.fetcher.wait = func(ctx context.Context, d time.Duration) error { now = now.Add(d); return ctx.Err() }
	return c
}

func TestCrossrefPagesAndPrecision(t *testing.T) {
	requests := 0
	c := fakeClient(t, func(w http.ResponseWriter, r *http.Request) {
		requests++
		if r.URL.Query().Get("sort") != "score" || !strings.Contains(r.URL.Query().Get("filter"), "until-pub-date:2026-08-31") {
			t.Error("missing explicit query ordering/bounds")
		}
		if requests == 1 {
			fmt.Fprint(w, crPage("second", crItem("a", "[[2025]]"), crItem("b", "[]")))
		} else {
			if r.URL.Query().Get("cursor") != "second" {
				t.Error("lost checkpoint")
			}
			fmt.Fprint(w, crPage("", crItem("c", "[[2025,2,28]]")))
		}
	})
	var saved []Page
	result, err := Collect(t.Context(), c, testScope(), 6, func(p Page) error { saved = append(saved, p); return nil })
	if err != nil || result.Status != "search_exhausted" || result.Unique != 3 || len(saved) != 2 {
		t.Fatalf("%+v, %v", result, err)
	}
	if d := saved[0].Records[0].Source.Publication; d.Precision != "year" || d.Value != "2025" {
		t.Fatalf("invented publication precision: %+v", d)
	}
	unknown := saved[0].Records[1]
	if unknown.Source.Publication != nil || unknown.TextStatus != "title_only" {
		t.Fatalf("invented unavailable fields: %+v", unknown)
	}
	if saved[1].Observed.Sub(saved[0].Observed) < time.Second {
		t.Error("limiter did not span pages")
	}
	for _, p := range saved {
		for _, r := range p.Records {
			if r.Source.ContentHash != digest([]byte(r.Text)) || len(r.Raw) == 0 || p.RawHash != digest(p.Raw) {
				t.Error("source/hash mismatch")
			}
		}
	}
}

func TestCrossrefRejectsBadData(t *testing.T) {
	for _, tc := range []struct{ name, item string }{
		{"impossible date", crItem("bad", "[[2025,2,30]]")},
		{"future", crItem("future", "[[2026,9,1]]")},
		{"straddling year", crItem("year", "[[2026]]")},
		{"wrong kind", strings.ReplaceAll(crItem("kind", "[[2025]]"), "journal-article", "book")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p, err := decodeCrossref([]byte(crPage("", tc.item)), testObserved(), testScope(), "*")
			if err != nil || len(p.Records) != 0 || len(p.Rejected) != 1 {
				t.Fatalf("bad record accepted/lost: %+v, %v", p, err)
			}
		})
	}
	for _, raw := range []string{`{`, `{"status":"ok","message":{}}`, crPage("", crItem("a", "[[2025]]"), crItem("b", "[[2025]]"))} {
		if _, err := decodeCrossref([]byte(raw), testObserved(), testScope(), "*"); err == nil {
			t.Error("malformed page accepted")
		}
	}
}

func TestCrossrefFinalFullPageWithoutCursor(t *testing.T) {
	raw := []byte(crPage("", crItem("a", "[[2025]]"), crItem("b", "[[2025]]")))
	page, err := decodeCrossref(raw, testObserved(), testScope(), "last-cursor")
	if err != nil || !page.Exhausted || page.Next != "" || len(page.Records) != 2 {
		t.Fatal("final full-sized Crossref page was not checkpointable", err)
	}
}

func TestCrossrefRetryAndLimits(t *testing.T) {
	for _, status := range []int{429, 503, 400} {
		t.Run(strconv.Itoa(status), func(t *testing.T) {
			requests := 0
			c := fakeClient(t, func(w http.ResponseWriter, _ *http.Request) { requests++; w.WriteHeader(status) })
			_, err := c.Fetch(t.Context(), testScope(), "")
			want := 3
			if status == 400 {
				want = 1
			}
			if err == nil || requests != want {
				t.Fatalf("requests=%d err=%v", requests, err)
			}
		})
	}
	t.Run("server cooldown is preserved", func(t *testing.T) {
		c := fakeClient(t, func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Retry-After", "120")
			w.WriteHeader(http.StatusTooManyRequests)
		})
		_, err := c.Fetch(t.Context(), testScope(), "")
		httpErr, ok := errors.AsType[*HTTPError](err)
		if !ok || httpErr.RetryAt.Sub(testObserved()) != 120*time.Second {
			t.Fatalf("lost cooldown: %v", err)
		}
	})
	t.Run("oversized response", func(t *testing.T) {
		c := fakeClient(t, func(w http.ResponseWriter, _ *http.Request) { fmt.Fprint(w, strings.Repeat("x", maxResponseBytes+1)) })
		if _, err := c.Fetch(t.Context(), testScope(), ""); err == nil {
			t.Error("oversize accepted")
		}
	})
	t.Run("no redirect", func(t *testing.T) {
		c := fakeClient(t, func(w http.ResponseWriter, r *http.Request) {
			http.Redirect(w, r, "http://127.0.0.1/private", http.StatusFound)
		})
		if _, err := c.Fetch(t.Context(), testScope(), ""); err == nil {
			t.Error("redirect followed")
		}
	})
}

func TestCollectionCheckpointAndReplay(t *testing.T) {
	for _, failSink := range []bool{false, true} {
		t.Run(strconv.FormatBool(failSink), func(t *testing.T) {
			calls, saves := 0, 0
			c := fakeClient(t, func(w http.ResponseWriter, _ *http.Request) {
				calls++
				fmt.Fprint(w, crPage("next", crItem("a", "[[2025]]"), crItem("b", "[[2025]]")))
			})
			r, err := Collect(t.Context(), c, testScope(), 6, func(Page) error {
				saves++
				if failSink {
					return errors.New("disk full")
				}
				return nil
			})
			if err == nil || saves != 1 {
				t.Fatalf("replay/failure accepted: %+v, %v", r, err)
			}
			if failSink && (r.Cursor != "" || calls != 1) {
				t.Error("cursor advanced past failed checkpoint")
			}
		})
	}
	c := fakeClient(t, func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprint(w, crPage("next", crItem("a", "[[2025]]"), crItem("b", "[[2025]]")))
	})
	r, err := Collect(t.Context(), c, testScope(), 2, func(Page) error { return nil })
	if err != nil || r.Status != "budget_exhausted" || r.Received != 2 {
		t.Fatalf("budget mistaken for full coverage: %+v, %v", r, err)
	}
	if _, err := Collect(t.Context(), c, testScope(), 201, func(Page) error {
		t.Error("persist called for over-budget run")
		return nil
	}); err == nil {
		t.Error("budget bypass")
	}
}

func TestRawMetadataRoundTrip(t *testing.T) {
	item := strings.Replace(crItem("jats", "[[2025,3]]"), `"type":`,
		`"abstract":"<jats:p>Abstract A &amp; B. &lt;script&gt; is source text.</jats:p>","type":`, 1)
	p, err := decodeCrossref([]byte(crPage("", item)), testObserved(), testScope(), "*")
	if err != nil || !strings.Contains(p.Records[0].Text, "<script>") {
		t.Fatalf("text mapping: %+v %v", p, err)
	}
	raw, err := json.Marshal(p)
	if err != nil {
		t.Fatal(err)
	}
	var restored Page
	if err := json.Unmarshal(raw, &restored); err != nil {
		t.Fatal(err)
	}
	if string(restored.Raw) != string(p.Raw) || restored.Records[0].Source.Publication.Precision != "month" {
		t.Error("lost raw response/precision")
	}
}

func TestExpiredCursorRewindsOnlyContinuation(t *testing.T) {
	c := fakeClient(t, func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusBadRequest) })
	if _, err := c.Fetch(t.Context(), testScope(), "expired"); !errors.Is(err, ErrCursorExpired) {
		t.Fatalf("missing rewind classification: %v", err)
	}
	if _, err := c.Fetch(t.Context(), testScope(), ""); errors.Is(err, ErrCursorExpired) {
		t.Fatal("invalid initial query caused an infinite rewind")
	}
}

func TestSuccessfulResponseWithExhaustedQuotaDoesNotSleepThroughLease(t *testing.T) {
	requests := 0
	c := fakeClient(t, func(w http.ResponseWriter, _ *http.Request) {
		requests++
		w.Header().Set("X-Ratelimit-Remaining", "0")
		w.Header().Set("X-Ratelimit-Reset", strconv.FormatInt(testObserved().Add(time.Hour).Unix(), 10))
		fmt.Fprint(w, crPage("next", crItem("one", "[[2024]]")))
	})
	if _, err := c.Fetch(t.Context(), testScope(), ""); err != nil {
		t.Fatal(err)
	}
	_, err := c.Fetch(t.Context(), testScope(), "next")
	limit, ok := errors.AsType[*HTTPError](err)
	if !ok || limit.Status != http.StatusTooManyRequests || requests != 1 {
		t.Fatalf("exhausted quota was retried or silently slept through: %d %v", requests, err)
	}
}
