package grpcserver

import (
	"context"
	"encoding/json"

	"github.com/example/db-service/internal/domain"
	commonv1 "github.com/example/db-service/internal/platform/genproto/common/v1"
	dbv1 "github.com/example/db-service/internal/platform/genproto/db/v1"
)

type ChatTurns struct {
	dbv1.UnimplementedChatTurnsServer
	repo domain.ChatTurns
}

func NewChatTurns(repo domain.ChatTurns) *ChatTurns { return &ChatTurns{repo: repo} }

// Begin возвращает квитанцию завершения либо аренду единственного исполнителя.
func (s *ChatTurns) Begin(ctx context.Context, in *dbv1.BeginChatTurnRequest) (*dbv1.BeginChatTurnResponse, error) {
	t, err := s.repo.BeginTurn(ctx, in.GetOwnerId(), in.GetChatId(), in.GetRequestId(), in.GetContent())
	if err != nil {
		return nil, toStatus(err)
	}
	out := &dbv1.BeginChatTurnResponse{}
	if t.Message != nil {
		out.Completed = messagePB(t.Message)
		return out, nil
	}
	out.Lease = &dbv1.ChatTurnLease{ChatId: t.ChatID, RequestId: t.RequestID, Token: t.Token}
	for _, m := range t.History {
		out.History = append(out.History, messagePB(m))
	}
	return out, nil
}

func turnFromLease(in *dbv1.ChatTurnLease) domain.ChatTurn {
	return domain.ChatTurn{ChatID: in.GetChatId(), RequestID: in.GetRequestId(), Token: in.GetToken()}
}

func (s *ChatTurns) Finish(ctx context.Context, in *dbv1.FinishChatTurnRequest) (*commonv1.Message, error) {
	sources, err := json.Marshal(in.GetSources())
	if err != nil {
		return nil, toStatus(domain.ErrInvalidArgument)
	}
	m, err := s.repo.FinishTurn(
		ctx,
		turnFromLease(
			in.GetLease(),
		),
		&domain.Message{
			Content: in.GetContent(),
			Sources: sources,
			Meta: []byte(
				in.GetMeta(),
			),
		},
	)
	if err != nil {
		return nil, toStatus(err)
	}
	return messagePB(m), nil
}

func (s *ChatTurns) Fail(ctx context.Context, in *dbv1.ChatTurnLease) (*dbv1.FailChatTurnResponse, error) {
	if err := s.repo.FailTurn(ctx, turnFromLease(in)); err != nil {
		return nil, toStatus(err)
	}
	return &dbv1.FailChatTurnResponse{}, nil
}
