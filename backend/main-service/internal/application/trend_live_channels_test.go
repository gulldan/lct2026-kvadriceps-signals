package application

import (
	"context"
	"encoding/json/v2"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"
)

type channelTestStore struct {
	raw  []byte
	fail bool
}

func (s *channelTestStore) GetBytes(context.Context, string) ([]byte, error) {
	return slices.Clone(s.raw), nil
}

func (s *channelTestStore) Put(_ context.Context, _ string, r io.Reader, _ int64, _ string) error {
	if s.fail {
		return errors.New("storage unavailable")
	}
	var err error
	s.raw, err = io.ReadAll(r)
	return err
}

func TestLiveSourceRecoveryPreservesScienceAndReloadsSameObservation(t *testing.T) {
	periods := livePeriods(time.Date(2026, 9, 10, 12, 0, 0, 0, time.UTC))
	snapshot := LiveSnapshot{Version: liveVersion, Method: liveMethod, ObservedAt: "2026-09-10T12:00:00Z", Periods: periods,
		Topics: []LiveTopic{{ID: "hydrogen", PaperIDs: []string{"a"}, Query: LiveQuery{HN: "hydrogen production"},
			Comparison: &LiveComparison{Current: 1}, Semantic: &LiveSemantic{Model: "local", Revision: "pinned", Distance: .5},
			Limits: []LiveLimit{{Code: liveLimitArxivKept, Text: "arxiv: bounded sample"},
				{Text: "PATENT_COLLECTOR_URL: HTTP 503"}}}},
		Papers: []StudyPaper{{ID: "a", Title: "Green hydrogen production", Date: "2026-09-09"}}}
	raw, err := json.Marshal(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(raw, &snapshot); err != nil {
		t.Fatal(err)
	}
	store := &channelTestStore{raw: raw}
	service := NewTrendLiveService(store, func(context.Context) string { return "" })
	reader := NewTrendLiveService(store, func(context.Context) string { return "" })
	reader.now = func() time.Time { return time.Date(2026, 9, 10, 13, 0, 0, 0, time.UTC) }
	if delay := reader.RefreshDelay(t.Context(), 6*time.Hour); delay != 5*time.Hour {
		t.Fatalf("restart would repeat a fresh collection: %s", delay)
	}
	reader.load(t.Context())
	reader.cache["keep"] = liveCacheEntry{}
	reader.load(t.Context())
	if len(reader.cache) != 1 {
		t.Fatal("unchanged snapshot invalidated search cache")
	}
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		var query struct {
			Terms   []string     `json:"terms"`
			Periods []LivePeriod `json:"periods"`
		}
		body, readErr := io.ReadAll(r.Body)
		if readErr != nil || json.Unmarshal(body, &query) != nil || r.URL.Path != "/v1/query" ||
			!slices.Contains(query.Terms, "hydrogen production") || slices.Contains(query.Terms, "hydrogen") ||
			len(query.Periods) != len(periods) {
			t.Error("wrong source query or a scientific provider was called")
		}
		counts := make([]*int, len(periods))
		counts[len(counts)-1] = new(3)
		response, marshalErr := json.Marshal(LiveChannel{
			Source: "dated sample", ObservedAt: "2026-09-10T13:00:00Z", Counts: counts, Total: 3, Partial: true,
		})
		if marshalErr != nil {
			t.Error(marshalErr)
		}
		_, _ = w.Write(response)
	}))
	defer server.Close()
	t.Setenv("PATENT_COLLECTOR_URL", server.URL)
	t.Setenv("WEB_COLLECTOR_URL", server.URL)
	if err := service.RefreshSources(t.Context()); err != nil {
		t.Fatal(err)
	}
	reader.load(t.Context())
	got := reader.snapshot
	if calls != 2 || len(reader.cache) != 0 || got.ObservedAt != snapshot.ObservedAt ||
		!reflect.DeepEqual(got.Papers, snapshot.Papers) ||
		!reflect.DeepEqual(got.Topics[0].Semantic, snapshot.Topics[0].Semantic) ||
		!reflect.DeepEqual(got.Topics[0].Comparison, snapshot.Topics[0].Comparison) || got.Topics[0].Patents.Total != 3 ||
		len(got.Topics[0].Limits) != 1 || got.Topics[0].Limits[0].Text != "arxiv: bounded sample" ||
		got.Topics[0].Limits[0].Code != liveLimitArxivKept {
		t.Fatalf("source recovery changed science, lost provenance, or failed to reload: %+v", got)
	}
	previous := service.snapshot
	stored := slices.Clone(store.raw)
	store.fail = true
	if err := service.RefreshSources(t.Context()); err == nil ||
		service.snapshot != previous || !slices.Equal(store.raw, stored) {
		t.Fatal("failed persistence replaced the last successful snapshot")
	}
}

