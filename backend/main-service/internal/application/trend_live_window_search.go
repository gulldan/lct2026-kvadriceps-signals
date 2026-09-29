package application

import (
	"cmp"
	"context"
	"encoding/json/v2"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"strings"
	"unicode"

	"github.com/example/main-service/internal/domain"
)

const (
	// Предел покрывает типичный запрос целиком; выборка разрежает фразы ниже порога допуска.
	liveWindowSearchCap   = 5000
	liveWindowSearchTerms = LiveSearchTerms
)

func (s *TrendLiveService) searchFamily(ctx context.Context, query string) (liveFamily, error) {
	plan, err := s.planQuery(ctx, query)
	if err != nil {
		return liveFamily{}, err
	}
	lead := plan.Terms[0]
	return liveFamily{id: "search:" + query, name: query, plan: &plan,
		query: LiveQuery{Arxiv: liveSearchExpression(plan.Terms), GitHub: `"` + lead + `"`, HN: lead, Terms: plan.Terms}}, nil
}

type LiveWindowSearch struct {
	Query   string         `json:"query"`
	Periods []LivePeriod   `json:"periods"`
	RunID   string         `json:"run_id,omitempty"`
	Plan    *LiveQueryPlan `json:"plan,omitempty"`
}

type LiveWindowResult struct {
	Scope      *LiveQueryScope              `json:"scope,omitempty"`
	Papers     []StudyPaper                 `json:"papers"`
	Metadata   map[string]LivePaperMetadata `json:"metadata"`
	Total      int                          `json:"total"`
	Scanned    int                          `json:"scanned"`
	Expression string                       `json:"expression"`
	Terms      []string                     `json:"terms"`
	Limits     []LiveLimit                  `json:"limits"`
}

func (s *TrendLiveService) SetNamer(name func(ctx context.Context, titles []string) (string, error)) {
	s.name = name
}

func (s *TrendLiveService) SetCardWriter(card CandidateCard) { s.card = card }

func (s *TrendLiveService) SetSourceDigest(digest SourceDigest) { s.digest = digest }

func (s *TrendLiveService) SetCorpusSearch(answerer domain.Answerer, documents liveDocumentReader) {
	s.mu.Lock()
	s.answerer, s.documents = answerer, documents
	s.mu.Unlock()
}

func (s *TrendLiveService) SetPlanner(plan LiveQueryPlanner) { s.plan = plan }

// setWindow строит индекс до замены окна, чтобы поиски во время построения шли по прежнему. Карта metadata
// переходит окну: поиск читает её из HTTP-горутин без блокировки.
func (s *TrendLiveService) setWindow(papers []StudyPaper, metadata map[string]LivePaperMetadata) {
	index := liveIndexWindow(papers, metadata)
	s.mu.Lock()
	s.window, s.windowMeta, s.windowIndex = papers, metadata, index
	s.mu.Unlock()
}

// liveWindowIndex — окно, разобранное на основы один раз. Работа i занимает tokens[bounds[i]:bounds[i+1]],
// первые titles[i] из них — заголовок; postings — номера работ по возрастанию.
type liveWindowIndex struct {
	terms    map[string]int32
	words    []string
	postings [][]int32
	tokens   []int32
	bounds   []int32
	titles   []int32
}

func liveIndexWindow(window []StudyPaper, metadata map[string]LivePaperMetadata) liveWindowIndex {
	index := liveWindowIndex{terms: map[string]int32{}, bounds: make([]int32, 1, len(window)+1),
		titles: make([]int32, 0, len(window))}
	var unique []int32
	for i, paper := range window {
		title := liveStems(liveTokens(paper.Title))
		index.titles = append(index.titles, int32(len(title)))
		for _, stem := range slices.Concat(title, liveStems(liveTokens(metadata[paper.ID].Abstract))) {
			id, ok := index.terms[stem]
			if !ok {
				// Без копии ключ-срез держал бы в памяти всю аннотацию ради одного слова.
				stem = strings.Clone(stem)
				id = int32(len(index.words))
				index.terms[stem] = id
				index.words = append(index.words, stem)
				index.postings = append(index.postings, nil)
			}
			index.tokens = append(index.tokens, id)
		}
		unique = append(unique[:0], index.tokens[index.bounds[i]:]...)
		slices.Sort(unique)
		for _, id := range slices.Compact(unique) {
			index.postings[id] = append(index.postings[id], int32(i))
		}
		index.bounds = append(index.bounds, int32(len(index.tokens)))
	}
	return index
}

