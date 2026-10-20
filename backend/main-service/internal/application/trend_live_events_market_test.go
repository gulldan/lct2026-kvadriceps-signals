package application

import (
	"archive/zip"
	"net/http"
	"net/http/httptest"
	"os"
	"slices"
	"strconv"
	"strings"
	"testing"
)

func TestLiveMarketEventSourcesParse(t *testing.T) {
	sbir := writeEventSample(t, "sbir.csv", `Company,"Award Title",Agency,Branch,Phase,Program,`+
		`"Agency Tracking Number",Contract,"Proposal Award Date","Award Year","Award Amount",Abstract,"PI Email"
"ZAKURO INC","Anode-free cells",Department of Defense,Air Force,"Phase II",SBIR,F22-1,FA8650-22-C-1,06/01/2022,2022,`+
		`"1,599,883","We scale up cells. The solid-state batteries reach 400 Wh/kg in a pouch format.",pi@example.com
"OLD CO","Old award",NASA,,"Phase I",SBIR,N1,,,2001,"50,000","A solid-state battery from 2001.",x@example.com
`)
	grants, err := liveParseSBIR(sbir, 2019)
	if err != nil || len(grants) != 1 {
		t.Fatalf("sbir grants = %+v, %v", grants, err)
	}
	events := liveGrantMatches(grants, liveSBIRSource(), "solid-state battery")
	if len(events) != 1 || events[0].Type != LiveEventGrantLate || events[0].Actor != "ZAKURO INC" ||
		events[0].Amount != "$1,599,883" || events[0].Record.Date != "2022-06-01" ||
		events[0].Quote != "The solid-state batteries reach 400 Wh/kg in a pouch format." ||
		events[0].Record.PublisherType != PublisherGovernment || strings.Contains(events[0].Phase, "@") {
		t.Fatalf("sbir event = %+v", events)
	}

	cordis := writeEventZip(t, map[string]string{
		"project.csv": `"id";"title";"ecMaxContribution";"ecSignatureDate";"fundingScheme";"objective"
"101";"PQC for banks";"2073781,25";"2024-05-12";"HORIZON-IA";"We pilot post-quantum cryptography in payment networks."
"102";"PQC theory";"1500000";"2024-01-01";"HORIZON-ERC";"Post-quantum cryptography lattices."
"103";"PQC network";"900000";"2026-05-02";"HORIZON-CSA";"A network on post-quantum cryptography."
`,
		"organization.csv": `"projectID";"name";"SME";"activityType";"country";"order";"role";"ecContribution"
"101";"TU BERLIN";"false";"HES";"DE";"1";"coordinator";"900000"
"101";"CRYPTO GMBH";"false";"PRC";"DE";"3";"participant";"451500,5"
"101";"NEGEV LAB";"true";"REC";"IL";"4";"participant";"120000"
"101";"BIG CORP";"false";"PRC";"US";"5";"associatedPartner";""
"103";"KENYA AGRI LTD";"true";"PRC";"KE";"1";"coordinator";"300000"
`,
	})
	grants, err = liveParseCORDIS(cordis, 2019)
	if err != nil || len(grants) != 2 || grants[0].Actor != "CRYPTO GMBH" || grants[0].Type != LiveEventGrantLate ||
		grants[0].Amount != "€2073781" || grants[0].Phase != "HORIZON-IA" || grants[1].Type != "" ||
		!slices.Equal(grants[0].Participants, []liveGrantParticipant{{"CRYPTO GMBH", "DE", 451500.5}, {"NEGEV LAB", "IL", 120000}}) {
		t.Fatalf("cordis grants = %+v, %v", grants, err)
	}
	if found := liveGrantMatches(grants, liveCORDISSource(), "post-quantum cryptography"); len(found) != 1 {
		t.Fatalf("a scheme without an event type must stay out of search: %+v", found)
	}
	money, records, _ := liveMoneyHorizonEvents(liveRubrics(), grants, "2026-04-01", "2026-10-18")
	if records != 1 || len(money) != 1 || money[0].Company != "KENYA AGRI LTD" || money[0].Country != "KE" ||
		money[0].Amount != "€300K" || money[0].Investors != "Horizon Europe, HORIZON-CSA" {
		t.Fatalf("horizon money events = %+v", money)
	}

	hn, err := liveParseHNEvents([]byte(`{"hits":[
{"objectID":"1","title":"Launch HN: Nori Robotics (YC S26) – A low-cost humanoid robot","url":"https://nori.example",
 "created_at":"2026-09-01T17:35:10Z","_tags":["story","launch_hn"]},
{"objectID":"2","title":"Show HN: Bambot – an open source, low cost (~$300) humanoid robot",
 "url":"https://github.com/timqian/bambot","created_at":"2025-03-19T00:00:00Z","_tags":["show_hn"]},
{"objectID":"3","title":"Show HN: A robot arm","url":"https://arm.example",
 "story_text":"<p>Not a <i>humanoid robot</i>.</p>","created_at":"2025-03-19T00:00:00Z","_tags":["show_hn"]}]}`),
		"humanoid robot")
	if err != nil || len(hn) != 2 || hn[0].Actor != "Nori Robotics" || hn[0].Phase != "YC S26" ||
		hn[0].Type != LiveEventProductLaunch || hn[1].Type != LiveEventCodeRelease ||
		hn[1].Actor != "Bambot" {
		t.Fatalf("hn events = %+v, %v", hn, err)
	}

	filings, err := liveEDGARFilings([]byte(`{"hits":{"hits":[
{"_id":"0001213900-26-012984:ex99-1.htm","_source":{"ciks":["0001805521"],
 "display_names":["FARADAY FUTURE INC.  (FFAI)  (CIK 0001805521)"],"file_date":"2026-02-06",
 "form":"8-K","adsh":"0001213900-26-012984","file_type":"EX-99.1","file_description":"PRESS RELEASE"}},
{"_id":"0001213900-26-069623:ex99-1.htm","_source":{"ciks":["0001805521"],
 "display_names":["FARADAY FUTURE INC.  (FFAI)  (CIK 0001805521)"],"file_date":"2026-06-17",
 "form":"8-K","adsh":"0001213900-26-069623","file_type":"EX-99.1"}}]}}`))
	if err != nil || len(filings) != 1 || filings[0].actor != "FARADAY FUTURE INC." || filings[0].form != "8-K EX-99.1" ||
		filings[0].url != "https://www.sec.gov/Archives/edgar/data/1805521/000121390026012984/ex99-1.htm" {
		t.Fatalf("edgar filings = %+v, %v", filings, err)
	}
	kind, quote := liveEDGARMention("Risk factors apply. Our humanoid robot is shown. "+
		"We started a pilot of the humanoid robot with a retailer. Revenue grew.", "humanoid robot")
	if kind != LiveEventPilot || !strings.HasPrefix(quote, "Our humanoid robot is shown. We started a pilot") {
		t.Fatalf("edgar mention = %q %q", kind, quote)
	}
	if passport := ClassifySource(filings[0].url); passport.Publisher != PublisherVendor {
		t.Fatalf("sec.gov passport = %+v", passport)
	}
}

