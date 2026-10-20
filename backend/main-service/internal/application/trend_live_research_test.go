package application

import (
	"encoding/json/v2"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestLiveResearchDiscoveryAndIsolation(t *testing.T) {
	requests := 0
	queries := map[string]bool{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		query := r.URL.Query().Get(liveCoherenceQuery)
		if r.URL.Path != "/dc" {
			query = r.URL.Query().Get(liveResearchSearchParam)
			queries[query] = true
		}
		// Одинаковый ответ всем двадцати областям сделал бы фразу общей, и до проверки выдачи она бы не дошла.
		if !strings.Contains(query, "chemistry") {
			_, _ = w.Write([]byte(`{"meta":{"total":0},"data":[],"header":{"numFound":0},"results":[]}`))
			return
		}
		if r.URL.Path == "/dc" {
			_, _ = w.Write([]byte(`{"meta":{"total":2},"data":[{"attributes":{"doi":"10.1234/shared",
				"titles":[{"title":"Electrochemical ammonia synthesis in porous electrodes"}],
				"dates":[{"date":"2026-08-02","dateType":"Issued"}],"types":{"resourceTypeGeneral":"Dataset"},
				"creators":[{"name":"Group A"}],"descriptions":[{"description":"Chemistry <b>experiment</b>"}]}},
				{"attributes":{"doi":"10.1234/undated","titles":[{"title":"Undated chemistry"}],
				"dates":[{"date":"2026","dateType":"Issued"},{"date":"2026-08-02","dateType":"Updated"}]}}]}`))
			return
		}
		rows := []map[string]any{}
		suffixes := []string{"in porous electrodes", "with plasma catalysts", "with solid electrolytes", "at ambient conditions"}
		for i, suffix := range suffixes {
			doi := fmt.Sprintf("10.1234/%d", i)
			if i == 0 {
				doi = "10.1234/shared"
			}
			rows = append(rows, map[string]any{"id": strconv.Itoa(i), "mainTitle": "Electrochemical ammonia synthesis " + suffix,
				"publicationDate": "2026-08-02", "type": "dataset", "descriptions": []string{"Chemistry research"},
				"pids":    []map[string]string{{"scheme": "doi", "value": doi}},
				"authors": []map[string]string{{"fullName": fmt.Sprintf("Group %d", i)}}})
		}
		body, _ := json.Marshal(map[string]any{"header": map[string]int{"numFound": 400}, "results": rows})
		_, _ = w.Write(body)
	}))
	defer server.Close()
	client := newLiveResearchClient()
	client.datacite, client.openaire = server.URL+"/dc", server.URL+"/oa"
	src := &liveSources{research: client, metadata: map[string]LivePaperMetadata{}}
	periods := livePeriods(time.Date(2026, 10, 17, 0, 0, 0, 0, time.UTC))
	channel := src.researchSearch(t.Context(), []string{"chemistry"}, periods)
	if channel == nil || len(channel.Records) != 4 || !channel.Partial || len(channel.Coverage) != 2 {
		t.Fatalf("lost source, counted duplicate or assigned an update date: %+v", channel)
	}
	if channel.Records[0].Genre != "dataset" || channel.Records[0].Excerpt != " Chemistry experiment" {
		t.Fatalf("lost resource type or description: %+v", channel.Records[0])
	}
	_ = src.researchSearch(t.Context(), []string{"chemistry"}, periods)
	if requests != 3 {
		t.Fatalf("cache missed: %d requests", requests)
	}
	umbrella := LiveTopic{Research: channel, Query: LiveQuery{Terms: []string{"chemistry"}}}
	liveResearchLimits(&umbrella)
	documents := liveChannelDocuments(umbrella)
	livePrepareAnswerMetadata([]LiveTopic{umbrella}, documents, src.metadata)
	candidates := liveDiscoverWithCoverage(documents, src.metadata, periods, nil, 3, 2, false)
	if len(candidates) == 0 {
		t.Fatal("research-only corpus produced no candidate")
	}
	topic := liveCandidateTopic(umbrella, candidates[0], periods, liveAbsoluteBar(), nil)
	if topic.PapersCount != 0 || topic.RecordsCount != 4 || topic.Groups < 2 || !liveApplyTemporalCoverage(&topic) ||
		topic.Stage != studyStageUnassessed || topic.Growth.CountRatio != nil {
		t.Fatalf("records became papers, source aggregators became authors, or sample claimed growth: %+v", topic)
	}
	input := liveClassifierInput("chemistry", "test", []LiveTopic{topic}, documents, src.metadata)
	if input.Candidates[0].Features.MarketRecords != 0 {
		t.Fatal("datasets became market adoption evidence")
	}
	profile, _ := liveTitleProfile(topic, documents)
	if len(profile) != 4 {
		t.Fatal("research evidence lost before semantic analysis")
	}
	it := LiveTopic{Stage: "emerging", Limits: umbrella.Limits}
	if liveApplyTemporalCoverage(&it) || it.Stage != "emerging" {
		t.Fatal("unrelated research sampling changed IT assessment")
	}
	liveResearchDedup(channel, []StudyPaper{{Title: "Electrochemical ammonia synthesis in porous electrodes",
		CanonicalID: "10.1234/shared"}}, nil)
	if channel.Total != 3 {
		t.Fatal("scientific paper duplicated in research channel")
	}
	control := LiveTopic{ID: "it-control", Name: "IT control", Stage: "emerging"}
	snapshot := LiveSnapshot{Periods: periods, Topics: []LiveTopic{control}}
	service := &TrendLiveService{}
	service.collectResearchDiscoveries(t.Context(), src, &snapshot)
	if domains := len(liveResearchDomains()); domains != 20 || len(queries) != domains+1 {
		t.Fatalf("expected 20 distinct domain searches plus the initial query, got %d", len(queries))
	}
	if len(snapshot.Discovered) != 1 || snapshot.Discovered[0].PapersCount != 0 ||
		!snapshot.Discovered[0].Analysis.Discovery.Sampled || len(snapshot.Papers) != 4 {
		t.Fatalf("nightly discovery still needs arXiv: topics=%d documents=%d", len(snapshot.Discovered), len(snapshot.Papers))
	}
	before, _ := json.Marshal(control)
	after, _ := json.Marshal(snapshot.Topics[0])
	if string(before) != string(after) {
		t.Fatal("research discovery modified the existing IT topic")
	}
}

