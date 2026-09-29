package application

import (
	"cmp"
	"context"
	"fmt"
	"github.com/example/main-service/internal/platform/contracts"
	"slices"
	"strings"
	"sync"
	"unicode"
	"unicode/utf8"

	commonv1 "github.com/example/main-service/internal/platform/genproto/common/v1"
	dbv1 "github.com/example/main-service/internal/platform/genproto/db/v1"
	llmv1 "github.com/example/main-service/internal/platform/genproto/llm/v1"
)

type generationContext struct {
	request      *commonv1.RagRequest
	works        []ExternalWork
	inputSources []*commonv1.Source
	inputIDs     map[string]struct{}
}

func (s *HypothesisService) prepareGeneration(
	ctx context.Context, ownerID string, kpi *commonv1.KPI, count int, constraints string,
	scopeDocIDs []string, settings HypothesisRuntimeSettings,
) generationContext {
	query := retrievalQuery(kpi)
	var (
		feedback      string
		works         []ExternalWork
		inputSources  []*commonv1.Source
		excludeDocIDs []string
		answerIDs     []string
		answerSources []*commonv1.Source
		wg            sync.WaitGroup
	)
	wg.Go(func() {
		feedback = s.feedbackNote(ctx, ownerID, kpi)
	})
	wg.Go(func() {
		works = s.externalWorks(ctx, kpi, constraints)
	})
	wg.Go(func() {
		inputDocs := s.kpiInputDocs(ctx, kpi)
		inputSources = s.inputDocSources(ctx, ownerID, inputDocs, query)
		excludeDocIDs = inputDocIDs(inputDocs)
	})
	wg.Go(func() {
		answer := s.answerDocs(ctx, ownerID)
		answerIDs = inputDocIDs(answer)
		answerSources = s.answerDocSources(ctx, ownerID, answer, query)
	})
	wg.Wait()
	// Набор входных документов считается до добавления документов-ответов: те исключаются из поиска,
	// но входными данными не становятся.
	inputSet := docIDSet(excludeDocIDs)
	// Эти исключения общие для генерации, судьи и поиска контраргументов.
	excludeDocIDs = append(excludeDocIDs, answerIDs...)
	basePrompt := buildGenPrompt(kpi, count, constraints, useLightGenSchema(ctx, s.ovr, count)) +
		exclusionNote(settings.ExcludedDirections) + worldPracticeNote(works) + feedback +
		inputDataNote(inputSources) + priorArtNote(answerSources)
	return generationContext{
		request: &commonv1.RagRequest{
			OwnerId: ownerID, Query: query, Prompt: basePrompt,
			TopK:               adaptiveTopK(query, genEvidenceTop, genEvidenceTopMax),
			ExcludeDocumentIds: excludeDocIDs, ScopeDocumentIds: scopeDocIDs,
		},
		works: works, inputSources: inputSources, inputIDs: inputSet,
	}
}

func (s *HypothesisService) feedbackNote(ctx context.Context, ownerID string, kpi *commonv1.KPI) string {
	kpiID := kpi.GetId()
	if kpiID == "" {
		return ""
	}
	rejected := s.recentTitles(ctx, ownerID, kpiID, statusRejected, genFeedbackTitles)
	approved := s.recentTitles(ctx, ownerID, kpiID, statusApproved, genFeedbackTitles)
	if len(rejected) == 0 && len(approved) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("\n\nОбратная связь экспертов по этому KPI (учитывай при генерации):")
	if len(rejected) > 0 {
		b.WriteString(" эксперты ОТКЛОНИЛИ такие направления — не повторяй их и предложи иные: ")
		b.WriteString(joinTitles(rejected))
		b.WriteByte('.')
	}
	if len(approved) > 0 {
		b.WriteString(" эксперты ОДОБРИЛИ такие направления — придерживайся близких по духу углов: ")
		b.WriteString(joinTitles(approved))
		b.WriteByte('.')
	}
	return b.String()
}

func (s *HypothesisService) recentTitles(
	ctx context.Context, ownerID, kpiID, status string, limit int,
) []string {
	hs, err := s.cat.ListHypotheses(ctx, &dbv1.ListHypothesesRequest{
		OwnerId: ownerID, KpiId: kpiID, Status: status,
		OrderBy: "created_at", Limit: genFeedbackFetch,
	})
	if err != nil {
		return nil
	}
	titles := make([]string, 0, limit)
	for _, h := range hs {
		if t := compactText(h.GetTitle()); t != "" {
			titles = append(titles, firstSentence(t, 80))
			if len(titles) >= limit {
				break
			}
		}
	}
	return titles
}

func joinTitles(titles []string) string {
	quoted := make([]string, 0, len(titles))
	for _, t := range titles {
		quoted = append(quoted, "«"+t+"»")
	}
	return strings.Join(quoted, ", ")
}

type kpiInputDoc struct {
	id   string
	name string
}

