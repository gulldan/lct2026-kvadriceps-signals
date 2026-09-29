package postgres

import (
	"context"
	"errors"
	"fmt"
	"math"

	"github.com/example/db-service/internal/platform/contracts"
	"github.com/example/db-service/internal/platform/logger"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/example/db-service/internal/domain"
	"github.com/example/db-service/internal/infrastructure/postgres/sqlcgen"
)

// orEmpty: столбец text[] NOT NULL не должен получить NULL.
func orEmpty(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}

// jsonbOr: столбец jsonb NOT NULL не должен получить NULL.
func jsonbOr(b []byte, def string) []byte {
	if len(b) == 0 {
		return []byte(def)
	}
	return b
}

func i16ToIntPtr(p *int16) *int {
	if p == nil {
		return nil
	}
	v := int(*p)
	return &v
}

func intToI16Ptr(p *int) *int16 {
	if p == nil {
		return nil
	}
	v := int16(*p)
	return &v
}

func intToI32Ptr(p *int) *int32 {
	if p == nil {
		return nil
	}
	v := int32(*p)
	return &v
}

type UserRepo struct{ q *sqlcgen.Queries }

func NewUserRepo(db *DB) *UserRepo { return &UserRepo{q: sqlcgen.New(db.Pool)} }

func userFromRow(u sqlcgen.User) *domain.User {
	return &domain.User{
		ID: u.ID, Username: u.Username, PasswordHash: u.PasswordHash, Roles: u.Roles, CreatedAt: u.CreatedAt,
	}
}

func (r *UserRepo) Create(ctx context.Context, u *domain.User) error {
	err := r.q.CreateUser(ctx, sqlcgen.CreateUserParams{
		ID: u.ID, Username: u.Username, PasswordHash: u.PasswordHash, Roles: orEmpty(u.Roles), CreatedAt: u.CreatedAt,
	})
	if err != nil {
		if pgErr, ok := errors.AsType[*pgconn.PgError](err); ok && pgErr.Code == "23505" {
			return domain.ErrAlreadyExists
		}
		return fmt.Errorf("insert user: %w", err)
	}
	return nil
}

func (r *UserRepo) GetByUsername(ctx context.Context, username string) (*domain.User, error) {
	u, err := r.q.GetUserByUsername(ctx, username)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, domain.ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("select user: %w", err)
	}
	return userFromRow(u), nil
}

func (r *UserRepo) List(ctx context.Context) ([]*domain.User, error) {
	rows, err := r.q.ListUsers(ctx)
	if err != nil {
		return nil, fmt.Errorf("list users: %w", err)
	}
	out := make([]*domain.User, 0, len(rows))
	for i := range rows {
		out = append(out, userFromRow(rows[i]))
	}
	return out, nil
}

func (r *UserRepo) Count(ctx context.Context) (int, error) {
	n, err := r.q.CountUsers(ctx)
	if err != nil {
		return 0, fmt.Errorf("count users: %w", err)
	}
	return int(n), nil
}

type DocumentRepo struct {
	q    *sqlcgen.Queries
	pool *pgxpool.Pool
}

func NewDocumentRepo(db *DB) *DocumentRepo {
	return &DocumentRepo{q: sqlcgen.New(db.Pool), pool: db.Pool}
}

func documentFromRow(d sqlcgen.Document) *domain.Document {
	return &domain.Document{
		ID: d.ID, OwnerID: d.OwnerID, Filename: d.Filename, MIMEType: d.MimeType, Size: d.Size,
		ObjectKey: d.ObjectKey, Status: d.Status, StatusMsg: d.StatusMsg, ChunkCount: int(d.ChunkCount),
		CreatedAt: d.CreatedAt, UpdatedAt: d.UpdatedAt, ContentHash: d.ContentHash, Title: d.Title,
		Author: d.Author, PublishedAt: d.PublishedAt, SourceRef: d.SourceRef, Kind: d.Kind,
		SourceMetadata: d.SourceMetadata, Generation: d.Generation,
	}
}

func (r *DocumentRepo) Create(ctx context.Context, d *domain.Document) error {
	return r.createWithIngestion(ctx, d)
}

func (r *DocumentRepo) Get(ctx context.Context, id string) (*domain.Document, error) {
	d, err := r.q.GetDocument(ctx, id)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, domain.ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("select document: %w", err)
	}
	return documentFromRow(d), nil
}

