package postgres

import (
	"context"
	"errors"
	"fmt"
	"time"
	"uuid"

	"github.com/example/db-service/internal/domain"
	"github.com/example/db-service/internal/platform/contracts"
	"github.com/jackc/pgx/v5"
)

const turnCompleted = "completed"

type storedTurn struct {
	state, token, content, userID, assistantID string
	sequence                                   int64
	live                                       bool
}

func readTurn(ctx context.Context, tx pgx.Tx, chat, request string) (storedTurn, error) {
	var t storedTurn
	err := tx.QueryRow(ctx, `SELECT state,token,content,user_message_id,coalesce(assistant_message_id,''),sequence,
 lease_until>now() FROM chat_turns WHERE chat_id=$1 AND request_id=$2`, chat, request).
		Scan(&t.state, &t.token, &t.content, &t.userID, &t.assistantID, &t.sequence, &t.live)
	if err != nil {
		return t, fmt.Errorf("read turn: %w", err)
	}
	return t, nil
}

func lockChat(ctx context.Context, tx pgx.Tx, chat string) (string, error) {
	var owner string
	err := tx.QueryRow(ctx, `SELECT owner_id FROM chats WHERE id=$1 FOR UPDATE`, chat).Scan(&owner)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", domain.ErrNotFound
	}
	if err != nil {
		return owner, fmt.Errorf("lock chat: %w", err)
	}
	return owner, nil
}

// BeginTurn сериализует ходы без удержания транзакции во время генерации.
func (r *ChatRepo) BeginTurn(ctx context.Context, owner, chat, request, content string) (domain.ChatTurn, error) {
	out := domain.ChatTurn{ChatID: chat, RequestID: request}
	if len(request) == 0 || len(request) > 128 || len(content) == 0 || len(content) > 65536 {
		return out, domain.ErrInvalidArgument
	}
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return out, fmt.Errorf("begin chat turn: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	actual, err := lockChat(ctx, tx, chat)
	if err != nil {
		return out, err
	}
	if actual != owner {
		return out, domain.ErrNotFound
	}
	_, err = tx.Exec(
		ctx,
		`UPDATE chat_turns SET state='failed',failure_code='lease_expired' WHERE chat_id=$1 AND state='pending' AND lease_until<=now()`,
		chat,
	)
	if err != nil {
		return out, fmt.Errorf("begin turn: %w", err)
	}
	t, err := readTurn(ctx, tx, chat, request)
	exists := err == nil
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return out, err
	}
	if exists && t.content != content {
		return out, domain.ErrConflict
	}
	if exists && t.state == turnCompleted {
		return completedChatTurn(ctx, tx, out, t.assistantID)
	}

	var active bool
	var latest int64
	err = tx.QueryRow(
		ctx,
		`SELECT coalesce(max(sequence),0),coalesce(bool_or(state='pending'),false) FROM chat_turns WHERE chat_id=$1`,
		chat,
	).Scan(
		&latest,
		&active,
	)
	if err != nil {
		return out, fmt.Errorf("begin turn: %w", err)
	}
	if active || (exists && t.sequence != latest) {
		return out, domain.ErrConflict
	}
	out.History, err = readChatHistory(ctx, tx, chat, t.userID, 8, "")
	if err != nil {
		return out, err
	}
	out.Token = uuid.NewV4().String()
	err = reserveChatTurn(ctx, tx, out, content, exists, latest)
	if err != nil {
		return out, fmt.Errorf("begin turn: %w", err)
	}
	if operationErr := tx.Commit(ctx); operationErr != nil {
		return out, fmt.Errorf("begin turn: %w", operationErr)
	}
	return out, nil
}

// FinishTurn фиксирует ответ и завершение одной транзакцией; повтор возвращает сохранённый ответ.
func (r *ChatRepo) FinishTurn(ctx context.Context, in domain.ChatTurn, m *domain.Message) (*domain.Message, error) {
	if len(m.Content) == 0 || len(m.Content) > 1<<20 || len(m.Sources) > 1<<20 || len(m.Meta) > 1<<20 {
		return nil, domain.ErrInvalidArgument
	}
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("finish turn: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err = lockChat(ctx, tx, in.ChatID); err != nil {
		return nil, err
	}
	t, err := readTurn(ctx, tx, in.ChatID, in.RequestID)
	if err != nil {
		return nil, err
	}
	if t.token != in.Token || in.Token == "" {
		return nil, domain.ErrConflict
	}
	if t.state == turnCompleted {
		return readChatMessage(ctx, tx, t.assistantID)
	}
	if t.state != "pending" || !t.live {
		return nil, domain.ErrConflict
	}
	m.ID = contracts.StableID("chat-answer-v1", in.ChatID, in.RequestID)
	m.ChatID, m.Role, m.CreatedAt = in.ChatID, "assistant", time.Now().UTC()
	_, err = tx.Exec(ctx, `INSERT INTO chat_messages(id,chat_id,role,content,sources,meta,created_at) VALUES($1,$2,$3,$4,$5,$6,$7)`,
		m.ID, m.ChatID, m.Role, m.Content, jsonbOr(m.Sources, "[]"), jsonbOr(m.Meta, "{}"), m.CreatedAt)
	if err != nil {
		return nil, fmt.Errorf("finish turn: %w", err)
	}
	_, err = tx.Exec(
		ctx,
		`UPDATE chat_turns SET state='completed',assistant_message_id=$3,failure_code='' WHERE chat_id=$1 AND request_id=$2`,
		in.ChatID,
		in.RequestID,
		m.ID,
	)
	if err != nil {
		return nil, fmt.Errorf("finish turn: %w", err)
	}
	m.TurnID, m.TurnState = in.RequestID, turnCompleted
	if operationErr := tx.Commit(ctx); operationErr != nil {
		return m, fmt.Errorf("finish turn: %w", operationErr)
	}
	return m, nil
}

