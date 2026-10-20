package application

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"slices"
	"strings"
	"testing"
	"time"
)

const pubmedArticleFixture = `<PubmedArticle><MedlineCitation><PMID>%s</PMID><Article>
<ArticleTitle>%s</ArticleTitle><Abstract>
<AbstractText Label="RESULTS">Delivery with <i>lipid nanoparticles</i>.</AbstractText></Abstract>
<Journal><Title>Test Biomedical Journal</Title><JournalIssue>
<PubDate><Year>2026</Year><Month>Sep</Month><Day>12</Day></PubDate></JournalIssue></Journal>
<ArticleDate DateType="Electronic"><Year>2026</Year><Month>08</Month><Day>14</Day></ArticleDate>
<AuthorList><Author><LastName>Lee</LastName><ForeName>Ann</ForeName></Author></AuthorList>
<Language>eng</Language><PublicationTypeList><PublicationType>Journal Article</PublicationType></PublicationTypeList>
</Article></MedlineCitation><PubmedData><ArticleIdList>
<ArticleId IdType="doi">10.1000/alpha</ArticleId></ArticleIdList></PubmedData></PubmedArticle>`

func TestPubMedSearchMergeAndPassport(t *testing.T) {
	first := fmt.Sprintf(pubmedArticleFixture, "123", "Targeted <i>lipid nanoparticles</i> for delivery")
	second := fmt.Sprintf(pubmedArticleFixture, "124", "Different title of the same DOI")
	undated := strings.ReplaceAll(fmt.Sprintf(pubmedArticleFixture, "125", "An undated scientific publication"),
		"<Day>14</Day>", "")
	undated = strings.ReplaceAll(undated, "<Day>12</Day>", "")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		if q.Get("db") != "pubmed" || q.Get("tool") == "" {
			t.Errorf("потеряны параметры НЦБИ: %s", r.URL)
		}
		switch r.URL.Path {
		case "/esearch.fcgi":
			if q.Get("term") != `"lipid nanoparticles"[Title/Abstract]` || q.Get("mindate") != "2026-08-01" ||
				q.Get("maxdate") != "2026-09-30" || q.Get("datetype") != "pdat" || q.Get("retmax") != "200" {
				t.Errorf("неверный поисковый запрос: %s", r.URL)
			}
			_, _ = w.Write([]byte(`{"esearchresult":{"count":"3","idlist":["123","124","125"]}}`))
		case "/efetch.fcgi":
			if q.Get("id") != "123,124,125" {
				t.Errorf("запрошены другие статьи: %s", r.URL)
			}
			_, _ = fmt.Fprintf(w, "<PubmedArticleSet>%s%s%s</PubmedArticleSet>", first, second, undated)
		default:
			t.Errorf("неизвестный запрос: %s", r.URL)
		}
	}))
	t.Cleanup(server.Close)
	src := &liveSources{pubmed: &livePubMedClient{http: server.Client(), base: server.URL}}
	papers, limits := src.withScholar(t.Context(), nil, nil, []string{"lipid nanoparticles"}, scholarPeriods())
	if len(papers) != 1 {
		t.Fatalf("ожидали одну работу после дедупликации DOI и отсева без даты: %+v", papers)
	}
	paper, meta := papers[0], src.metadata[papers[0].ID]
	if paper.ID != "pubmed:123" || paper.Title != "Targeted lipid nanoparticles for delivery" ||
		paper.Date != "2026-08-14" || paper.CanonicalID != "10.1000/alpha" ||
		paper.URL != "https://pubmed.ncbi.nlm.nih.gov/123/" || !slices.Equal(paper.Authors, []string{"Ann Lee"}) ||
		meta.Abstract != "RESULTS: Delivery with lipid nanoparticles." || meta.CitedBy != nil {
		t.Fatalf("метаданные или первая дата публикации потеряны: %+v %+v", paper, meta)
	}
	if !slices.ContainsFunc(limits, func(l LiveLimit) bool {
		return l.Code == liveLimitPubMedCapped && l.Values["taken"] == 2 && l.Values[liveValueFound] == 3
	}) || !liveTemporalCoverageLimited(limits) {
		t.Fatalf("неполное временное покрытие скрыто: %+v", limits)
	}
	stampPaperPassports(papers, src.metadata)
	passport := livePaperPassports(papers, src.metadata)[paper.ID]
	if passport.DiscoverySource != "pubmed" || passport.SourceName != "Test Biomedical Journal" ||
		passport.Language != "en" || passport.PublisherType != PublisherAggregator || passport.Trust != TrustMedium {
		t.Fatalf("паспорт PubMed: %+v", passport)
	}
	// Уже найденная через другой канал работа не увеличивает число публикаций и авторских групп.
	existing := paper
	existing.ID, existing.Title = "openalex:10.1000/alpha", "Journal landing title"
	merged, _ := liveScholarMerge([]StudyPaper{existing}, nil, papers, src.metadata)
	if len(merged) != 1 || merged[0].ID != existing.ID {
		t.Fatalf("повтор через другой агрегатор учтён дважды: %+v", merged)
	}
	old := strings.ReplaceAll(first, "<Year>2026</Year><Month>08</Month>", "<Year>2024</Year><Month>08</Month>")
	oldPapers, _, err := livePubMedPapers([]byte("<PubmedArticleSet>"+old+"</PubmedArticleSet>"), []string{"123"}, scholarPeriods())
	if err != nil || len(oldPapers) != 0 {
		t.Fatalf("поздняя печать сделала старую работу новой: %+v %v", oldPapers, err)
	}
}

