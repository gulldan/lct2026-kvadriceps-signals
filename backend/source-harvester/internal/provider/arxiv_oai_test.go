package provider

import (
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"
)

func arxivOAIFeed(token string, entries ...string) string {
	return `<OAI-PMH xmlns="http://www.openarchives.org/OAI/2.0/"><ListRecords>` + strings.Join(entries, "") +
		`<resumptionToken>` + token + `</resumptionToken></ListRecords></OAI-PMH>`
}

func arxivOAIWork(id, published, updated, category string) string {
	first, _ := time.Parse(time.DateOnly, published)
	latest, _ := time.Parse(time.DateOnly, updated)
	versions := `<version version="v1"><date>` + first.Format(http.TimeFormat) + `</date></version>`
	if updated != published {
		versions += `<version version="v2"><date>` + latest.Format(http.TimeFormat) + `</date></version>`
	}
	return `<record><header><identifier>oai:arXiv.org:` + id + `</identifier><datestamp>2026-08-21</datestamp></header>` +
		`<metadata><arXivRaw xmlns="http://arxiv.org/OAI/arXivRaw/"><id>` + id + `</id>` + versions +
		`<title>Retrieval-Augmented Generation 新しい方法</title><authors>张伟, A. Researcher</authors>` +
		`<abstract>Measured evidence for retrieval augmented generation.</abstract><categories>` + category +
		`</categories><license>http://arxiv.org/licenses/nonexclusive-distrib/1.0/</license></arXivRaw></metadata></record>`
}

func TestArxivOAIPagingAndRestart(t *testing.T) {
	scope := testScope()
	scope.PageSize, scope.Category = 1, "cs.CL"
	calls := 0
	changed := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		q := r.URL.Query()
		if r.Header.Get("Accept") != "*/*" || q.Get("verb") != "ListRecords" {
			t.Error("missing OAI protocol or accept header")
		}
		if q.Get("resumptionToken") != "" {
			if q.Get("resumptionToken") != "opaque%token=2" || len(q) != 2 {
				t.Error("resumption token was decoded twice or mixed with original parameters")
			}
			fmt.Fprint(w, arxivOAIFeed("", arxivOAIWork("2501.00004", "2025-01-01", "2025-01-01", "cs.CL")))
			return
		}
		if q.Get("from") != scope.From || q.Get("until") != scope.Through || q.Get("set") != "cs:cs:CL" ||
			q.Get("metadataPrefix") != "arXivRaw" {
			t.Error("scope or versioned metadata format was lost")
		}
		var items []string
		for i := range 3 {
			items = append(items, arxivOAIWork("2501.0000"+strconv.Itoa(i+1), "2025-01-01", "2026-08-20", "cs.CL"))
		}
		if changed {
			items[0] = strings.ReplaceAll(items[0], "Measured evidence", "Changed evidence")
		}
		fmt.Fprint(w, arxivOAIFeed("opaque%token=2", items...))
	}))
	t.Cleanup(server.Close)
	client := func() *ArXiv {
		a := NewArXiv()
		a.base = server.URL + "/oai"
		a.fetcher.interval = 0
		a.fetcher.now = testObserved
		return a
	}
	a := client()
	one, err := a.Fetch(t.Context(), scope, "")
	if err != nil || len(one.Records) != 1 || one.Exhausted {
		t.Fatalf("first OAI slice: %+v %v", one.Rejected, err)
	}
	two, err := a.Fetch(t.Context(), scope, one.Next)
	if err != nil || len(two.Records) != 1 || calls != 1 || one.RawHash == two.RawHash {
		t.Fatalf("bounded slices re-fetched, lost evidence or replayed the same page: %v calls=%d", err, calls)
	}
	record := one.Records[0]
	if record.Source.Revision != "2501.00001v2" || record.Source.Publication.Value != "2025-01-01" ||
		record.Source.RevisionPublication.Value != "2026-08-20" ||
		record.Source.AvailableAt.Format(time.DateOnly) != "2026-08-21" || !strings.Contains(string(record.Raw), "张伟") ||
		!strings.Contains(record.Text, "新しい方法") {
		t.Fatal("version, publication, metadata observation or original language was lost")
	}
	a = client() // Перезапуск обязан продолжить с точного сохранённого смещения записи.
	three, err := a.Fetch(t.Context(), scope, two.Next)
	if err != nil || len(three.Records) != 1 || calls != 2 || three.Records[0].Source.NativeID != "2501.00003" {
		t.Fatalf("restart skipped or duplicated a record: %v", err)
	}
	four, err := a.Fetch(t.Context(), scope, three.Next)
	if err != nil || !four.Exhausted || len(four.Records) != 1 || four.Records[0].Source.NativeID != "2501.00004" {
		t.Fatalf("next upstream page not consumed: %v", err)
	}
	changed = true
	if _, err = client().Fetch(t.Context(), scope, one.Next); !errors.Is(err, ErrCursorExpired) {
		t.Fatalf("changed OAI page silently skipped a prefix: %v", err)
	}
	previousCalls := calls
	for range 2 {
		if _, err = a.Fetch(t.Context(), scope, ""); err != nil {
			t.Fatal(err)
		}
	}
	if calls != previousCalls+2 {
		t.Fatal("a new overlap sweep reused an indefinitely cached first OAI page")
	}
	if _, err = a.Fetch(t.Context(), scope, "50"); !errors.Is(err, ErrCursorExpired) {
		t.Fatal("saved Atom offset used as an OAI cursor")
	}
}