func TestLiveWebChannelAsksRussianFeedsInRussian(t *testing.T) {
	periods := livePeriods(time.Date(2026, 9, 17, 12, 0, 0, 0, time.UTC))
	topic := LiveTopic{ID: "search:x", Source: "search",
		Name: "зарождающиеся технологии промышленного искусственного интеллекта",
		Query: LiveQuery{Terms: []string{"industrial artificial intelligence", "industrial ai", "factory robotics",
			"predictive maintenance", "defect detection", "digital twin", "robot learning", "process control",
			"edge inference", "sensor fusion", "adaptive manufacturing", "machine vision"}}}
	asked := map[string][]string{}
	var mu sync.Mutex
	channel := func(name string) *httptest.Server {
		return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			var query struct {
				Terms   []string     `json:"terms"`
				Periods []LivePeriod `json:"periods"`
			}
			body, err := io.ReadAll(r.Body)
			if err != nil || json.Unmarshal(body, &query) != nil {
				t.Error(err)
			}
			if len(query.Terms) > liveChannelTerms {
				t.Errorf("сборщик отвергнет пакет целиком: %d терминов", len(query.Terms))
			}
			mu.Lock()
			asked[name] = append(asked[name], query.Terms...)
			mu.Unlock()
			counts := make([]*int, len(periods))
			response, err := json.Marshal(LiveChannel{Source: name, Counts: counts})
			if err != nil {
				t.Error(err)
			}
			_, _ = w.Write(response)
		}))
	}
	web, patents := channel("web"), channel("patents")
	defer web.Close()
	defer patents.Close()
	t.Setenv("WEB_COLLECTOR_URL", web.URL)
	t.Setenv("PATENT_COLLECTOR_URL", patents.URL)
	liveChannels(t.Context(), NewTrendLiveService(&channelTestStore{}, func(context.Context) string { return "" }).newSources(),
		&topic, periods)
	if !slices.Contains(asked["web"], "промышленного искусственного интеллекта") ||
		!slices.Contains(asked["web"], "искусственного интеллекта") ||
		!slices.Contains(asked["web"], "industrial artificial intelligence") {
		t.Fatalf("русские ленты по-прежнему не могут совпасть: %q", asked["web"])
	}
	if slices.ContainsFunc(asked["patents"], liveNeedsTranslation) {
		t.Fatalf("русская фраза заняла место англоязычных ведомств в патентах: %q", asked["patents"])
	}
	if len(asked["web"]) != len(topic.Query.Terms)+2 || len(asked["patents"]) != len(topic.Query.Terms) {
		t.Fatalf("план источника усечён при разбиении: %+v", asked)
	}
}

