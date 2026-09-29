package application

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"
)

func liveTestServer(t *testing.T) *httptest.Server {
	t.Helper()
	entry := func(id, date string, authors ...string) string {
		names := make([]string, 0, len(authors))
		for _, a := range authors {
			names = append(names, "<author><name>"+a+"</name></author>")
		}
		return fmt.Sprintf(`<entry><id>http://arxiv.org/abs/%sv2</id><title>Paper %s</title>`+
			`<published>%sT00:00:00Z</published>%s</entry>`, id, id, date, strings.Join(names, ""))
	}
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		switch {
		case r.URL.Path == "/arxiv-429":
			w.Header().Set("Retry-After", "60")
			w.WriteHeader(http.StatusTooManyRequests)
		case strings.HasPrefix(r.URL.Path, "/arxiv"):
			head := `<feed xmlns="http://www.w3.org/2005/Atom" xmlns:opensearch="x"><opensearch:totalResults>`
			if q.Get("max_results") == "1" {
				fmt.Fprint(w, head+`100</opensearch:totalResults></feed>`)
				return
			}
			fmt.Fprint(w, head+`4</opensearch:totalResults>`+
				entry("2501.00001", "2025-09-02", "A One", "B Two")+entry("2504.00002", "2025-12-01", "C Three")+
				entry("2507.00003", "2026-02-01", "D Four", "A One")+entry("2601.00004", "2026-08-01", "E Five")+`</feed>`)
		case strings.HasPrefix(r.URL.Path, "/github"):
			if r.Header.Get("Authorization") != "Bearer tok" {
				w.WriteHeader(http.StatusForbidden)
				return
			}
			fmt.Fprint(w, `{"total_count":3,"items":[{"full_name":"org/repo","html_url":"https://github.com/org/repo",`+
				`"created_at":"2025-06-01T00:00:00Z","stargazers_count":42,"owner":{"type":"Organization"}}]}`)
		default:
			fmt.Fprint(w, `{"nbHits":2,"hits":[{"objectID":"1","title":"Show HN","url":"",`+
				`"created_at":"2025-07-01T00:00:00Z","points":9}]}`)
		}
	}))
}

func TestLiveCollectMeasuresMonthsGroupsAndStage(t *testing.T) {
	server := liveTestServer(t)
	defer server.Close()
	src := newLiveSources(func(context.Context) string { return "tok" })
	src.arxiv, src.github, src.hn = server.URL+"/arxiv", server.URL+"/github", server.URL+"/hn"
	src.sleep = func(context.Context, time.Duration) error { return nil }
	now := time.Date(2026, 9, 10, 12, 0, 0, 0, time.UTC)
	periods := livePeriods(now)
	if q := arxivQuery("", liveCategories, now, now); strings.Contains(q, "()") || !strings.HasPrefix(q, "(cat:") {
		t.Fatalf("population query must not wrap an empty family: %s", q)
	}
	if len(periods) != 14 || periods[0].ID != "2025-08" || periods[13].ID != "2026-09" || periods[13].Complete ||
		!periods[12].Complete || periods[13].EndExclusive != "2026-09-11" {
		t.Fatalf("wrong months: %+v", periods)
	}
	for i := range periods {
		periods[i].Population = 100
	}
	window := []StudyPaper{
		{ID: "arxiv:2501.00001", Title: "GraphRAG at scale", Date: "2025-09-02", Authors: []string{"A One", "B Two"}},
		{ID: "arxiv:2504.00002", Title: "Graph-based retrieval-augmented generation", Date: "2025-12-01", Authors: []string{"C Three"}},
		{ID: "arxiv:2507.00003", Title: "Paper three", Date: "2026-02-01", Authors: []string{"D Four", "A One"}},
		{ID: "arxiv:2601.00004", Title: "Paper four", Date: "2026-08-01", Authors: []string{"E Five"}},
		{ID: "arxiv:2601.00005", Title: "Unrelated", Date: "2026-08-02", Authors: []string{"F Six"}},
	}
	metadata := map[string]LivePaperMetadata{
		"arxiv:2507.00003": {Abstract: "We combine a graph RAG pipeline with agents."},
		"arxiv:2601.00004": {Abstract: "Knowledge graphs for GraphRAG."},
	}
	members, err := liveMatchFamilies(liveFamilies(), window, metadata)
	if err != nil {
		t.Fatalf("match: %v", err)
	}
	papers := members[0]
	if len(papers) != 4 || papers[0].ID != "arxiv:2501.00001" {
		t.Fatalf("graphrag members: %+v", papers)
	}
	topic := liveBuild(t.Context(), src, liveFamilies()[0], periods, papers, true)
	if topic.PapersCount != 4 {
		t.Fatalf("collect: %+v", topic)
	}
	series := topic.Series
	if series[1].Count != 1 || series[6].Groups != 1 || series[6].RetainedGroups != 1 || series[12].NewGroups != 1 {
		t.Fatalf("series: %+v", topic.Series)
	}
	if topic.Groups != 3 || topic.Signals.Novelty != "recent" || topic.Signals.Coherence != "query" ||
		topic.Signals.Application != "implementations" || len(topic.Repos) != 14 || topic.Discussions[0] != 2 ||
		len(topic.External) != 2 || topic.External[0].Community != "organization" || topic.EarliestExternal != "2025-06-01" {
		t.Fatalf("assessment: %+v", topic)
	}
	src.token = func(context.Context) string { return "" }
	src.ghDown = false
	limited := liveBuild(t.Context(), src, liveFamilies()[0], periods, papers, true)
	if limited.Repos != nil || len(limited.Limits) == 0 || !src.ghDown {
		t.Fatalf("github limit must degrade, not fail: %+v", limited.Limits)
	}
}

