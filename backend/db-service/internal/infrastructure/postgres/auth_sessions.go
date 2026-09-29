package postgres

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/example/db-service/internal/domain"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type AuthSessionsRepo struct{ pool *pgxpool.Pool }

func NewAuthSessionsRepo(db *DB) *AuthSessionsRepo { return &AuthSessionsRepo{pool: db.Pool} }

const readSessionSQL = `SELECT s.id,s.user_id,u.username,u.roles,s.expires_at
 FROM auth_sessions s JOIN users u ON u.id=s.user_id
 WHERE s.id=$1 AND s.revoked_at IS NULL AND s.expires_at>now()`

func scanSession(row pgx.Row) (domain.AuthSession, error) {
	var session domain.AuthSession
	err := row.Scan(&session.ID, &session.UserID, &session.Username, &session.Roles, &session.ExpiresAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return session, domain.ErrNotFound
	}
	if err != nil {
		return session, fmt.Errorf("read auth session: %w", err)
	}
	return session, nil
}

// Create ограничивает срок абсолютной датой; перезапуск не продлевает сессию.
func (r *AuthSessionsRepo) Create(ctx context.Context, id, user, hash string, ttl time.Duration) (domain.AuthSession, error) {
	if ttl < time.Minute || ttl > 30*24*time.Hour {
		return domain.AuthSession{}, domain.ErrInvalidArgument
	}
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return domain.AuthSession{}, fmt.Errorf("begin session: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err = tx.Exec(ctx, `DELETE FROM auth_sessions WHERE id IN
 (SELECT id FROM auth_sessions WHERE expires_at<now()-interval '7 days' ORDER BY expires_at LIMIT 256)`); err != nil {
		return domain.AuthSession{}, fmt.Errorf("prune expired sessions: %w", err)
	}
	if _, err = tx.Exec(ctx, `INSERT INTO auth_sessions(id,user_id,expires_at) VALUES($1,$2,now()+make_interval(secs=>$3))`,
		id, user, ttl.Seconds()); err != nil {
		return domain.AuthSession{}, fmt.Errorf("create session: %w", err)
	}
	if _, err = tx.Exec(ctx, `INSERT INTO auth_refresh_tokens(hash,session_id) VALUES($1,$2)`, hash, id); err != nil {
		return domain.AuthSession{}, fmt.Errorf("create refresh token: %w", err)
	}
	session, err := scanSession(tx.QueryRow(ctx, readSessionSQL, id))
	if err != nil {
		return session, err
	}
	if err = tx.Commit(ctx); err != nil {
		return session, fmt.Errorf("commit session: %w", err)
	}
	return session, nil
}

// Rotate помнит использованные хеши до конца сессии и отзывает семейство при повторе. Повтор в
// первые 30 секунд считаем гонкой, а не кражей: перезагрузка страницы обрывает ответ с новым
// cookie уже после ротации, а без navigator.locks (HTTP не на localhost) вкладки обновляют
// сессию наперегонки. Такому повтору выдаём ещё один токен той же сессии.
func (r *AuthSessionsRepo) Rotate(ctx context.Context, hash, next string) (domain.AuthSession, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return domain.AuthSession{}, fmt.Errorf("begin refresh: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var id string
	var used, racing bool
	err = tx.QueryRow(ctx, `SELECT s.id, t.consumed_at IS NOT NULL,
 COALESCE(t.consumed_at > now() - interval '30 seconds', false) FROM auth_refresh_tokens t
 JOIN auth_sessions s ON s.id=t.session_id
 WHERE t.hash=$1 AND s.revoked_at IS NULL AND s.expires_at>now() FOR UPDATE OF s,t`, hash).Scan(&id, &used, &racing)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.AuthSession{}, domain.ErrNotFound
	}
	if err != nil {
		return domain.AuthSession{}, fmt.Errorf("lock refresh: %w", err)
	}
	if used && !racing {
		if _, err = tx.Exec(ctx, `UPDATE auth_sessions SET revoked_at=now() WHERE id=$1`, id); err != nil {
			return domain.AuthSession{}, fmt.Errorf("revoke replayed session: %w", err)
		}
		if err = tx.Commit(ctx); err != nil {
			return domain.AuthSession{}, fmt.Errorf("commit replay revocation: %w", err)
		}
		return domain.AuthSession{}, domain.ErrNotFound
	}
	if !used {
		if _, err = tx.Exec(ctx, `UPDATE auth_refresh_tokens SET consumed_at=now() WHERE hash=$1`, hash); err != nil {
			return domain.AuthSession{}, fmt.Errorf("consume refresh: %w", err)
		}
	}
	if _, err = tx.Exec(ctx, `INSERT INTO auth_refresh_tokens(hash,session_id) VALUES($1,$2)`, next, id); err != nil {
		return domain.AuthSession{}, fmt.Errorf("rotate refresh: %w", err)
	}
	session, err := scanSession(tx.QueryRow(ctx, readSessionSQL, id))
	if err != nil {
		return session, err
	}
	if err = tx.Commit(ctx); err != nil {
		return session, fmt.Errorf("commit refresh: %w", err)
	}
	return session, nil
}

// Check читает актуальные роли, поэтому понижение прав действует и для выданных JWT.
func (r *AuthSessionsRepo) Check(ctx context.Context, id string) (domain.AuthSession, error) {
	return scanSession(r.pool.QueryRow(ctx, readSessionSQL, id))
}

// Revoke принимает также использованный токен; выход отзывает всё семейство.
func (r *AuthSessionsRepo) Revoke(ctx context.Context, hash string) error {
	_, err := r.pool.Exec(ctx, `UPDATE auth_sessions SET revoked_at=COALESCE(revoked_at,now())
 WHERE id=(SELECT session_id FROM auth_refresh_tokens WHERE hash=$1)`, hash)
	if err != nil {
		return fmt.Errorf("revoke auth session: %w", err)
	}
	return nil
}