func TestLiveRussianQuerySplitsIntoNounGroups(t *testing.T) {
	for _, tc := range []struct {
		query string
		want  []string
	}{
		// Предмет запроса стоит в начале перечисления, и хвост целого запроса его терял бы.
		{"цифровой рубль и платёжная инфраструктура", []string{"цифровой рубль", "платёжная инфраструктура"}},
		{"промышленные роботы и захваты", []string{"промышленные роботы", "захваты"}},
		// Резать нечего: одна группа идёт фразой и своим двухсловным хвостом.
		{"защита ии-агентов от подмены инструкций",
			[]string{"защита ии агентов от подмены инструкций", "подмены инструкций"}},
		{"беспилотный транспорт, логистика, а также склады",
			[]string{"беспилотный транспорт", "логистика", "склады"}},
		{"зарождающиеся технологии промышленного искусственного интеллекта",
			[]string{"промышленного искусственного интеллекта", "искусственного интеллекта"}},
		{"слабые сигналы в области кибербезопасности", []string{"кибербезопасности"}},
		{"emerging battery technologies", nil},
	} {
		if got := liveRussianTerms(tc.query); !slices.Equal(got, tc.want) {
			t.Fatalf("liveRussianTerms(%q) = %q, want %q", tc.query, got, tc.want)
		}
	}
}

func TestLiveFailedSourceQueryIsNotSuccessfulRecovery(t *testing.T) {
	periods := livePeriods(time.Date(2026, 9, 10, 12, 0, 0, 0, time.UTC))
	snapshot := LiveSnapshot{Version: liveVersion, Method: liveMethod, ObservedAt: "2026-09-10T12:00:00Z", Periods: periods,
		Topics: []LiveTopic{{
			ID: "hydrogen", Query: LiveQuery{HN: "hydrogen"},
			Patents: &LiveChannel{Total: 3, ObservedAt: "2026-09-09T12:00:00Z"},
		}},
	}
	raw, err := json.Marshal(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	store := &channelTestStore{raw: raw}
	service := NewTrendLiveService(store, func(context.Context) string { return "" })
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls++
		http.Error(w, "unavailable", http.StatusServiceUnavailable)
	}))
	defer server.Close()
	t.Setenv("PATENT_COLLECTOR_URL", server.URL)
	t.Setenv("WEB_COLLECTOR_URL", server.URL)
	if err = service.RefreshSources(t.Context()); err == nil {
		t.Fatal("failed source queries reported success")
	}
	if calls != 2 || service.snapshot.Topics[0].Patents.Total != 3 || service.snapshot.ObservedAt != snapshot.ObservedAt {
		t.Fatal("failure lost successful source/scientific observation")
	}
}

func TestLiveFailureCodeSeparatesCancellationFromSourceRefusal(t *testing.T) {
	cases := map[string]string{
		"": "",
		"live: collection canceled: context canceled":                                              LiveFailureCanceled,
		"writer interrupted; publication not confirmed":                                            LiveFailureCanceled,
		"source status request: Get \"http://web-collector/status\": dial tcp: connection refused": LiveFailureSource,
		"live: no family collected":                                                                LiveFailureOther,
	}
	for text, want := range cases {
		if got := LiveFailureCode(text); got != want {
			t.Fatalf("LiveFailureCode(%q) = %q, want %q", text, got, want)
		}
	}
}

