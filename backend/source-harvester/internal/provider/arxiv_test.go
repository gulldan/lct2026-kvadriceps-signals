package provider

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"
)

func atomFeed(start, total int, entries ...string) string {
	return fmt.Sprintf(`<feed xmlns="http://www.w3.org/2005/Atom" xmlns:op="http://a9.com/-/spec/opensearch/1.1/"
xmlns:arxiv="http://arxiv.org/schemas/atom"><op:totalResults>%d</op:totalResults><op:startIndex>%d</op:startIndex>%s</feed>`,
		total, start, strings.Join(entries, ""))
}

func atomWork(id, first, updated string) string {
	return fmt.Sprintf(`<entry><id>http://arxiv.org/abs/%s</id><title>RAG method</title><summary>Measured evidence.</summary>
<published>%sT00:00:00Z</published><updated>%sT00:00:00Z</updated><author><name>A. Researcher</name></author>
<category term="cs.CL"/><arxiv:doi>10.1234/method</arxiv:doi></entry>`, id, first, updated)
}

func TestArxivCategoryIsAppliedAndRecorded(t *testing.T) {
	scope := testScope()
	scope.Category = "cs.IR"
	scope.PageSize = 1
	query := `cat:cs.IR AND all:"retrieval augmented generation" AND submittedDate:[202301010000 TO 202608312359]`
	entry := strings.ReplaceAll(atomWork("2501.00001v1", "2025-01-01", "2025-01-01"), "cs.CL", "cs.IR")
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.URL.Query().Get("search_query") != query || r.URL.Query().Get("start") != "0" ||
			r.URL.Query().Get("sortBy") != "submittedDate" || r.URL.Query().Get("sortOrder") != "ascending" {
			t.Error("category, interval or stable ordering missing from provider request")
		}
		fmt.Fprint(w, atomFeed(0, 1, entry))
	}))
	t.Cleanup(server.Close)
	a := NewArXiv()
	a.base = server.URL
	p, err := a.Fetch(t.Context(), scope, "")
	if err != nil || len(p.Records) != 1 || p.Scope.Category != "cs.IR" || !p.Exhausted || p.RawHash != digest(p.Raw) {
		t.Fatalf("filtered page lacks exact scope or evidence: %+v, %v", p, err)
	}
	scope.Category = "cs.IR OR all:biology"
	if _, err = a.Fetch(t.Context(), scope, ""); err == nil || calls != 1 {
		t.Fatal("invalid category reached the provider")
	}
	scope.Category = "cs.CL"
	p, err = decodeArxiv([]byte(atomFeed(0, 1, entry)), testObserved(), scope, 0)
	if err != nil || len(p.Records) != 0 || len(p.Rejected) != 1 {
		t.Fatal("a mismatched category was silently included")
	}
}

func TestOtherProvidersRejectCategoryBeforeRequest(t *testing.T) {
	scope := testScope()
	scope.Category = "cs.IR"
	for _, name := range []string{"crossref", "github", "patent", "web"} {
		if err := scope.ValidateFor(name); err == nil {
			t.Fatalf("%s silently ignores the arXiv filter", name)
		}
	}
}

func TestArxivVersionsAndCutoff(t *testing.T) {
	scope := testScope()
	scope.From = "1999-01-01"
	scope.PageSize = 3
	raw := atomFeed(0, 3, atomWork("2501.00001v1", "2025-01-01", "2025-01-01"),
		atomWork("2501.00001v2", "2025-01-01", "2025-02-02"), atomWork("hep-th/9901001v1", "1999-01-01", "1999-01-01"))
	p, err := decodeArxiv([]byte(raw), testObserved(), scope, 0)
	if err != nil || len(p.Records) != 3 {
		t.Fatalf("versions rejected: %+v %v", p, err)
	}
	one, two := p.Records[0].Source, p.Records[1].Source
	if one.CanonicalID != two.CanonicalID || one.Revision == two.Revision ||
		two.ArXiv.FirstPublished.Equal(two.ArXiv.VersionUpdated) {
		t.Error("version/date identity lost")
	}
	if p.Records[2].Source.NativeID != "hep-th/9901001" || !strings.HasSuffix(p.Records[2].ContentURL, "hep-th/9901001v1") {
		t.Error("legacy ID or versioned PDF URL lost")
	}
	future := atomFeed(0, 1, atomWork("2501.00001v3", "2025-01-01", "2026-09-01"))
	p, err = decodeArxiv([]byte(future), testObserved(), scope, 0)
	if err != nil || len(p.Records) != 0 || len(p.Rejected) != 1 || !strings.Contains(p.Rejected[0].Reason, "cutoff") {
		t.Fatalf("future version counted: %+v %v", p, err)
	}
}

