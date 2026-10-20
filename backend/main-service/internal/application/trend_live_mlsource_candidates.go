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
	"sync"
	"time"
	"unicode"
	"unicode/utf8"
)

const (
	liveMLExtractPreprint = "preprint"
	liveMLExtractOther    = "other"
	// liveMLExtractTextChars: 20 000 знаков — первые 6–8 страниц, где обычно стоят нужные стадии предложения.
	liveMLExtractTextChars = 20000
)

func (s *TrendLiveService) mlStartExtraction(ctx context.Context, query string, umbrella *LiveTopic,
	documents []StudyPaper, metadata map[string]LivePaperMetadata,
) (func() []LiveExtractedTechnology, *LiveMLExtractionRun) {
	if s.ml == nil {
		return s.startExtraction(ctx, query, documents, metadata), nil
	}
	run := &LiveMLExtractionRun{Requested: liveMLSourceOn, Used: liveMLSourceOff, Version: s.ml.version}
	// Общий путь переносит паспорта только открытых каналов, а извлечению нужно и доверие загруженных документов.
	livePrepareAnswerMetadata([]LiveTopic{{Web: umbrella.Corpus}}, documents, metadata)
	pool, skipped, omitted := liveMLExtractPool(*umbrella, documents, metadata)
	input := liveMLExtractInput(query, s.ml.version, pool, metadata, liveMLRecordTexts(*umbrella))
	input.ObservationDate = s.mlObservationDate(ctx).Format(time.DateOnly)
	liveMLDropTexts(umbrella)
	done := make(chan liveMLExtracted, 1)
	go func() { done <- s.mlExtract(ctx, input) }()
	return sync.OnceValue(func() []LiveExtractedTechnology {
		got := <-done
		if got.err != nil {
			run.FallbackReason = got.err.Error()
			umbrella.Limits = append(umbrella.Limits, liveLimitOf(liveLimitMLExtractFallback,
				"Извлечение технологий недоступно; использовано извлечение моделью генерации: "+got.err.Error(), nil))
			slog.Warn("radar candidate extraction fallback", "source", liveMLSourceOff, "error", got.err)
			if ctx.Err() != nil {
				return nil
			}
			return s.startExtraction(ctx, query, documents, metadata)()
		}
		liveMLRecordRun(run, umbrella, got, skipped, omitted)
		return liveMLTechnologies(got.output)
	}), run
}

type liveMLExtracted struct {
	input  LiveMLExtractRequest
	output LiveMLExtractResponse
	fit    liveMLExtractFit
	err    error
}

// mlExtract идёт в фоне и не трогает ни тему, ни метаданные: пакет собран заранее.
func (s *TrendLiveService) mlExtract(ctx context.Context, input LiveMLExtractRequest) liveMLExtracted {
	if s.ml.extract == nil {
		return liveMLExtracted{err: errors.New("сервис извлечения не настроен")}
	}
	output, err := s.ml.extract(ctx, input)
	var fit liveMLExtractFit
	var tooLarge *liveMLExtractTooLargeError
	if errors.As(err, &tooLarge) {
		if fitted, fitting, ok := liveMLExtractFitBudget(input, tooLarge.limit); ok {
			input, fit = fitted, fitting
			output, err = s.ml.extract(ctx, input)
		}
	}
	return liveMLExtracted{input: input, output: output, fit: fit, err: err}
}