func TestPubMedFailuresAreNotZeroEvidence(t *testing.T) {
	for _, scenario := range []string{"rate limit", "bad JSON", "missing count", "bad ID", "bad XML", "fetch failure"} {
		t.Run(scenario, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch {
				case scenario == "rate limit", scenario == "fetch failure" && r.URL.Path == "/efetch.fcgi":
					w.WriteHeader(http.StatusTooManyRequests)
				case scenario == "bad JSON":
					_, _ = w.Write([]byte("not JSON"))
				case scenario == "missing count":
					_, _ = w.Write([]byte(`{"esearchresult":{"idlist":[]}}`))
				case scenario == "bad ID":
					_, _ = w.Write([]byte(`{"esearchresult":{"count":"1","idlist":["123&term=other"]}}`))
				case r.URL.Path == "/efetch.fcgi":
					_, _ = w.Write([]byte(`<ERROR>unavailable</ERROR>`))
				default:
					_, _ = w.Write([]byte(`{"esearchresult":{"count":"1","idlist":["123"]}}`))
				}
			}))
			t.Cleanup(server.Close)
			src := &liveSources{pubmed: &livePubMedClient{http: server.Client(), base: server.URL}}
			papers, _, limits := src.livePubMedSearch(t.Context(), []string{"lipid nanoparticles"}, scholarPeriods())
			if len(papers) != 0 || !slices.ContainsFunc(limits, func(l LiveLimit) bool { return l.Code == liveLimitPubMedOff }) {
				t.Fatalf("ошибка превратилась в пустой успешный ответ: %+v %+v", papers, limits)
			}
		})
	}
	// Отменённое ожидание ограничителя не выполняет сетевой запрос.
	client := &livePubMedClient{next: time.Now().Add(time.Hour)}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := client.get(ctx, "esearch", nil); err == nil {
		t.Fatal("отменённый запрос продолжил работу")
	}
	service := NewTrendLiveService(nil, nil)
	if a, b := service.newSources(), service.newSources(); a.pubmed == nil || a.pubmed != b.pubmed {
		t.Fatal("поиски получили разные ограничители запросов PubMed")
	}
}

// Явно включаемый сетевой прогон не входит в обычные юниты и не зависит от ключей или живого стенда.
func TestPubMedLiveDomains(t *testing.T) {
	if os.Getenv("RADAR_PUBMED_LIVE_TEST") != "1" {
		t.Skip("set RADAR_PUBMED_LIVE_TEST=1 to query NCBI")
	}
	src := &liveSources{pubmed: newLivePubMedClient()}
	for _, phrase := range []string{
		"lipid nanoparticles", "photoredox catalysis", "solid oxide electrolysis", "retrieval augmented generation",
	} {
		t.Run(phrase, func(t *testing.T) {
			papers, _, limits := src.livePubMedSearch(t.Context(), []string{phrase}, livePeriods(time.Now()))
			if slices.ContainsFunc(limits, func(l LiveLimit) bool { return l.Code == liveLimitPubMedOff }) || len(papers) == 0 {
				t.Fatalf("PubMed не вернул работы: %+v", limits)
			}
			t.Logf("%s: %d dated papers, first %s; limits %+v", phrase, len(papers), papers[0].URL, limits)
		})
	}
}
