package application

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"testing"
	"time"
)

// Синтетическая фикстура не поймает ошибку в самой схеме: разбирается сохранённый ответ живого oaipmh.arxiv.org.
func TestArxivOAIRealResponse(t *testing.T) {
	fixture, err := os.ReadFile("testdata/arxiv-oai-cs-ir.xml")
	if err != nil {
		t.Fatalf("фикстура: %v", err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write(fixture)
	}))
	defer server.Close()
	src := newLiveSources(func(context.Context) string { return "" })
	src.oai = server.URL
	src.sleep = func(context.Context, time.Duration) error { return nil }

	periods := livePeriods(time.Date(2026, 9, 13, 12, 0, 0, 0, time.UTC))
	papers, err := src.arxivHarvest(t.Context(), periods,
		liveDay(periods[0].Start), liveDay(periods[len(periods)-1].EndExclusive).AddDate(0, 0, -1))
	if err != nil {
		t.Fatalf("разбор настоящего ответа: %v", err)
	}
	if len(papers) == 0 {
		t.Fatal("из настоящего ответа не получилось ни одной работы")
	}
	first := papers[0]
	if first.Title == "" || len(first.Authors) == 0 || len(first.Date) != 10 {
		t.Fatalf("работа разобрана наполовину: %+v", first)
	}
	meta, ok := src.metadata[first.ID]
	if !ok || meta.Abstract == "" || meta.Category == "" {
		t.Fatalf("метаданные не собраны: %+v", meta)
	}
	// Ревизии старых статей приходят в выдаче и обязаны отсеиваться по месяцу идентификатора.
	for _, paper := range papers {
		if paper.Date < periods[0].Start {
			t.Fatalf("статья, поданная до окна, попала в ряд: %s от %s", paper.ID, paper.Date)
		}
	}
}

// Запускается явно:
//
//	TR_ARXIV_OAI_LIVE=1 go test ./internal/application/ -run ArxivOAIWindowLive -v -timeout 40m
func TestArxivOAIWindowLive(t *testing.T) {
	if os.Getenv("TR_ARXIV_OAI_LIVE") == "" {
		t.Skip("set TR_ARXIV_OAI_LIVE=1 to run the live arXiv OAI-PMH check")
	}
	months := 14
	if v, err := strconv.Atoi(os.Getenv("TR_ARXIV_OAI_MONTHS")); err == nil && v > 0 {
		months = v
	}
	src := newLiveSources(func(context.Context) string { return "" })
	all := livePeriods(time.Now().UTC())
	periods := all[max(0, len(all)-months):]

	start := time.Now()
	papers, err := src.arxivHarvest(t.Context(), periods,
		liveDay(periods[0].Start), liveDay(periods[len(periods)-1].EndExclusive).AddDate(0, 0, -1))
	took := time.Since(start)
	if err != nil {
		t.Fatalf("живое перечисление упало через %s: %v", took.Round(time.Second), err)
	}
	population := make([]int, len(periods))
	for _, paper := range papers {
		if i := livePeriodIndex(periods, paper.Date); i >= 0 {
			population[i]++
		}
	}
	t.Logf("окно %s..%s: %d работ за %s", periods[0].ID, periods[len(periods)-1].ID,
		len(papers), took.Round(time.Second))
	for i, period := range periods {
		t.Logf("  %s: %d", period.ID, population[i])
	}
	if len(papers) == 0 {
		t.Fatal("живое окно пустое")
	}
}