// FindByHash: пустой ownerID ищет по всем владельцам.
func (r *DocumentRepo) FindByHash(ctx context.Context, ownerID, hash string) (*domain.Document, error) {
	d, err := r.q.FindDocumentByHash(ctx, sqlcgen.FindDocumentByHashParams{ContentHash: hash, OwnerID: ownerID})
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, domain.ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("find document by hash: %w", err)
	}
	return documentFromRow(d), nil
}

// ListByOwner: пустой ownerID — документы всех владельцев.
func (r *DocumentRepo) ListByOwner(
	ctx context.Context,
	ownerID string,
	options ...contracts.DocumentScan,
) ([]*domain.Document, error) {
	var opt contracts.DocumentScan
	if len(options) > 0 {
		opt = options[0]
	}
	if opt.Limit <= 0 {
		opt.Limit = 500
	}
	rows, err := r.q.ListDocumentsByOwner(
		ctx,
		sqlcgen.ListDocumentsByOwnerParams{
			OwnerID:  ownerID,
			BeforeID: opt.BeforeID,
			Status:   opt.Status,
			Kind:     opt.Kind,
			Lim: int32(
				min(
					opt.Limit,
					500,
				),
			),
		},
	)
	if err != nil {
		return nil, fmt.Errorf("list documents: %w", err)
	}
	out := make([]*domain.Document, 0, len(rows))
	for i := range rows {
		out = append(out, documentFromRow(rows[i]))
	}
	return out, nil
}

func (r *DocumentRepo) UpdateStatus(ctx context.Context, id, status, msg string, chunkCount *int) error {
	changed, err := r.q.UpdateDocumentStatus(ctx, sqlcgen.UpdateDocumentStatusParams{
		ID: id, Status: status, StatusMsg: msg, ChunkCount: intToI32Ptr(chunkCount), Generation: contracts.DocumentGeneration(ctx),
	})
	if err != nil {
		return fmt.Errorf("update document status: %w", err)
	}
	if changed == 0 {
		// Ноль строк — ограждение по поколению, стадии или активному снимку; молчать
		// нельзя, иначе документ навсегда застревает в queued без ошибки.
		_, err = r.Get(ctx, id)
		if err == nil {
			logger.From(ctx).Warn().Str("document_id", id).Str("status", status).
				Int64("generation", contracts.DocumentGeneration(ctx)).
				Msg("status write fenced; document state unchanged")
		}
		return err
	}
	return nil
}

func (r *DocumentRepo) UpdateTitle(ctx context.Context, id, title string) error {
	if err := r.q.UpdateDocumentTitle(ctx, sqlcgen.UpdateDocumentTitleParams{ID: id, Title: title}); err != nil {
		return fmt.Errorf("update document title: %w", err)
	}
	return nil
}

func (r *DocumentRepo) UpdateKind(ctx context.Context, id, kind string) error {
	if err := r.q.UpdateDocumentKind(ctx, sqlcgen.UpdateDocumentKindParams{ID: id, Kind: kind}); err != nil {
		return fmt.Errorf("update document kind: %w", err)
	}
	return nil
}

func (r *DocumentRepo) UpdateMeta(ctx context.Context, id, author, publishedAt, sourceRef string) error {
	err := r.q.UpdateDocumentMeta(ctx, sqlcgen.UpdateDocumentMetaParams{
		ID: id, Author: author, PublishedAt: publishedAt, SourceRef: sourceRef,
	})
	if err != nil {
		return fmt.Errorf("update document meta: %w", err)
	}
	return nil
}

type ChatRepo struct {
	q    *sqlcgen.Queries
	pool *pgxpool.Pool
}

func NewChatRepo(db *DB) *ChatRepo { return &ChatRepo{q: sqlcgen.New(db.Pool), pool: db.Pool} }

func chatFromRow(c sqlcgen.Chat) *domain.Chat {
	return &domain.Chat{
		ID: c.ID, OwnerID: c.OwnerID, Title: c.Title, Source: c.Source, CreatedAt: c.CreatedAt,
	}
}

