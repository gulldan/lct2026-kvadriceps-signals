package application

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"sync"

	commonv1 "github.com/example/main-service/internal/platform/genproto/common/v1"
	"github.com/example/main-service/internal/platform/indexdoc"
	"github.com/example/main-service/internal/platform/sourcedoc"
)

// sourceAccessConcurrency: восьми хватает, чтобы 56 пачек уложились в один-два round-trip, и db-service
// не заваливается разовым открытием списка.
const sourceAccessConcurrency = 8

// filterSourceDocuments применяется до пагинации, фильтров и подсчётов; видимость старых документов
// без происхождения по-прежнему решает маршрут вызывающего.
func (s *IngestionService) filterSourceDocuments(ctx context.Context, reader string,
	docs []*commonv1.Document) ([]*commonv1.Document, error) {
	ids := []string{}
	for _, doc := range docs {
		if doc.GetSourceMetadata() != "" {
			ids = append(ids, doc.GetId())
		}
	}
	if len(ids) == 0 {
		return docs, nil
	}
	if s.sourceIndexes == nil {
		return nil, errors.New("source access service unavailable")
	}
	// Пачки параллельно: 256 id на запрос — граница контракта db-service, а последовательный обход 56 пачек
	// давал 0,88 с на открытие списка при 11 мс самого SQL.
	batches := slices.Collect(slices.Chunk(ids, 256))
	states := make([]indexdoc.States, len(batches))
	errs := make([]error, len(batches))
	var wg sync.WaitGroup
	slots := make(chan struct{}, sourceAccessConcurrency)
	for i, batch := range batches {
		select {
		case slots <- struct{}{}:
		case <-ctx.Done():
			wg.Wait()
			return nil, fmt.Errorf("wait for source access batch: %w", ctx.Err())
		}
		wg.Go(func() {
			defer func() { <-slots }()
			states[i], errs[i] = s.sourceIndexes.ReadIndexes(ctx, indexdoc.Read{
				ReaderID: reader, SharedCorpus: s.sharedCorpus, DocumentIDs: batch, AccessOnly: true,
			})
		})
	}
	wg.Wait()
	if err := errors.Join(errs...); err != nil {
		return nil, err
	}
	allowed := map[string]bool{}
	for _, batch := range states {
		for _, state := range batch.Documents {
			allowed[state.DocumentID] = state.Allowed
		}
	}
	out := make([]*commonv1.Document, 0, len(docs))
	for _, doc := range docs {
		if doc.GetSourceMetadata() == "" || allowed[doc.GetId()] {
			out = append(out, doc)
		}
	}
	return latestPaperDocuments(out), nil
}

// latestPaperDocuments оставляет одно представление работы; неизменяемые ревизии по-прежнему доступны
// по id в анализах, цитатах и истории.
func latestPaperDocuments(docs []*commonv1.Document) []*commonv1.Document {
	type paper struct {
		index  int
		source *sourcedoc.Source
	}
	chosen := map[[4]string]paper{}
	out := make([]*commonv1.Document, 0, len(docs))
	for _, doc := range docs {
		source, err := sourcedoc.Decode([]byte(doc.GetSourceMetadata()))
		if err != nil || source.Kind != "paper" {
			out = append(out, doc)
			continue
		}
		key := [4]string{doc.GetOwnerId(), source.AccessScope, source.CanonicalID, source.TestRunID}
		old, exists := chosen[key]
		if !exists {
			chosen[key] = paper{len(out), source}
			out = append(out, doc)
			continue
		}
		quality, priorQuality := paperTextQuality(source.TextStatus), paperTextQuality(old.source.TextStatus)
		if quality > priorQuality || quality == priorQuality && source.ObservedAt.After(old.source.ObservedAt) {
			out[old.index] = doc
			chosen[key] = paper{old.index, source}
		}
	}
	return out
}

func paperTextQuality(status string) int {
	switch status {
	case "full_text", "pdf":
		return 4
	case "":
		return 3 // старое содержимое неизвестной полноты важнее заведомых аннотации или заголовка
	case "abstract_only", "title_and_abstract":
		return 2
	case "title_only", "metadata_only":
		return 1
	default:
		return 0
	}
}
