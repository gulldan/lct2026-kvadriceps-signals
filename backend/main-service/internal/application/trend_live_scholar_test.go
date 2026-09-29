package application

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/example/main-service/internal/platform/openalex"
)

func scholarPeriods() []LivePeriod {
	return []LivePeriod{
		{ID: "2026-08", Start: "2026-08-01", EndExclusive: "2026-09-01"},
		{ID: "2026-09", Start: "2026-09-01", EndExclusive: "2026-10-01"},
	}
}

const scholarPage = `{"meta":{"count":2,"page":1,"per_page":200},"results":[
{"id":"https://openalex.org/W1","doi":"https://doi.org/10.1000/alpha","title":"Solid oxide electrolysis at scale",
 "publication_date":"2026-08-14","cited_by_count":7,"type":"article","language":"en",
 "primary_location":{"source":{"id":"https://openalex.org/S1","display_name":"Journal of Energy"}},
 "authorships":[{"author":{"id":"https://openalex.org/A1","display_name":"Ann Lee"},
   "institutions":[{"id":"https://openalex.org/I1","display_name":"Institute One"}]}],
 "abstract_inverted_index":{"Electrolysis":[0],"works":[1]}},
{"id":"https://openalex.org/W2","doi":null,"title":"Short",
 "publication_date":"2026-09-02","cited_by_count":0,"authorships":[]}
]}`

func scholarSources(t *testing.T, body string) (*liveSources, *int) {
	t.Helper()
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if !strings.Contains(r.URL.RawQuery, "title_and_abstract.search") {
			t.Errorf("запрос без поиска по заголовку и аннотации: %s", r.URL.RawQuery)
		}
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(server.Close)
	client, err := openalex.New(openalex.Config{BaseURL: server.URL, Mailto: "test@example.org"})
	if err != nil {
		t.Fatalf("клиент OpenAlex: %v", err)
	}
	return &liveSources{oa: client, metadata: map[string]LivePaperMetadata{}}, &calls
}

func TestScholarSearchMapsWorks(t *testing.T) {
	src, calls := scholarSources(t, scholarPage)
	papers, metadata, limits := src.liveScholarSearch(t.Context(), []string{"solid oxide electrolysis"}, scholarPeriods())
	if *calls == 0 {
		t.Fatal("запрос к OpenAlex не выполнен")
	}
	// Вторая запись отбрасывается: слишком короткий заголовок и нет DOI.
	if len(papers) != 1 {
		t.Fatalf("работ %d, ожидали 1: %+v", len(papers), papers)
	}
	paper := papers[0]
	if paper.ID != "openalex:10.1000/alpha" {
		t.Errorf("идентификатор %q", paper.ID)
	}
	if paper.URL != "https://doi.org/10.1000/alpha" || paper.Date != "2026-08-14" {
		t.Errorf("адрес или дата: %+v", paper)
	}
	meta := metadata[paper.ID]
	if meta.CitedBy == nil || *meta.CitedBy != 7 {
		t.Errorf("цитируемость не перенесена: %+v", meta.CitedBy)
	}
	if len(meta.Authorships) != 1 || meta.Authorships[0].OrganizationID == "" {
		t.Errorf("организация автора нужна для счёта независимых групп: %+v", meta.Authorships)
	}
	if meta.Category != "Journal of Energy" {
		t.Errorf("издание: %q", meta.Category)
	}
	if meta.Genre != "article" || meta.Language != "en" || meta.LanguageBasis != "язык указан в метаданных OpenAlex" {
		t.Errorf("тип или явный язык источника потерян: %+v", meta)
	}
	for _, limit := range limits {
		if limit.Code == liveLimitScholarOff {
			t.Errorf("неожиданное ограничение: %s", limit.Text)
		}
	}
}

