package application

import (
	"context"
	"fmt"

	"github.com/example/llm-service/internal/domain"
)

type cacheRevisionKey struct{}

// SetCacheRevision связывает кэш с динамическим выбором модели и версии конвейера.
func (s *RAGService) SetCacheRevision(revision func(context.Context) string) {
	s.cacheRevision = revision
}

func (s *RAGService) currentCacheRevision(ctx context.Context) string {
	revision := fmt.Sprintf("rag-answer-v4/%g/%t/%d/%g/%g/%d/%d", s.scoreFloor, s.rerankerActive,
		s.maxPerDoc, s.vecWeight, s.lexWeight, s.retrievalMult, s.retrievalMin)
	if s.agentic != nil {
		revision += fmt.Sprintf("/agentic:%+v", s.agentic.cfg)
	}
	if s.cacheRevision != nil {
		revision += "/generation:" + s.cacheRevision(ctx)
	}
	return revision
}

func (s *RAGService) answerCacheScope(ctx context.Context, q domain.Query) string {
	k := q.TopK
	if k <= 0 {
		k = s.topK
	}
	revision, ok := ctx.Value(cacheRevisionKey{}).(string)
	if !ok {
		revision = s.currentCacheRevision(ctx)
	}
	return fmt.Sprintf("%s\x00top_k:%d\x00%s", docScopeKey(q), k, revision)
}