func TestLiveDiscoverFindsRecurringNewTitleGrams(t *testing.T) {
	periods := livePeriods(time.Date(2026, 9, 10, 12, 0, 0, 0, time.UTC))
	papers := make([]StudyPaper, 0, 240)
	for i := range 40 {
		date := "2026-05-0" + string(rune('1'+i%9))
		if i%2 == 1 {
			date = "2026-08-1" + string(rune('0'+i%10))
		}
		papers = append(papers, StudyPaper{ID: fmt.Sprintf("arxiv:n%d", i), Title: "Latent Reasoning Chains for LLM Agents", Date: date,
			Authors: []string{fmt.Sprintf("Author %d", i)}})
	}
	for i := range 200 {
		papers = append(papers, StudyPaper{ID: fmt.Sprintf("arxiv:b%d", i), Title: "Large Language Models for Question Answering",
			Date: periods[i%8].Start, Authors: []string{"Same Person"}})
	}
	for i := range 20 {
		papers = append(papers, StudyPaper{ID: fmt.Sprintf("arxiv:e%d", i), Title: "Evolving Agents in the Wild",
			Date: "2026-05-0" + string(rune('1'+i%9)), Authors: []string{fmt.Sprintf("E %d", i)}})
		papers = append(papers, StudyPaper{ID: fmt.Sprintf("arxiv:f%d", i), Title: "An Evolving Agent Benchmark",
			Date: "2026-05-0" + string(rune('1'+i%9)), Authors: []string{fmt.Sprintf("F %d", i)}})
	}
	curated := []liveFamily{{id: "x", name: "Evolving agents", query: LiveQuery{HN: "evolving agents"}}}
	found := liveDiscover(papers, nil, periods, curated, liveDiscoverRecent, 10)
	if len(found) == 0 || found[0].gram != "latent reasoning" || len(found[0].papers) != 40 {
		t.Fatalf("discovery: %+v", found)
	}
	if found[0].surface != "Latent Reasoning" {
		t.Fatalf("surface must keep the title's case: %+v", found[0].surface)
	}
	acronym := liveCandidateFamily(liveCandidate{gram: "llm pipeline", surface: "LLM pipelines"})
	if acronym.name != "LLM Pipelines" || acronym.query.Arxiv != `ti:"llm pipelines"` {
		t.Fatalf("acronyms keep their case, queries are lowercase: %+v", acronym)
	}
	family := liveCandidateFamily(found[0])
	if family.id != "ngram:latent-reasoning" || family.name != "Latent Reasoning" || family.query.Arxiv != `ti:"latent reasoning"` {
		t.Fatalf("family: %+v", family)
	}
	if slices.ContainsFunc(found, func(c liveCandidate) bool { return strings.HasPrefix(c.gram, "evolving agent") }) {
		t.Fatal("plural and singular forms fold together and a curated family is not rediscovered")
	}
}