func (s *HypothesisService) kpiInputDocs(ctx context.Context, kpi *commonv1.KPI) []kpiInputDoc {
	if kpi.GetId() == "" {
		return nil
	}
	links, err := s.cat.ListKPIDocuments(ctx, kpi.GetOwnerId(), kpi.GetId())
	if err != nil {
		return nil
	}
	out := make([]kpiInputDoc, 0, len(links))
	for _, l := range links {
		if l.GetRole() != "" && l.GetRole() != "input" {
			continue
		}
		doc := l.GetDocument()
		if doc.GetId() == "" {
			continue
		}
		name := doc.GetTitle()
		if name == "" {
			name = doc.GetFilename()
		}
		out = append(out, kpiInputDoc{id: doc.GetId(), name: name})
	}
	return out
}

func inputDocIDs(docs []kpiInputDoc) []string {
	out := make([]string, 0, len(docs))
	for _, d := range docs {
		out = append(out, d.id)
	}
	return out
}

// docKindHypotheses — документ, который сам список готовых гипотез («слитые ответы»): из поиска он исключён,
// а генератору даётся только как уже известные идеи.
const docKindHypotheses = "hypotheses"

func (s *HypothesisService) answerDocs(ctx context.Context, ownerID string) []kpiInputDoc {
	docs, err := s.cat.ListDocuments(ctx, ownerID, contracts.DocumentScan{Kind: docKindHypotheses})
	if err != nil {
		return nil
	}
	out := make([]kpiInputDoc, 0, 4)
	for _, d := range docs {
		if d.GetKind() != docKindHypotheses {
			continue
		}
		name := d.GetTitle()
		if name == "" {
			name = d.GetFilename()
		}
		out = append(out, kpiInputDoc{id: d.GetId(), name: name})
	}
	return out
}

func (s *HypothesisService) answerDocIDs(ctx context.Context, ownerID string) []string {
	return inputDocIDs(s.answerDocs(ctx, ownerID))
}

func docIDSet(ids []string) map[string]struct{} {
	if len(ids) == 0 {
		return nil
	}
	set := make(map[string]struct{}, len(ids))
	for _, id := range ids {
		set[id] = struct{}{}
	}
	return set
}

const (
	genInputDocsMax      = 4
	genInputChunksPerDoc = 3
	genInputSnippetChars = 700
)

// docSources — единственный путь, которым входные документы и документы-ответы попадают к модели: поиск их исключает.
func (s *HypothesisService) docSources(
	ctx context.Context, ownerID string, docs []kpiInputDoc, query string,
	chunksPerDoc int, clip func(string) string,
) []*commonv1.Source {
	if s.chunks == nil || len(docs) == 0 {
		return nil
	}
	qTokens := tokenSet(query)
	out := make([]*commonv1.Source, 0, genInputDocsMax*chunksPerDoc)
	for i, d := range docs {
		if i >= genInputDocsMax {
			break
		}
		chunks, err := s.chunks.DocumentChunks(ctx, ownerID, d.id)
		if err != nil || len(chunks) == 0 {
			continue
		}
		slices.SortStableFunc(chunks, func(a, b *llmv1.DocumentChunk) int {
			return cmp.Compare(overlap(qTokens, tokenSet(b.GetText())), overlap(qTokens, tokenSet(a.GetText())))
		})
		for j, c := range chunks {
			if j >= chunksPerDoc {
				break
			}
			snippet := clip(compactText(c.GetText()))
			if snippet == "" {
				continue
			}
			out = append(out, &commonv1.Source{DocumentId: d.id, ChunkId: c.GetId(), Filename: d.name, Snippet: snippet})
		}
	}
	return out
}

func (s *HypothesisService) inputDocSources(
	ctx context.Context, ownerID string, docs []kpiInputDoc, query string,
) []*commonv1.Source {
	return s.docSources(ctx, ownerID, docs, query, genInputChunksPerDoc,
		func(text string) string { return firstSentence(text, genInputSnippetChars) })
}

func inputDataNote(inputSources []*commonv1.Source) string {
	if len(inputSources) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("\n\nВходные данные предприятия (отчёты, регламенты и ограничения, приложенные к цели):\n")
	for i, src := range inputSources {
		fmt.Fprintf(&b, "[D%d] %s: %s\n", i+1, src.GetFilename(), src.GetSnippet())
	}
	b.WriteString("\nПравило работы с данными: проблему, текущие значения и факты бери ИЗ ВХОДНЫХ ДАННЫХ " +
		"ПРЕДПРИЯТИЯ выше; мировую практику, аналоги и обоснование рекомендаций бери ТОЛЬКО из контекста " +
		"базы знаний (выдержки в начале). Не выдавай сами входные данные предприятия за мировую практику " +
		"или внешнее подтверждение.\n")
	return b.String()
}

const (
	genAnswerChunksPerDoc = 2
	genAnswerSnippetChars = 600
)

// answerDocSources режет по символам, а не по предложениям, чтобы уцелел нумерованный список гипотез;
// в цитируемые свидетельства не попадает.
func (s *HypothesisService) answerDocSources(
	ctx context.Context, ownerID string, docs []kpiInputDoc, query string,
) []*commonv1.Source {
	return s.docSources(ctx, ownerID, docs, query, genAnswerChunksPerDoc,
		func(text string) string { return clipRunes(text, genAnswerSnippetChars) })
}

