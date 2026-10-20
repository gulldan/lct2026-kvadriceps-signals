package application

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// В окно идёт первая версия препринта с датой её появления; ревизии и чужие месяцы — нет.
func TestRxivHarvestKeepsFirstVersions(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.URL.Path, "/details/biorxiv/2026-09-01/") {
			fmt.Fprint(w, `{"messages":[{"status":"no posts found"}],"collection":[]}`)
			return
		}
		entry := `{"doi":"10.1101/2026.09.02.1","title":"Phage  therapy","authors":"Doe, J.; Roe, R.",` +
			`"date":"2026-09-02","version":"1","category":"Microbiology","abstract":"Phages."}`
		if strings.HasSuffix(r.URL.Path, "/1/json") {
			entry = `{"doi":"10.1101/2025.01.01.9","date":"2026-09-03","version":"2","title":"Old work"}`
		}
		fmt.Fprintf(w, `{"messages":[{"status":"ok","total":"2"}],"collection":[%s]}`, entry)
	}))
	defer server.Close()
	src := newLiveSources(func(context.Context) string { return "" })
	src.rxiv, src.sleep = server.URL, func(context.Context, time.Duration) error { return nil }

	periods := livePeriods(time.Date(2026, 9, 13, 0, 0, 0, 0, time.UTC))
	papers, err := src.rxivHarvest(t.Context(), periods, time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC),
		time.Date(2026, 9, 12, 0, 0, 0, 0, time.UTC))
	if err != nil || len(papers) != 1 {
		t.Fatalf("one first version expected: %v %+v", err, papers)
	}
	paper, meta := papers[0], src.metadata[papers[0].ID]
	if paper.ID != "biorxiv:10.1101/2026.09.02.1" || paper.Title != "Phage therapy" || len(paper.Authors) != 2 ||
		meta.Category != "biorxiv.microbiology" || liveBranch(meta.Category) != liveBranchLife {
		t.Fatalf("preprint recorded wrong: %+v %+v", paper, meta)
	}
}

// У Crossref ревизия — отдельный DOI с суффиксом версии, а аннотация в разметке JATS.
func TestCrossrefKeepsFirstVersionAndPlainAbstract(t *testing.T) {
	periods := livePeriods(time.Date(2026, 9, 13, 0, 0, 0, 0, time.UTC))
	src := newLiveSources(func(context.Context) string { return "" })
	var work crossrefWork
	work.DOI, work.Title, work.GroupTitle = "10.26434/chemrxiv-2026-abc-v1", []string{"Single-atom catalysts"}, "Catalysis"
	work.Abstract = "<jats:p>Atoms <jats:italic>alone</jats:italic>.</jats:p>"
	work.Posted.DateParts = [][]int{{2026, 9, 2}}
	paper, ok := src.recordCrossref("chemrxiv", work, periods)
	if !ok || paper.Date != "2026-09-02" || src.metadata[paper.ID].Abstract != "Atoms alone ." ||
		liveBranch(src.metadata[paper.ID].Category) != liveBranchMaterials {
		t.Fatalf("first version recorded wrong: %v %+v %+v", ok, paper, src.metadata[paper.ID])
	}
	work.DOI = "10.31234/osf.io/abcde_v2"
	if _, ok := src.recordCrossref("psyarxiv", work, periods); ok {
		t.Fatal("a revision must not enter the window as new work")
	}
}

// Работа из окна препринтов — препринт с названием сервера, а не «тип издателя не подтверждён».
func TestPreprintPassportByDOIPrefix(t *testing.T) {
	for _, address := range []string{"https://doi.org/10.64898/2026.09.02.1", "https://doi.org/10.26434/chemrxiv-2026-abc"} {
		if passport := ClassifySource(address); passport.Publisher != PublisherPreprint {
			t.Fatalf("%s classified as %+v", address, passport)
		}
	}
	if ClassifySource("https://doi.org/10.1038/s41586-026-0001").Publisher == PublisherPreprint {
		t.Fatal("a journal DOI became a preprint")
	}
	metadata := map[string]LivePaperMetadata{"biorxiv:10.64898/x": {Abstract: "Phages lyse bacteria."}}
	papers := []StudyPaper{{ID: "biorxiv:10.64898/x", Title: "Phage therapy", URL: "https://doi.org/10.64898/x"}}
	stampPaperPassports(papers, metadata)
	card := livePaperPassports(papers, metadata)["biorxiv:10.64898/x"]
	if card.SourceName != "bioRxiv" || card.Language != "en" || card.PublisherType != PublisherPreprint {
		t.Fatalf("preprint passport: %+v", card)
	}
}