func liveMLRecordRun(run *LiveMLExtractionRun, umbrella *LiveTopic, got liveMLExtracted, skipped, omitted int) {
	coverage := got.output.Coverage
	for _, document := range got.input.Documents {
		if document.Text != document.Title+"\n\n"+document.Abstract {
			coverage.DocumentsFullText++
		} else {
			coverage.DocumentsAbstractOnly++
		}
	}
	coverage.DocumentsOmitted = omitted + got.fit.omitted
	if coverage.DocumentsOmitted > 0 {
		coverage.Notes = append(coverage.Notes, fmt.Sprintf(
			"В извлечение отправлены ближайшие к запросу документы; дальше от запроса осталось %d — "+
				"их по имени технологии находит общий путь", coverage.DocumentsOmitted))
	}
	if trimmed := got.fit.truncated + got.fit.abstractOnly; trimmed > 0 {
		coverage.DocumentsTextTruncated, coverage.DocumentsTextDropped = got.fit.truncated, got.fit.abstractOnly
		coverage.Notes = append(coverage.Notes, fmt.Sprintf(
			"Вход извлечения превышал предел %d байт: полный текст сокращён до %d знаков у %d документов, "+
				"заменён заголовком и аннотацией у %d", got.fit.limit, liveMLExtractTextChars, got.fit.truncated,
			got.fit.abstractOnly))
	}
	if coverage.DocumentsAbstractOnly > 0 {
		coverage.Notes = append(coverage.Notes, fmt.Sprintf(
			"Для %d документов доступен только заголовок и аннотация", coverage.DocumentsAbstractOnly))
	}
	if skipped > 0 {
		coverage.DocumentsSkippedLanguage = skipped
		note := fmt.Sprintf("Пропущено документов с неизвестным или неподдерживаемым языком: %d", skipped)
		coverage.Notes = append(coverage.Notes, note)
		umbrella.Limits = append(umbrella.Limits, liveLimitOf("ml_extract_language_skipped", note, nil))
	}
	run.Used, run.Coverage, run.ModelVersions = liveMLSourceOn, &coverage, got.output.ModelVersions
	if coverage.Degraded || coverage.DocumentsFailed > 0 {
		umbrella.Limits = append(umbrella.Limits, liveLimitOf(liveLimitMLExtractDegraded,
			"Извлечение завершено с неполным покрытием; ограничения сервиса сохранены в результате.", nil))
	}
}

// liveMLTechnologies берёт имя в форме из документов: русское даст проверка сущности.
func liveMLTechnologies(output LiveMLExtractResponse) []LiveExtractedTechnology {
	edge := func(word string) bool { return liveStopword(liveStem(word)) || liveRussianFunctionWord(word) }
	found := make([]LiveExtractedTechnology, 0, len(output.Candidates))
	for i := range output.Candidates {
		candidate := &output.Candidates[i]
		name := strings.Join(strings.Fields(cmp.Or(candidate.Name, candidate.CanonicalRU)), " ")
		words := liveWords(name)
		if len(words) == 0 || len(candidate.DocIDs) == 0 || edge(words[0]) || edge(words[len(words)-1]) {
			continue
		}
		found = append(found, LiveExtractedTechnology{Name: name, Documents: candidate.DocIDs, ML: candidate,
			Label: cmp.Or(liveTechnologyLabel(candidate.CanonicalRU), liveTechnologyLabel(candidate.Name)),
			Terms: liveExtractTerms([]string{candidate.CanonicalRU})})
	}
	return found
}

// liveMLExtractPool берёт ближайшие к запросу документы: весь корпус широкого запроса не помещается в 4 МиБ входа.
// ponytail: близость по словам терминов, а не по смыслу; эмбеддинги документов — если лексики окажется мало.
func liveMLExtractPool(umbrella LiveTopic, documents []StudyPaper, metadata map[string]LivePaperMetadata,
) (pool []StudyPaper, skipped, omitted int) {
	terms := liveQueryTerms(umbrella)
	closeness := map[string]int{}
	for _, paper := range liveUniquePapers(documents) {
		// Документ неизвестного языка исключается явно: иначе сервис отверг бы весь пакет.
		if lang, _ := livePaperLanguage(paper, metadata[paper.ID]); lang != "ru" && lang != "en" {
			skipped++
			continue
		}
		pool = append(pool, paper)
		if liveAboutQuery(paper.Title, terms) {
			closeness[paper.ID] += 2
		}
		if liveAboutQuery(metadata[paper.ID].Abstract, terms) {
			closeness[paper.ID]++
		}
	}
	work := func(paper StudyPaper) int {
		if strings.HasPrefix(paper.ID, liveRecordPrefix) {
			return 0
		}
		return 1
	}
	slices.SortStableFunc(pool, func(a, b StudyPaper) int {
		return cmp.Or(-cmp.Compare(closeness[a.ID], closeness[b.ID]), cmp.Compare(work(a), work(b)),
			-cmp.Compare(a.Date, b.Date))
	})
	omitted = max(0, len(pool)-liveExtractDocuments)
	return pool[:len(pool)-omitted], skipped, omitted
}

