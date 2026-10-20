package main

import (
	"encoding/json/v2"
	"io"
	"net/http"
	"net/url"
	"os"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestRepositoryRecordsDoNotInflateNovelty(t *testing.T) {
	for _, tc := range []struct{ kind, response, genre, day string }{
		{kindZenodo, `{"hits":{"total":2,"hits":[{"id":123,"doi":"10.5281/zenodo.123",
			"conceptdoi":"10.5281/zenodo.120","metadata":{"title":"Soil measurements",
			"description":"Measured soil moisture.","publication_date":"2026-09-10","resource_type":{"type":"dataset"}}},
			{"id":124,"metadata":{"title":"Poster","publication_date":"2026-09-10","resource_type":{"type":"poster"}}}]},
			"links":{"next":"https://untrusted.invalid/ignored"}}`, "dataset", "2026-09-10"},
		{kindFigshare, `[{"id":123,"doi":"10.1000/dataset","title":"Legacy soil measurements",
			"defined_type_name":"dataset","published_date":"2026-09-10T12:00:00Z","timeline":{"firstOnline":"2003-12-31"}},
			{"id":124,"title":"Uploaded old article","defined_type_name":"journal contribution",
			"published_date":"2026-09-10T12:00:00Z"}]`, "dataset", "2003-12-31"},
		{kindBioStudies, `{"totalHits":10000,"isTotalHitsExact":false,"nextCursor":"opaque","hits":[
			{"accession":"E-MTAB-12345","title":"Organoid experiment","release_date":"2026-09-10","isPublic":true},
			{"accession":"S-EPMC12345","title":"Mirror of an article","release_date":"2026-09-10","isPublic":true}]}`,
			"dataset", "2026-09-10"},
	} {
		t.Run(tc.kind, func(t *testing.T) {
			page, err := publicationRows([]byte(tc.response), tc.kind, "")
			if err != nil || len(page.rows) != 1 || page.rows[0].Genre != tc.genre || page.rows[0].Date != tc.day {
				t.Fatalf("repository type or publication date lost: %+v, %v", page, err)
			}
			if tc.kind != kindZenodo && page.total != nil {
				t.Fatal("unknown or approximate count was reported as exact")
			}
			if _, err := publicationRows([]byte(`{"error":"blocked"}`), tc.kind, ""); err == nil {
				t.Fatal("provider failure was reported as empty data")
			}
		})
	}
	rows := &snapshot{From: "2026-09-01", Through: "2026-09-30", Records: []record{
		{ID: "concept", DOI: "10.5281/zenodo.120", Date: "2026-09-10"},
		{ID: "version", DOI: "10.5281/zenodo.123", DOIAliases: []string{"10.5281/zenodo.120"}, Date: "2026-09-10"},
		{ID: "mirror", DOI: "10.5281/zenodo.123", Date: "2026-09-10"},
		{ID: "independent", DOI: "10.5281/zenodo.777", Date: "2026-09-10"},
	}}
	mergeRetained(rows, nil, time.Date(2026, 9, 30, 0, 0, 0, 0, time.UTC), func(record) bool { return true })
	if len(rows.Records) != 2 || rows.Records[1].ID != "independent" {
		t.Fatalf("versions counted as independent evidence: %+v", rows.Records)
	}
}

func TestPublicationSourcesPreserveEvidenceTypeAndPublicationDay(t *testing.T) {
	for _, tc := range []struct {
		kind, response, genre, date, next string
	}{
		{kindEuropePMC, `{"hitCount":3,"nextCursorMark":"page-two","resultList":{"result":[
			{"id":"12345","source":"MED","doi":"10.1000/PAPER","title":"Vaccine delivery",
			 "abstractText":"<p>Measured vaccine response.</p>","firstPublicationDate":"2026-09-10"},
			{"id":"undated","source":"MED","title":"No first publication day","pubYear":"2026"}]}}`,
			"article", "2026-09-10", "page-two"},
		{kindCrossref, `{"status":"ok","message":{"total-results":2,"next-cursor":"cursor","items":[
			{"DOI":"10.1000/PAPER","title":["Perovskite solar cells"],"abstract":"<jats:p>Measured stability.</jats:p>",
			 "type":"journal-article","published-online":{"date-parts":[[2026,9,10]]},
			 "created":{"date-parts":[[2026,10,10]]}},
			{"DOI":"10.1000/year","title":["Only a year"],"published":{"date-parts":[[2026]]}}]}}`,
			"article", "2026-09-10", ""},
		{kindClinicalTrials, `{"totalCount":2,"nextPageToken":"more","studies":[{"protocolSection":{
			"identificationModule":{"nctId":"NCT01234567","briefTitle":"Gene therapy trial"},
			"descriptionModule":{"briefSummary":"Trial registration description."},
			"statusModule":{"studyFirstPostDateStruct":{"date":"2026-09-10","type":"ACTUAL"},
			"startDateStruct":{"date":"2025-01-01"}}}},{"protocolSection":{
			"identificationModule":{"nctId":"NCT01234568","briefTitle":"Unposted trial"},
			"statusModule":{"studyFirstPostDateStruct":{"date":"2026-09-11","type":"ESTIMATED"}}}}]}`,
			"clinical_trial", "2026-09-10", "more"},
		{kindBiorxiv, `{"messages":[{"status":"ok","total":"4"}],"collection":[
			{"doi":"10.1000/PAPER","title":"Plant immunity","abstract":"Measured plant immunity.",
			 "date":"2026-09-10","version":"1"},
			{"doi":"10.1000/old","title":"Updated paper","date":"2026-09-11","version":"2"}]}`,
			"preprint", "2026-09-10", "2"},
		{kindDataCite, `{"meta":{"total":3},"links":{"next":"https://untrusted.invalid/not-followed"},"data":[
			{"attributes":{"doi":"10.1000/PAPER","titles":[{"title":"Ocean temperature dataset"}],
			 "descriptions":[{"description":"Ocean measurements."}],"dates":[{"dateType":"Issued","date":"2026-09-10"}],
			 "types":{"resourceTypeGeneral":"Dataset"}}},
			{"attributes":{"doi":"10.1000/year","titles":[{"title":"Only a year"}],
			 "dates":[{"dateType":"Issued","date":"2026"}],"registered":"2026-09-10T00:00:00Z",
			 "types":{"resourceTypeGeneral":"Dataset"}}}]}`,
			"dataset", "2026-09-10", "2"},
	} {
		t.Run(tc.kind, func(t *testing.T) {
			page, err := publicationRows([]byte(tc.response), tc.kind, "")
			if err != nil || len(page.rows) != 1 || page.next != tc.next {
				t.Fatalf("unexpected publication page: %+v, %v", page, err)
			}
			row := page.rows[0]
			if row.Genre != tc.genre || row.Date != tc.date || row.TextStatus != feedAbstractText ||
				row.Text == "" || row.DateBasis == "" || strings.Contains(row.Text, "<") {
				t.Fatalf("publication evidence was altered: %+v", row)
			}
			if tc.kind != kindClinicalTrials && (row.ID != "https://doi.org/10.1000/paper" || row.DOI != "10.1000/paper") {
				t.Fatalf("cross-provider DOI identity is unstable: %+v", row)
			}
			if _, err := publicationRows([]byte(`{"error":"temporarily unavailable"}`), tc.kind, ""); err == nil {
				t.Fatal("provider error was accepted as an empty successful archive")
			}
		})
	}
}

func TestPublicationHistoryPaginatesWithinDomainBudget(t *testing.T) {
	source := webSource{Name: "PubMed", URL: "https://example.org/search?query=vaccine", Kind: kindEuropePMC,
		Industry: "Медицина и здоровье", Pages: 2}
	calls := 0
	s := &service{kind: kindWeb, dir: t.TempDir(), config: sourceConfig{Web: []webSource{source}, RequestBudget: 2}}
	s.http = &http.Client{Transport: ledgerTransport(func(r *http.Request) (*http.Response, error) {
		calls++
		q := r.URL.Query()
		if !strings.Contains(q.Get("query"), "FIRST_PDATE:[2026-09-01 TO 2026-09-30]") ||
			q.Get("pageSize") != "100" || calls == 1 && q.Get("cursorMark") != "*" ||
			calls == 2 && q.Get("cursorMark") != "next" {
			t.Fatalf("wrong date filter or cursor: %s", r.URL)
		}
		body := `{"hitCount":2,"nextCursorMark":"next","resultList":{"result":[{"id":"12345","source":"MED",
			"title":"Vaccine delivery","firstPublicationDate":"2026-09-10","abstractText":"Published evidence."}]}}`
		if calls == 2 {
			body = `{"hitCount":2,"resultList":{"result":[{"id":"12346","source":"MED",
			"title":"Cancer diagnosis","firstPublicationDate":"2026-09-11","abstractText":"Published evidence."}]}}`
		}
		return &http.Response{StatusCode: http.StatusOK, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(body))}, nil
	})}
	data := &snapshot{From: "2026-09-01", Through: "2026-09-30", ObservedAt: "2026-09-30T12:00:00Z"}
	s.sourceHistory(t.Context(), source, data, nil)
	if calls != 2 || len(data.Records) != 2 || len(data.Coverage) != 1 || !data.Coverage[0].Exhausted {
		t.Fatalf("bounded collection failed: calls=%d data=%+v", calls, data)
	}
	for _, row := range data.Records {
		if row.Source != source.Name || row.Industry != source.Industry || len(row.Provenance) != 1 ||
			row.Provenance[0].SHA256 == "" || row.ObservedAt != data.ObservedAt {
			t.Fatalf("source attribution lost: %+v", row)
		}
	}
}