func clipRunes(s string, n int) string {
	r := []rune(strings.TrimSpace(s))
	if len(r) <= n {
		return string(r)
	}
	return strings.TrimRight(string(r[:n]), " ,;:") + "…"
}

func priorArtNote(sources []*commonv1.Source) string {
	if len(sources) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("\n\nУже предложенные идеи и гипотезы (из документов с готовыми гипотезами — " +
		"итоги прежнего мозгового штурма):\n")
	for i, src := range sources {
		fmt.Fprintf(&b, "[H%d] %s\n", i+1, src.GetSnippet())
	}
	b.WriteString("\nЭто НЕ мировая практика и НЕ доказательства — это уже рассмотренные направления. " +
		"Не повторяй и не перефразируй их: предлагай принципиально ДРУГИЕ, новые гипотезы. " +
		"Не цитируй эти идеи как источник.\n")
	return b.String()
}

func markEvidenceOrigin(evidence []*commonv1.HypothesisEvidence, inputSet map[string]struct{}) {
	for _, ev := range evidence {
		if _, ok := inputSet[ev.GetDocumentId()]; ok {
			ev.Origin = "input"
		} else {
			ev.Origin = "knowledge"
		}
	}
}

// selectEvidence группирует фрагменты по документам: интерфейс показывает несколько фрагментов под одной статьёй,
// а не повторяет её как разные источники.
func selectEvidence(sources []*commonv1.Source, item genItem) []*commonv1.HypothesisEvidence {
	ranked := rankEvidenceSources(sources, item)
	groups := make([][]*commonv1.Source, 0, genPerHypEvidence)
	byKey := make(map[string]int, genPerHypEvidence)
	seenChunks := make(map[string]struct{}, len(ranked))
	for _, src := range ranked {
		if src.GetChunkId() != "" {
			if _, ok := seenChunks[src.GetChunkId()]; ok {
				continue
			}
			seenChunks[src.GetChunkId()] = struct{}{}
		}
		key := evidenceArticleKey(src)
		if idx, ok := byKey[key]; ok {
			if len(groups[idx]) < genEvidenceChunks {
				groups[idx] = append(groups[idx], src)
			}
			continue
		}
		if len(groups) >= genPerHypEvidence {
			continue
		}
		byKey[key] = len(groups)
		groups = append(groups, []*commonv1.Source{src})
	}

	total := 0
	for _, group := range groups {
		total += len(group)
	}
	out := make([]*commonv1.HypothesisEvidence, 0, total)
	for _, group := range groups {
		for _, src := range group {
			out = append(out, evidenceFromSource(src, int32(len(out))))
		}
	}
	return out
}

func rankEvidenceSources(sources []*commonv1.Source, item genItem) []*commonv1.Source {
	hyp := tokenSet(item.Statement + " " + item.Rationale)
	ranked := slices.Clone(sources)
	scores := make(map[*commonv1.Source]int, len(ranked))
	for _, src := range ranked {
		scores[src] = overlap(hyp, tokenSet(src.GetSnippet()))
	}
	slices.SortStableFunc(ranked, func(a, b *commonv1.Source) int {
		if order := cmp.Compare(scores[b], scores[a]); order != 0 {
			return order
		}
		switch {
		case a.GetScore() > b.GetScore():
			return -1
		case a.GetScore() < b.GetScore():
			return 1
		default:
			return 0
		}
	})

	return ranked
}

func evidenceArticleKey(src *commonv1.Source) string {
	if src.GetDocumentId() != "" {
		return "doc:" + src.GetDocumentId()
	}
	if src.GetFilename() != "" {
		return "file:" + strings.ToLower(src.GetFilename())
	}
	return "chunk:" + src.GetChunkId()
}

// tokenSet берёт токены от 4 рун: короткие и служебные слова шумят в пересечении.
func tokenSet(s string) map[string]struct{} {
	set := make(map[string]struct{})
	for _, tok := range strings.FieldsFunc(strings.ToLower(s), func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r)
	}) {
		if utf8.RuneCountInString(tok) >= 4 {
			set[tok] = struct{}{}
		}
	}
	return set
}

func overlap(a, b map[string]struct{}) int {
	n := 0
	for tok := range a {
		if _, ok := b[tok]; ok {
			n++
		}
	}
	return n
}

// evidenceFromSource ставит нейтральную позицию context: поддержкой фрагмент становится только после оценки модели.
func evidenceFromSource(src *commonv1.Source, ord int32) *commonv1.HypothesisEvidence {
	score := src.GetScore()
	ev := &commonv1.HypothesisEvidence{
		ChunkId: src.GetChunkId(), Filename: src.GetFilename(), Snippet: src.GetSnippet(),
		Stance: stanceContext, Score: &score, Ord: ord,
		PageStart: src.GetPageStart(), PageEnd: src.GetPageEnd(), SectionHeading: src.GetSectionHeading(),
	}
	if docID := src.GetDocumentId(); docID != "" {
		ev.DocumentId = &docID
	}
	return ev
}