func TestLiveResearchGeneralPhrase(t *testing.T) {
	sample := func(titles ...string) []StudyPaper {
		papers := make([]StudyPaper, 0, len(titles))
		for _, title := range titles {
			papers = append(papers, StudyPaper{ID: title, Title: title, Date: "2026-08-02"})
		}
		return papers
	}
	mining := sample("Machine learning for ore sorting", "Machine learning in flotation", "Machine learning of tailings",
		"Heap bioleaching of copper", "Heap bioleaching with acidophiles", "Heap bioleaching for nickel")
	samples := [][]StudyPaper{mining, sample("Machine learning for crops"), sample("Machine learning in clinics"),
		sample("Veterinary medicine for horses")}
	periods := livePeriods(time.Date(2026, 10, 17, 0, 0, 0, 0, time.UTC))
	grams := func(general []string) []string {
		candidates := liveResearchCandidates(mining, nil, periods, nil, general)
		names := make([]string, 0, len(candidates))
		for _, candidate := range candidates {
			names = append(names, candidate.gram)
		}
		return names
	}
	general := liveResearchGeneral(samples)
	if !slices.Contains(grams(nil), "machine learning") || slices.Contains(grams(general), "machine learning") ||
		!slices.Contains(grams(general), "heap bioleaching") || !slices.Contains(general, "veterinary medicine") {
		t.Fatalf("general phrase or field name became a discovered technology: %v", grams(general))
	}
}

