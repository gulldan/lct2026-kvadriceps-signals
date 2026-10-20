package application

import (
	"cmp"
	"context"
	"slices"
	"strings"
	"time"

	commonv1 "github.com/example/main-service/internal/platform/genproto/common/v1"
	"github.com/example/main-service/internal/platform/sourcedoc"
)

// Загруженные документы — ещё один канал свидетельств: ответ шлюза не нужен, берутся только найденные документы.
const (
	liveStepCorpus   = "corpus"
	liveCorpusSource = "Загруженные документы"
	liveCorpusTopK   = 24
	liveCorpusPrompt = "Назови одним предложением, о чём эти источники."
	// Поиск по своей базе идёт через RAG с генерацией: перегруженный сервис ответов не должен задерживать открытие ответа.
	liveCorpusPatience = 12 * time.Second
)

// Поиск отдаёт имя файла, а каналу нужны название и дата, иначе запись не попадёт ни в один период.
type liveDocumentReader interface {
	GetDocument(ctx context.Context, id string) (*commonv1.Document, error)
}

// Отказ — именованное ограничение, а не ошибка: остальные каналы уже собраны и должны дойти.
func liveCorpusSearch(ctx context.Context, src *liveSources, owner string, terms []string,
	periods []LivePeriod) *LiveChannel {
	if src.answerer == nil || owner == "" || len(periods) == 0 {
		return nil
	}
	observed := time.Now().UTC().Format(time.RFC3339)
	channel := &LiveChannel{
		SourceSet: "corpus", Source: liveCorpusSource, ObservedAt: observed,
		Counts: make([]*int, len(periods)), Records: []LiveSourceRecord{}, Limits: []string{},
	}
	query := strings.Join(liveCorpusTerms(terms), " ")
	if query == "" {
		channel.Limits = append(channel.Limits, "corpus: запрос пуст, по своей базе не искали")
		return channel
	}
	src.step(liveStepCorpus, 0)
	ask, cancel := context.WithTimeout(ctx, liveCorpusPatience)
	defer cancel()
	answer, err := src.answerer.Answer(ask, &commonv1.RagRequest{
		OwnerId: owner, Query: query, Prompt: liveCorpusPrompt, TopK: liveCorpusTopK,
	})
	if err != nil {
		channel.Limits = append(channel.Limits, "corpus: "+err.Error())
		return channel
	}
	counts := make([]int, len(periods))
	seen := map[string]bool{}
	for _, source := range answer.GetSources() {
		id := source.GetDocumentId()
		if id == "" || seen[id] {
			continue
		}
		seen[id] = true
		record, index, ok := liveCorpusRecord(ctx, src.documents, source, periods, observed)
		if !ok {
			continue
		}
		if index >= 0 {
			counts[index]++
		}
		channel.Records = append(channel.Records, record)
	}
	for i := range counts {
		channel.Counts[i] = &counts[i]
	}
	channel.Total = len(channel.Records)
	slices.SortFunc(channel.Records, func(a, b LiveSourceRecord) int { return strings.Compare(b.Date, a.Date) })
	return channel
}

func liveCorpusRecord(ctx context.Context, documents liveDocumentReader, source *commonv1.Source,
	periods []LivePeriod, observed string) (LiveSourceRecord, int, bool) {
	id := source.GetDocumentId()
	document := liveCorpusDocument(ctx, documents, id)
	title := cmp.Or(strings.TrimSpace(document.GetTitle()), strings.TrimSpace(source.GetFilename()))
	if record, ok := liveCorpusInvestorRecord(id, title, document.GetSourceMetadata()); ok {
		return record, -1, true
	}
	date := liveCorpusDate(document)
	index := livePeriodIndex(periods, date)
	if title == "" || index < 0 {
		return LiveSourceRecord{}, 0, false
	}
	// Ссылка ведёт на страницу источника в приложении, а не на /documents/…: такой страницы нет.
	return LiveSourceRecord{
		ID: id, Title: title, Date: date, URL: "/sources/" + id, Source: liveCorpusSource,
		// Свой документ загружен в этот контур: паспорт известен точно, а не со слов издателя.
		PublisherType: "own", Genre: "document", Trust: "high",
		TrustBasis: "документ загружен в эту систему", ObservedAt: observed,
		PublishedAt: date, TextStatus: "article_text",
	}, index, true
}

func liveCorpusDocument(ctx context.Context, documents liveDocumentReader, id string) *commonv1.Document {
	if documents == nil {
		return nil
	}
	document, err := documents.GetDocument(ctx, id)
	if err != nil {
		return nil
	}
	return document
}

// Дата загрузки — не дата публикации фонда; импорт не превращает его позицию в собственное исследование.
func liveCorpusInvestorRecord(id, title, metadata string) (LiveSourceRecord, bool) {
	source, err := sourcedoc.Decode([]byte(metadata))
	if title == "" || err != nil || !liveInvestorContext(source.Genre) {
		return LiveSourceRecord{}, false
	}
	passport, genre := liveApplyRecordGenre(ClassifySource(source.URL), source.URL, title, source.Genre)
	record := LiveSourceRecord{ID: id, Title: title, URL: "/sources/" + id, OriginalURL: source.URL,
		Source: liveCorpusSource, Genre: genre, PublisherType: passport.Publisher,
		Trust: passport.Trust, TrustBasis: passport.TrustBasis, ObservedAt: source.ObservedAt.UTC().Format(time.RFC3339),
		ContentHash: source.ContentHash, TextStatus: cmp.Or(source.TextStatus, "metadata_only")}
	if source.Publication != nil {
		record.Date, record.PublishedAt, record.DateBasis = source.Publication.Value, source.Publication.Value,
			source.Publication.Basis
	}
	return record, true
}

func liveCorpusDate(document *commonv1.Document) string {
	for _, value := range []string{document.GetPublishedAt(), document.GetCreatedAt()} {
		if len(value) >= 10 {
			return value[:10]
		}
	}
	return ""
}

// Гибридный поиск ранжирует по смыслу целой фразы, а не по объединению разрозненных слов.
func liveCorpusTerms(terms []string) []string {
	kept := make([]string, 0, liveScholarTerms)
	for _, term := range terms {
		term = strings.Join(strings.Fields(term), " ")
		if term == "" || slices.Contains(kept, term) {
			continue
		}
		kept = append(kept, term)
		if len(kept) == liveScholarTerms {
			break
		}
	}
	return kept
}

// Документы принадлежат владельцу и не должны приехать другому из общего хранилища.
func liveCorpusStripped(detail LiveDetail) LiveDetail {
	detail.Topic.Corpus = nil
	for i := range detail.Candidates {
		detail.Candidates[i].Corpus = nil
	}
	return detail
}
