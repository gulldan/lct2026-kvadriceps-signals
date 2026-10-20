package domain

import (
	"context"
	"errors"
	"github.com/example/db-service/internal/platform/contracts"
	"time"
)

var ErrAlreadyExists = errors.New("already exists")

// ErrNotFound обозначает отсутствие доступного ресурса.
var ErrNotFound = errors.New("not found")

var ErrInvalidArgument = errors.New("invalid argument")

// ErrConflict: попытка записать другое значение в неизменяемую ревизию источника.
var ErrConflict = errors.New("conflict")

type User struct {
	ID           string
	Username     string
	PasswordHash string
	Roles        []string
	CreatedAt    time.Time
}

type Document struct {
	Generation   int64
	Enqueue      bool
	InlineText   string
	ArchiveDepth int32
	ID           string
	OwnerID      string
	Filename     string
	MIMEType     string
	Size         int64
	ObjectKey    string
	Status       string
	StatusMsg    string
	ChunkCount   int
	CreatedAt    time.Time
	UpdatedAt    time.Time
	// ContentHash — BLAKE3-256 в hex, пусто — неизвестен; по нему идёт дедупликация.
	ContentHash string
	Title       string
	Author      string
	PublishedAt string
	SourceRef   string
	// Kind «hypotheses» — готовые записки, которые не участвуют в поиске при генерации гипотез.
	Kind string
	// SourceMetadata — неизменяемый JSON-конверт sourcedoc v1; у старых строк nil.
	SourceMetadata []byte
}

// Chat: OwnerUsername подставляется при чтении и не хранится.
type Chat struct {
	ID            string
	OwnerID       string
	OwnerUsername string
	Title         string
	Source        string
	CreatedAt     time.Time
}

type Message struct {
	TurnID    string
	TurnState string
	ID        string
	ChatID    string
	Role      string
	Content   string
	Sources   []byte
	Meta      []byte
	CreatedAt time.Time
}

type Model struct {
	ID      string
	Name    string
	Role    string
	Backend string
}

type UserRepository interface {
	Create(ctx context.Context, u *User) error
	GetByUsername(ctx context.Context, username string) (*User, error)
	List(ctx context.Context) ([]*User, error)
	Count(ctx context.Context) (int, error)
}

type DocumentRepository interface {
	Create(ctx context.Context, d *Document) error
	Get(ctx context.Context, id string) (*Document, error)
	FindByHash(ctx context.Context, ownerID, hash string) (*Document, error)
	ListByOwner(ctx context.Context, ownerID string, options ...contracts.DocumentScan) ([]*Document, error)
	UpdateStatus(ctx context.Context, id, status, msg string, chunkCount *int) error
	UpdateTitle(ctx context.Context, id, title string) error
	UpdateKind(ctx context.Context, id, kind string) error
	UpdateMeta(ctx context.Context, id, author, publishedAt, sourceRef string) error
	SetSource(ctx context.Context, id, ownerID string, metadata []byte) error
}

type ChatRepository interface {
	MessagePage(context.Context, string, string, int) ([]*Message, error)
	CreateChat(ctx context.Context, c *Chat) error
	GetChat(ctx context.Context, id string) (*Chat, error)
	ListChats(ctx context.Context, ownerID string, limit, offset int, allOwners bool) ([]*Chat, int64, error)
	AddMessage(ctx context.Context, m *Message) error
	ListMessages(ctx context.Context, chatID string) ([]*Message, error)
}

type ModelRepository interface {
	List(ctx context.Context) ([]*Model, error)
	Upsert(ctx context.Context, m *Model) error
}

// Значения по умолчанию обязаны совпадать с CHECK-ограничениями в SQL.
const (
	StatusActive         = "active"
	DirectionIncrease    = "increase"
	HypothesisGenerated  = "generated"
	MethodClusterKPI     = "cluster_kpi"
	StanceSupports       = "supports"
	ActionCreated        = "created"
	ActionEdited         = "edited"
	KPIDocumentRoleInput = "input"
)

