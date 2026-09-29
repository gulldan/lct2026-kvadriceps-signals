package grpcserver

import (
	"context"
	"strings"
	"time"

	"google.golang.org/genproto/googleapis/rpc/errdetails"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/durationpb"

	"github.com/example/llm-service/internal/application"
	"github.com/example/llm-service/internal/domain"
	"github.com/example/llm-service/internal/platform/logger"

	commonv1 "github.com/example/llm-service/internal/platform/genproto/common/v1"
	llmv1 "github.com/example/llm-service/internal/platform/genproto/llm/v1"
)

// Server ограничивает число одновременных конвейеров: при всплеске лишние ждут maxWait, а дальше
// сразу получают ResourceExhausted (на входе это 503), чтобы не положить бэкенды.
type Server struct {
	llmv1.UnimplementedRagServiceServer
	svc     *application.RAGService
	slots   chan struct{}
	maxWait time.Duration
}

func New(svc *application.RAGService, maxConcurrent int, queueWait time.Duration) *Server {
	if maxConcurrent < 1 {
		maxConcurrent = 1
	}
	return &Server{
		UnimplementedRagServiceServer: llmv1.UnimplementedRagServiceServer{},
		svc:                           svc,
		slots:                         make(chan struct{}, maxConcurrent),
		maxWait:                       queueWait,
	}
}

func (s *Server) Answer(ctx context.Context, req *commonv1.RagRequest) (*commonv1.RagResponse, error) {
	owner := strings.TrimSpace(req.GetOwnerId())
	query := strings.TrimSpace(req.GetQuery())
	if owner == "" || query == "" {
		return nil, status.Error(codes.InvalidArgument, "owner_id and query are required")
	}

	release, err := s.acquire(ctx)
	if err != nil {
		return nil, err
	}
	defer release()

	res, err := s.svc.Answer(ctx, domain.Query{
		OwnerID: owner, Text: query, TopK: int(req.GetTopK()), Prompt: req.GetPrompt(),
		ScopeDocumentIDs:   req.GetScopeDocumentIds(),
		ExcludeDocumentIDs: req.GetExcludeDocumentIds(),
	})
	if err != nil {
		logger.From(ctx).Error().Err(err).Msg("rag answer failed")
		return nil, status.Error(codes.Internal, "failed to answer query")
	}

	sources := make([]*commonv1.Source, 0, len(res.Sources))
	for _, src := range res.Sources {
		sources = append(sources, &commonv1.Source{
			DocumentId:          src.DocumentID,
			Filename:            src.Filename,
			ChunkId:             src.ChunkID,
			Snippet:             src.Snippet,
			Score:               src.Score,
			CharStart:           int32(src.CharStart),
			CharEnd:             int32(src.CharEnd),
			PageStart:           int32(src.PageStart),
			PageEnd:             int32(src.PageEnd),
			SectionHeading:      src.SectionHeading,
			Origin:              src.Origin,
			RaptorSummaryId:     src.RaptorSummaryID,
			SourceIndexSnapshot: src.IndexSnapshot,
		})
	}
	return &commonv1.RagResponse{
		Answer: res.Answer, Sources: sources, Model: res.Model, Cached: res.Cached,
		Trace: toProtoTrace(res.Trace),
	}, nil
}

func toProtoTrace(t *domain.Trace) *commonv1.RagTrace {
	if t == nil {
		return nil
	}
	stages := make([]*commonv1.RagStage, 0, len(t.Stages))
	for _, st := range t.Stages {
		stages = append(stages, &commonv1.RagStage{Stage: st.Stage, Millis: st.Millis})
	}
	return &commonv1.RagTrace{
		Stages:             stages,
		CandidatesDense:    int32(t.CandidatesDense),
		CandidatesLexical:  int32(t.CandidatesLexical),
		CandidatesFused:    int32(t.CandidatesFused),
		CandidatesReturned: int32(t.CandidatesReturned),
		Degraded:           t.Degraded,
		DegradedReason:     t.DegradedReason,
		AbstainReason:      t.AbstainReason,
		TopScore:           t.TopScore,
		ScoreFloor:         t.ScoreFloor,
		Cited:              t.Cited,
		UncitedRemoved:     t.UncitedRemoved,
		RaptorExpanded:     int32(t.RaptorExpanded),
	}
}

// DocumentChunks идёт мимо очереди: чтение фрагментов дешёвое и должно отвечать даже при забитом конвейере.
func (s *Server) DocumentChunks(
	ctx context.Context, req *llmv1.DocumentChunksRequest,
) (*llmv1.DocumentChunksResponse, error) {
	owner := strings.TrimSpace(req.GetOwnerId())
	docID := strings.TrimSpace(req.GetDocumentId())
	if owner == "" || docID == "" {
		return nil, status.Error(codes.InvalidArgument, "owner_id and document_id are required")
	}
	chunks, err := s.svc.Chunks(ctx, owner, docID)
	if err != nil {
		logger.From(ctx).Error().Err(err).Msg("document chunks failed")
		return nil, status.Error(codes.Internal, "failed to read document chunks")
	}
	out := make([]*llmv1.DocumentChunk, 0, len(chunks))
	for _, c := range chunks {
		out = append(out, &llmv1.DocumentChunk{Id: c.ID, Index: int32(c.Index), Text: c.Text})
	}
	return &llmv1.DocumentChunksResponse{Chunks: out}, nil
}

func (s *Server) acquire(ctx context.Context) (func(), error) {
	timer := time.NewTimer(s.maxWait)
	defer timer.Stop()
	select {
	case s.slots <- struct{}{}:
		return func() { <-s.slots }, nil
	case <-ctx.Done():
		return nil, status.FromContextError(ctx.Err()).Err()
	case <-timer.C:
		busy, err := status.New(codes.ResourceExhausted, "rag pipeline at capacity").WithDetails(
			&errdetails.RetryInfo{RetryDelay: durationpb.New(time.Second)})
		if err != nil {
			return nil, status.Error(codes.Unavailable, "rag pipeline at capacity")
		}
		return nil, busy.Err()
	}
}