func TestDomainSourcesAreShippedAndWithinPerimeter(t *testing.T) {
	for _, filename := range []string{"source-config.example.json", "../../infra/trend-sources.json"} {
		raw, err := os.ReadFile(filename)
		if err != nil {
			t.Fatal(err)
		}
		var config sourceConfig
		if err := json.Unmarshal(raw, &config); err != nil {
			t.Fatal(err)
		}
		if err := validateSourceConfig(config); err != nil {
			t.Fatal(err)
		}
		for _, expected := range append(domainArchiveSources(), domainResearchFeeds()...) {
			if !slices.ContainsFunc(config.Web, func(actual webSource) bool {
				return actual.Name == expected.Name && actual.URL == expected.URL && actual.Kind == expected.Kind
			}) {
				t.Errorf("%s missing domain source %s", filename, expected.Name)
			}
			u, err := url.Parse(expected.URL)
			if err != nil || allowedAddress(u, config.domains()) != nil {
				t.Errorf("%s excludes %s from the network perimeter", filename, expected.URL)
			}
		}
	}
}

func TestLivePublicationSources(t *testing.T) {
	seen := map[string]bool{}
	for _, source := range domainArchiveSources() {
		if seen[source.Kind] && source.Kind != kindBiorxiv {
			continue
		}
		seen[source.Kind] = true
		name := source.Kind
		if source.Kind == kindBiorxiv {
			name += "/" + strings.TrimPrefix(source.URL, "https://api.biorxiv.org/details/")
		}
		t.Run(name, func(t *testing.T) {
			s, data, ctx := liveSourceService(t, kindWeb)
			source.Pages = 1
			s.config.Web = []webSource{source}
			s.config.AllowedDomains = nil
			s.http = newWebClient(40*time.Second, s.config.domains())
			s.sourceHistory(ctx, source, data, nil)
			if len(data.Records) == 0 {
				t.Fatalf("no usable publications: %+v", data.Coverage)
			}
			for _, row := range data.Records {
				if row.Date < data.From || row.Date > data.Through || row.Genre == "" || row.TextStatus == articleText {
					t.Fatalf("invalid scientific metadata: %+v", row)
				}
			}
			total := "unknown"
			if value := data.Coverage[0].Total; value != nil {
				total = strconv.Itoa(*value)
			}
			t.Logf("source=%s accepted=%d total=%s next=%t sample=%s", source.Name, len(data.Records),
				total, data.Coverage[0].Cursor != "", data.Records[0].URL)
		})
	}
}

func TestLiveDomainFeeds(t *testing.T) {
	for _, source := range domainResearchFeeds() {
		t.Run(source.Name, func(t *testing.T) {
			s, data, ctx := liveSourceService(t, kindWeb)
			s.config.Web = []webSource{source}
			s.config.AllowedDomains = nil
			s.http = newWebClient(30*time.Second, s.config.domains())
			raw, err := s.fetch(ctx, source.URL, data)
			if err != nil {
				t.Fatal(err)
			}
			rows, err := feedRows(raw, feed{Name: source.Name, URL: source.URL}, data.Through)
			if err != nil || len(rows) == 0 {
				t.Fatalf("no dated feed publications: %v", err)
			}
			t.Logf("source=%s records=%d sample=%s", source.Name, len(rows), rows[0].URL)
		})
	}
}
