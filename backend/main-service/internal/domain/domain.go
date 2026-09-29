package domain

import (
	"context"
	"github.com/example/main-service/internal/platform/contracts"
	"io"
	"time"

	"google.golang.org/protobuf/proto"

	"github.com/example/main-service/internal/platform/storage"

	commonv1 "github.com/example/main-service/internal/platform/genproto/common/v1"
	dbv1 "github.com/example/main-service/internal/platform/genproto/db/v1"
	llmv1 "github.com/example/main-service/internal/platform/genproto/llm/v1"
)

// UploadCommand.Text заполнен только для text/plain — такие загрузки минуют разборщики.
type UploadCommand struct {
	OwnerID  string
	Filename string
	Size     int64
	Body     io.Reader
	Text     string
	// Hash — BLAKE3-256 тела, считается отдельным проходом до передачи Body в S3: SDK нужен
	// перематываемый поток для контрольных сумм.
	Hash string
	// Reuse при совпадении хэша возвращает уже проиндексированный документ вместо дубля.
	Reuse          bool
	SourceMetadata string
}

type AskCommand struct {
	RequestID string
	OwnerID   string
	ChatID    string
	Content   string
}

// ObjectStore пишет поток, не буферизуя объект целиком в памяти.
type ObjectStore interface {
	Put(ctx context.Context, key string, r io.Reader, size int64, contentType string) error
	Get(ctx context.Context, key string) (io.ReadCloser, error)
}

type MultipartStore interface {
	CreateMultipart(ctx context.Context, key, contentType string) (string, error)
	PresignUploadPart(ctx context.Context, key, uploadID string, partNumber int32, ttl time.Duration) (string, error)
	CompleteMultipart(ctx context.Context, key, uploadID string, parts []storage.MultipartPart) error
	AbortMultipart(ctx context.Context, key, uploadID string) error
}

// SessionStore держит сессии загрузок вне памяти процесса, чтобы main-service оставался без состояния.
type SessionStore interface {
	SetJSON(ctx context.Context, key string, v any, ttl time.Duration) error
	GetJSON(ctx context.Context, key string, dest any) (bool, error)
	Del(ctx context.Context, keys ...string) error
}

type DocumentCatalog interface {
	DocumentPage(context.Context, contracts.DocumentQuery) (*dbv1.DocumentPageResponse, error)
	DocumentStats(context.Context, contracts.DocumentQuery) (contracts.DocumentStats, error)
	CreateDocument(ctx context.Context, req *dbv1.CreateDocumentRequest) (*commonv1.Document, error)
	RequeueDocument(ctx context.Context, id string, expected int64, inline string) (*commonv1.Document, error)
	GetDocument(ctx context.Context, id string) (*commonv1.Document, error)
	FindDocumentByHash(ctx context.Context, ownerID, hash string) (*commonv1.Document, error)
	ListDocuments(ctx context.Context, ownerID string, options ...contracts.DocumentScan) ([]*commonv1.Document, error)
	UpdateDocumentStatus(ctx context.Context, id, status, message string, chunkCount *int32) error
}

type ChatCatalog interface {
	MessagePage(context.Context, string, string, int) ([]*commonv1.Message, error)
	BeginChatTurn(context.Context, *dbv1.BeginChatTurnRequest) (*dbv1.BeginChatTurnResponse, error)
	FinishChatTurn(context.Context, *dbv1.FinishChatTurnRequest) (*commonv1.Message, error)
	FailChatTurn(context.Context, *dbv1.ChatTurnLease) error
	CreateChat(ctx context.Context, ownerID, title, source string) (*commonv1.Chat, error)
	GetChat(ctx context.Context, id string) (*commonv1.Chat, error)
	ListChats(ctx context.Context, ownerID string, limit, offset int, allOwners bool) ([]*commonv1.Chat, int64, error)
	AddMessage(
		ctx context.Context, chatID, role, content string, sources []*commonv1.Source, meta string,
	) (*commonv1.Message, error)
	ListMessages(ctx context.Context, chatID string) ([]*commonv1.Message, error)
	ListModels(ctx context.Context) ([]*commonv1.Model, error)
}

// UserCatalog не заполняет хэши паролей.
type UserCatalog interface {
	ListUsers(ctx context.Context) ([]*commonv1.User, error)
}

type ChunkReader interface {
	DocumentChunks(ctx context.Context, ownerID, documentID string) ([]*llmv1.DocumentChunk, error)
}

// HypothesisCatalog не ограничивает выдачу владельцем — это делает слой приложения.
type HypothesisCatalog interface {
	HypothesisBoard(context.Context, *dbv1.ListHypothesesRequest) (*dbv1.ListHypothesesResponse, error)
	CreateKPI(ctx context.Context, req *dbv1.CreateKPIRequest) (*commonv1.KPI, error)
	GetKPI(ctx context.Context, id string) (*commonv1.KPI, error)
	ListKPIs(ctx context.Context, ownerID string) ([]*commonv1.KPI, error)
	UpdateKPI(ctx context.Context, kpi *commonv1.KPI) error
	DeleteKPI(ctx context.Context, id string) error
	AttachKPIDocuments(ctx context.Context, readerID, kpiID string, documentIDs []string, role string) error
	ListKPIDocuments(ctx context.Context, readerID, kpiID string) ([]*dbv1.KpiDocumentLink, error)
	DetachKPIDocument(ctx context.Context, kpiID, documentID string) error
	ListDocuments(ctx context.Context, ownerID string, options ...contracts.DocumentScan) ([]*commonv1.Document, error)

	CreateCluster(ctx context.Context, req *dbv1.CreateClusterRequest) (*commonv1.Cluster, error)
	ClusterHead(ctx context.Context, ownerID string) (string, error)
	PublishClusters(ctx context.Context, req *dbv1.PublishClustersRequest) ([]*commonv1.Cluster, error)
	GetCluster(ctx context.Context, id string) (*commonv1.Cluster, error)
	ListClusters(ctx context.Context, ownerID string) ([]*commonv1.Cluster, error)
	UpdateCluster(ctx context.Context, cluster *commonv1.Cluster) error
	DeleteClusters(ctx context.Context, ownerID string) error

	CreateHypothesis(ctx context.Context, req *dbv1.CreateHypothesisRequest) (*commonv1.Hypothesis, error)
	GetHypothesis(ctx context.Context, id string) (*commonv1.Hypothesis, error)
	ListHypotheses(ctx context.Context, req *dbv1.ListHypothesesRequest) ([]*commonv1.Hypothesis, error)
	UpdateHypothesis(ctx context.Context, req *dbv1.UpdateHypothesisRequest) error
	AddHypothesisRevision(ctx context.Context, rev *commonv1.HypothesisRevision) (*commonv1.HypothesisRevision, error)
	ListHypothesisRevisions(ctx context.Context, hypothesisID string) ([]*commonv1.HypothesisRevision, error)
	ListHypothesisEvidence(ctx context.Context, hypothesisID string) ([]*commonv1.HypothesisEvidence, error)
}

type EventPublisher interface {
	PublishProto(ctx context.Context, exchange, routingKey string, msg proto.Message) error
}

type Answerer interface {
	Answer(ctx context.Context, req *commonv1.RagRequest) (*commonv1.RagResponse, error)
}
