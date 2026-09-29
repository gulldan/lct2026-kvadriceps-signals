package domain

import (
	"context"
	"time"
)

const (
	OriginDense = "dense"
	OriginBM25  = "bm25"
	OriginBoth  = "both"
)

const (
	DegradedEmbedUnavailable    = "embed_unavailable"
	DegradedDenseUnavailable    = "dense_unavailable"
	DegradedLexicalUnavailable  = "lexical_unavailable"
	DegradedRerankerUnavailable = "reranker_unavailable"
)

const (
	AbstainNoSources    = "no_sources"
	AbstainWeakEvidence = "weak_evidence"
)

type Query struct {
	OwnerID string
	Text    string
	TopK    int
	// Prompt, если задан, идёт в генерацию, а Text — только в поиск и реранк.
	Prompt           string
	ScopeDocumentIDs []string
	// ExcludeDocumentIDs не даёт входным данным цели засчитаться её же «мировой практикой».
	ExcludeDocumentIDs []string
	AccessRevision     string
}

// Source хранит смещения CharStart/CharEnd полуинтервалом в рунах, а не в байтах.
type Source struct {
	DocumentID      string  `json:"document_id"`
	Filename        string  `json:"filename"`
	ChunkID         string  `json:"chunk_id"`
	Snippet         string  `json:"snippet"`
	Score           float64 `json:"score"`
	CharStart       int     `json:"char_start,omitempty"`
	CharEnd         int     `json:"char_end,omitempty"`
	PageStart       int     `json:"page_start,omitempty"`
	PageEnd         int     `json:"page_end,omitempty"`
	SectionHeading  string  `json:"section_heading,omitempty"`
	Origin          string  `json:"origin,omitempty"`
	RaptorSummaryID string  `json:"raptor_summary_id,omitempty"`
	IndexSnapshot   string  `json:"source_index_snapshot,omitempty"`
}

type TraceStage struct {
	Stage  string `json:"stage"`
	Millis int64  `json:"millis"`
}

// Trace едет в кэш вместе с Result, поэтому ответ из кэша несёт трассу исходного прогона.
type Trace struct {
	Stages             []TraceStage `json:"stages,omitempty"`
	CandidatesDense    int          `json:"candidates_dense,omitempty"`
	CandidatesLexical  int          `json:"candidates_lexical,omitempty"`
	CandidatesFused    int          `json:"candidates_fused,omitempty"`
	CandidatesReturned int          `json:"candidates_returned,omitempty"`
	Degraded           bool         `json:"degraded,omitempty"`
	DegradedReason     string       `json:"degraded_reason,omitempty"`
	AbstainReason      string       `json:"abstain_reason,omitempty"`
	TopScore           float64      `json:"top_score,omitempty"`
	ScoreFloor         float64      `json:"score_floor,omitempty"`
	Cited              []string     `json:"cited,omitempty"`
	UncitedRemoved     []string     `json:"uncited_removed,omitempty"`
	RaptorExpanded     int          `json:"raptor_expanded,omitempty"`
}

func (t *Trace) AddStage(stage string, d time.Duration) {
	ms := d.Milliseconds()
	for i := range t.Stages {
		if t.Stages[i].Stage == stage {
			t.Stages[i].Millis += ms
			return
		}
	}
	t.Stages = append(t.Stages, TraceStage{Stage: stage, Millis: ms})
}

// MarkDegraded оставляет первую причину: она ближе всех к истоку сбоя.
func (t *Trace) MarkDegraded(reason string) {
	t.Degraded = true
	if t.DegradedReason == "" {
		t.DegradedReason = reason
	}
}

// Result хранится в кэше Valkey как JSON, поэтому JSON-теги — часть формата кэша.
type Result struct {
	Answer  string   `json:"answer"`
	Sources []Source `json:"sources"`
	Model   string   `json:"model"`
	Cached  bool     `json:"cached"`
	Trace   *Trace   `json:"trace,omitempty"`
}

type RetrievedChunk struct {
	ID             string // общий id Qdrant и OpenSearch — ключ слияния
	Text           string
	DocumentID     string
	Filename       string
	ChunkIndex     int
	Score          float64
	CharStart      int
	CharEnd        int
	PageStart      int
	PageEnd        int
	SectionHeading string
	Origin         string
	// RaptorSummaryID — id сводки RAPTOR, из которой развёрнут этот лист.
	RaptorSummaryID string
	IndexSnapshot   string
	NodeType        string
	NodeLevel       int
	// MemberIDs перечисляет всех членов сводки, а Members — только урезанную часть для цитирования,
	// денормализованную raptor-worker, чтобы развернуть сводку без лишнего запроса.
	MemberIDs []string
	Members   []RetrievedChunk
}

// Retriever на неподготовленном бэкенде (нет коллекции) возвращает пустой результат, а не ошибку.
type Retriever interface {
	Retrieve(ctx context.Context, query string, embedding []float32, f RetrievalFilter, topN int) ([]RetrievedChunk, error)
}

type RetrievalFilter struct {
	OwnerID            string
	ScopeDocumentIDs   []string
	ExcludeDocumentIDs []string
}

// Ranker возвращает по баллу на фрагмент в порядке входного среза.
type Ranker interface {
	Rank(ctx context.Context, query string, chunks []RetrievedChunk) ([]float64, error)
}

type Answerer interface {
	Answer(ctx context.Context, contextText, query string, cite bool) (answer, model string, err error)
}

type Embedder interface {
	Embed(ctx context.Context, text string) ([]float32, error)
}

type Cache interface {
	Get(ctx context.Context, key string) (Result, bool, error)
	Set(ctx context.Context, key string, res Result) error
	Epoch(ctx context.Context, scope string) int64
}

type StoredChunk struct {
	ID             string
	Index          int
	Text           string
	IndexSnapshot  string
	Filename       string
	CharStart      int
	CharEnd        int
	PageStart      int
	PageEnd        int
	SectionHeading string
}

type ChunkSource interface {
	DocumentChunks(ctx context.Context, ownerID, documentID string) ([]StoredChunk, error)
}

// LoadMarker просит chunk-splitter уступать мощности, пока идут интерактивные запросы;
// сбой маркера не должен ронять запрос.
type LoadMarker interface {
	QueryStarted(ctx context.Context)
	QueryFinished(ctx context.Context)
}

type Reasoner interface {
	Reason(ctx context.Context, system, user string) (string, error)
}

type GraphExpander interface {
	Expand(ctx context.Context, ownerID string, seedDocIDs []string, topN int) ([]string, error)
}