func TestArxivOAIRejectsOutOfScopeAndMissingVersions(t *testing.T) {
	scope := testScope()
	scope.Category, scope.PageSize = "cs.CL", 5
	valid := arxivOAIWork("2501.00001", "2025-01-01", "2025-01-01", "cs.CL")
	items := []string{
		arxivOAIWork("2005.11401", "2020-05-22", "2026-08-20", "cs.CL"),
		arxivOAIWork("2501.00002", "2025-01-01", "2026-09-01", "cs.CL"),
		strings.ReplaceAll(valid, `version="v1"`, `version="v2"`),
		arxivOAIWork("2501.00003", "2025-01-01", "2025-01-01", "physics.optics"),
		strings.ReplaceAll(valid, "2026-08-21", "2026-09-01"),
	}
	response, err := decodeArxivOAI([]byte(arxivOAIFeed("", items...)))
	if err != nil {
		t.Fatal(err)
	}
	cache := arxivOAICache{Raw: []byte(arxivOAIFeed("", items...)), Response: response, Observed: testObserved()}
	page, err := cache.page(scope, "", arxivOAICursor{})
	if err != nil || !page.Exhausted || page.Received != 5 || len(page.Rejected) != 5 || len(page.Records) != 0 ||
		len(page.Limitations) == 0 {
		t.Fatalf("old publication, future metadata/version or invalid history accepted: %+v %v", page.Rejected, err)
	}
}

func TestArxivOAIProtocolAndQuery(t *testing.T) {
	for _, raw := range []string{
		`<!DOCTYPE feed [<!ENTITY x SYSTEM "http://127.0.0.1/private">]>` + arxivOAIFeed(""),
		`<OAI-PMH xmlns="http://wrong.example"><ListRecords/></OAI-PMH>`,
		`<OAI-PMH xmlns="http://www.openarchives.org/OAI/2.0/"><error code="badArgument"/></OAI-PMH>`,
		arxivOAIFeed("more"),
	} {
		if _, err := decodeArxivOAI([]byte(raw)); err == nil {
			t.Fatal("broken OAI envelope was treated as empty data")
		}
	}
	_, err := decodeArxivOAI([]byte(`<OAI-PMH xmlns="http://www.openarchives.org/OAI/2.0/">` +
		`<error code="badResumptionToken"/></OAI-PMH>`))
	if !errors.Is(err, ErrCursorExpired) {
		t.Fatal("expired token did not request a safe overlapping restart")
	}
	response, err := decodeArxivOAI([]byte(`<OAI-PMH xmlns="http://www.openarchives.org/OAI/2.0/">` +
		`<error code="noRecordsMatch"/></OAI-PMH>`))
	if err != nil {
		t.Fatal(err)
	}
	cache := arxivOAICache{Response: response, Observed: testObserved(), Raw: []byte("empty window")}
	empty, err := cache.page(testScope(), "", arxivOAICursor{})
	if err != nil || !empty.Exhausted || empty.Received != 0 || len(empty.Limitations) == 0 {
		t.Fatal("empty OAI window was not distinguished from complete publication coverage")
	}
	scope := testScope()
	scope.Query = "cat:cs.AI OR cat:cs.CL OR cat:cs.LG"
	set, err := arxivOAIQuerySet(scope)
	if err != nil || set != "cs" || !arxivOAIMatches(scope.Query, "", []string{"cs.CL"}) ||
		arxivOAIMatches(scope.Query, "", []string{"math.NA"}) {
		t.Fatal("configured category OR query lost its semantics")
	}
	for _, query := range []string{"cat:", "cat:cs.AI AND cat:cs.CL", "cat:cs.AI OR all:biology", "!!!"} {
		scope.Query = query
		if _, err = arxivOAIQuerySet(scope); err == nil {
			t.Fatalf("invalid OAI query accepted: %s", query)
		}
	}
	if arxivOAISet("cond-mat.mtrl-sci") != "physics:cond-mat:mtrl-sci" ||
		arxivOAISet("hep-th") != "physics:hep-th" || arxivOAISet("q-bio.GN") != "q-bio:q-bio:GN" {
		t.Fatal("arXiv OAI subject set mapping is wrong")
	}
}
