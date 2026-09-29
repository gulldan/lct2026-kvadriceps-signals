package application

import (
	"context"
	"errors"
	"testing"
	"time"

	commonv1 "github.com/example/main-service/internal/platform/genproto/common/v1"
)

// Неизвестный документ отвечает ошибкой, как db-service.
type fakeDocuments struct{ byID map[string]*commonv1.Document }

func (f *fakeDocuments) GetDocument(_ context.Context, id string) (*commonv1.Document, error) {
	if document, ok := f.byID[id]; ok {
		return document, nil
	}
	return nil, errors.New("not found")
}

func corpusSources(ids ...string) []*commonv1.Source {
	sources := make([]*commonv1.Source, 0, len(ids))
	for _, id := range ids {
		sources = append(sources, &commonv1.Source{DocumentId: id, Filename: id + ".pdf"})
	}
	return sources
}

func TestLiveCorpusSearchBucketsDocumentsByDate(t *testing.T) {
	now := time.Date(2026, 9, 17, 12, 0, 0, 0, time.UTC)
	periods := livePeriods(now)
	inWindow := periods[len(periods)-1].Start
	documents := &fakeDocuments{byID: map[string]*commonv1.Document{
		"a": {Id: "a", Title: "Гидрометаллургия рения", PublishedAt: inWindow + "T00:00:00Z"},
		"b": {Id: "b", CreatedAt: inWindow + "T10:00:00Z"},
		"c": {Id: "c", PublishedAt: "1999-01-01T00:00:00Z"},
	}}
	src := newLiveSources(nil)
	src.answerer = &fakeAnswerer{sources: corpusSources("a", "b", "a", "c")}
	src.documents = documents

	channel := liveCorpusSearch(t.Context(), src, "owner", []string{"рений"}, periods)
	if channel == nil {
		t.Fatal("канал своей базы не собран")
	}
	if channel.Total != 2 {
		t.Fatalf("записей %d, ожидалось 2 (повтор склеен, документ вне окна отброшен)", channel.Total)
	}
	titles := map[string]bool{}
	for _, record := range channel.Records {
		titles[record.Title] = true
		if record.Trust != "high" || record.PublisherType != "own" {
			t.Fatalf("паспорт своего документа не проставлен: %+v", record)
		}
	}
	if !titles["Гидрометаллургия рения"] {
		t.Error("настоящее название документа не заменило имя файла")
	}
	if !titles["b.pdf"] {
		t.Error("документ без названия обязан остаться под именем файла")
	}
	last := channel.Counts[len(channel.Counts)-1]
	if last == nil || *last != 2 {
		t.Fatalf("последний период считает %v, ожидалось 2", last)
	}
}

func TestLiveCorpusSearchReportsRefusalAsLimit(t *testing.T) {
	periods := livePeriods(time.Date(2026, 9, 17, 12, 0, 0, 0, time.UTC))
	src := newLiveSources(nil)
	src.answerer = &fakeAnswerer{err: errors.New("llm-service недоступен")}

	channel := liveCorpusSearch(t.Context(), src, "owner", []string{"рений"}, periods)
	if channel == nil || len(channel.Limits) != 1 || channel.Total != 0 {
		t.Fatalf("отказ шлюза не стал оговоркой канала: %+v", channel)
	}
}

func TestLiveCorpusSearchSkippedWithoutOwner(t *testing.T) {
	periods := livePeriods(time.Date(2026, 9, 17, 12, 0, 0, 0, time.UTC))
	src := newLiveSources(nil)
	src.answerer = &fakeAnswerer{sources: corpusSources("a")}
	if channel := liveCorpusSearch(t.Context(), src, "", []string{"рений"}, periods); channel != nil {
		t.Fatalf("канал собран без владельца: %+v", channel)
	}
}

func TestLiveCorpusStrippedBeforeSharedStore(t *testing.T) {
	detail := LiveDetail{
		Topic:      LiveTopic{Corpus: &LiveChannel{Total: 3}},
		Candidates: []LiveTopic{{Corpus: &LiveChannel{Total: 1}}},
	}
	stripped := liveCorpusStripped(detail)
	if stripped.Topic.Corpus != nil || stripped.Candidates[0].Corpus != nil {
		t.Fatal("канал загруженных документов остался в ответе, который переиспользуют другие")
	}
}