func liveMLRecordTexts(topic LiveTopic) map[string]string {
	texts := map[string]string{}
	for _, channel := range []*LiveChannel{topic.Patents, topic.PatentsCN, topic.Registry, topic.Web, topic.Corpus, topic.Research} {
		for _, record := range liveChannelRecords(channel) {
			if strings.TrimSpace(record.Text) != "" {
				texts[liveRecordPrefix+cmp.Or(record.ID, record.URL)] = record.Text
			}
		}
	}
	return texts
}

// liveMLExtractText: теггер ищет заголовок и аннотацию в тексте дословно, поэтому без них (EPO OPS, веб) они идут первыми.
func liveMLExtractText(paper StudyPaper, meta LivePaperMetadata, texts map[string]string) string {
	text, ok := texts[paper.ID]
	if !ok {
		return paper.Title + "\n\n" + meta.Abstract
	}
	var prefix strings.Builder
	for _, part := range []string{paper.Title, meta.Abstract} {
		if strings.TrimSpace(part) != "" && !strings.Contains(text, part) {
			prefix.WriteString(part + "\n\n")
		}
	}
	return prefix.String() + text
}

func liveMLExtractInput(query, version string, documents []StudyPaper, metadata map[string]LivePaperMetadata,
	texts map[string]string,
) LiveMLExtractRequest {
	input := LiveMLExtractRequest{Query: query, ModelVersion: version, Documents: []LiveMLExtractDocument{}}
	for _, paper := range liveUniquePapers(documents) {
		meta := metadata[paper.ID]
		lang, _ := livePaperLanguage(paper, meta)
		var date *string
		if parsed, err := time.Parse(time.DateOnly, paper.Date); err == nil {
			date = new(parsed.Format(time.DateOnly))
		}
		input.Documents = append(input.Documents, LiveMLExtractDocument{
			DocID: paper.ID, Source: cmp.Or(liveSourceOf(paper.ID), PublisherUnknown), Title: paper.Title,
			Abstract: meta.Abstract, Text: liveMLExtractText(paper, meta, texts),
			Lang: lang, SourceType: liveMLExtractSourceType(paper, meta), TrustLevel: liveMLExtractTrust(meta),
			Date: date, URL: paper.URL, Authors: paper.Authors,
		})
	}
	return input
}

type liveMLExtractFit struct {
	limit, truncated, abstractOnly, omitted int
}

// liveMLBudget.state: 0 — как есть, 1 — текст сокращён, 2 — только заголовок и аннотация.
type liveMLBudget struct {
	documents    []LiveMLExtractDocument
	sizes, state []int
	total, limit int
}

func (b *liveMLBudget) resize(i int) bool {
	raw, err := json.Marshal(b.documents[i])
	if err != nil {
		return false
	}
	b.total += len(raw) - b.sizes[i]
	b.sizes[i] = len(raw)
	return true
}

// shrink идёт с конца: пул отсортирован по близости, там самые далёкие от запроса.
func (b *liveMLBudget) shrink(mark int, change func(*LiveMLExtractDocument) bool) bool {
	for i := len(b.documents) - 1; i >= 0 && b.total > b.limit; i-- {
		if change(&b.documents[i]) {
			if b.state[i] = mark; !b.resize(i) {
				return false
			}
		}
	}
	return true
}

