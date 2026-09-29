package application

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/rs/zerolog"

	"github.com/example/llm-service/internal/domain"
	"github.com/example/llm-service/internal/platform/aiclients"
	"github.com/example/llm-service/internal/platform/logger"
)

// Общепринятая константа RRF: гасит вклад хвостов, чтобы ни один из двух поисков не перевешивал другой.
const rrfK = 60

const snippetLen = 200

const extractiveFallbackMaxSources = 3

// Ответ без генератора намеренно не кэшируется: как только LLM вернётся, пойдут обычные ответы.
const extractiveFallbackModel = "extractive-fallback"

// Сводка RAPTOR выигрывает поиск по широким запросам, но источником не является — см. expandSummaries.
const nodeTypeRaptorSummary = "raptor_summary"

// Не даёт одной сводке залить контекст и список ссылок всем своим поддеревом.
const maxSummaryExpansion = 6

type StageRecorder interface {
	RecordStage(stage string, seconds float64)
}

type RAGService struct {
	cacheRevision      func(context.Context) string
	sourceIndexes      SourceIndexReader
	requireSourceReady bool
	embedder           domain.Embedder
	retrieval          domain.Retriever // векторная половина гибридного поиска (Qdrant)
	lexical            domain.Retriever
	ranker             domain.Ranker
	answerer           domain.Answerer
	cache              domain.Cache
	chunks             domain.ChunkSource
	load               domain.LoadMarker
	metrics            StageRecorder

	topK int
	// Режим «Справка»: поиск по всей базе без фильтра владельца; чаты и загрузки остаются приватными.
	sharedCorpus bool

	// Ниже этого лучшего балла реранкера модель не зовём: на слабом контексте она выдумывает.
	scoreFloor float64
	// У заглушки-реранкера (Жаккар) другая шкала баллов, поэтому без настоящего реранкера порог не применяется.
	rerankerActive bool

	maxPerDoc     int
	vecWeight     float64
	lexWeight     float64
	retrievalMult int
	retrievalMin  int

	agentic *agenticController
}

const abstainAnswer = "Не удалось найти достаточно релевантный контекст в базе знаний, " +
	"чтобы дать обоснованный ответ."

type Tuning struct {
	MaxPerDoc     int // потолок фрагментов одного документа в топ-K; 0 — без потолка
	VecWeight     float64
	LexWeight     float64
	RetrievalMult int // во сколько раз брать кандидатов сверх топ-K
	RetrievalMin  int
}

func New(
	embedder domain.Embedder,
	retrieval domain.Retriever,
	lexical domain.Retriever,
	ranker domain.Ranker,
	answerer domain.Answerer,
	cache domain.Cache,
	chunks domain.ChunkSource,
	load domain.LoadMarker,
	metrics StageRecorder,
	topK int,
	sharedCorpus bool,
	scoreFloor float64,
	rerankerActive bool,
	tuning Tuning,
) *RAGService {
	if topK < 1 {
		topK = 1
	}
	if tuning.VecWeight <= 0 {
		tuning.VecWeight = 1
	}
	if tuning.LexWeight <= 0 {
		tuning.LexWeight = 1
	}
	if tuning.RetrievalMult <= 0 {
		tuning.RetrievalMult = 4
	}
	if tuning.RetrievalMin <= 0 {
		tuning.RetrievalMin = 20
	}
	return &RAGService{
		embedder:  embedder,
		retrieval: retrieval,
		lexical:   lexical,
		ranker:    ranker,
		answerer:  answerer,
		cache:     cache,
		chunks:    chunks,
		load:      load,
		metrics:   metrics,
		topK:      topK,

		sharedCorpus:   sharedCorpus,
		scoreFloor:     scoreFloor,
		rerankerActive: rerankerActive,

		maxPerDoc:     tuning.MaxPerDoc,
		vecWeight:     tuning.VecWeight,
		lexWeight:     tuning.LexWeight,
		retrievalMult: tuning.RetrievalMult,
		retrievalMin:  tuning.RetrievalMin,
	}
}

// Неверные сертификат, ключ, модель или адрес сами не пройдут: это ошибка с именем настройки, остальное — предупреждение.
func logGenerationFailure(log zerolog.Logger, err error, sources int) {
	event := log.Warn()
	if reason := aiclients.ConfigFailure(err); reason != "" {
		event = log.Error().Str("configuration", reason)
	}
	event.Err(err).Int("sources", sources).Msg("generation failed; returning extractive fallback")
}

