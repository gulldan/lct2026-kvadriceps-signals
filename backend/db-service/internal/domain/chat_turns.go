package domain

import "context"

// ChatTurn связывает повтор запроса с единственным вопросом и его ответом.
type ChatTurn struct {
	ChatID    string
	RequestID string
	Token     string
	Message   *Message
	History   []*Message
}

type ChatTurns interface {
	BeginTurn(ctx context.Context, owner, chat, request, content string) (ChatTurn, error)
	FinishTurn(ctx context.Context, turn ChatTurn, message *Message) (*Message, error)
	FailTurn(ctx context.Context, turn ChatTurn) error
}
