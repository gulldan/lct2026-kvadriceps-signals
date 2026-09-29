package application

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
	"time"
)

func oaiTestRecord(id, created, updated string) string {
	return fmt.Sprintf(`<record><header><identifier>oai:arXiv.org:%s</identifier>`+
		`<datestamp>2026-09-11</datestamp></header><metadata>`+
		`<arXiv xmlns="http://arxiv.org/OAI/arXiv/"><id>%s</id><created>%s</created><updated>%s</updated>`+
		`<authors><author><keyname>Ivanova</keyname><forenames>A. B.</forenames></author></authors>`+
		`<title>Paper %s</title><categories>cs.CL cs.AI</categories><doi>10.1/%s</doi>`+
		`<abstract>  Abstract  of %s  </abstract></arXiv></metadata></record>`, id, id, created, updated, id, id, id)
}

func TestArxivHarvestFiltersByCreatedAndFollowsToken(t *testing.T) {
	var requests []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		requests = append(requests, q.Get("set")+"|"+q.Get("from")+"|"+q.Get("resumptionToken"))
		head := `<OAI-PMH xmlns="http://www.openarchives.org/OAI/2.0/"><ListRecords>`
		switch {
		case q.Get("resumptionToken") == "more%3Dyes":
			fmt.Fprint(w, head+oaiTestRecord("2609.00002", "2026-09-02", "")+`</ListRecords></OAI-PMH>`)
		case q.Get("set") == "cs":
			fmt.Fprint(w, head+oaiTestRecord("2609.00001", "2026-09-01", "2026-09-11")+
				// подана в 2013, поправлена сейчас: в ряд попасть не должна
				oaiTestRecord("1303.2033", "2013-03-08", "2026-09-11")+
				// 2112.04169: v1 от 08.12.2021, а created указывает на третью версию от 09.09.2026.
				oaiTestRecord("2112.04169", "2026-09-09", "2026-09-10")+
				`<record><header status="deleted"><identifier>oai:arXiv.org:2609.09999</identifier></header></record>`+
				`<resumptionToken>more%3Dyes</resumptionToken></ListRecords></OAI-PMH>`)
		default:
			fmt.Fprint(w, head+`</ListRecords></OAI-PMH>`)
		}
	}))
	defer server.Close()

	src := newLiveSources(func(context.Context) string { return "" })
	src.oai, src.sleep = server.URL, func(context.Context, time.Duration) error { return nil }
	periods := livePeriods(time.Date(2026, 9, 13, 0, 0, 0, 0, time.UTC))
	papers, err := src.arxivHarvest(t.Context(), periods, liveDay(periods[0].Start), time.Date(2026, 9, 13, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("harvest: %v", err)
	}
	ids := make([]string, 0, len(papers))
	for _, paper := range papers {
		ids = append(ids, paper.ID)
	}
	slices.Sort(ids)
	if !slices.Equal(ids, []string{"arxiv:2609.00001", "arxiv:2609.00002"}) {
		t.Fatalf("filtered by created and status: %v", ids)
	}
	if papers[0].Date != "2026-09-01" || papers[0].Authors[0] != "A. B. Ivanova" {
		t.Fatalf("created date and author name: %+v", papers[0])
	}
	meta := src.metadata["arxiv:2609.00001"]
	if meta.Abstract != "Abstract of 2609.00001" || meta.Category != "cs.CL" || meta.AvailableAt != "2026-09-11" {
		t.Fatalf("metadata: %+v", meta)
	}
	if len(requests) != len(liveOAISets())+1 || !strings.Contains(requests[0], "cs|2025-08-01|") {
		t.Fatalf("one pass per set plus one token page: %v", requests)
	}
}

func TestArxivSubmittedPrefersIdentifierMonth(t *testing.T) {
	cases := [][3]string{
		{"2602.00001", "2026-09-01", "2026-02-01"},
		{"2112.04169", "2026-09-09", "2021-12-01"},
		{"2509.12345", "2025-09-17", "2025-09-17"},
		{"cs/0701001", "2026-09-09", ""},
		{"1303.2033", "2013-03-08", "2013-03-08"},
	}
	for _, c := range cases {
		if got := liveArxivSubmitted(c[0], c[1]); got != c[2] {
			t.Fatalf("%s + created %s: want %q, got %q", c[0], c[1], c[2], got)
		}
	}
}

func TestOAIPageBacksOffOnThrottle(t *testing.T) {
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls++
		if calls == 1 {
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		fmt.Fprint(w, `<OAI-PMH><ListRecords>`+oaiTestRecord("2609.00003", "2026-09-03", "")+
			`</ListRecords></OAI-PMH>`)
	}))
	defer server.Close()

	waits := []time.Duration{}
	src := newLiveSources(func(context.Context) string { return "" })
	src.oai = server.URL
	src.sleep = func(_ context.Context, d time.Duration) error { waits = append(waits, d); return nil }
	out, err := src.oaiPage(t.Context(), nil)
	if err != nil || len(out.Records) != 1 || calls != 2 {
		t.Fatalf("retry after throttle: %v %d", err, calls)
	}
	if len(waits) != 2 || waits[0] != liveOAIPause || waits[1] != liveOAIBackoff {
		t.Fatalf("three seconds politely, then a minute of backoff: %v", waits)
	}
}