func (s *RAGService) answer(ctx context.Context, q domain.Query) (domain.Result, error) {
	if s.agentic != nil {
		if res, handled, err := s.agentic.answer(ctx, s, q); err != nil || handled {
			return res, err
		}
	}

	log := logger.From(ctx).With().Str("owner_id", q.OwnerID).Logger()

	// Пока идёт интерактивный запрос, пакетная индексация уступает ему мощности.
	if s.load != nil {
		s.load.QueryStarted(ctx)
		defer s.load.QueryFinished(ctx)
	}

	scope := q.OwnerID
	if s.sharedCorpus {
		scope = ""
	}

	tr := &domain.Trace{ScoreFloor: s.scoreFloor}
	overall := time.Now()
	stage := s.stageFn(tr)

	scopeKey := scope
	if scopeKey == "" {
		scopeKey = "shared"
	}
	key := cacheKey(scopeKey, s.cache.Epoch(ctx, scopeKey), q.Text, q.Prompt, s.answerCacheScope(ctx, q))
	if res, hit := s.cachedAnswer(ctx, log, key); hit {
		return res, nil
	}

	tEmbed := time.Now()
	embedding, err := s.embedder.Embed(ctx, q.Text)
	if err != nil {
		log.Warn().Err(err).Msg("embed failed; continuing lexical-only")
		embedding = nil
		tr.MarkDegraded(domain.DegradedEmbedUnavailable)
	}
	stage("embed", tEmbed)

	ranked, rerankOK := s.retrieveAndRank(ctx, log, q, embedding, scope, stage, tr)

	expanded, raptorExpanded := expandSummaries(ranked)
	tr.RaptorExpanded = raptorExpanded
	// Структурированные проверки должны сверять цитаты со всем переданным текстом, а не с коротким превью.
	contextText, sources := buildEvidence(expanded, q.Prompt != "")

	if res, abstained := s.maybeAbstain(log, ranked, sources, rerankOK, tr); abstained {
		stage("total", overall)
		s.cacheResult(ctx, log, key, res, tr.Degraded)
		return res, nil
	}
	if q.Prompt == "" {
		expanded = s.withEvidenceNeighbors(ctx, q.OwnerID, expanded)
		contextText, sources = buildEvidence(expanded, false)
		tr.CandidatesReturned = len(sources)
	}

	// Инструкция ссылок — только для чата: вызовам со своим Prompt (генерация, проверка гипотез) нужен нетронутый JSON.
	tGen := time.Now()
	genQuestion := q.Text
	cite := true
	if q.Prompt != "" {
		genQuestion = q.Prompt
		cite = false
	}
	answer, model, err := s.answerer.Answer(ctx, contextText, genQuestion, cite)
	if err != nil {
		if cite && len(sources) > 0 {
			stage("generate", tGen)
			stage("total", overall)
			logGenerationFailure(log, err, len(sources))
			return domain.Result{
				Answer:  extractiveFallbackAnswer(sources),
				Sources: sources,
				Model:   extractiveFallbackModel,
				Cached:  false,
				Trace:   tr,
			}, nil
		}
		return domain.Result{}, fmt.Errorf("generate answer: %w", err)
	}
	stage("generate", tGen)

	if cite {
		answer = sanitizeAnswerCitations(log, answer, sources, tr)
	}

	res := domain.Result{
		Answer:  answer,
		Sources: sources,
		Model:   model,
		Cached:  false,
		Trace:   tr,
	}

	stage("total", overall)
	s.cacheResult(ctx, log, key, res, tr.Degraded)
	log.Info().Int("sources", len(sources)).Str("model", model).Msg("answered")
	return res, nil
}

func (s *RAGService) stageFn(tr *domain.Trace) func(string, time.Time) {
	return func(name string, start time.Time) {
		d := time.Since(start)
		if s.metrics != nil {
			s.metrics.RecordStage(name, d.Seconds())
		}
		tr.AddStage(traceStage(name), d)
	}
}

func traceStage(name string) string {
	if name == "retrieve_vector" || name == "retrieve_lexical" {
		return "retrieve"
	}
	return name
}

// Деградированный результат не кэшируем: он пережил бы сбой, который его вызвал.
func (s *RAGService) cacheResult(ctx context.Context, log zerolog.Logger, key string, res domain.Result, degraded bool) {
	if degraded {
		return
	}
	if initial, ok := ctx.Value(cacheRevisionKey{}).(string); ok && initial != s.currentCacheRevision(ctx) {
		return
	}
	if err := s.cache.Set(ctx, key, res); err != nil {
		log.Warn().Err(err).Msg("cache set failed")
	}
}

func (s *RAGService) cachedAnswer(ctx context.Context, log zerolog.Logger, key string) (domain.Result, bool) {
	res, hit, err := s.cache.Get(ctx, key)
	if err != nil {
		log.Warn().Err(err).Msg("cache get failed")
		return domain.Result{}, false
	}
	if !hit {
		return domain.Result{}, false
	}
	log.Info().Msg("cache hit")
	res.Cached = true
	return res, true
}

