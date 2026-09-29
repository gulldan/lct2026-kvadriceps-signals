package application

import (
	"context"
	"encoding/json"
	"fmt"
	"maps"
	"time"
	"uuid"

	"github.com/example/db-service/internal/domain"
	"github.com/example/db-service/internal/platform/config"
	"github.com/example/db-service/internal/platform/contracts"
)

type Service struct {
	users      domain.UserRepository
	docs       domain.DocumentRepository
	chats      domain.ChatRepository
	models     domain.ModelRepository
	kpis       domain.KPIRepository
	clusters   domain.ClusterRepository
	hypotheses domain.HypothesisRepository
	llmUsage   domain.LLMUsageRepository
	settings   domain.AppSettingsRepository
}

func New(
	users domain.UserRepository,
	docs domain.DocumentRepository,
	chats domain.ChatRepository,
	models domain.ModelRepository,
	kpis domain.KPIRepository,
	clusters domain.ClusterRepository,
	hypotheses domain.HypothesisRepository,
	llmUsage domain.LLMUsageRepository,
	settings domain.AppSettingsRepository,
) *Service {
	return &Service{
		users:      users,
		docs:       docs,
		chats:      chats,
		models:     models,
		kpis:       kpis,
		clusters:   clusters,
		hypotheses: hypotheses,
		llmUsage:   llmUsage,
		settings:   settings,
	}
}

func (s *Service) SetLLMUsage(ctx context.Context, row *domain.LLMUsageDaily) error {
	return s.llmUsage.SetDaily(ctx, row)
}

// ListLLMUsage: диапазон дат включает обе границы.
func (s *Service) ListLLMUsage(ctx context.Context, from, to time.Time) ([]*domain.LLMUsageDaily, error) {
	return s.llmUsage.ListDaily(ctx, from, to)
}

func (s *Service) AppSettings(ctx context.Context) (domain.AppSettingsSnapshot, error) {
	snapshot, err := s.settings.Snapshot(ctx)
	if err != nil {
		return snapshot, err
	}
	snapshot.Values = maps.Clone(snapshot.Values)
	maps.DeleteFunc(snapshot.Values, func(key, _ string) bool { return !config.CanOverride(key) })
	return snapshot, nil
}

func (s *Service) ApplyAppSettings(ctx context.Context, revision int64, actor string, changes map[string]*string) (int64, error) {
	if _, err := uuid.Parse(actor); err != nil || revision < 0 || len(changes) == 0 || len(changes) > 128 {
		return 0, domain.ErrInvalidArgument
	}
	for key, value := range changes {
		if !config.CanOverride(key) || (value != nil && len(*value) > 65536) {
			return 0, domain.ErrInvalidArgument
		}
	}
	return s.settings.Apply(ctx, revision, actor, changes)
}

func now() time.Time { return time.Now().UTC() }

func (s *Service) CreateUser(ctx context.Context, username, passwordHash string, roles []string) (*domain.User, error) {
	if username == "" || passwordHash == "" {
		return nil, fmt.Errorf("username and password_hash are required: %w", domain.ErrInvalidArgument)
	}
	if len(roles) == 0 {
		roles = []string{"user"}
	}
	u := &domain.User{ID: uuid.NewV4().String(), Username: username, PasswordHash: passwordHash, Roles: roles, CreatedAt: now()}
	if err := s.users.Create(ctx, u); err != nil {
		return nil, err
	}
	return u, nil
}

func (s *Service) GetUserByUsername(ctx context.Context, username string) (*domain.User, error) {
	return s.users.GetByUsername(ctx, username)
}

func (s *Service) ListUsers(ctx context.Context) ([]*domain.User, error) {
	return s.users.List(ctx)
}

type DocumentOptions struct {
	Enqueue      bool
	InlineText   string
	ArchiveDepth int32
	RequestID    string
}

// CreateDocument фиксирует документ и намерение обработки одной транзакцией.
func (s *Service) CreateDocument(
	ctx context.Context, ownerID, filename, mimeType, objectKey, contentHash string,
	size int64, sourceMetadata []byte, options ...DocumentOptions,
) (*domain.Document, error) {
	if ownerID == "" || objectKey == "" {
		return nil, fmt.Errorf("owner_id and object_key are required: %w", domain.ErrInvalidArgument)
	}
	metadata, err := validateInitialSource(sourceMetadata, contentHash)
	if err != nil {
		return nil, err
	}
	d := &domain.Document{
		ID: uuid.NewV4().String(), OwnerID: ownerID, Filename: filename, MIMEType: mimeType,
		Size: size, ObjectKey: objectKey, Status: contracts.StatusUploaded, StatusMsg: "",
		ChunkCount: 0, CreatedAt: now(), UpdatedAt: now(), ContentHash: contentHash,
		SourceMetadata: metadata,
	}
	d.Generation = 1
	key := objectKey
	if len(options) > 0 {
		opt := options[0]
		if len(opt.InlineText) > 4<<20 || opt.ArchiveDepth < 0 || opt.ArchiveDepth > 3 || len(opt.RequestID) > 4096 {
			return nil, domain.ErrInvalidArgument
		}
		d.Enqueue, d.InlineText, d.ArchiveDepth = opt.Enqueue, opt.InlineText, opt.ArchiveDepth
		if opt.RequestID != "" {
			key = opt.RequestID
		}
		if d.Enqueue {
			d.Status = contracts.StatusQueued
		}
	}
	d.ID = contracts.StableID("document-create-v1", ownerID, key)
	if err := s.docs.Create(ctx, d); err != nil {
		return nil, err
	}
	return d, nil
}