func TestScholarPaperPreservesPrimarySourcePassport(t *testing.T) {
	for _, test := range []struct {
		name, doi, landing, workType, wantURL, wantGenre string
	}{
		{"preprint without DOI", "", "http://arxiv.org/abs/2601.04227", "preprint", "http://arxiv.org/abs/2601.04227", "preprint"},
		{"primary before DOI", "10.1000/paper", "https://journal.example/paper", "article", "https://journal.example/paper", "article"},
		{"unsafe location and unknown type", "10.1000/paper", "javascript:alert(1)",
			"peer_reviewed", "https://doi.org/10.1000/paper", GenreUnknown},
	} {
		t.Run(test.name, func(t *testing.T) {
			work := openAlexWork{ID: "https://openalex.org/W7120272630", DOI: test.doi,
				Title: "Synthetic speech detection method", PublicationDate: "2026-08-14",
				Type: test.workType, Language: "de", PrimaryLocation: &openalex.Location{
					LandingPageURL: test.landing, Source: &openalex.Entity{DisplayName: "Original venue"},
				}}
			paper, meta, ok := liveScholarPaper(work, scholarPeriods())
			if !ok || paper.URL != test.wantURL || meta.SourceURL != test.wantURL || paper.CanonicalID != test.doi {
				t.Fatalf("первоисточник или DOI потерян: paper=%+v metadata=%+v", paper, meta)
			}
			metadata := map[string]LivePaperMetadata{paper.ID: meta}
			stampPaperPassports([]StudyPaper{paper}, metadata)
			passport := livePaperPassports([]StudyPaper{paper}, metadata)[paper.ID]
			if passport.Language != "de" || passport.LanguageBasis != "язык указан в метаданных OpenAlex" ||
				passport.Genre != test.wantGenre || passport.DiscoverySource != "openalex" || passport.SourceName != "Original venue" {
				t.Fatalf("паспорт первоисточника подменён: %+v", passport)
			}
			if test.workType == "preprint" && passport.PublisherType != PublisherPreprint {
				t.Fatalf("площадка препринта подменена агрегатором: %+v", passport)
			}
		})
	}
}

// Работа вне окна исказила бы сравнение периодов, по которому считается рост.
func TestScholarSearchDropsWorkOutsideWindow(t *testing.T) {
	body := strings.Replace(scholarPage, `"publication_date":"2026-08-14"`, `"publication_date":"2024-01-05"`, 1)
	src, _ := scholarSources(t, body)
	papers, _, _ := src.liveScholarSearch(t.Context(), []string{"electrolysis"}, scholarPeriods())
	if len(papers) != 0 {
		t.Fatalf("работа вне окна попала в выдачу: %+v", papers)
	}
}

func TestScholarSearchReportsMissingClient(t *testing.T) {
	src := &liveSources{}
	papers, _, limits := src.liveScholarSearch(t.Context(), []string{"x"}, scholarPeriods())
	if len(papers) != 0 || len(limits) == 0 {
		t.Fatalf("ожидали ограничение без работ, получили %d работ и %d ограничений", len(papers), len(limits))
	}
}

// Следующий поиск уже не тратит запросы: остаток ниже резерва истории терминов модели.
func TestScholarSearchReportsExhaustedBudget(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		w.Header().Set("Retry-After", "64764")
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	t.Cleanup(server.Close)
	client, err := openalex.New(openalex.Config{BaseURL: server.URL, Mailto: "test@example.org"})
	if err != nil {
		t.Fatalf("клиент OpenAlex: %v", err)
	}
	src := &liveSources{oa: client, metadata: map[string]LivePaperMetadata{}}
	papers, _, limits := src.liveScholarSearch(t.Context(), []string{"electrolysis"}, scholarPeriods())
	if len(papers) != 0 || len(limits) != 1 || limits[0].Code != liveLimitOpenAlexBudget {
		t.Fatalf("ожидали строку о бюджете без работ: %d работ, %+v", len(papers), limits)
	}
	spent := calls.Load()
	if _, _, limits = src.liveScholarSearch(t.Context(), []string{"quantum"}, scholarPeriods()); calls.Load() != spent ||
		len(limits) != 1 || limits[0].Code != liveLimitOpenAlexBudget {
		t.Fatalf("резерв истории не соблюдён: %d запросов после исчерпания, %+v", calls.Load()-spent, limits)
	}
}