func TestOAIPageRejectsProtocolError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprint(w, `<OAI-PMH><error code="badArgument">bad from</error></OAI-PMH>`)
	}))
	defer server.Close()
	src := newLiveSources(func(context.Context) string { return "" })
	src.oai, src.sleep = server.URL, func(context.Context, time.Duration) error { return nil }
	if _, err := src.oaiPage(t.Context(), nil); err == nil || !strings.Contains(err.Error(), "badArgument") {
		t.Fatalf("protocol error must surface: %v", err)
	}
}

type memoryTestStore map[string][]byte

func (m memoryTestStore) GetBytes(_ context.Context, key string) ([]byte, error) {
	raw, ok := m[key]
	if !ok {
		return nil, fmt.Errorf("missing %s", key)
	}
	return raw, nil
}

func (m memoryTestStore) Put(_ context.Context, key string, r io.Reader, _ int64, _ string) error {
	raw, err := io.ReadAll(r)
	m[key] = raw
	return err
}

func TestLiveWindowResumesFromCursor(t *testing.T) {
	from := ""
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		from = r.URL.Query().Get("from")
		body := ""
		if r.URL.Query().Get("set") == "cs" && from != "2025-08-01" {
			body = oaiTestRecord("2609.00002", "2026-09-12", "")
		} else if r.URL.Query().Get("set") == "cs" {
			body = oaiTestRecord("2609.00001", "2026-09-01", "")
		}
		fmt.Fprint(w, `<OAI-PMH><ListRecords>`+body+`</ListRecords></OAI-PMH>`)
	}))
	defer server.Close()

	store := memoryTestStore{}
	service := NewTrendLiveService(store, func(context.Context) string { return "" })
	periods := livePeriods(time.Date(2026, 9, 13, 0, 0, 0, 0, time.UTC))
	newSource := func() *liveSources {
		src := newLiveSources(func(context.Context) string { return "" })
		src.oai, src.sleep = server.URL, func(context.Context, time.Duration) error { return nil }
		return src
	}

	first, _, err := service.liveWindow(t.Context(), newSource(), periods)
	if err != nil || len(first) != 1 || from != "2025-08-01" {
		t.Fatalf("first pass enumerates the window: %v %d %s", err, len(first), from)
	}

	second := newSource()
	papers, population, err := service.liveWindow(t.Context(), second, periods)
	if err != nil {
		t.Fatalf("second pass: %v", err)
	}
	// Курсор — последний охваченный день, а не дата записи: сутки
	// перекрытия дешевле пропущенной ревизии.
	if from != "2026-09-13" {
		t.Fatalf("second pass must resume from the stored cursor, asked from %s", from)
	}
	ids := make([]string, 0, len(papers))
	for _, paper := range papers {
		ids = append(ids, paper.ID)
	}
	slices.Sort(ids)
	if !slices.Equal(ids, []string{"arxiv:2609.00001", "arxiv:2609.00002"}) {
		t.Fatalf("cached papers must survive the top-up: %v", ids)
	}
	if second.metadata["arxiv:2609.00001"].Abstract != "Abstract of 2609.00001" {
		t.Fatalf("carried papers keep their abstract: %+v", second.metadata["arxiv:2609.00001"])
	}
	if population[len(population)-1] != 2 {
		t.Fatalf("population counts what was actually enumerated: %v", population)
	}
	// Цикл дальше пишет в src.metadata, а поиск читает окно из HTTP-горутин.
	clear(second.metadata)
	found, err := service.SearchWindow(t.Context(), LiveWindowSearch{Query: "abstract", Periods: periods,
		Plan: new(liveFixtureQueryPlan("abstract", []string{"abstract"}))})
	if err != nil || found.Metadata["arxiv:2609.00001"].Abstract == "" {
		t.Fatalf("window search must not share the cycle's metadata: %v %+v", err, found)
	}
}

// Тихая ночь без новых записей — обычный случай, а карта аннотаций при этом ещё nil.
func TestLiveWindowCarriesCacheWhenNothingIsNew(t *testing.T) {
	empty := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body := ""
		if empty == 0 && r.URL.Query().Get("set") == "cs" {
			body = oaiTestRecord("2609.00001", "2026-09-01", "")
		}
		fmt.Fprint(w, `<OAI-PMH><ListRecords>`+body+`</ListRecords></OAI-PMH>`)
	}))
	defer server.Close()

	store := memoryTestStore{}
	service := NewTrendLiveService(store, func(context.Context) string { return "" })
	periods := livePeriods(time.Date(2026, 9, 13, 0, 0, 0, 0, time.UTC))
	newSource := func() *liveSources {
		src := newLiveSources(func(context.Context) string { return "" })
		src.oai, src.sleep = server.URL, func(context.Context, time.Duration) error { return nil }
		return src
	}
	if _, _, err := service.liveWindow(t.Context(), newSource(), periods); err != nil {
		t.Fatalf("first pass: %v", err)
	}
	empty = 1
	quiet := newSource()
	papers, population, err := service.liveWindow(t.Context(), quiet, periods)
	if err != nil || len(papers) != 1 || papers[0].ID != "arxiv:2609.00001" {
		t.Fatalf("cached window must survive a night with no new records: %v %+v", err, papers)
	}
	if quiet.metadata["arxiv:2609.00001"].Abstract != "Abstract of 2609.00001" {
		t.Fatalf("carried metadata: %+v", quiet.metadata)
	}
	if population[len(population)-1] != 1 {
		t.Fatalf("population: %v", population)
	}
}
