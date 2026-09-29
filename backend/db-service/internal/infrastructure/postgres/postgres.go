package postgres

import (
	"context"
	"database/sql"
	"embed"
	"fmt"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	_ "github.com/jackc/pgx/v5/stdlib" // драйвер database/sql «pgx» для goose
	"github.com/pressly/goose/v3"
	"github.com/rs/zerolog"

	"github.com/example/db-service/internal/platform/config"
)

// defaultStatementTimeoutSec не даёт зависшему запросу держать соединение пула.
const defaultStatementTimeoutSec = 15

// migrateLockID сериализует миграции: goose сам не блокирует, а параллельный
// CREATE EXTENSION IF NOT EXISTS падает на дубликате ключа.
const migrateLockID = 4097083626

//go:embed migrations/*.sql
var migrationsFS embed.FS

// DB хранит DSN, потому что goose нужен собственный дескриптор database/sql.
type DB struct {
	Pool *pgxpool.Pool
	dsn  string
}

func Connect(ctx context.Context, dsn string, log zerolog.Logger) (*DB, error) {
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		return nil, fmt.Errorf("parse dsn: %w", err)
	}
	cfg.MaxConns = 10

	// statement_timeout срабатывает на сервере, даже если контекст не отменён.
	if sec := config.GetInt("DB_STATEMENT_TIMEOUT_SEC", defaultStatementTimeoutSec); sec > 0 {
		if cfg.ConnConfig.RuntimeParams == nil {
			cfg.ConnConfig.RuntimeParams = map[string]string{}
		}
		cfg.ConnConfig.RuntimeParams["statement_timeout"] = strconv.Itoa(sec * 1000)
	}

	var lastErr error
	for attempt := 1; attempt <= 30; attempt++ {
		pool, perr := pgxpool.NewWithConfig(ctx, cfg)
		if perr == nil {
			if perr = pool.Ping(ctx); perr == nil {
				log.Info().Msg("connected to postgres")
				return &DB{Pool: pool, dsn: dsn}, nil
			}
			pool.Close()
		}
		lastErr = perr
		log.Warn().Int("attempt", attempt).Err(perr).Msg("postgres not ready, retrying")
		select {
		case <-ctx.Done():
			return nil, fmt.Errorf("connect postgres: %w", ctx.Err())
		case <-time.After(2 * time.Second):
		}
	}
	return nil, fmt.Errorf("connect postgres: %w", lastErr)
}

func (db *DB) Migrate(ctx context.Context) error {
	sqlDB, err := sql.Open("pgx", db.dsn)
	if err != nil {
		return fmt.Errorf("open migration db: %w", err)
	}
	defer func() { _ = sqlDB.Close() }()

	// Блокировка сессионная, поэтому живёт на отдельном соединении: goose берёт свои соединения из пула.
	conn, err := sqlDB.Conn(ctx)
	if err != nil {
		return fmt.Errorf("migration lock connection: %w", err)
	}
	defer func() { _ = conn.Close() }()
	if _, lerr := conn.ExecContext(ctx, "SELECT pg_advisory_lock($1)", migrateLockID); lerr != nil {
		return fmt.Errorf("acquire migration lock: %w", lerr)
	}
	defer func() {
		unlockCtx := context.WithoutCancel(ctx)
		_, _ = conn.ExecContext(unlockCtx, "SELECT pg_advisory_unlock($1)", migrateLockID)
	}()

	goose.SetBaseFS(migrationsFS)
	if derr := goose.SetDialect("postgres"); derr != nil {
		return fmt.Errorf("goose dialect: %w", derr)
	}
	if uerr := goose.UpContext(ctx, sqlDB, "migrations"); uerr != nil {
		return fmt.Errorf("apply migrations: %w", uerr)
	}
	return nil
}

func (db *DB) Close() { db.Pool.Close() }

func (db *DB) Ping(ctx context.Context) error {
	if err := db.Pool.Ping(ctx); err != nil {
		return fmt.Errorf("ping postgres: %w", err)
	}
	return nil
}
