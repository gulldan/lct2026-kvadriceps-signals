package main

import (
	"errors"
	"net/url"
	"slices"
	"strings"
	"testing"
)

func TestPatentPanelCoversItsOwnEndpoints(t *testing.T) {
	config := defaultConfig()
	web := collectorDomains(config, "web")
	if slices.Contains(web, "uspto.gov") || slices.Contains(web, "epo.org") {
		t.Fatalf("the web panel was widened by the patent collector's hosts: %v", web)
	}
	patents := collectorDomains(config, kindPatents)
	for _, host := range []string{
		"patentsgazette.uspto.gov", "www.uspto.gov", "ppubs.uspto.gov", "data.epo.org",
	} {
		address := mustParse(t, "https://"+host+"/x")
		if err := allowedAddress(address, patents); err != nil {
			t.Fatalf("the patent collector cannot reach its own endpoint %s: %v", host, err)
		}
	}
	for _, host := range []string{"evil.example", "uspto.gov.evil.example", "news.mit.edu.evil.example"} {
		address := mustParse(t, "https://"+host+"/x")
		if allowedAddress(address, patents) == nil {
			t.Fatalf("the widened panel admitted %s", host)
		}
	}
	if err := allowedAddress(mustParse(t, "https://news.mit.edu/rss/topic/energy"), web); err != nil {
		t.Fatalf("a configured web source was refused: %v", err)
	}
}

func TestEPScopeSurvivesAFailingUSScope(t *testing.T) {
	t.Parallel()
	data := &snapshot{Version: 3, SourceSet: "test", ObservedAt: "2026-09-13T00:00:00Z",
		From: "2026-08-01", Through: "2026-09-13", Records: []record{}, Pages: []provenance{}, Limits: []string{}}
	s := &service{kind: kindPatents, dir: t.TempDir(), config: defaultConfig()}
	s.config.EPDocuments = 5
	s.http = newWebClient(1, collectorDomains(s.config, kindPatents))
	err := s.patentScopes(t.Context(), data)
	if err == nil {
		t.Fatal("a pass that collected nothing at all reported success")
	}
	parts := make([]string, 0, len(data.Coverage))
	for _, c := range data.Coverage {
		parts = append(parts, c.Source+":"+c.StopReason)
	}
	reasons := strings.Join(parts, " | ")
	if !strings.Contains(reasons, patentSource) || !strings.Contains(reasons, epSource) {
		t.Fatalf("a failing pass did not record both scopes separately: %s", reasons)
	}
}

func mustParse(t *testing.T, raw string) *url.URL {
	t.Helper()
	address, err := url.Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	return address
}

func TestUSMetadataIndexIgnoresOtherJurisdictions(t *testing.T) {
	data := &snapshot{Records: []record{
		{ID: "US12733373", KindCode: "B2", Date: "2026-09-02", Source: patentSource, Title: "A display device"},
		{ID: "EP1004359B1", KindCode: "B1", Date: "2013-09-04", Source: epSource, Title: "Unterteilte Etikette"},
	}}
	index, dates, err := patentMetadataIndex(data)
	if err != nil {
		t.Fatalf("an EP publication failed the US metadata index: %v", err)
	}
	if len(index) != 1 {
		t.Fatalf("the US index holds %d records, want only the US one", len(index))
	}
	if !slices.Contains(dates, "2026-09-02") {
		t.Fatalf("the US publication day was lost: %v", dates)
	}
	if data.Records[1].ExcludedReason != "" {
		t.Fatalf("the US pass excluded an EP publication: %q", data.Records[1].ExcludedReason)
	}
}

// Номер бюллетеня с нулями и номер поиска — один патент: проход не отменяется, остаётся проверенная запись.
func TestUSMetadataIndexFoldsPaddedGazetteID(t *testing.T) {
	data := &snapshot{Records: []record{
		{ID: "USPP037486", Date: "2026-06-23", Source: patentSource, Title: "Apple tree"},
		{ID: "USPP37486", Date: "2026-06-23", Source: patentSource, ParserVersion: patentMetadataVersion},
	}}
	index, _, err := patentMetadataIndex(data)
	if err != nil || len(index) != 1 || len(data.Records) != 1 || !metadataVerified(data.Records[0]) {
		t.Fatalf("a padded Gazette ID broke the pass: err=%v index=%v records=%+v", err, index, data.Records)
	}
}

