package application

import (
	"context"
	commonv1 "github.com/example/chunk-splitter/internal/platform/genproto/common/v1"
	"github.com/example/chunk-splitter/internal/platform/logger"
	"strings"
)

// Вид документа пишется всегда, чтобы переиндексация стирала устаревший класс; сбои терпимы.
func (ix *Indexer) maybeSetTitleAndKind(ctx context.Context, evt *commonv1.DocumentParsed, text string) string {
	if ix.capture != nil && ix.capture.pinnedTitle {
		// Захваченный источник берёт сохранённый заголовок, а не зовёт генерацию на каждой пересборке снапшота.
		return ix.capture.title
	}
	var title, llmKind string
	if ix.titler != nil && ix.titles != nil {
		title, llmKind = ix.titler.Extract(ctx, text)
	}
	if title != "" {
		if err := ix.titles.SetDocumentTitle(ctx, evt.GetDocumentId(), title); err != nil {
			logger.From(ctx).Warn().Err(err).Str("document_id", evt.GetDocumentId()).Msg("set document title failed")
		}
	}
	if ix.kinds != nil {
		kind := docKind(text, llmKind)
		if err := ix.kinds.SetDocumentKind(ctx, evt.GetDocumentId(), kind); err != nil {
			logger.From(ctx).Warn().Err(err).Str("document_id", evt.GetDocumentId()).Msg("set document kind failed")
		}
	}
	return title
}

const docKindHypotheses = "hypotheses"

func docKind(text, llmKind string) string {
	if llmKind == docKindHypotheses || heuristicDocKind(text) == docKindHypotheses {
		return docKindHypotheses
	}
	return ""
}

// heuristicDocKind ловит начала вида «Гипотезы по результатам мозгового штурма: 1. …».
func heuristicDocKind(text string) string {
	head := strings.ToLower(strings.TrimSpace(text))
	if r := []rune(head); len(r) > 400 {
		head = string(r[:400])
	}
	if head == "" {
		return ""
	}
	firstLine := head
	if nl := strings.IndexAny(firstLine, "\r\n"); nl >= 0 {
		firstLine = strings.TrimSpace(firstLine[:nl])
	}
	numbered := strings.Contains(head, "1.") || strings.Contains(head, "1)")
	if strings.HasPrefix(firstLine, "гипотез") && numbered {
		return docKindHypotheses
	}
	if strings.Contains(head, "гипотез") && strings.Contains(head, "мозгово") {
		return docKindHypotheses
	}
	return ""
}

func (ix *Indexer) maybeSetMeta(ctx context.Context, evt *commonv1.DocumentParsed) {
	if ix.meta == nil {
		return
	}
	md := evt.GetMetadata()
	author, published, ref := md["author"], md["published_at"], md["source_ref"]
	if author == "" && published == "" && ref == "" {
		return
	}
	if err := ix.meta.SetDocumentMeta(ctx, evt.GetDocumentId(), author, published, ref); err != nil {
		logger.From(ctx).Warn().Err(err).Str("document_id", evt.GetDocumentId()).Msg("set document meta failed")
	}
}