// Сеть стенда рвёт ответы на середине; второй запрос просит остаток через Range.
func TestLiveEventFetchResumes(t *testing.T) {
	body := strings.Repeat("0123456789", 5000)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if from, ok := strings.CutPrefix(r.Header.Get("Range"), "bytes="); ok {
			offset, _ := strconv.Atoi(strings.TrimSuffix(from, "-"))
			w.WriteHeader(http.StatusPartialContent)
			_, _ = w.Write([]byte(body[offset:]))
			return
		}
		w.Header().Set("Content-Length", strconv.Itoa(len(body)))
		_, _ = w.Write([]byte(body[:20000]))
		if hijacker, ok := w.(http.Hijacker); ok {
			if conn, _, err := hijacker.Hijack(); err == nil {
				_ = conn.Close()
			}
		}
	}))
	defer server.Close()
	got, err := liveMarketFetch(t.Context(), server.Client(), http.MethodGet, server.URL, nil, nil, liveMarketMaxJSON)
	if err != nil || string(got) != body {
		t.Fatalf("resumed body: %d bytes of %d, %v", len(got), len(body), err)
	}
}

func writeEventSample(t *testing.T, name, content string) *os.File {
	t.Helper()
	path := t.TempDir() + "/" + name
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	file, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = file.Close() })
	return file
}

func writeEventZip(t *testing.T, files map[string]string) *os.File {
	t.Helper()
	file := writeEventSample(t, "cordis.zip", "")
	out, err := os.Create(file.Name())
	if err != nil {
		t.Fatal(err)
	}
	archive := zip.NewWriter(out)
	for name, content := range files {
		entry, err := archive.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		_, _ = entry.Write([]byte(content))
	}
	if err := archive.Close(); err != nil {
		t.Fatal(err)
	}
	_ = out.Close()
	return file
}