// liveMLExtractFitBudget сокращает текст только префиксом: иначе координаты цитат в ответе съедут.
func liveMLExtractFitBudget(input LiveMLExtractRequest, limit int) (LiveMLExtractRequest, liveMLExtractFit, bool) {
	fit := liveMLExtractFit{limit: limit}
	n := len(input.Documents)
	b := &liveMLBudget{documents: slices.Clone(input.Documents), sizes: make([]int, n), state: make([]int, n), limit: limit}
	input.Documents = []LiveMLExtractDocument{}
	base, err := json.Marshal(input)
	if err != nil {
		return input, fit, false
	}
	b.total = len(base) + max(0, n-1)
	for i := range n {
		if !b.resize(i) {
			return input, fit, false
		}
	}
	if !b.shrink(1, liveMLExtractCut) || !b.shrink(2, liveMLExtractAbstractOnly) {
		return input, fit, false
	}
	for len(b.documents) > 0 && b.total > limit {
		last := len(b.documents) - 1
		b.total -= b.sizes[last] + min(1, last)
		b.documents = b.documents[:last]
		fit.omitted++
	}
	for _, done := range b.state[:len(b.documents)] {
		switch done {
		case 1:
			fit.truncated++
		case 2:
			fit.abstractOnly++
		}
	}
	input.Documents = b.documents
	return input, fit, b.total <= limit && len(b.documents) > 0
}

func liveMLExtractCut(document *LiveMLExtractDocument) bool {
	text := []rune(document.Text)
	required := 0
	for _, part := range []string{document.Title, document.Abstract} {
		if strings.TrimSpace(part) == "" {
			continue
		}
		if at := strings.Index(document.Text, part); at >= 0 {
			required = max(required, utf8.RuneCountInString(document.Text[:at])+utf8.RuneCountInString(part))
		}
	}
	cut := max(liveMLExtractTextChars, required)
	if cut >= len(text) {
		return false
	}
	for i := cut; i > max(required, cut-200); i-- {
		if unicode.IsSpace(text[i]) {
			cut = i
			break
		}
	}
	document.Text = string(text[:cut])
	return true
}

func liveMLExtractAbstractOnly(document *LiveMLExtractDocument) bool {
	short := document.Title + "\n\n" + document.Abstract
	if len(short) >= len(document.Text) {
		return false
	}
	document.Text = short
	return true
}

func liveMLExtractTrust(meta LivePaperMetadata) string {
	trust := meta.Trust
	if trust == TrustLowered {
		// Непроверенный домен понижен из осторожности, а не по замеру: модели передаётся неизвестность.
		if meta.PublisherType == PublisherUnknown {
			return PublisherUnknown
		}
		trust = levelLow
	}
	if !slices.Contains([]string{TrustHigh, TrustMedium, levelLow}, trust) {
		trust = PublisherUnknown
	}
	return trust
}

func liveMLExtractSourceType(paper StudyPaper, meta LivePaperMetadata) string {
	switch meta.Genre {
	case GenrePeerReviewed:
		return "journal_article"
	case GenreAnnouncement, GenreNews:
		return meta.Genre
	case GenreBlog:
		return "blog"
	}
	switch meta.PublisherType {
	case PublisherPreprint:
		return liveMLExtractPreprint
	case PublisherJournal:
		return "journal_article"
	case PublisherPatentOffice:
		return "patent"
	case PublisherRegistry:
		return liveStepRegistry
	case PublisherCodeHost:
		return "repository"
	}
	if liveSourceOf(paper.ID) == liveStepArxiv {
		return liveMLExtractPreprint
	}
	if meta.Genre == GenreRecord {
		return liveStepRegistry
	}
	return liveMLExtractOther
}