func TestUSCoverageStampingLeavesOtherScopesAlone(t *testing.T) {
	data := &snapshot{
		Coverage: []coverage{
			{Source: patentSource, From: "2026-01-01", Through: "2026-12-31", Exhausted: true},
			{Source: epSource, From: "2026-01-01", Through: "2026-12-31", Exhausted: true,
				StopReason: "EP publication weeks are read in bounded passes", Cursor: "EP2151037NWB1"},
		},
		Records: []record{
			{ID: "US12733373", Date: "2026-09-02", Source: patentSource, Title: "A display device"},
			{ID: "EP1004359B1", Date: "2026-09-02", Source: epSource, Title: "Unterteilte Etikette"},
		},
	}
	metadataCoverage(data, "invalid requested patent ID")
	us, ep := data.Coverage[0], data.Coverage[1]
	if us.StopReason == "" || us.Exhausted {
		t.Fatalf("the US scope was not told that its verification is pending: %+v", us)
	}
	if !strings.Contains(ep.StopReason, "bounded passes") || ep.Cursor != "EP2151037NWB1" || !ep.Exhausted {
		t.Fatalf("the US verification pass rewrote the EP scope: %+v", ep)
	}
}

func TestUSWindowLookupSkipsOtherScopes(t *testing.T) {
	data := &snapshot{Coverage: []coverage{
		{Source: epSource, From: "2025-08-01", Through: "2026-09-13"},
		{Source: patentSource, From: "2026-09-01", Through: "2026-09-30"},
	}}
	found := coverageFor(data, "2026-09-02")
	if found < 0 || data.Coverage[found].Source != patentSource {
		t.Fatalf("the US window lookup landed on %d (%+v)", found, data.Coverage)
	}
	if got := coverageFor(data, "2025-10-01"); got >= 0 {
		t.Fatalf("a day outside every US window matched row %d", got)
	}
}

func TestUSHistoryPreparationSkipsOtherScopes(t *testing.T) {
	data := &snapshot{From: "2025-08-01", Through: "2026-09-13", PatentIndexFrom: "2026-01-01",
		Coverage: []coverage{
			{Source: epSource, From: "2025-08-01", Through: "2026-09-13", Exhausted: true,
				StopReason: "EP publication weeks are read in bounded passes", Cursor: "EP2151037NWB1"},
			{Source: patentSource, From: "2025-09-01", Through: "2025-09-30", Exhausted: true},
		}}
	preparePatentHistory(data, &patentMetadataProgress{})
	ep, us := data.Coverage[0], data.Coverage[1]
	if !strings.Contains(ep.StopReason, "bounded passes") || ep.Cursor != "EP2151037NWB1" || !ep.Exhausted {
		t.Fatalf("preparing the US sweep rewrote the EP scope: %+v", ep)
	}
	if us.Exhausted || !strings.Contains(us.StopReason, "USPTO") {
		t.Fatalf("the US window was not marked pending: %+v", us)
	}
}

func TestEPFairUseStopsBeforeTheProviderDoes(t *testing.T) {
	dir := t.TempDir()
	config := defaultConfig()
	config.EPDocuments, config.EPFairUseBytes = 5, 1024

	spender := &service{kind: kindPatents, dir: dir, config: config}
	if spender.epFairUseCheck() != nil {
		t.Fatal("an untouched ledger already reports the quota spent")
	}
	if err := spender.epFairUseAdd(2048); err != nil {
		t.Fatalf("record volume: %v", err)
	}

	// Новый сервис над тем же каталогом — это перезапуск.
	restarted := &service{kind: kindPatents, dir: dir, config: config}
	if !errors.Is(restarted.epFairUseCheck(), errEPFairUse) {
		t.Fatal("the spent quota did not survive a restart")
	}

	data := &snapshot{Version: 3, SourceSet: "test", ObservedAt: "2026-09-13T00:00:00Z",
		From: "2026-08-01", Through: "2026-09-13", Records: []record{}, Pages: []provenance{}, Limits: []string{}}
	restarted.http = newWebClient(1, collectorDomains(config, kindPatents))
	if _, err := restarted.fetch(t.Context(), epBase+"/publication-dates", data); !errors.Is(err, errEPFairUse) {
		t.Fatalf("a spent quota did not refuse the request before it left the process: %v", err)
	}
	restarted.epHistory(t.Context(), data)

	var ep *coverage
	for i := range data.Coverage {
		if data.Coverage[i].Source == epSource {
			ep = &data.Coverage[i]
		}
	}
	if ep == nil {
		t.Fatal("the EP scope recorded no coverage row at all")
	}
	if ep.Exhausted {
		t.Fatal("a pass stopped by the provider quota claimed the history was fully read")
	}
	if !strings.Contains(ep.StopReason, "fair-use") {
		t.Fatalf("the stop reason does not name the quota: %q", ep.StopReason)
	}
	if len(data.Records) != 0 {
		t.Fatalf("a refused pass invented %d records", len(data.Records))
	}
}
