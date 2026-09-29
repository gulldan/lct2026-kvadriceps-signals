package application

import (
	"context"
	dbv1 "github.com/example/main-service/internal/platform/genproto/db/v1"
	"sync"

	"github.com/example/main-service/internal/platform/dbclient"

	commonv1 "github.com/example/main-service/internal/platform/genproto/common/v1"
)

type fakeChats struct {
	mu          sync.Mutex
	chats       map[string]*commonv1.Chat
	messages    map[string][]*commonv1.Message
	models      []*commonv1.Model
	createErr   error
	getErr      error
	listErr     error
	addErr      error
	listMsgErr  error
	listModErr  error
	addMsgCalls int
}

func newChats() *fakeChats {
	return &fakeChats{chats: map[string]*commonv1.Chat{}, messages: map[string][]*commonv1.Message{}}
}

func (f *fakeChats) CreateChat(_ context.Context, ownerID, title, source string) (*commonv1.Chat, error) {
	if f.createErr != nil {
		return nil, f.createErr
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	c := &commonv1.Chat{Id: "chat-" + title, OwnerId: ownerID, Title: title, Source: source}
	f.chats[c.GetId()] = c
	return c, nil
}

func (f *fakeChats) GetChat(_ context.Context, id string) (*commonv1.Chat, error) {
	if f.getErr != nil {
		return nil, f.getErr
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	c, ok := f.chats[id]
	if !ok {
		return nil, dbclient.ErrNotFound
	}
	return c, nil
}

func (f *fakeChats) ListChats(
	_ context.Context, ownerID string, _, _ int, allOwners bool,
) ([]*commonv1.Chat, int64, error) {
	if f.listErr != nil {
		return nil, 0, f.listErr
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]*commonv1.Chat, 0, len(f.chats))
	for _, c := range f.chats {
		if allOwners || c.GetOwnerId() == ownerID {
			out = append(out, c)
		}
	}
	return out, int64(len(out)), nil
}

func (f *fakeChats) AddMessage(
	_ context.Context, chatID, role, content string, sources []*commonv1.Source, meta string,
) (*commonv1.Message, error) {
	f.mu.Lock()
	f.addMsgCalls++
	f.mu.Unlock()
	if f.addErr != nil {
		return nil, f.addErr
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	m := &commonv1.Message{Id: "msg", ChatId: chatID, Role: role, Content: content, Sources: sources, Meta: meta}
	f.messages[chatID] = append(f.messages[chatID], m)
	return m, nil
}

func (f *fakeChats) ListMessages(_ context.Context, chatID string) ([]*commonv1.Message, error) {
	if f.listMsgErr != nil {
		return nil, f.listMsgErr
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.messages[chatID], nil
}

func (f *fakeChats) ListModels(_ context.Context) ([]*commonv1.Model, error) {
	if f.listModErr != nil {
		return nil, f.listModErr
	}
	return f.models, nil
}

type fakeUsers struct {
	users []*commonv1.User
	err   error
}

func (f *fakeUsers) ListUsers(_ context.Context) ([]*commonv1.User, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.users, nil
}

func (f *fakeChats) BeginChatTurn(ctx context.Context, in *dbv1.BeginChatTurnRequest) (*dbv1.BeginChatTurnResponse, error) {
	history, err := f.ListMessages(ctx, in.GetChatId())
	if err != nil {
		return nil, err
	}
	if _, err = f.AddMessage(ctx, in.GetChatId(), "user", in.GetContent(), nil, ""); err != nil {
		return nil, err
	}
	return &dbv1.BeginChatTurnResponse{
		Lease: &dbv1.ChatTurnLease{
			ChatId:    in.GetChatId(),
			RequestId: in.GetRequestId(),
			Token:     "lease",
		},
		History: history,
	}, nil
}
func (f *fakeChats) FinishChatTurn(ctx context.Context, in *dbv1.FinishChatTurnRequest) (*commonv1.Message, error) {
	return f.AddMessage(ctx, in.GetLease().GetChatId(), "assistant", in.GetContent(), in.GetSources(), in.GetMeta())
}
func (f *fakeChats) FailChatTurn(context.Context, *dbv1.ChatTurnLease) error { return nil }

func (f *fakeChats) MessagePage(ctx context.Context, chat, _ string, _ int) ([]*commonv1.Message, error) {
	return f.ListMessages(ctx, chat)
}