func (r *ChatRepo) CreateChat(ctx context.Context, c *domain.Chat) error {
	err := r.q.CreateChat(ctx, sqlcgen.CreateChatParams{
		ID: c.ID, OwnerID: c.OwnerID, Title: c.Title, Source: c.Source, CreatedAt: c.CreatedAt,
	})
	if err != nil {
		return fmt.Errorf("insert chat: %w", err)
	}
	return nil
}

func (r *ChatRepo) GetChat(ctx context.Context, id string) (*domain.Chat, error) {
	c, err := r.q.GetChat(ctx, id)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, domain.ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("select chat: %w", err)
	}
	return chatFromRow(c), nil
}

func (r *ChatRepo) ListChats(
	ctx context.Context, ownerID string, limit, offset int, allOwners bool,
) ([]*domain.Chat, int64, error) {
	lim := int32(math.MaxInt32)
	if limit > 0 {
		lim = int32(limit)
	}
	off := int32(0)
	if offset > 0 {
		off = int32(offset)
	}
	if allOwners {
		rows, err := r.q.ListChatsAll(ctx, sqlcgen.ListChatsAllParams{Limit: lim, Offset: off})
		if err != nil {
			return nil, 0, fmt.Errorf("list chats: %w", err)
		}
		out := make([]*domain.Chat, 0, len(rows))
		for i := range rows {
			out = append(out, &domain.Chat{
				ID: rows[i].ID, OwnerID: rows[i].OwnerID, OwnerUsername: rows[i].OwnerUsername,
				Title: rows[i].Title, Source: rows[i].Source, CreatedAt: rows[i].CreatedAt,
			})
		}
		total, err := r.q.CountChats(ctx)
		if err != nil {
			return nil, 0, fmt.Errorf("count chats: %w", err)
		}
		return out, total, nil
	}
	rows, err := r.q.ListChatsByOwner(ctx, sqlcgen.ListChatsByOwnerParams{
		OwnerID: ownerID, Limit: lim, Offset: off,
	})
	if err != nil {
		return nil, 0, fmt.Errorf("list chats: %w", err)
	}
	out := make([]*domain.Chat, 0, len(rows))
	for i := range rows {
		out = append(out, &domain.Chat{
			ID: rows[i].ID, OwnerID: rows[i].OwnerID, OwnerUsername: rows[i].OwnerUsername,
			Title: rows[i].Title, Source: rows[i].Source, CreatedAt: rows[i].CreatedAt,
		})
	}
	total, err := r.q.CountChatsByOwner(ctx, ownerID)
	if err != nil {
		return nil, 0, fmt.Errorf("count chats: %w", err)
	}
	return out, total, nil
}

func (r *ChatRepo) AddMessage(ctx context.Context, m *domain.Message) error {
	err := r.q.AddMessage(ctx, sqlcgen.AddMessageParams{
		ID: m.ID, ChatID: m.ChatID, Role: m.Role, Content: m.Content,
		Sources: jsonbOr(m.Sources, "[]"), Meta: jsonbOr(m.Meta, "{}"), CreatedAt: m.CreatedAt,
	})
	if err != nil {
		return fmt.Errorf("insert message: %w", err)
	}
	return nil
}

func (r *ChatRepo) ListMessages(ctx context.Context, chatID string) ([]*domain.Message, error) {
	return r.MessagePage(ctx, chatID, "", 200)
}

type ModelRepo struct{ q *sqlcgen.Queries }

func NewModelRepo(db *DB) *ModelRepo { return &ModelRepo{q: sqlcgen.New(db.Pool)} }

func (r *ModelRepo) List(ctx context.Context) ([]*domain.Model, error) {
	rows, err := r.q.ListModels(ctx)
	if err != nil {
		return nil, fmt.Errorf("list models: %w", err)
	}
	out := make([]*domain.Model, 0, len(rows))
	for i := range rows {
		out = append(out, &domain.Model{ID: rows[i].ID, Name: rows[i].Name, Role: rows[i].Role, Backend: rows[i].Backend})
	}
	return out, nil
}

func (r *ModelRepo) Upsert(ctx context.Context, m *domain.Model) error {
	err := r.q.UpsertModel(ctx, sqlcgen.UpsertModelParams{ID: m.ID, Name: m.Name, Role: m.Role, Backend: m.Backend})
	if err != nil {
		return fmt.Errorf("upsert model: %w", err)
	}
	return nil
}