// Запрос с поиском по тексту стоит десять кредитов суточного бюджета OpenAlex.
func TestScholarSearchCachesClosedMonths(t *testing.T) {
	src, calls := scholarSources(t, scholarPage)
	src.scholarCache = newLiveScholarCache(nil)
	periods := scholarPeriods()
	periods[0].Complete = true
	first, _, _ := src.liveScholarSearch(t.Context(), []string{"electrolysis"}, periods)
	cold := *calls
	second, _, _ := src.liveScholarSearch(t.Context(), []string{"electrolysis"}, periods)
	if warm := *calls - cold; warm >= cold || len(second) != len(first) {
		t.Fatalf("закрытый месяц спрошен заново: %d запросов против %d, работ %d и %d",
			warm, cold, len(second), len(first))
	}
}

func TestScholarPhrasesDropSeparators(t *testing.T) {
	got := liveScholarPhrases([]string{"solid, oxide|cells", `"quantum"  sensing`, "электролиз", "solid, oxide|cells"})
	want := []string{"solid oxide cells", "quantum sensing"}
	if len(got) != len(want) {
		t.Fatalf("фразы %q, ожидали %q", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("фразы %q, ожидали %q", got, want)
		}
	}
}

// Препринт и его журнальная версия — одна работа. Двойной счёт завысил бы рост.
func TestScholarMergeDropsDuplicates(t *testing.T) {
	arxiv := []StudyPaper{
		{ID: "arxiv:2608.1", Title: "Solid oxide electrolysis at scale", Date: "2026-08-01"},
		{ID: "arxiv:2608.2", Title: "Another paper", Date: "2026-08-02", CanonicalID: "10.1000/beta"},
	}
	arxivMeta := map[string]LivePaperMetadata{"arxiv:2608.1": {}, "arxiv:2608.2": {DOI: "10.1000/beta"}}
	scholar := []StudyPaper{
		{ID: "openalex:10.1000/alpha", Title: "solid oxide  electrolysis AT scale", Date: "2026-08-14"},
		{ID: "openalex:10.1000/beta", Title: "Another paper, journal version", Date: "2026-08-20", CanonicalID: "10.1000/beta"},
		{ID: "openalex:10.1000/gamma", Title: "Genuinely new work on catalysts", Date: "2026-09-03"},
	}
	scholarMeta := map[string]LivePaperMetadata{"openalex:10.1000/gamma": {DOI: "10.1000/gamma"}}
	merged, metadata := liveScholarMerge(arxiv, arxivMeta, scholar, scholarMeta)
	if len(merged) != 3 {
		t.Fatalf("после слияния работ %d, ожидали 3: %+v", len(merged), merged)
	}
	if merged[2].ID != "openalex:10.1000/gamma" {
		t.Errorf("осталась не та работа: %s", merged[2].ID)
	}
	if metadata["openalex:10.1000/gamma"].DOI != "10.1000/gamma" {
		t.Errorf("метаданные добавленной работы потеряны")
	}
}

// Равные доли месяцев разрежают корпус по существу, и у кандидатов не хватает документов.
func TestLiveScholarQuotasFollowMonthlyWeight(t *testing.T) {
	t.Parallel()
	counts := []int{4000, 1000, 0, 50}
	quotas := liveScholarQuotas(counts)
	if quotas[2] != 0 {
		t.Fatalf("пустой месяц получил долю: %v", quotas)
	}
	if quotas[0] <= quotas[1] {
		t.Fatalf("плотный месяц получил не больше редкого: %v", quotas)
	}
	if quotas[3] < liveScholarFloor || quotas[3] > 50 {
		t.Fatalf("редкий месяц вне нижней доли и своего запаса: %v", quotas)
	}
	for i, quota := range quotas {
		if quota > liveScholarPerPeriod {
			t.Fatalf("месяц %d просит больше страницы: %v", i, quotas)
		}
	}
	if total := quotas[0] + quotas[1] + quotas[3]; total > liveScholarBudget+3*liveScholarFloor {
		t.Fatalf("бюджет превышен: %d при %d", total, liveScholarBudget)
	}
}