func TestArxivRejectsDTDAndBrokenPaging(t *testing.T) {
	entry := atomWork("2501.00001v1", "2025-01-01", "2025-01-01")
	for _, raw := range []string{
		`<!DOCTYPE feed [<!ENTITY x SYSTEM "http://127.0.0.1/private">]>` + atomFeed(0, 1, entry),
		`<!ENTITY x "entity">` + atomFeed(0, 1, entry), "<feed>", atomFeed(0, 5, entry), atomFeed(2, 3, entry),
		strings.ReplaceAll(atomFeed(0, 1, entry), "http://www.w3.org/2005/Atom", "https://wrong.example"),
	} {
		if _, err := decodeArxiv([]byte(raw), testObserved(), testScope(), 0); err == nil {
			t.Error("unsafe/broken Atom accepted")
		}
	}
	raw := strings.ReplaceAll(atomFeed(0, 1, entry), "<summary>Measured evidence.</summary>", "")
	p, err := decodeArxiv([]byte(raw), testObserved(), testScope(), 0)
	if err != nil || p.Records[0].TextStatus != "title_only" {
		t.Fatalf("empty abstract fabricated: %+v %v", p, err)
	}
}

func TestArxivLimiterAcrossScopesAndRetries(t *testing.T) {
	now := testObserved()
	var starts []time.Time
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		starts = append(starts, now)
		if r.Header.Get("Accept") != "*/*" ||
			!strings.Contains(r.URL.Query().Get("search_query"), "submittedDate:[202301010000 TO 202608312359]") {
			t.Error("missing protocol/scope")
		}
		if len(starts) == 1 {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		fmt.Fprint(w, atomFeed(0, 0))
	}))
	t.Cleanup(server.Close)
	a := NewArXiv()
	a.base = server.URL
	a.fetcher.now = func() time.Time { return now }
	a.fetcher.wait = func(ctx context.Context, d time.Duration) error { now = now.Add(d); return ctx.Err() }
	for _, query := range []string{"retrieval augmented generation", "machine learning"} {
		scope := testScope()
		scope.Query = query
		if _, err := a.Fetch(t.Context(), scope, ""); err != nil {
			t.Fatal(err)
		}
	}
	if len(starts) != 3 {
		t.Fatalf("calls=%d", len(starts))
	}
	for i := 1; i < len(starts); i++ {
		if starts[i].Sub(starts[i-1]) < 3*time.Second {
			t.Error("provider limit bypassed by retry/scope")
		}
	}
}

func TestRecordedCrossrefPages(t *testing.T) {
	for _, name := range []string{"page-1.json", "page-2.json"} {
		raw, err := os.ReadFile("testdata/crossref/" + name)
		if err != nil {
			t.Fatal(err)
		}
		p, err := decodeCrossref(raw, testObserved(), testScope(), "*")
		if err != nil || len(p.Records) != 2 || len(p.Rejected) != 0 {
			t.Fatalf("recorded API fixture: %+v %v", p, err)
		}
		date := p.Records[1].Source.Publication
		if name == "page-1.json" && (date.Value != "2025-01" || date.Precision != "month") {
			t.Error("recorded month became a day")
		}
	}
}

func TestArxivPDFMirrorAndPhraseGroups(t *testing.T) {
	mirror := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/2608/2608.15087v1.pdf" {
			http.NotFound(w, r)
			return
		}
		fmt.Fprint(w, "%PDF-mirror")
	}))
	t.Cleanup(mirror.Close)
	a := NewArXiv()
	a.mirror = mirror.URL
	direct := 0
	a.fetcher.http.Transport = responseFunc(func(r *http.Request) (*http.Response, error) {
		direct++
		return &http.Response{StatusCode: http.StatusOK, Header: http.Header{}, Request: r,
			Body: io.NopCloser(strings.NewReader("%PDF-arxiv"))}, nil
	})
	for id, want := range map[string]string{"2608.15087v1": "%PDF-mirror", "2609.00001v1": "%PDF-arxiv"} {
		p, err := decodeArxiv([]byte(atomFeed(0, 1, atomWork(id, "2026-08-01", "2026-08-01"))), testObserved(),
			Scope{Query: "retrieval augmented generation", From: "2026-01-01", Through: "2026-09-30", PageSize: 1}, 0)
		if err != nil || len(p.Records) != 1 {
			t.Fatalf("%s: %v %+v", id, err, p.Rejected)
		}
		raw, err := a.PDF(t.Context(), p.Records[0].Source)
		if err != nil || string(raw) != want {
			t.Fatalf("%s: got %q, %v", id, raw, err)
		}
	}
	if direct != 1 {
		t.Fatalf("arxiv.org requests: %d, want only the file missing from the mirror", direct)
	}
	if got := atomPhrases("edge computing OR tinyML"); got != `(all:"edge computing" OR all:"tinyML")` {
		t.Fatalf("grouped phrases: %s", got)
	}
	if got := atomPhrases("tinyML"); got != `all:"tinyML"` {
		t.Fatalf("single phrase changed: %s", got)
	}
}
