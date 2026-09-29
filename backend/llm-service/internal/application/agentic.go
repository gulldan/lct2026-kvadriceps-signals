package application

import (
	"context"
	"fmt"
	"regexp"
	"sort"
	"strings"

	"github.com/rs/zerolog"

	"github.com/example/llm-service/internal/domain"
	"github.com/example/llm-service/internal/platform/logger"
)

// Ложное срабатывание стоит лишь задержки, а пропуск безопасен (остаётся обычный путь) — отсюда осторожный порог.
const (
	gateThreshold         = 2
	gateComparativeWeight = 2 // сравнение само по себе уже достигает порога
	gateMinQuestions      = 2
	gateMinClauses        = 2
	gateMinEntities       = 2
	gateLongQueryTokens   = 18
)

// Короткие отрывки держат цикл рассуждения дешёвым, а потолок пула — список ссылок обозримым.
const (
	condenseChunkClip  = 600
	agenticMaxEvidence = 12
	graphChunksPerDoc  = 3
	condenseNone       = "NONE"
)

// Рассуждающая модель только выбирает следующий поиск или стоп, итоговый ответ со ссылками пишет генератор.
const reasonSystemPrompt = "You are a meticulous research controller working a question step by step. " +
	"You are given the question and the facts gathered so far from a document corpus. " +
	"Decide whether those facts already answer the question. " +
	"Reply with EXACTLY ONE of: the token [answer] on its own line if the facts suffice, " +
	"or a single line '[search] <query>' requesting ONE focused follow-up search for a specific missing fact. " +
	"Do not answer the question yourself and do not add commentary."

const condenseSystemPrompt = "You distill retrieved passages into only the facts relevant to a question " +
	"(a reason-in-documents step). Read the passages and extract ONLY facts that help answer the question, " +
	"as a few short sentences. Omit anything irrelevant. If no passage is relevant, reply with exactly NONE."

var searchActionRe = regexp.MustCompile(`(?im)^\s*\[search]\s*(.+?)\s*$`)

var (
	comparativeRe = regexp.MustCompile(`(?i)(compare|comparison|compared to|versus| vs\.?\b|` +
		`difference|differ|trade-?off|pros and cons|relationship between|` +
		`сравн|различ|разниц|отлич|взаимосвяз|соотнош|по сравнению)`)
	clauseRe = regexp.MustCompile(`(?i)(\band\b|\band then\b|\bas well as\b|\bor\b` +
		`| и | или | а также | затем | потом )`)
	quotedRe   = regexp.MustCompile(`"[^"]{1,80}"|«[^»]{1,80}»`)
	capMultiRe = regexp.MustCompile(`[A-ZА-ЯЁ][\p{L}]+(?:[ \-][A-ZА-ЯЁ][\p{L}]+)+`)
	acronymRe  = regexp.MustCompile(`\b[A-ZА-ЯЁ]{2,6}\b`)
	capWordRe  = regexp.MustCompile(`^[A-ZА-ЯЁ][\p{Ll}]+$`)
)

type AgenticConfig struct {
	MaxHops   int
	GraphTopN int
}

type agenticController struct {
	cfg      AgenticConfig
	reasoner domain.Reasoner
	graph    domain.GraphExpander
}

type agentAction struct {
	search bool
	query  string
}

func (s *RAGService) EnableAgentic(cfg AgenticConfig, reasoner domain.Reasoner, graph domain.GraphExpander) {
	if cfg.MaxHops < 1 {
		cfg.MaxHops = 1
	}
	if cfg.GraphTopN < 1 {
		cfg.GraphTopN = 5
	}
	s.agentic = &agenticController{cfg: cfg, reasoner: reasoner, graph: graph}
}