type KPI struct {
	ID           string
	OwnerID      string
	Title        string
	Description  string
	Metric       string
	Unit         string
	Direction    string   // increase | decrease | maintain
	Baseline     *float64 // nil — неизвестна или KPI качественный
	Target       *float64
	FunctionArea string
	Status       string // active | archived
	Detail       []byte // JSONB: ограничения, горизонт, веса
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

// Cluster — только заголовок: принадлежность чанков хранится в векторном хранилище.
type Cluster struct {
	ID              string
	OwnerID         string
	Label           string
	Summary         string
	Keywords        []string
	Method          string // hdbscan | kmeans | ...
	ChunkCount      int
	DocumentCount   int
	Representatives []byte // JSONB: главные документы и фрагменты для объяснимости
	Params          []byte
	Status          string // active | archived
	CreatedAt       time.Time
	UpdatedAt       time.Time
}

type Hypothesis struct {
	ID               string
	Version          int64
	OwnerID          string
	RunID            string // общий для одной пачки генерации
	Title            string
	Statement        string
	Rationale        string
	Method           string // cluster_kpi | transfer | gap | combination | manual
	Status           string // draft | generated | under_review | approved | rejected | archived
	KPIID            *string
	PrimaryClusterID *string
	TRL              *int     // 1..9
	NoveltyScore     *float64 // 0..1
	RiskScore        *float64 // 0..1, больше — рискованнее
	ValueScore       *float64 // 0..1
	ConfidenceScore  *float64 // 0..1
	CompositeScore   *float64 // итог ранжирования; веса задаёт сервис
	Measurable       bool     // false — научная гипотеза без измеримого эффекта
	Organization     string
	FunctionArea     string
	SourceType       string
	Location         string
	Tags             []string
	Assessment       []byte // JSONB: детали novelty/risk/value/confidence/trl
	Detail           []byte // JSONB-ключи: verification, quantitative_parameters, application_potential, drivers
	Generation       []byte // JSONB-ключи: model, prompt_version, inputs, reasoning_trace
	CreatedAt        time.Time
	UpdatedAt        time.Time
	// Evidence заполняет только Get; List оставляет пустым, чтобы запросы доски были дешёвыми.
	Evidence []*Evidence
}

// Evidence: имя файла и фрагмент копируются, чтобы пережить удаление источника.
type Evidence struct {
	ID             string
	HypothesisID   string
	DocumentID     *string // nil после удаления исходного документа
	ChunkID        string  // id в Qdrant/OpenSearch; пусто — уровень документа
	Filename       string
	Snippet        string
	Stance         string // supports | contradicts | context
	Score          *float64
	Ord            int
	CreatedAt      time.Time
	Relation       string
	PageStart      int // страницы с 1; 0 — неизвестно
	PageEnd        int
	SectionHeading string
	Origin         string // "" | input | knowledge | web
}

// Revision — запись журнала правок гипотезы; журнал только дописывается.
type Revision struct {
	ID           string
	HypothesisID string
	RevisionNo   int
	EditorID     string // пусто — система
	Action       string // created | edited | status_changed | score_override | approved | rejected | commented
	Summary      string
	Patch        []byte // JSONB: {"before": ..., "after": ...}
	CreatedAt    time.Time
}

// HypothesisFilter: нулевые поля не фильтруют.
type HypothesisFilter struct {
	BeforeID     string
	Search       string
	Queue        string
	Compact      bool
	OwnerID      string
	Status       string
	KPIID        string
	ClusterID    string
	FunctionArea string
	SourceType   string
	Organization string
	MinTRL       int
	MaxTRL       int
	Tags         []string // гипотезы со ВСЕМИ перечисленными тегами
	DocumentIDs  []string // гипотезы с доказательствами из ЛЮБОГО перечисленного документа
	OrderBy      string   // composite_score (по умолчанию) | diversity | created_at | trl | novelty | value
	Limit        int      // 0 — 100, максимум 500
	Offset       int
}

type KPIRepository interface {
	Create(ctx context.Context, k *KPI) error
	Get(ctx context.Context, id string) (*KPI, error)
	ListByOwner(ctx context.Context, ownerID string) ([]*KPI, error)
	Update(ctx context.Context, k *KPI) error
	Delete(ctx context.Context, id string) error
	AttachDocuments(ctx context.Context, readerID, kpiID string, documentIDs []string, role string) error
	ListDocuments(ctx context.Context, readerID, kpiID string) ([]*KPIDocumentLink, error)
	DetachDocument(ctx context.Context, kpiID, documentID string) error
}

type KPIDocumentLink struct {
	Document   *Document
	Role       string // input | reference
	AttachedAt time.Time
}

type ClusterRepository interface {
	Head(ctx context.Context, ownerID string) (string, error)
	Publish(ctx context.Context, ownerID, requestID, expected, hash string, clusters []*Cluster) ([]*Cluster, error)
	Create(ctx context.Context, c *Cluster) error
	Get(ctx context.Context, id string) (*Cluster, error)
	ListByOwner(ctx context.Context, ownerID string) ([]*Cluster, error)
	Update(ctx context.Context, c *Cluster) error
	DeleteByOwner(ctx context.Context, ownerID string) (int64, error)
}

type HypothesisRepository interface {
	Board(context.Context, HypothesisFilter) ([]*Hypothesis, []byte, error)
	// Create пишет гипотезу, её Evidence и первую ревизию в одной транзакции.
	Create(ctx context.Context, h *Hypothesis, initial *Revision) error
	Get(ctx context.Context, id string) (*Hypothesis, error)
	List(ctx context.Context, f HypothesisFilter) ([]*Hypothesis, error)
	// Update пишет изменяемые поля и необязательную ревизию в одной транзакции.
	Update(ctx context.Context, h *Hypothesis, rev *Revision) error
	ListEvidence(ctx context.Context, hypothesisID string) ([]*Evidence, error)
	AddRevision(ctx context.Context, rev *Revision) error
	ListRevisions(ctx context.Context, hypothesisID string) ([]*Revision, error)
}