func TestLiveResearchFailuresAndVersions(t *testing.T) {
	failing := true
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if failing {
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		if r.URL.Path == "/dc" {
			_, _ = w.Write([]byte(`{"meta":{"total":0},"data":[]}`))
			return
		}
		_, _ = w.Write([]byte(`{"header":{"numFound":0},"results":[]}`))
	}))
	defer server.Close()
	client := newLiveResearchClient()
	client.datacite, client.openaire = server.URL+"/dc", server.URL+"/oa"
	src := &liveSources{research: client}
	periods := livePeriods(time.Now().UTC())
	channel := src.researchSearch(t.Context(), []string{"chemistry"}, periods)
	if len(channel.Limits) != 2 || len(channel.Coverage) != 0 {
		t.Fatal("HTTP failures became empty successes")
	}
	failing = false
	channel = src.researchSearch(t.Context(), []string{"chemistry"}, periods)
	if len(channel.Limits) != 0 || len(channel.Coverage) != 2 {
		t.Fatal("HTTP failure cached as an empty result")
	}
	for range 60 {
		client.openaireRequests = append(client.openaireRequests, time.Now())
	}
	channel = src.researchSearch(t.Context(), []string{"energy"}, periods)
	if len(channel.Limits) != 1 || len(channel.Coverage) != 1 {
		t.Fatal("rate budget disabled the healthy source")
	}
	for _, parse := range []func([]byte) ([]LiveSourceRecord, int, error){liveDataCiteRecords, liveOpenAIRERecords} {
		if _, _, err := parse([]byte(`{"error":"upstream unavailable"}`)); err == nil {
			t.Fatal("malformed response accepted")
		}
	}
	channel = &LiveChannel{Records: []LiveSourceRecord{
		{Title: "Dataset version one", DOI: "10.1234/v1", DOIAliases: []string{"10.1234/concept", "10.1234/v1"}},
		{Title: "Dataset version two", DOI: "10.1234/v2", DOIAliases: []string{"10.1234/concept"}},
	}}
	liveResearchDedup(channel, nil, nil)
	if channel.Total != 1 {
		t.Fatal("versions counted as independent research outputs")
	}
	if !liveResearchAuxiliary("Review for Chemical Catalysis") || liveResearchDay("2026") != "" ||
		liveResearchDay("2026-99-42") != "" {
		t.Fatal("review or unknown date accepted for discovery")
	}
}

// Сеть включается явно: обычный тест воспроизводим и не расходует часовой бюджет каталога.
func TestLiveResearchAPIs(t *testing.T) {
	if os.Getenv("RADAR_RESEARCH_LIVE") != "1" {
		t.Skip("set RADAR_RESEARCH_LIVE=1")
	}
	src := &liveSources{research: newLiveResearchClient(), metadata: map[string]LivePaperMetadata{}}
	periods := livePeriods(time.Now().UTC())
	domains := liveResearchDomains()
	if len(domains) != 20 {
		t.Fatalf("expected 20 discovery domains, got %d", len(domains))
	}
	for _, domain := range domains {
		query := domain.name
		t.Run(query, func(t *testing.T) {
			channel := src.researchSearch(t.Context(), domain.terms, periods)
			if channel == nil || len(channel.Coverage) != 2 || len(channel.Records) == 0 || len(channel.Limits) > 0 {
				t.Fatalf("source collection failed: %+v", channel)
			}
			umbrella := LiveTopic{Research: channel, Query: LiveQuery{Terms: []string{query}}}
			liveResearchLimits(&umbrella)
			documents := liveResearchDocuments(channel)
			livePrepareAnswerMetadata([]LiveTopic{umbrella}, documents, src.metadata)
			candidates := liveResearchCandidates(documents, src.metadata, periods, domain.terms, nil)
			t.Logf("query=%q records=%d candidates=%d", query, len(documents), len(candidates))
			for _, coverage := range channel.Coverage {
				for _, page := range coverage.Pages {
					t.Logf("source=%s received=%d found=%d url=%s sha256=%s",
						coverage.Source, coverage.Received, *coverage.ProviderTotal, page.URL, page.SHA256)
				}
			}
			if len(candidates) == 0 {
				t.Fatal("live data produced no new candidate")
			}
			for _, candidate := range candidates {
				topic := liveCandidateTopic(umbrella, candidate, periods, liveAbsoluteBar(), nil)
				if !liveApplyTemporalCoverage(&topic) {
					t.Fatal("sample lost its coverage limitation")
				}
				ids := []string{}
				for _, paper := range candidate.papers {
					ids = append(ids, paper.CanonicalID)
				}
				t.Logf("candidate=%q documents=%d dois=%s", topic.Name, len(candidate.papers), strings.Join(ids, ","))
			}
		})
	}
}
