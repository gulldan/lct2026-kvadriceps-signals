package dbclient

import (
	"context"
	"time"

	commonv1 "github.com/example/auth/internal/platform/genproto/common/v1"
	dbv1 "github.com/example/auth/internal/platform/genproto/db/v1"
)

// BeginChatTurn открывает ход либо возвращает ранее сохранённый ответ.
func (c *Client) BeginChatTurn(ctx context.Context, in *dbv1.BeginChatTurnRequest) (*dbv1.BeginChatTurnResponse, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	out, err := dbv1.NewChatTurnsClient(c.conn).Begin(ctx, in)
	return out, mapErr(err)
}

func (c *Client) FinishChatTurn(ctx context.Context, in *dbv1.FinishChatTurnRequest) (*commonv1.Message, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	out, err := dbv1.NewChatTurnsClient(c.conn).Finish(ctx, in)
	return out, mapErr(err)
}

// FailChatTurn освобождает ход для явного повтора клиентом.
func (c *Client) FailChatTurn(ctx context.Context, in *dbv1.ChatTurnLease) error {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	_, err := dbv1.NewChatTurnsClient(c.conn).Fail(ctx, in)
	return mapErr(err)
}