func (a *agenticController) answer(ctx context.Context, s *RAGService, q domain.Query) (domain.Result, bool, error) {
	escalate, reasons := agenticGate(q)
	if !escalate {
		return domain.Result{}, false, nil
	}
	log := logger.From(ctx).With().Str("owner_id", q.OwnerID).Str("mode", "agentic").Logger()
	log.Info().Strs("gate", reasons).Int("max_hops", a.cfg.MaxHops).Msg("agentic escalation")

	if s.load != nil {
		s.load.QueryStarted(ctx)
		defer s.load.QueryFinished(ctx)
	}

	scope := q.OwnerID
	if s.sharedCorpus {
		scope = ""
	}
	scopeKey := scope
	if scopeKey == "" {
		scopeKey = "shared"
	}
	key := cacheKey(scopeKey, s.cache.Epoch(ctx, scopeKey), q.Text, q.Prompt, s.answerCacheScope(ctx, q))
	if res, hit := s.cachedAnswer(ctx, log, key); hit {
		return res, true, nil
	}

	tr := &domain.Trace{ScoreFloor: s.scoreFloor}
	pool, rerankOK, err := a.gather(ctx, s, log, q, scope, tr)
	if err != nil {
		return domain.Result{}, true, err
	}
	res, err := a.finalize(ctx, s, log, q, pool, key, tr, rerankOK)
	return res, true, err
}

// Первый шаг всегда ищет исходный вопрос, так что покрытие не хуже обычного пути. rerankOK ложен, если хоть на одном
// шаге баллы не пересчитал настоящий реранкер: тогда порог неприменим.
func (a *agenticController) gather(
	ctx context.Context, s *RAGService, log zerolog.Logger, q domain.Query, scope string, tr *domain.Trace,
) ([]domain.RetrievedChunk, bool, error) {
	stage := s.stageFn(tr)
	pool := make([]domain.RetrievedChunk, 0, agenticMaxEvidence)
	findings := make([]string, 0, a.cfg.MaxHops)
	rerankOK := true
	subquery := strings.TrimSpace(q.Text)

	for hop := 1; hop <= a.cfg.MaxHops; hop++ {
		embedding, err := s.embedder.Embed(ctx, subquery)
		if err != nil {
			return nil, rerankOK, fmt.Errorf("agentic embed (hop %d): %w", hop, err)
		}
		subQ := domain.Query{
			OwnerID: q.OwnerID, Text: subquery, TopK: q.TopK, Prompt: "",
			ScopeDocumentIDs: q.ScopeDocumentIDs, ExcludeDocumentIDs: q.ExcludeDocumentIDs,
		}
		ranked, hopRerankOK := s.retrieveAndRank(ctx, log, subQ, embedding, scope, stage, tr)
		rerankOK = rerankOK && hopRerankOK
		ranked = a.expandWithGraph(ctx, s, log, q, subquery, scope, pool, ranked)
		pool = mergeEvidence(pool, ranked)
		log.Info().Int("hop", hop).Str("subquery", subquery).
			Int("hits", len(ranked)).Int("pool", len(pool)).Msg("agentic hop")

		if finding := a.condense(ctx, log, q.Text, subquery, ranked); finding != "" {
			findings = append(findings, finding)
		}
		if hop == a.cfg.MaxHops {
			break
		}
		action := a.nextAction(ctx, log, q.Text, findings)
		if !action.search {
			log.Info().Int("hop", hop).Msg("agentic reasoner signalled final answer")
			break
		}
		subquery = action.query
	}
	return pool, rerankOK, nil
}

func (a *agenticController) finalize(
	ctx context.Context, s *RAGService, log zerolog.Logger,
	q domain.Query, pool []domain.RetrievedChunk, key string, tr *domain.Trace, rerankOK bool,
) (domain.Result, error) {
	expanded, raptorExpanded := expandSummaries(pool)
	tr.RaptorExpanded = raptorExpanded
	_, sources := buildEvidence(expanded, false)

	if res, abstained := s.maybeAbstain(log, pool, sources, rerankOK, tr); abstained {
		s.cacheResult(ctx, log, key, res, tr.Degraded)
		return res, nil
	}
	expanded = s.withEvidenceNeighbors(ctx, q.OwnerID, expanded)
	contextText, sources := buildEvidence(expanded, false)
	tr.CandidatesReturned = len(sources)

	answer, model, err := s.answerer.Answer(ctx, contextText, q.Text, true)
	if err != nil {
		return domain.Result{}, fmt.Errorf("agentic generate: %w", err)
	}
	answer = sanitizeAnswerCitations(log, answer, sources, tr)

	res := domain.Result{Answer: answer, Sources: sources, Model: model, Cached: false, Trace: tr}
	s.cacheResult(ctx, log, key, res, tr.Degraded)
	log.Info().Int("sources", len(sources)).Str("model", model).Msg("agentic answered")
	return res, nil
}

