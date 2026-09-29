package postgres_test

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"
	"uuid"

	"github.com/example/db-service/internal/domain"
	"github.com/example/db-service/internal/infrastructure/postgres"
)

func TestChatTurnRetryConcurrencyAndFencing(t *testing.T) {
	db := newTestDB(t)
	repo := postgres.NewChatRepo(db)
	ctx := t.Context()
	chat := &domain.Chat{ID: uuid.NewV4().String(), OwnerID: uuid.NewV4().String(), Title: "test", CreatedAt: time.Now()}
	if err := repo.CreateChat(ctx, chat); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, err := db.Pool.Exec(context.Background(), `DELETE FROM chats WHERE id=$1`, chat.ID)
		if err != nil {
			t.Error(err)
		}
	})
	var winners atomic.Int32
	var wg sync.WaitGroup
	var winner domain.ChatTurn
	for range 8 {
		wg.Go(func() {
			out, err := repo.BeginTurn(ctx, chat.OwnerID, chat.ID, "request", "question")
			if err == nil {
				winners.Add(1)
				winner = out
			} else if !errors.Is(err, domain.ErrConflict) {
				t.Error(err)
			}
		})
	}
	wg.Wait()
	if winners.Load() != 1 {
		t.Fatalf("active attempts=%d", winners.Load())
	}
	if _, err := repo.BeginTurn(ctx, "another", chat.ID, "request", "question"); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("owner guard: %v", err)
	}
	if err := repo.FailTurn(ctx, winner); err != nil {
		t.Fatal(err)
	}
	retry, err := repo.BeginTurn(ctx, chat.OwnerID, chat.ID, "request", "question")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = repo.FinishTurn(ctx, winner, &domain.Message{Content: "obsolete"}); !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("stale writer: %v", err)
	}
	answer, err := repo.FinishTurn(ctx, retry, &domain.Message{Content: "answer"})
	if err != nil {
		t.Fatal(err)
	}
	// Клиент потерял подтверждение фиксации: обе формы повтора возвращают прежний ответ.
	repeated, err := repo.FinishTurn(ctx, retry, &domain.Message{Content: "another"})
	if err != nil || repeated.ID != answer.ID {
		t.Fatalf("finish replay: %+v %v", repeated, err)
	}
	complete, err := repo.BeginTurn(ctx, chat.OwnerID, chat.ID, "request", "question")
	if err != nil || complete.Message.ID != answer.ID {
		t.Fatalf("begin replay: %+v %v", complete, err)
	}
	msgs, err := repo.ListMessages(ctx, chat.ID)
	if err != nil || len(msgs) != 2 || msgs[0].TurnState != "completed" {
		t.Fatalf("duplicate question: %+v %v", msgs, err)
	}
	if _, err = repo.BeginTurn(ctx, chat.OwnerID, chat.ID, "request", "different"); !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("payload collision: %v", err)
	}
	abandoned, err := repo.BeginTurn(ctx, chat.OwnerID, chat.ID, "next", "next question")
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.Pool.Exec(
		ctx,
		`UPDATE chat_turns SET lease_until=now()-interval '1 second' WHERE chat_id=$1 AND request_id='next'`,
		chat.ID,
	)
	if err != nil {
		t.Fatal(err)
	}
	takeover, err := repo.BeginTurn(ctx, chat.OwnerID, chat.ID, "next", "next question")
	if err != nil || takeover.Token == abandoned.Token {
		t.Fatalf("restart recovery: %+v %v", takeover, err)
	}
	if _, err = repo.FinishTurn(ctx, abandoned, &domain.Message{Content: "late"}); !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("expired writer: %v", err)
	}
}

// Одинаковые timestamps не должны терять сообщения на границах страниц.
func TestChatHistoryCursor(t *testing.T) {
	db := newTestDB(t)
	ctx := t.Context()
	repo := postgres.NewChatRepo(db)
	id := uuid.NewV4().String()
	chat := &domain.Chat{ID: id, OwnerID: uuid.NewV4().String(), Title: "pagination", CreatedAt: time.Now()}
	if err := repo.CreateChat(ctx, chat); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = db.Pool.Exec(context.Background(), `DELETE FROM chats WHERE id=$1`, id) })
	_, err := db.Pool.Exec(ctx, `INSERT INTO chat_messages(id,chat_id,role,content,sources,meta,created_at)
 SELECT $1||'-'||lpad(n::text,4,'0'),$1,'user',n::text,'[]','{}',now() FROM generate_series(1,205) n`, id)
	if err != nil {
		t.Fatal(err)
	}
	seen := map[string]bool{}
	before := ""
	for _, count := range []int{100, 100, 5} {
		page, readErr := repo.MessagePage(ctx, id, before, 100)
		if readErr != nil || len(page) != count {
			t.Fatalf("page size: %d %v", len(page), readErr)
		}
		for i, message := range page {
			if seen[message.ID] || i > 0 && page[i-1].ID >= message.ID {
				t.Fatal("duplicate or unstable history order")
			}
			seen[message.ID] = true
		}
		before = page[0].ID
	}
	page, err := repo.MessagePage(ctx, id, before, 100)
	if err != nil || len(page) != 0 || len(seen) != 205 {
		t.Fatalf("history incomplete: %d %v", len(seen), err)
	}
	limited, err := repo.MessagePage(ctx, id, "", 10000)
	if err != nil || len(limited) != 200 {
		t.Fatalf("server cap missing: %d %v", len(limited), err)
	}
}