// text восстанавливает токены работы из индекса: запрос проверяется по тем же токенам без повторного разбора.
func (ix liveWindowIndex) text(buf []string, doc int32) (title, text []string) {
	for _, id := range ix.tokens[ix.bounds[doc]:ix.bounds[doc+1]] {
		buf = append(buf, ix.words[id])
	}
	return buf[:ix.titles[doc]], buf
}

// candidates — надмножество совпадений: порядок слов и поле проверяет сам запрос.
func (ix liveWindowIndex) candidates(phrases [][]string) []int32 {
	var out []int32
	for _, phrase := range phrases {
		lists := make([][]int32, 0, len(phrase))
		for _, stem := range phrase {
			id, ok := ix.terms[stem]
			if !ok {
				lists = nil
				break
			}
			lists = append(lists, ix.postings[id])
		}
		if len(lists) == 0 {
			continue
		}
		slices.SortFunc(lists, func(a, b []int32) int { return cmp.Compare(len(a), len(b)) })
		docs := slices.Clone(lists[0])
		for _, list := range lists[1:] {
			docs = liveIntersect(docs, list)
		}
		out = append(out, docs...)
	}
	slices.Sort(out)
	return slices.Compact(out)
}

func liveIntersect(a, b []int32) []int32 {
	out := a[:0]
	for i, j := 0, 0; i < len(a) && j < len(b); {
		switch {
		case a[i] < b[j]:
			i++
		case a[i] > b[j]:
			j++
		default:
			out = append(out, a[i])
			i++
			j++
		}
	}
	return out
}

// LoadWindow поднимает окно из кеша, чтобы после перезапуска поиск работал до первого ночного цикла.
func (s *TrendLiveService) LoadWindow(ctx context.Context) {
	cache := s.loadWindowCache(ctx)
	if len(cache.Papers) == 0 {
		return
	}
	s.setWindow(cache.Papers, cache.Metadata)
	slog.Info("radar window loaded", "papers", len(cache.Papers))
}

var ErrLiveWindowEmpty = errors.New("live: window not collected yet")

// SearchWindow использует готовый план API; самостоятельный запрос разбирается один раз.
func (s *TrendLiveService) SearchWindow(ctx context.Context, req LiveWindowSearch) (LiveWindowResult, error) {
	s.mu.RLock()
	window, metadata, index := s.window, s.windowMeta, s.windowIndex
	s.mu.RUnlock()
	if len(window) == 0 {
		return LiveWindowResult{}, ErrLiveWindowEmpty
	}
	query, err := normalizeLiveQuery(req.Query)
	if err != nil {
		return LiveWindowResult{}, err
	}
	if liveValidSearchRunID(req.RunID) {
		ctx = context.WithValue(ctx, liveLLMRunKey{}, req.RunID)
	}
	plan := req.Plan
	if plan == nil {
		generated, planErr := s.planQuery(ctx, query)
		if planErr != nil {
			return LiveWindowResult{}, planErr
		}
		plan = &generated
	}
	if err := liveValidateQueryPlan(*plan, query); err != nil {
		return LiveWindowResult{}, err
	}
	result := liveSearchWindowTerms(window, metadata, index, req.Periods, plan.Terms)
	result.Scope = liveCloneQueryScope(plan.Scope)
	return result, nil
}

// Признаки обучения измеряют саму фразу: расширение запроса меняет её частоту и добавляет сетевую задержку.
func (s *TrendLiveService) searchLiteralWindow(ctx context.Context, req LiveWindowSearch) (LiveWindowResult, error) {
	if err := ctx.Err(); err != nil {
		return LiveWindowResult{}, fmt.Errorf("literal window context: %w", err)
	}
	s.mu.RLock()
	window, metadata, index := s.window, s.windowMeta, s.windowIndex
	s.mu.RUnlock()
	if len(window) == 0 {
		return LiveWindowResult{}, ErrLiveWindowEmpty
	}
	query, err := normalizeLiveQuery(req.Query)
	if err != nil {
		return LiveWindowResult{}, err
	}
	return liveSearchWindowTerms(window, metadata, index, req.Periods, []string{query}), nil
}