// Отказ любой ноги поиска или реранкера не роняет запрос, а помечает трассу деградированной.
// rerankOK — пересчитал ли баллы настоящий реранкер, то есть применим ли порог.
func (s *RAGService) retrieveAndRank(
	ctx context.Context,
	log zerolog.Logger,
	q domain.Query,
	embedding []float32,
	scope string,
	stage func(string, time.Time),
	tr *domain.Trace,
) (ranked []domain.RetrievedChunk, rerankOK bool) {
	topN := retrievalDepth(q.TopK, s.topK, s.retrievalMult, s.retrievalMin)
	filter := domain.RetrievalFilter{
		OwnerID:            scope,
		ScopeDocumentIDs:   q.ScopeDocumentIDs,
		ExcludeDocumentIDs: q.ExcludeDocumentIDs,
	}
	if s.sourceIndexes != nil {
		filter.OwnerID = "" // доступ решает политика источников, она же пускает явно публичные
	}
	var vectorHits []domain.RetrievedChunk
	if embedding != nil {
		tVec := time.Now()
		hits, vecOK := s.retrieve(ctx, log, "qdrant", s.retrieval, q.Text, embedding, filter, topN)
		stage("retrieve_vector", tVec)
		if !vecOK {
			tr.MarkDegraded(domain.DegradedDenseUnavailable)
		}
		vectorHits = hits
	}
	tLex := time.Now()
	lexicalHits, lexOK := s.retrieve(ctx, log, "opensearch", s.lexical, q.Text, embedding, filter, topN)
	stage("retrieve_lexical", tLex)
	if !lexOK {
		tr.MarkDegraded(domain.DegradedLexicalUnavailable)
	}
	vectorHits = s.allowedHits(ctx, q.OwnerID, vectorHits, tr)
	lexicalHits = s.allowedHits(ctx, q.OwnerID, lexicalHits, tr)
	tagOrigin(vectorHits, domain.OriginDense)
	tagOrigin(lexicalHits, domain.OriginBM25)
	tr.CandidatesDense += len(vectorHits)
	tr.CandidatesLexical += len(lexicalHits)

	fused := fuse([]weightedList{{s.vecWeight, vectorHits}, {s.lexWeight, lexicalHits}})
	tr.CandidatesFused += len(fused)

	// Сбой реранкера оставляет порядок RRF; порог тогда не применяется — он откалиброван по баллам реранкера.
	tRerank := time.Now()
	ranked, rerankOK = fused, true
	if scored, err := s.rerank(ctx, log, q.Text, fused); err != nil {
		log.Warn().Err(err).Msg("reranker failed; keeping fusion order")
		tr.MarkDegraded(domain.DegradedRerankerUnavailable)
		rerankOK = false
	} else {
		ranked = scored
	}
	stage("rerank", tRerank)
	// TopK запроса важнее RAG_TOP_K: генерации гипотез нужно больше доказательств, чем чату.
	eff := s.topK
	if q.TopK > 0 {
		eff = q.TopK
	}
	ranked = capPerDocument(ranked, eff, s.maxPerDoc)
	tr.CandidatesReturned += len(ranked)
	return ranked, rerankOK
}

func tagOrigin(chunks []domain.RetrievedChunk, origin string) {
	for i := range chunks {
		chunks[i].Origin = origin
	}
}

// false — бэкенд ответил ошибкой (деградация, возможно временная), в отличие от исправного, но ничего не нашедшего.
func (s *RAGService) retrieve(
	ctx context.Context,
	log zerolog.Logger,
	name string,
	r domain.Retriever,
	query string,
	embedding []float32,
	f domain.RetrievalFilter,
	topN int,
) ([]domain.RetrievedChunk, bool) {
	hits, err := r.Retrieve(ctx, query, embedding, f, topN)
	if err != nil {
		log.Warn().Str("retriever", name).Err(err).Msg("retriever failed; continuing with no results")
		return nil, false
	}
	return hits, true
}

func (s *RAGService) rerank(
	ctx context.Context,
	log zerolog.Logger,
	query string,
	chunks []domain.RetrievedChunk,
) ([]domain.RetrievedChunk, error) {
	if len(chunks) == 0 {
		return chunks, nil
	}
	scores, err := s.ranker.Rank(ctx, query, chunks)
	if err != nil {
		return nil, err
	}
	if len(scores) != len(chunks) {
		log.Warn().Int("scores", len(scores)).Int("chunks", len(chunks)).
			Msg("reranker returned mismatched score count; keeping fusion order")
		return chunks, nil
	}
	for i := range chunks {
		chunks[i].Score = scores[i]
	}
	sort.SliceStable(chunks, func(i, j int) bool {
		return chunks[i].Score > chunks[j].Score
	})
	return chunks, nil
}