// FindDocumentByHash берёт самый новый не упавший документ владельца с этим хешем.
func (s *Service) FindDocumentByHash(ctx context.Context, ownerID, hash string) (*domain.Document, error) {
	if hash == "" {
		return nil, domain.ErrNotFound
	}
	return s.docs.FindByHash(ctx, ownerID, hash)
}

func (s *Service) GetDocument(ctx context.Context, id string) (*domain.Document, error) {
	return s.docs.Get(ctx, id)
}

func (s *Service) ListDocuments(
	ctx context.Context,
	ownerID string,
	options ...contracts.DocumentScan,
) ([]*domain.Document, error) {
	return s.docs.ListByOwner(ctx, ownerID, options...)
}

func (s *Service) UpdateDocumentStatus(ctx context.Context, id, status, msg string, chunkCount *int) error {
	switch status {
	case contracts.StatusUploaded, contracts.StatusQueued, contracts.StatusParsing, contracts.StatusOCR,
		contracts.StatusParsed, contracts.StatusChunking, contracts.StatusIndexed, contracts.StatusFailed:
	default:
		return domain.ErrInvalidArgument
	}
	return s.docs.UpdateStatus(ctx, id, status, msg, chunkCount)
}

func (s *Service) SetDocumentTitle(ctx context.Context, id, title string) error {
	if id == "" {
		return fmt.Errorf("id is required: %w", domain.ErrInvalidArgument)
	}
	return s.docs.UpdateTitle(ctx, id, title)
}

func (s *Service) SetDocumentKind(ctx context.Context, id, kind string) error {
	if id == "" {
		return fmt.Errorf("id is required: %w", domain.ErrInvalidArgument)
	}
	return s.docs.UpdateKind(ctx, id, kind)
}

func (s *Service) SetDocumentMeta(ctx context.Context, id, author, publishedAt, sourceRef string) error {
	if id == "" {
		return fmt.Errorf("id is required: %w", domain.ErrInvalidArgument)
	}
	return s.docs.UpdateMeta(ctx, id, author, publishedAt, sourceRef)
}

func (s *Service) CreateChat(ctx context.Context, ownerID, title, source string) (*domain.Chat, error) {
	if ownerID == "" {
		return nil, fmt.Errorf("owner_id is required: %w", domain.ErrInvalidArgument)
	}
	if title == "" {
		title = "New chat"
	}
	c := &domain.Chat{ID: uuid.NewV4().String(), OwnerID: ownerID, Title: title, Source: source, CreatedAt: now()}
	if err := s.chats.CreateChat(ctx, c); err != nil {
		return nil, err
	}
	return c, nil
}

func (s *Service) GetChat(ctx context.Context, id string) (*domain.Chat, error) {
	return s.chats.GetChat(ctx, id)
}

// ListChats: limit ≤ 0 — без ограничения.
func (s *Service) ListChats(
	ctx context.Context, ownerID string, limit, offset int, allOwners bool,
) ([]*domain.Chat, int64, error) {
	return s.chats.ListChats(ctx, ownerID, limit, offset, allOwners)
}

func (s *Service) AddMessage(
	ctx context.Context, chatID, role, content string, sources, meta []byte,
) (*domain.Message, error) {
	if role == "" || content == "" {
		return nil, fmt.Errorf("role and content are required: %w", domain.ErrInvalidArgument)
	}
	if len(meta) > 0 && !json.Valid(meta) {
		return nil, fmt.Errorf("meta must be valid JSON: %w", domain.ErrInvalidArgument)
	}
	m := &domain.Message{
		ID: uuid.NewV4().String(), ChatID: chatID, Role: role, Content: content,
		Sources: sources, Meta: meta, CreatedAt: now(),
	}
	if err := s.chats.AddMessage(ctx, m); err != nil {
		return nil, err
	}
	return m, nil
}

func (s *Service) ListMessages(ctx context.Context, chatID string) ([]*domain.Message, error) {
	return s.chats.ListMessages(ctx, chatID)
}

func (s *Service) ListModels(ctx context.Context) ([]*domain.Model, error) {
	return s.models.List(ctx)
}

// Seed заводит администратора только на пустой таблице пользователей; модели обновляет всегда.
func (s *Service) Seed(ctx context.Context, adminUsername, adminPasswordHash string, models []*domain.Model) error {
	count, err := s.users.Count(ctx)
	if err != nil {
		return err
	}
	if count == 0 && adminUsername != "" && adminPasswordHash != "" {
		if _, cerr := s.CreateUser(ctx, adminUsername, adminPasswordHash, []string{"admin", "user"}); cerr != nil {
			return cerr
		}
	}
	for _, m := range models {
		if uerr := s.models.Upsert(ctx, m); uerr != nil {
			return uerr
		}
	}
	return nil
}