func liveSearchWindowTerms(window []StudyPaper, metadata map[string]LivePaperMetadata, index liveWindowIndex,
	periods []LivePeriod, terms []string,
) LiveWindowResult {
	result := LiveWindowResult{Metadata: map[string]LivePaperMetadata{}, Terms: terms, Limits: []LiveLimit{},
		Scanned: len(window), Expression: liveSearchExpression(terms)}
	if matcher, phrases, compileErr := liveCompileQueryPhrases(result.Expression); compileErr == nil {
		result.Papers, result.Total = liveMatchWindow(window, index, periods, matcher, index.candidates(phrases))
		for _, paper := range result.Papers {
			if meta, ok := metadata[paper.ID]; ok {
				result.Metadata[paper.ID] = meta
			}
		}
	} else {
		// Запрос без латиницы в англоязычном окне: пустой ответ с названной причиной, а не ошибка.
		result.Papers = []StudyPaper{}
		result.Limits = append(result.Limits,
			liveLimitOf(liveLimitArxivNoTerms, "arxiv: the query has no English terms to search for", nil))
	}
	slices.SortFunc(result.Papers, func(a, b StudyPaper) int { return strings.Compare(a.Date, b.Date) })
	if result.Total > len(result.Papers) {
		// Выборка распределена по месяцам окна: это доля внутри каждого, и стадия с ростом по ней считаются.
		result.Limits = append(result.Limits, liveLimitOf(liveLimitArxivSampled,
			fmt.Sprintf("arxiv: пропорциональная выборка %d работ из %d совпавших в окне",
				len(result.Papers), result.Total),
			map[string]int{"kept": len(result.Papers), liveValueFound: result.Total}))
	}
	return result
}

func liveNeedsTranslation(query string) bool {
	return strings.ContainsFunc(query, func(r rune) bool { return unicode.Is(unicode.Cyrillic, r) })
}

// Идентификатор связывает журналы сервисов и не должен переносить произвольный текст.
func liveValidSearchRunID(id string) bool {
	if id == "" || len(id) > 64 {
		return false
	}
	return !strings.ContainsFunc(id, func(r rune) bool {
		return !strings.ContainsRune("abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789-_", r)
	})
}

// Рядом с «asset tokenization» одиночное «tokenization» нашло бы и токенизацию текста.
func liveDropCoveredWords(terms []string) []string {
	return slices.DeleteFunc(slices.Clone(terms), func(term string) bool {
		return !strings.Contains(term, " ") && slices.ContainsFunc(terms, func(other string) bool {
			return other != term && slices.Contains(strings.Fields(strings.ReplaceAll(other, "-", " ")), term)
		})
	})
}

func liveSearchExpression(terms []string) string {
	parts := make([]string, 0, len(terms))
	for _, term := range terms {
		term = strings.Join(strings.Fields(strings.ReplaceAll(term, `"`, " ")), " ")
		if term == "" {
			continue
		}
		if strings.Contains(term, " ") {
			parts = append(parts, `all:"`+term+`"`)
		} else {
			parts = append(parts, "all:"+term)
		}
	}
	return strings.Join(parts, " OR ")
}

// LiveSearchTerms покрывает несколько поднаправлений одним проходом по локальному окну arXiv.
const LiveSearchTerms = 12

// ParseSearchTerms ставит фразы до четырёх слов первыми: длинная фраза почти не находит работ.
func ParseSearchTerms(raw string, limit int) []string {
	raw = strings.TrimSpace(raw)
	if start, end := strings.Index(raw, "["), strings.LastIndex(raw, "]"); start >= 0 && end > start {
		raw = raw[start : end+1]
	}
	var terms []string
	if json.Unmarshal([]byte(raw), &terms) != nil {
		return nil
	}
	short, long := []string{}, []string{}
	for _, term := range terms {
		term = strings.ToLower(strings.Join(strings.Fields(term), " "))
		words := len(strings.Fields(term))
		switch {
		case term == "" || words > 6 || slices.Contains(short, term) || slices.Contains(long, term):
		case words <= 4:
			short = append(short, term)
		default:
			long = append(long, term)
		}
	}
	out := slices.Concat(short, long)
	return out[:min(len(out), limit)]
}

func liveMatchWindow(window []StudyPaper, index liveWindowIndex, periods []LivePeriod,
	matcher liveQuery, candidates []int32) ([]StudyPaper, int) {
	papers, total := []StudyPaper{}, 0
	var buf []string
	for _, i := range candidates {
		paper := window[i]
		if livePeriodIndex(periods, paper.Date) < 0 {
			continue
		}
		title, text := index.text(buf[:0], i)
		buf = text
		if !matcher(title, text) {
			continue
		}
		total++
		papers = append(papers, paper)
	}
	if total > liveWindowSearchCap {
		// Первые совпадения зависят от порядка окна и отрезают новые месяцы.
		papers = liveStratifiedPapers(papers, nil, liveWindowSearchCap)
	}
	return papers, total
}