// Без источников отказываемся всегда, при любом реранкере: иначе пустой поиск дал бы уверенный ответ без опоры.
// Порог — только если настоящий реранкер реально пересчитал баллы: у заглушки и у порядка RRF другая шкала.
func (s *RAGService) maybeAbstain(
	log zerolog.Logger,
	ranked []domain.RetrievedChunk,
	sources []domain.Source,
	rerankOK bool,
	tr *domain.Trace,
) (domain.Result, bool) {
	empty := len(sources) == 0
	topScore, hasScore := topRerankScore(ranked)
	if rerankOK && hasScore {
		tr.TopScore = topScore
	}
	weak := false
	if !empty && s.rerankerActive && rerankOK {
		weak = !hasScore || topScore < s.scoreFloor
	}
	if !empty && !weak {
		return domain.Result{}, false
	}
	if empty {
		tr.AbstainReason = domain.AbstainNoSources
	} else {
		tr.AbstainReason = domain.AbstainWeakEvidence
	}
	log.Warn().
		Bool("empty", empty).
		Bool("weak", weak).
		Bool("degraded", tr.Degraded).
		Float64("top_score", topScore).
		Float64("score_floor", s.scoreFloor).
		Msg("answer abstained: insufficient context")
	return domain.Result{Answer: abstainAnswer, Sources: sources, Trace: tr}, true
}

// Кандидатов берём с запасом над топ-K, чтобы слиянию и реранкеру было из чего выбирать.
func retrievalDepth(requestTopK, defaultTopK, mult, minDepth int) int {
	k := requestTopK
	if k < 1 {
		k = defaultTopK
	}
	depth := k * mult
	if depth < minDepth {
		depth = minDepth
	}
	return depth
}

// Сводка RAPTOR заменяется своими листьями, чтобы модель опиралась и ссылалась на реальные фрагменты,
// а не на синтетический пересказ. Листья наследуют балл и происхождение сводки.
func expandSummaries(ranked []domain.RetrievedChunk) ([]domain.RetrievedChunk, int) {
	hasSummary := false
	for i := range ranked {
		if ranked[i].NodeType == nodeTypeRaptorSummary && len(ranked[i].Members) > 0 {
			hasSummary = true
			break
		}
	}
	if !hasSummary {
		return ranked, 0
	}
	d := &chunkDedup{
		out:  make([]domain.RetrievedChunk, 0, len(ranked)),
		seen: make(map[string]bool, len(ranked)),
	}
	expanded := 0
	for _, c := range ranked {
		if c.NodeType == nodeTypeRaptorSummary && len(c.Members) > 0 {
			expanded++
			d.expandInto(c)
			continue
		}
		d.add(c)
	}
	return d.out, expanded
}

type chunkDedup struct {
	out  []domain.RetrievedChunk
	seen map[string]bool
}

func (d *chunkDedup) add(c domain.RetrievedChunk) bool {
	if c.ID != "" {
		if d.seen[c.ID] {
			return false
		}
		d.seen[c.ID] = true
	}
	d.out = append(d.out, c)
	return true
}

func (d *chunkDedup) expandInto(sum domain.RetrievedChunk) {
	added := 0
	for _, m := range sum.Members {
		if added >= maxSummaryExpansion {
			return
		}
		if sum.ID != "" && m.ID == sum.ID {
			continue
		}
		m.Score = sum.Score
		m.Origin = sum.Origin
		m.RaptorSummaryID = sum.ID
		if d.add(m) {
			added++
		}
	}
}

// Эпоха корпуса в ключе: переиндексация сама отправляет старые ответы в отставку, без обхода ключей.
func cacheKey(scope string, epoch int64, query, prompt, docScope string) string {
	sum := sha256.Sum256([]byte(query + "\x00" + prompt + "\x00" + docScope))
	return "answer:" + scope + ":e" + strconv.FormatInt(epoch, 10) + ":" + hex.EncodeToString(sum[:])
}

func docScopeKey(q domain.Query) string {
	if len(q.ScopeDocumentIDs) == 0 && len(q.ExcludeDocumentIDs) == 0 && q.AccessRevision == "" {
		return ""
	}
	scope := append([]string(nil), q.ScopeDocumentIDs...)
	excl := append([]string(nil), q.ExcludeDocumentIDs...)
	sort.Strings(scope)
	sort.Strings(excl)
	key := "s:" + strings.Join(scope, ",") + "|x:" + strings.Join(excl, ",")
	if q.AccessRevision != "" {
		key += "|access:" + q.AccessRevision
	}
	return key
}