// FailTurn освобождает только текущую аренду, сохраняя один вопрос для повтора.
func (r *ChatRepo) FailTurn(ctx context.Context, in domain.ChatTurn) error {
	_, err := r.pool.Exec(ctx, `UPDATE chat_turns SET state='failed',failure_code='generation_failed'
 WHERE chat_id=$1 AND request_id=$2 AND token=$3 AND state='pending'`, in.ChatID, in.RequestID, in.Token)
	if err != nil {
		return fmt.Errorf("fail turn: %w", err)
	}
	return nil
}

type chatReader interface {
	Query(context.Context, string, ...any) (pgx.Rows, error)
	QueryRow(context.Context, string, ...any) pgx.Row
}

const chatMessageColumns = `m.id,m.chat_id,m.role,m.content,m.sources,m.meta,m.created_at,
 coalesce(t.request_id,''),coalesce(t.state,'')`
const chatMessageJoin = ` FROM chat_messages m LEFT JOIN chat_turns t ON m.id=t.user_message_id OR m.id=t.assistant_message_id `

func scanChatMessage(row pgx.Row) (*domain.Message, error) {
	m := new(domain.Message)
	err := row.Scan(&m.ID, &m.ChatID, &m.Role, &m.Content, &m.Sources, &m.Meta, &m.CreatedAt, &m.TurnID, &m.TurnState)
	if err != nil {
		return m, fmt.Errorf("scan chat message: %w", err)
	}
	return m, nil
}

func readChatMessage(ctx context.Context, q chatReader, id string) (*domain.Message, error) {
	return scanChatMessage(q.QueryRow(ctx, `SELECT `+chatMessageColumns+chatMessageJoin+`WHERE m.id=$1`, id))
}

func readChatHistory(
	ctx context.Context,
	q chatReader,
	chat, exclude string,
	limit int,
	before string,
) ([]*domain.Message, error) {
	limit = min(max(limit, 1), 200)
	rows, err := q.Query(ctx, `SELECT `+chatMessageColumns+chatMessageJoin+`WHERE m.chat_id=$1 AND m.id<>$2
 AND ($4='' OR (m.created_at,m.id)<(SELECT created_at,id FROM chat_messages WHERE chat_id=$1 AND id=$4))
 ORDER BY m.created_at DESC,m.id DESC LIMIT $3`, chat, exclude, limit, before)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]*domain.Message, 0, limit)
	for rows.Next() {
		m, e := scanChatMessage(rows)
		if e != nil {
			return nil, e
		}
		out = append(out, m)
	}
	for i, j := 0, len(out)-1; i < j; i, j = i+1, j-1 {
		out[i], out[j] = out[j], out[i]
	}
	if operationErr := rows.Err(); operationErr != nil {
		return out, fmt.Errorf("read chat history: %w", operationErr)
	}
	return out, nil
}

func (r *ChatRepo) MessagePage(ctx context.Context, chat, before string, limit int) ([]*domain.Message, error) {
	return readChatHistory(ctx, r.pool, chat, "", limit, before)
}

func reserveChatTurn(ctx context.Context, tx pgx.Tx, out domain.ChatTurn, content string, exists bool, latest int64) error {
	var err error
	if !exists {
		id := contracts.StableID("chat-question-v1", out.ChatID, out.RequestID)
		_, err = tx.Exec(ctx, `INSERT INTO chat_messages(id,chat_id,role,content,sources,meta,created_at)
 VALUES($1,$2,'user',$3,'[]','{}',clock_timestamp())`, id, out.ChatID, content)
		if err != nil {
			return fmt.Errorf("begin turn: %w", err)
		}
		_, err = tx.Exec(ctx, `INSERT INTO chat_turns(chat_id,request_id,sequence,content,state,token,lease_until,user_message_id)
 VALUES($1,$2,$3,$4,'pending',$5,now()+interval '2 minutes',$6)`, out.ChatID, out.RequestID, latest+1, content, out.Token, id)
	} else {
		_, err = tx.Exec(ctx, `UPDATE chat_turns SET state='pending',token=$3,lease_until=now()+interval '2 minutes',failure_code=''
 WHERE chat_id=$1 AND request_id=$2`, out.ChatID, out.RequestID, out.Token)
	}
	if err != nil {
		return fmt.Errorf("reserve chat turn: %w", err)
	}
	return nil
}

func completedChatTurn(ctx context.Context, tx pgx.Tx, out domain.ChatTurn, id string) (domain.ChatTurn, error) {
	var err error
	out.Message, err = readChatMessage(ctx, tx, id)
	if err != nil {
		return out, err
	}
	if err = tx.Commit(ctx); err != nil {
		return out, fmt.Errorf("read completed chat turn: %w", err)
	}
	return out, nil
}