func TestLiveUniquenessAndRelevance(t *testing.T) {
	a := LiveTopic{ID: "a", PaperIDs: []string{"1", "2", "3", "4"}}
	b := LiveTopic{ID: "b", PaperIDs: []string{"3", "4", "5"}}
	liveAnnotate([]LiveTopic{a}, []LiveTopic{b})
	if u := liveUniqueness(a.PaperIDs, []LiveTopic{a, b}, "a"); u != 0.5 {
		t.Fatalf("half of a is shared with b: %v", u)
	}
	papers := []StudyPaper{{Title: "Policy Distillation for Agents"}, {Title: "Something Else"}}
	if r := liveRelevance([]string{"дистилляция политик", "policy distillation"}, papers); r != 0.5 {
		t.Fatalf("relevance by translated term = %v, want 0.5", r)
	}
	if r := liveRelevance([]string{"policy distillation"}, papers); r != 0.5 {
		t.Fatalf("one of two titles matches: %v", r)
	}
}

func TestLiveRelevanceUsesWholeWords(t *testing.T) {
	tests := []struct {
		query, title string
		want         float64
	}{
		{query: "RAG", title: "Fragments of a training corpus", want: 0},
		{query: "RAG", title: "RAG: retrieval augmented generation", want: 1},
		{query: "test-time scaling", title: "Test Time Scaling for Reasoning", want: 1},
		{query: "policy, distillation", title: "Policy-Distillation for Agents", want: 1},
		{query: "модель", title: "Моделирование данных", want: 0},
		{query: "МОДЕЛЬ", title: "Новая модель: первые результаты", want: 1},
	}
	for _, tt := range tests {
		t.Run(tt.title, func(t *testing.T) {
			if got := liveRelevance([]string{tt.query}, []StudyPaper{{Title: tt.title}}); got != tt.want {
				t.Fatalf("relevance(%q, %q) = %v, want %v", tt.query, tt.title, got, tt.want)
			}
		})
	}
}

func TestLiveEmptyPaperSetHasNoUniquenessMeasurement(t *testing.T) {
	topics := []LiveTopic{{ID: "empty", PaperIDs: []string{}, Uniqueness: new(0.8)}}
	liveAnnotate(topics)
	if topics[0].Uniqueness != nil {
		t.Fatal("an empty result must not report measured zero uniqueness")
	}
}

func TestArxivPageWaitsOutTransientRateLimit(t *testing.T) {
	var calls int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls++
		if calls <= 3 {
			w.Header().Set("Retry-After", "45")
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		fmt.Fprint(w, `<feed xmlns="http://www.w3.org/2005/Atom" xmlns:opensearch="x">`+
			`<opensearch:totalResults>0</opensearch:totalResults></feed>`)
	}))
	defer server.Close()
	var waits []time.Duration
	src := newLiveSources(func(context.Context) string { return "" })
	src.arxiv = server.URL
	src.sleep = func(_ context.Context, d time.Duration) error {
		waits = append(waits, d)
		return nil
	}
	if _, err := src.arxivPage(t.Context(), "cat:cs.IR", 0, 10); err != nil {
		t.Fatalf("transient 429 must not fail the family: %v", err)
	}
	if calls != 4 {
		t.Fatalf("want 4 attempts, got %d", calls)
	}
	if len(waits) != 4 || waits[1] != 45*time.Second || waits[2] != 45*time.Second || waits[3] != 45*time.Second {
		t.Fatalf("Retry-After must drive the cooldown, got %v", waits)
	}
}

func TestArxivPageGivesUpAfterPersistentRateLimit(t *testing.T) {
	var calls int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls++
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer server.Close()
	src := newLiveSources(func(context.Context) string { return "" })
	src.arxiv = server.URL
	src.sleep = func(context.Context, time.Duration) error { return nil }
	_, err := src.arxivPage(t.Context(), "cat:cs.IR", 0, 10)
	if err == nil || !strings.Contains(err.Error(), "429") {
		t.Fatalf("persistent 429 must surface as a declared limitation, got %v", err)
	}
	if calls != liveArxivAttempts {
		t.Fatalf("want %d attempts, got %d", liveArxivAttempts, calls)
	}
}

