package grpcserver

import (
	"context"
	"fmt"
	"strconv"

	"github.com/example/db-service/internal/domain"
	"github.com/example/db-service/internal/platform/contracts"
	commonv1 "github.com/example/db-service/internal/platform/genproto/common/v1"
	dbv1 "github.com/example/db-service/internal/platform/genproto/db/v1"
	"google.golang.org/protobuf/proto"
)

type Ingestion struct {
	dbv1.UnimplementedIngestionControlServer
	repo domain.IngestionRepository
}

func NewIngestion(repo domain.IngestionRepository) *Ingestion { return &Ingestion{repo: repo} }

func (s *Ingestion) QueueEvent(
	ctx context.Context,
	in *dbv1.QueueDocumentEventRequest,
) (*dbv1.QueueDocumentEventResponse, error) {
	var event proto.Message
	var id string
	var generation int64
	if parsed := in.GetParsed(); parsed != nil && in.GetRoute() == contracts.RouteChunk {
		clone, ok := proto.Clone(parsed).(*commonv1.DocumentParsed)
		if !ok {
			return nil, toStatus(domain.ErrInvalidArgument)
		}
		clone.TraceId = ""
		event, id, generation = clone, clone.GetDocumentId(), max(1, clone.GetGeneration())
	} else if upload := in.GetUploaded(); upload != nil && in.GetRoute() != contracts.RouteChunk {
		clone, ok := proto.Clone(upload).(*commonv1.DocumentUploaded)
		if !ok {
			return nil, toStatus(domain.ErrInvalidArgument)
		}
		clone.TraceId = ""
		event, id, generation = clone, clone.GetDocumentId(), max(1, clone.GetGeneration())
	}
	if _, valid := contracts.ParserQueues()[in.GetRoute()]; !valid || event == nil || id == "" || proto.Size(event) > 5<<20 {
		return nil, toStatus(domain.ErrInvalidArgument)
	}
	raw, err := proto.MarshalOptions{Deterministic: true}.Marshal(event)
	if err != nil {
		return nil, toStatus(domain.ErrInvalidArgument)
	}
	err = s.repo.Queue(ctx, domain.IngestionEvent{ID: id + "/" + strconv.FormatInt(generation, 10) + "/" + in.GetRoute(),
		DocumentID: id, Generation: generation, Route: in.GetRoute(), Type: string(
			event.ProtoReflect().Descriptor().FullName(),
		), Payload: raw})
	if err != nil {
		return nil, toStatus(err)
	}
	return &dbv1.QueueDocumentEventResponse{}, nil
}

// Requeue создаёт новое поколение с ограждением прежних писателей.
func (s *Ingestion) Requeue(ctx context.Context, in *dbv1.RequeueDocumentRequest) (*commonv1.Document, error) {
	if in.GetId() == "" || in.GetExpectedGeneration() < 1 || len(in.GetInlineText()) > 4<<20 {
		return nil, toStatus(fmt.Errorf("invalid generation: %w", domain.ErrInvalidArgument))
	}
	doc, err := s.repo.Requeue(ctx, in.GetId(), in.GetExpectedGeneration(), in.GetInlineText())
	if err != nil {
		return nil, toStatus(err)
	}
	return documentPB(doc), nil
}