// Ошибка сжатия не страшна: найденные фрагменты попадают в пул ссылок в любом случае.
func (a *agenticController) condense(
	ctx context.Context, log zerolog.Logger, question, subquery string, chunks []domain.RetrievedChunk,
) string {
	if a.reasoner == nil || len(chunks) == 0 {
		return ""
	}
	var b strings.Builder
	for i, c := range chunks {
		if t := strings.TrimSpace(c.Text); t != "" {
			fmt.Fprintf(&b, "[D%d] %s\n", i+1, clip(t, condenseChunkClip))
		}
	}
	passages := b.String()
	if passages == "" {
		return ""
	}
	user := "Question: " + question + "\nSearch query: " + subquery + "\n\nPassages:\n" + passages
	out, err := a.reasoner.Reason(ctx, condenseSystemPrompt, user)
	if err != nil {
		log.Warn().Err(err).Msg("agentic condense failed; skipping hop findings")
		return ""
	}
	out = strings.TrimSpace(out)
	if out == "" || strings.EqualFold(out, condenseNone) {
		return ""
	}
	return out
}

// Любая ошибка означает стоп, поэтому цикл всегда завершается на уже собранных доказательствах.
func (a *agenticController) nextAction(
	ctx context.Context, log zerolog.Logger, question string, findings []string,
) agentAction {
	if a.reasoner == nil {
		return agentAction{search: false, query: ""}
	}
	var b strings.Builder
	for i, f := range findings {
		fmt.Fprintf(&b, "%d. %s\n", i+1, f)
	}
	user := "Question: " + question + "\n\nFindings so far:\n" + b.String()
	out, err := a.reasoner.Reason(ctx, reasonSystemPrompt, user)
	if err != nil {
		log.Warn().Err(err).Msg("agentic reasoner failed; finalising")
		return agentAction{search: false, query: ""}
	}
	action := parseAction(out)
	log.Info().Bool("search", action.search).Str("next", action.query).Msg("agentic action")
	return action
}

// Соседей по графу пересчитываем тем же реранкером, иначе их баллы несравнимы с остальными кандидатами.
func (a *agenticController) expandWithGraph(
	ctx context.Context, s *RAGService, log zerolog.Logger,
	q domain.Query, subquery, scope string, pool, ranked []domain.RetrievedChunk,
) []domain.RetrievedChunk {
	if a.graph == nil || s.chunks == nil {
		return ranked
	}
	have := docIDSet(pool, ranked)
	seeds := make([]string, 0, len(have))
	for id := range have {
		seeds = append(seeds, id)
	}
	if len(seeds) == 0 {
		return ranked
	}
	sort.Strings(seeds) // детерминированный запрос — стабильные кэш и трасса
	docIDs, err := a.graph.Expand(ctx, scope, seeds, a.cfg.GraphTopN)
	if err != nil {
		log.Warn().Err(err).Msg("agentic graph expand failed; continuing without graph")
		return ranked
	}
	docIDs = filterDocIDs(docIDs, q)
	extra := a.fetchGraphChunks(ctx, s, log, q.OwnerID, docIDs, have)
	if len(extra) == 0 {
		return ranked
	}
	reranked, err := s.rerank(ctx, log, subquery, extra)
	if err != nil {
		log.Warn().Err(err).Msg("agentic graph rerank failed; continuing without graph")
		return ranked
	}
	merged := make([]domain.RetrievedChunk, 0, len(ranked)+len(reranked))
	merged = append(merged, ranked...)
	merged = append(merged, reranked...)
	sort.SliceStable(merged, func(i, j int) bool { return merged[i].Score > merged[j].Score })
	log.Info().Int("graph_docs", len(docIDs)).Int("graph_chunks", len(extra)).Msg("agentic graph expansion")
	return merged
}

// Потолок на документ, чтобы один сосед по графу не занял весь пул.
func (a *agenticController) fetchGraphChunks(
	ctx context.Context, s *RAGService, log zerolog.Logger,
	scope string, docIDs []string, have map[string]bool,
) []domain.RetrievedChunk {
	extra := make([]domain.RetrievedChunk, 0, len(docIDs)*graphChunksPerDoc)
	for _, id := range docIDs {
		if id == "" || have[id] {
			continue
		}
		stored, err := s.Chunks(ctx, scope, id)
		if err != nil {
			log.Warn().Str("document_id", id).Err(err).Msg("agentic graph chunk fetch failed; skipping")
			continue
		}
		for i, sc := range stored {
			if i >= graphChunksPerDoc {
				break
			}
			extra = append(extra, domain.RetrievedChunk{
				ID: sc.ID, Text: sc.Text, DocumentID: id, ChunkIndex: sc.Index, IndexSnapshot: sc.IndexSnapshot,
			})
		}
	}
	return extra
}