func TestLiveSearchProgressNamesTheSourceAndItsWait(t *testing.T) {
	src := newLiveSources(func(context.Context) string { return "tok" })
	src.sleep = func(context.Context, time.Duration) error { return nil }
	seen := map[string]time.Duration{}
	src.progress = func(step string, wait time.Duration) { seen[step] = wait }

	src.step(liveStepArxiv, 26*time.Second)
	src.step(liveStepPatents, 0)
	if seen[liveStepArxiv] != 26*time.Second {
		t.Fatalf("ожидание источника не дошло до наблюдателя: %v", seen)
	}
	if _, ok := seen[liveStepPatents]; !ok {
		t.Fatal("шаг без объявленного ожидания не отмечен")
	}
	// Ночной сбор наблюдателя не имеет и обязан работать без него.
	quiet := newLiveSources(func(context.Context) string { return "tok" })
	quiet.step(liveStepArxiv, time.Second)

	if liveStepIndex(liveStepArxiv) != 1 || liveStepIndex(liveStepCards) != len(liveSearchSteps()) {
		t.Fatalf("порядок шагов сбился: %d, %d", liveStepIndex(liveStepArxiv), liveStepIndex(liveStepCards))
	}
	if liveStepIndex("нет такого") != 0 {
		t.Fatal("неизвестный шаг получил номер")
	}
}

func TestLiveSearchRejectsBadQueryBeforeStartingWork(t *testing.T) {
	if _, err := normalizeLiveQuery("   "); err == nil {
		t.Fatal("пустой запрос принят")
	}
	if _, err := normalizeLiveQuery(strings.Repeat("а", liveQueryRunes+1)); err == nil {
		t.Fatal("слишком длинный запрос принят")
	}
	if _, err := normalizeLiveQuery(" Undefined "); err == nil {
		t.Fatal("артефакт JavaScript принят как запрос")
	}
	// Предел считается в символах, а не в байтах: кириллица занимает по два байта.
	for _, query := range []string{
		"слабые сигналы в области кибербезопасности",
		"зарождающиеся технологии в области кибербезопасности и защиты ИИ",
		strings.Repeat("а", liveQueryRunes),
	} {
		if _, err := normalizeLiveQuery(query); err != nil {
			t.Fatalf("русский запрос из постановки задачи отклонён: %q %v", query, err)
		}
	}
	got, err := normalizeLiveQuery("  GPU   Server ")
	if err != nil || got != "gpu server" {
		t.Fatalf("нормализация: %q %v", got, err)
	}
}

func TestLiveSearchSurvivesArxivOutage(t *testing.T) {
	server := liveTestServer(t)
	defer server.Close()
	newSrc := func() *liveSources {
		src := newLiveSources(func(context.Context) string { return "tok" })
		src.arxiv = server.URL + "/arxiv-429"
		src.github, src.hn = server.URL+"/github", server.URL+"/hn"
		src.sleep = func(context.Context, time.Duration) error { return nil }
		src.patience = liveSearchPatience
		return src
	}
	periods := livePeriods(time.Date(2026, 9, 10, 12, 0, 0, 0, time.UTC))

	topic, _, err := liveCollect(t.Context(), newSrc(), liveFamilies()[0], periods)
	if err != nil {
		t.Fatalf("поиск умер вместе с arXiv: %v", err)
	}
	if !slices.ContainsFunc(topic.Limits, func(l LiveLimit) bool { return l.Code == liveLimitArxivOff }) {
		t.Fatalf("отказ arXiv не назван в ограничениях: %+v", topic.Limits)
	}
	if topic.PapersCount != 0 {
		t.Fatalf("работы взялись из недоступного источника: %d", topic.PapersCount)
	}

	// Отменённый поиск — отказ, а не «ничего не нашли» на сутки в кэше.
	canceled, cancel := context.WithCancel(t.Context())
	cancel()
	if _, _, err = liveCollect(canceled, newSrc(), liveFamilies()[0], periods); err == nil {
		t.Fatal("отменённый поиск отдал результат")
	}

	// Источник, просящий больше потолка терпения, отпускается сразу: пять минут ради одного канала из шести не стоят того.
	quick := newSrc()
	// GitHub и HN в живом поиске спрашиваются одновременно: счётчик общий.
	var mu sync.Mutex
	waited := time.Duration(0)
	quick.sleep = func(_ context.Context, d time.Duration) error {
		mu.Lock()
		defer mu.Unlock()
		waited += d
		return nil
	}
	if _, _, err = liveCollect(t.Context(), quick, liveFamilies()[0], periods); err != nil {
		t.Fatalf("поиск снова умер вместе с arXiv: %v", err)
	}
	if waited > liveSearchPatience {
		t.Fatalf("поиск прождал закрытый источник %s при потолке %s", waited, liveSearchPatience)
	}
}