func TestLiveMarketEvidenceReachesCard(t *testing.T) {
	periods := livePeriods(time.Date(2026, 9, 17, 12, 0, 0, 0, time.UTC))
	day := periods[len(periods)-1].Start
	record := func(id, title, excerpt, url string) LiveSourceRecord {
		return LiveSourceRecord{ID: id, Title: title, Excerpt: excerpt, URL: url, Date: day}
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var query struct {
			Terms  []string `json:"terms"`
			Titles bool     `json:"titles"`
		}
		body, err := io.ReadAll(r.Body)
		if err != nil || json.Unmarshal(body, &query) != nil || !query.Titles ||
			!slices.Equal(query.Terms, []string{"stablecoin payments"}) {
			t.Errorf("wrong market query: %s", body)
		}
		response, err := json.Marshal(LiveChannel{Source: "web", Counts: make([]*int, len(periods)),
			Records: []LiveSourceRecord{
				record("j", "Stablecoin payments in theory", "", "https://journals.plos.org/plosone/article?id=1"),
				record("pr", "Acme launches stablecoin payments wallet", "Acme opened it to merchants.",
					"https://www.prnewswire.com/news-releases/acme/"),
				record("m1", "Bank pilots stablecoin payments", "", "https://www.pymnts.com/1/"),
				record("m2", "Stablecoin payments startup raises a round", "It serves exporters in Asia.",
					"https://techcrunch.com/2/"),
				record("m3", "Stablecoin payments grow", "", "https://sifted.eu/3/"),
			}})
		if err != nil {
			t.Error(err)
		}
		_, _ = w.Write(response)
	}))
	defer server.Close()
	t.Setenv("WEB_COLLECTOR_URL", server.URL)
	papers := make([]StudyPaper, 10)
	ids := make([]string, 10)
	for i := range papers {
		ids[i] = "arxiv:" + string(rune('a'+i))
		papers[i] = StudyPaper{ID: ids[i], Title: "Stablecoin payments study", Date: day}
	}
	candidates := []LiveTopic{
		{StudyTopic: StudyTopic{ID: "ngram:stablecoin-payment", PaperIDs: ids}, Name: "Stablecoin Payments",
			Query: LiveQuery{HN: "stablecoin payments"}, Qualification: &LiveQualification{Verified: true}},
		{Name: "Unverified Thing", Query: LiveQuery{HN: "unverified thing"}},
		{Name: "Stablecoin Payments", Shortfall: &LiveShortfall{Code: liveRejectBudget}},
	}
	service := NewTrendLiveService(nil, func(context.Context) string { return "" })
	service.marketEvidence(t.Context(), &LiveTopic{}, candidates, periods)
	got := &candidates[0]
	if !slices.Equal(got.EvidenceIDs, []string{"rec:m1", "rec:m2", "rec:m3"}) || got.RecordsCount != 3 ||
		candidates[1].Web != nil || !slices.Equal(candidates[2].EvidenceIDs, got.EvidenceIDs) {
		t.Fatalf("market records picked wrong: %v, records %d", got.EvidenceIDs, got.RecordsCount)
	}
	documents := liveWithCandidateRecords(candidates, papers)
	metadata := map[string]LivePaperMetadata{}
	livePrepareAnswerMetadata(candidates, documents, metadata)
	byID := map[string]StudyPaper{}
	for _, document := range documents {
		byID[document.ID] = document
	}
	works, used := liveCardInput(*got, byID, metadata)
	if len(works) != liveCardWorks || !slices.Equal(used[5:], got.EvidenceIDs) ||
		!slices.ContainsFunc(works, func(work string) bool { return strings.Contains(work, "exporters in Asia") }) {
		t.Fatalf("card did not see the market: %v", works)
	}
	got.Card = &LiveCard{Evidence: []LiveClaimEvidence{
		{Field: liveClaimExample, SourceID: "rec:m2", Verified: true},
		{Field: liveClaimAdvantage, SourceID: "rec:m1", Verified: false},
	}}
	liveKeepCitedMarket(got)
	if !slices.Equal(got.EvidenceIDs, []string{"rec:m2"}) || got.RecordsCount != 1 || got.Web.Total != 1 {
		t.Fatalf("uncited market records stayed: %v, web %+v", got.EvidenceIDs, got.Web)
	}

	// Запись из общего сбора приходит без фрагмента; рыночная — впереди патента, иначе квота записей карточки уйдёт ему.
	inherited := LiveTopic{StudyTopic: StudyTopic{EvidenceIDs: []string{"rec:old", "rec:patent"}},
		Web: &LiveChannel{Records: []LiveSourceRecord{record("old", "Acme pilots stablecoin payments", "", "")}}}
	liveAttachMarket(&inherited, &LiveChannel{Records: []LiveSourceRecord{
		record("old", "Acme pilots stablecoin payments", "Acme runs the pilot with two banks.", ""),
		record("new", "Stablecoin payments startup raises a round", "It serves exporters in Asia.", ""),
	}}, "stablecoin payments")
	if inherited.Web.Records[0].Excerpt == "" || len(inherited.Web.Records) != 2 ||
		!slices.Equal(inherited.EvidenceIDs, []string{"rec:new", "rec:old", "rec:patent"}) {
		t.Fatalf("inherited record lost its excerpt or the market lost the card: %+v, %v",
			inherited.Web.Records, inherited.EvidenceIDs)
	}
}