func filterDocIDs(docIDs []string, q domain.Query) []string {
	if len(q.ScopeDocumentIDs) == 0 && len(q.ExcludeDocumentIDs) == 0 {
		return docIDs
	}
	scope := map[string]bool{}
	for _, id := range q.ScopeDocumentIDs {
		scope[id] = true
	}
	excl := map[string]bool{}
	for _, id := range q.ExcludeDocumentIDs {
		excl[id] = true
	}
	out := docIDs[:0]
	for _, id := range docIDs {
		if excl[id] || (len(scope) > 0 && !scope[id]) {
			continue
		}
		out = append(out, id)
	}
	return out
}

// Запросы со своим Prompt (генерация и проверка гипотез) не эскалируем: им нужен свой JSON, а не чатовый цикл.
func agenticGate(q domain.Query) (bool, []string) {
	if strings.TrimSpace(q.Prompt) != "" {
		return false, nil
	}
	text := q.Text
	lower := strings.ToLower(text)
	score := 0
	reasons := make([]string, 0, 5)

	if comparativeRe.MatchString(lower) {
		score += gateComparativeWeight
		reasons = append(reasons, "comparative")
	}
	if strings.Count(text, "?") >= gateMinQuestions {
		score++
		reasons = append(reasons, "multi-question")
	}
	if len(clauseRe.FindAllString(lower, -1)) >= gateMinClauses {
		score++
		reasons = append(reasons, "multi-clause")
	}
	if entityCount(text) >= gateMinEntities {
		score++
		reasons = append(reasons, "multi-entity")
	}
	if len(strings.Fields(text)) >= gateLongQueryTokens {
		score++
		reasons = append(reasons, "long-query")
	}
	return score >= gateThreshold, reasons
}

// Первое слово пропускаем: оно с заглавной из-за начала предложения, а не потому что это имя.
func entityCount(text string) int {
	set := make(map[string]struct{})
	add := func(s string) {
		if s = strings.ToLower(strings.TrimSpace(s)); s != "" {
			set[s] = struct{}{}
		}
	}
	for _, m := range quotedRe.FindAllString(text, -1) {
		add(m)
	}
	for _, m := range capMultiRe.FindAllString(text, -1) {
		add(m)
	}
	for _, m := range acronymRe.FindAllString(text, -1) {
		add(m)
	}
	for i, f := range strings.Fields(text) {
		if i > 0 && capWordRe.MatchString(f) {
			add(f)
		}
	}
	return len(set)
}

// Всё, кроме строки [search], считается стопом: так цикл ограничен даже на кривом ответе модели.
func parseAction(text string) agentAction {
	if m := searchActionRe.FindStringSubmatch(text); m != nil {
		query := strings.Trim(strings.TrimSpace(m[1]), "\"'`«»")
		if query != "" {
			return agentAction{search: true, query: query}
		}
	}
	return agentAction{search: false, query: ""}
}

// Первое вхождение побеждает: порядок шагов сохраняется, и нумерация [Sn] стабильна.
func mergeEvidence(pool, add []domain.RetrievedChunk) []domain.RetrievedChunk {
	seen := make(map[string]bool, len(pool))
	for _, c := range pool {
		if c.ID != "" {
			seen[c.ID] = true
		}
	}
	for _, c := range add {
		if len(pool) >= agenticMaxEvidence {
			break
		}
		if c.ID != "" {
			if seen[c.ID] {
				continue
			}
			seen[c.ID] = true
		}
		pool = append(pool, c)
	}
	return pool
}

func docIDSet(lists ...[]domain.RetrievedChunk) map[string]bool {
	set := make(map[string]bool)
	for _, list := range lists {
		for _, c := range list {
			if c.DocumentID != "" {
				set[c.DocumentID] = true
			}
		}
	}
	return set
}

func clip(s string, maxRunes int) string {
	s = strings.TrimSpace(s)
	runes := []rune(s)
	if len(runes) <= maxRunes {
		return s
	}
	return strings.TrimSpace(string(runes[:maxRunes])) + "…"
}
