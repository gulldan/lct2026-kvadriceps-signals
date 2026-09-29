package application

import (
	"context"
	"google.golang.org/protobuf/proto"
	"time"
)

type StatusUpdater interface {
	UpdateDocumentStatus(ctx context.Context, id, status, message string, chunkCount *int32) error
}

// Titler: nil отключает проход LLM, но эвристика вида документа всё равно работает.
type Titler interface {
	Extract(ctx context.Context, text string) (title, kind string)
}

type TitleSetter interface {
	SetDocumentTitle(ctx context.Context, id, title string) error
}

type KindSetter interface {
	SetDocumentKind(ctx context.Context, id, kind string) error
}

type MetaSetter interface {
	SetDocumentMeta(ctx context.Context, id, author, publishedAt, sourceRef string) error
}

type Embedder interface {
	Embed(ctx context.Context, texts []string) ([][]float32, error)
}

// Tokenizer считает токены настоящим токенизатором модели эмбеддингов, чтобы чанк укладывался
// в бюджет токенов, а не рун.
type Tokenizer interface {
	CountTokens(ctx context.Context, text string) (int, error)
}

// VectorPoint: CharStart/CharEnd — абсолютные смещения в рунах; [0, 0] значит «не найдено»,
// неверное смещение недопустимо. Страницы нумеруются с 1, 0 — структуры страниц нет.
type VectorPoint struct {
	ID             string
	Vector         []float32
	DocumentID     string
	OwnerID        string
	Filename       string
	Text           string
	ChunkIndex     int
	CharStart      int
	CharEnd        int
	PageStart      int
	PageEnd        int
	SectionHeading string
	Metadata       map[string]string
	NodeType       string
	NodeLevel      int
	MemberIDs      []string
}

type VectorStore interface {
	Upsert(ctx context.Context, points []VectorPoint) error
}

type SearchDoc struct {
	SourceText string
	ID         string
	Text       string
	DocumentID string
	OwnerID    string
	Filename   string
	Section    string
	Metadata   map[string]string
}

type SearchStore interface {
	Index(ctx context.Context, docs []SearchDoc) error
}

type Publisher interface {
	PublishProto(ctx context.Context, exchange, routingKey string, msg proto.Message) error
}

// Pacer уступает мощность интерактивному трафику, но не морит индексацию голодом: она обязана продвигаться.
type Pacer interface {
	Wait(ctx context.Context)
}

type ObjectStore interface {
	GetBytes(ctx context.Context, key string) ([]byte, error)
}

type StageRecorder interface {
	RecordStage(stage string, seconds float64)
}

type stageTimes struct {
	split time.Duration
	embed time.Duration
	index time.Duration
}
