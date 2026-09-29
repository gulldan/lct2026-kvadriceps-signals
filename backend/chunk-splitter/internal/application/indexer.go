package application

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"github.com/example/chunk-splitter/internal/platform/indexdoc"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/rs/zerolog"

	"github.com/example/chunk-splitter/internal/domain"
	"github.com/example/chunk-splitter/internal/infrastructure/delimited"
	"github.com/example/chunk-splitter/internal/infrastructure/splitter"
	"github.com/example/chunk-splitter/internal/platform/contracts"
	"github.com/example/chunk-splitter/internal/platform/logger"

	commonv1 "github.com/example/chunk-splitter/internal/platform/genproto/common/v1"
)

const source = "chunk-splitter"

const nodeTypeChunk = "chunk"

// Пространство имён детерминированных id чанков менять нельзя: повторно доставленный документ
// задвоится, а не перезапишется.
const chunkIDNamespace = "6f8a4d2e-1c3b-4f5a-9d7e-2b6c8a0f1e34"

type Indexer struct {
	sourceIndexes *sourceIndexing
	capture       *captureIndex
	splitter      domain.Splitter
	status        StatusUpdater
	embedder      Embedder
	vectors       VectorStore
	search        SearchStore
	pub           Publisher
	pacer         Pacer
	store         ObjectStore
	tokenizer     Tokenizer
	maxTokens     int
	textMax       int
	batchSize     int
	splitWindow   int
	overlap       int // руны между окнами; равен перекрытию сплиттера
	headers       bool
	metrics       StageRecorder
	titler        Titler
	titles        TitleSetter
	kinds         KindSetter
	meta          MetaSetter
}

func (ix *Indexer) EnableTitles(t Titler, setter TitleSetter) {
	ix.titler, ix.titles = t, setter
}

// EnableKinds сохраняет класс документа, чтобы готовые списки гипотез не попадали в поиск гипотез.
func (ix *Indexer) EnableKinds(setter KindSetter) {
	ix.kinds = setter
}

func (ix *Indexer) EnableMeta(setter MetaSetter) {
	ix.meta = setter
}

// New берёт размеры вне допустимого диапазона по умолчанию; headers добавляет хлебные крошки
// только в текст для эмбеддинга и BM25.
func New(
	splitter domain.Splitter,
	status StatusUpdater,
	embedder Embedder,
	vectors VectorStore,
	search SearchStore,
	pub Publisher,
	pacer Pacer,
	store ObjectStore,
	tokenizer Tokenizer,
	maxTokens int,
	textMax int,
	batchSize int,
	splitWindow int,
	overlap int,
	headers bool,
	metrics StageRecorder,
) *Indexer {
	if batchSize < 1 {
		batchSize = 64
	}
	if textMax < 1 {
		textMax = indexdoc.MaxTextBytes
	}
	if splitWindow < 1 {
		splitWindow = 8 << 20
	}
	if overlap < 0 {
		overlap = 0
	}
	if maxTokens < 1 {
		maxTokens = 512
	}
	return &Indexer{
		splitter:    splitter,
		status:      status,
		embedder:    embedder,
		vectors:     vectors,
		search:      search,
		pub:         pub,
		pacer:       pacer,
		store:       store,
		tokenizer:   tokenizer,
		maxTokens:   maxTokens,
		textMax:     textMax,
		batchSize:   batchSize,
		splitWindow: splitWindow,
		overlap:     overlap,
		headers:     headers,
		metrics:     metrics,
	}
}

func (ix *Indexer) normalizeDelimited(
	log zerolog.Logger, evt *commonv1.DocumentParsed, text string, trunc truncation,
) (string, truncation) {
	transformed, ok := delimited.TransformWithLimit(evt.GetFilename(), evt.GetMimeType(), text, ix.textMax)
	if !ok {
		return text, trunc
	}
	if transformed.Truncated {
		if !trunc.Truncated {
			trunc.OriginalBytes = len(text)
		}
		trunc.Truncated = true
		trunc.IndexedBytes = len(transformed.Text)
	}
	log.Info().
		Int("rows", transformed.Rows).
		Int("columns", transformed.Columns).
		Str("delimiter", string(transformed.Delimiter)).
		Bool("truncated", transformed.Truncated).
		Msg("delimited text normalized for indexing")
	return transformed.Text, trunc
}

func (ix *Indexer) process(ctx context.Context, evt *commonv1.DocumentParsed) error {
	ctx = contracts.WithDocumentGeneration(ctx, evt.GetGeneration())
	log := logger.From(ctx).With().Str("document_id", evt.GetDocumentId()).Str("source", source).Logger()

	// Фоновая индексация уступает интерактивным запросам, прежде чем трогать хранилища, из которых они читают.
	if ix.pacer != nil {
		ix.pacer.Wait(ctx)
	}

	if statusErr := ix.setStatus(ctx, evt.GetDocumentId(), contracts.StatusChunking, "", nil); statusErr != nil {
		return statusErr
	}
	ix.emit(ctx, evt, contracts.StatusChunking, "")

	text, trunc, err := ix.prepareText(ctx, log, evt)
	if err != nil {
		return ix.fail(ctx, evt, err)
	}

	// Заголовок и метаданные извлекаются по возможности и никогда не валят индексацию;
	// заголовок ещё и префикс шапки чанка.
	metaDone := make(chan struct{})
	go func() {
		defer close(metaDone)
		ix.maybeSetMeta(ctx, evt)
	}()
	title := ix.maybeSetTitleAndKind(ctx, evt, text)
	if ix.capture != nil {
		ix.capture.title = title
	}
	<-metaDone

	ds := structureOf(evt, text)
	if ix.capture != nil {
		ix.capture.sections = ds.spans(utf8.RuneCountInString(text))
	}
	workbookAnchors := parseWorkbookAnchorIndex(text)

	// С токенизатором меняется только мера размера: смещения всё равно берутся сопоставлением текста (LocateOffsets).
	sp := ix.documentSplitter(ctx)

	// Разбиение по окнам: все чанки разом — вторая полная копия текста, и на сотнях мегабайт это OOM.
	total := 0
	written := 0
	carry := ""
	var st stageTimes
	for start := 0; start < len(text); {
		end := windowEnd(text, start, ix.splitWindow)
		// Окно не пересекает границу страницы, так что чанк не охватывает две страницы;
		// огромная страница всё равно режется по байтам.
		atPageBreak := false
		if pb := ds.nextPageByteAfter(start, len(text)); pb > start && pb <= end {
			end = pb
			atPageBreak = pb < len(text)
		}
		seg := carry + text[start:end]
		tsplit := time.Now()
		chunks := sp.Split(seg)
		st.split += time.Since(tsplit)
		offsets := splitter.LocateOffsets(seg, chunkTexts(chunks))
		baseRune := windowBaseRune(text, start, carry)
		w, werr := ix.indexChunks(ctx, evt, title, chunks, total, offsets, baseRune, ds, workbookAnchors, &st)
		if werr != nil {
			// Сбой хранилища посреди пачки — статус failed, а не чистый indexed, чтобы частичная двойная запись была видна.
			return ix.fail(ctx, evt, werr)
		}
		written += w
		total += len(chunks)
		if atPageBreak {
			carry = ""
		} else {
			carry = tailRunes(seg, ix.overlap)
		}
		start = end
	}
	ix.recordStages(st)

	if total == 0 {
		log.Info().Msg("no chunks produced; marking indexed with zero chunks")
		count := int32(0)
		if statusErr := ix.setStatus(ctx, evt.GetDocumentId(), contracts.StatusIndexed, "no text content", &count); statusErr != nil {
			return statusErr
		}
		ix.emit(ctx, evt, contracts.StatusIndexed, "no text content")
		return nil
	}

	// written < total — молчаливая частичная двойная запись: её надо показать, а не рапортовать чистый indexed.
	if written != total {
		log.Error().
			Int("expected_chunks", total).
			Int("written_chunks", written).
			Msg("dual-write reconciliation mismatch")
		return ix.fail(ctx, evt, fmt.Errorf("incomplete index: wrote %d of %d chunks", written, total))
	}

	msg := indexedStatusMsg(total, written, trunc)
	count := int32(written)
	if statusErr := ix.setStatus(ctx, evt.GetDocumentId(), contracts.StatusIndexed, msg, &count); statusErr != nil {
		return statusErr
	}
	ix.emit(ctx, evt, contracts.StatusIndexed, msg)
	if ix.capture == nil {
		log.Info().Int("chunks", total).Int("written", written).Msg("indexed")
	}
	return nil
}

// Машиночитаемые маркеры в статусе позволяют потребителям понизить вес обрезанного или недописанного документа.
func indexedStatusMsg(total, written int, trunc truncation) string {
	var parts []string
	if note := truncationNote(trunc); note != "" {
		parts = append(parts, note)
	}
	if written != total {
		parts = append(parts, fmt.Sprintf("[reconcile expected=%d written=%d]", total, written))
	}
	return strings.Join(parts, " ")
}

func truncationNote(trunc truncation) string {
	if !trunc.Truncated {
		return ""
	}
	return fmt.Sprintf("текст усечён до %d МиБ для индексации %s", trunc.IndexedBytes>>20, trunc.statusToken())
}

func capTextToMax(text string, textMax int, trunc truncation) (string, truncation) {
	if textMax < 1 || len(text) <= textMax {
		return text, trunc
	}
	cut := textMax
	for cut > 0 && !utf8.RuneStart(text[cut]) {
		cut--
	}
	if cut < 0 {
		cut = 0
	}
	if !trunc.Truncated {
		trunc.OriginalBytes = len(text)
	}
	trunc.Truncated = true
	trunc.IndexedBytes = cut
	return text[:cut], trunc
}

// Мера кэширует подсчёты: рекурсивный сплиттер и перекрытие окон меряют одни и те же куски. Кэш живёт
// один документ, блокировки не нужны; при ошибке токенизатора кусок меряется в рунах.
func (ix *Indexer) documentSplitter(ctx context.Context) domain.Splitter {
	if ix.tokenizer == nil {
		return ix.splitter
	}
	log := logger.From(ctx)
	memo := make(map[string]int)
	measure := func(s string) int {
		if n, ok := memo[s]; ok {
			return n
		}
		n, err := ix.tokenizer.CountTokens(ctx, s)
		if err != nil {
			// Запасное значение не кладётся в кэш, чтобы следующий успешный подсчёт записал настоящее.
			log.Warn().Err(err).Msg("tokenizer count failed; falling back to rune length for this piece")
			return runeLen(s)
		}
		memo[s] = n
		return n
	}
	return splitter.NewRecursiveWithMeasure(ix.maxTokens, ix.overlap, measure)
}

func runeLen(s string) int { return utf8.RuneCountInString(s) }

// Префикс chunk_ разводит эти стадии с запросными стадиями llm-service в общей гистограмме.
func (ix *Indexer) recordStages(st stageTimes) {
	if ix.metrics == nil {
		return
	}
	ix.metrics.RecordStage("chunk_split", st.split.Seconds())
	ix.metrics.RecordStage("chunk_embed", st.embed.Seconds())
	ix.metrics.RecordStage("chunk_index", st.index.Seconds())
}

// base сохраняет сквозную нумерацию чанков между окнами, baseRune переводит смещения в координаты документа.
func (ix *Indexer) indexChunks(
	ctx context.Context,
	evt *commonv1.DocumentParsed,
	title string,
	chunks []domain.Chunk,
	base int,
	offsets [][2]int,
	baseRune int,
	ds docStructure,
	workbookAnchors workbookAnchorIndex,
	st *stageTimes,
) (int, error) {
	// Пока следующая пачка эмбеддится, одна запись в хранилища уже в полёте — GPU не простаивает за Qdrant и OpenSearch.
	written := 0
	var inflight chan batchWrite
	for start := 0; start < len(chunks); start += ix.batchSize {
		end := min(start+ix.batchSize, len(chunks))
		batch := chunks[start:end]

		vectors, err := ix.embedBatch(ctx, batch, title, offsets, baseRune, ds, base+start, base+end, st)
		if err != nil {
			if werr := ix.settleWrite(inflight, &written, st); werr != nil {
				return written, werr
			}
			return written, err
		}

		points, docs := buildBatch(evt, title, ix.headers, batch, base, offsets, baseRune, ds, workbookAnchors, vectors)

		if werr := ix.settleWrite(inflight, &written, st); werr != nil {
			return written, werr
		}
		inflight = ix.startBatchWrite(ctx, points, docs, base+start, base+end)

		// Между пачками длинного документа снова уступить запросам: большой документ не должен монополизировать хранилища.
		if end < len(chunks) && ix.pacer != nil {
			ix.pacer.Wait(ctx)
		}
	}
	if werr := ix.settleWrite(inflight, &written, st); werr != nil {
		return written, werr
	}
	return written, nil
}

type batchWrite struct {
	n   int
	dur time.Duration
	err error
}

func (ix *Indexer) settleWrite(inflight chan batchWrite, written *int, st *stageTimes) error {
	if inflight == nil {
		return nil
	}
	res := <-inflight
	st.index += res.dur
	if res.err != nil {
		return res.err
	}
	*written += res.n
	return nil
}

func (ix *Indexer) embedBatch(
	ctx context.Context, batch []domain.Chunk, title string,
	offsets [][2]int, baseRune int, ds docStructure, lo, hi int, st *stageTimes,
) ([][]float32, error) {
	texts := make([]string, len(batch))
	for i, c := range batch {
		charStart, _ := absOffset(offsets, c.Index, baseRune)
		texts[i] = withHeader(ix.headers, title, sectionOf(ds, charStart, c.Section), c.Text)
	}
	tembed := time.Now()
	vectors, err := ix.embedder.Embed(ctx, texts)
	st.embed += time.Since(tembed)
	if err != nil {
		return nil, fmt.Errorf("embed chunks [%d:%d]: %w", lo, hi, err)
	}
	if len(vectors) != len(batch) {
		return nil, fmt.Errorf("embedder returned %d vectors for %d chunks", len(vectors), len(batch))
	}
	return vectors, nil
}

func (ix *Indexer) startBatchWrite(
	ctx context.Context, points []VectorPoint, docs []SearchDoc, lo, hi int,
) chan batchWrite {
	ch := make(chan batchWrite, 1)
	go func() {
		tindex := time.Now()
		if uerr := ix.vectors.Upsert(ctx, points); uerr != nil {
			ch <- batchWrite{dur: time.Since(tindex), err: fmt.Errorf("upsert vectors [%d:%d]: %w", lo, hi, uerr)}
			return
		}
		if ierr := ix.search.Index(ctx, docs); ierr != nil {
			ch <- batchWrite{dur: time.Since(tindex), err: fmt.Errorf("index chunks [%d:%d]: %w", lo, hi, ierr)}
			return
		}
		ch <- batchWrite{n: hi - lo, dur: time.Since(tindex)}
	}()
	return ch
}

func windowEnd(text string, start, limit int) int {
	end := start + limit
	if end >= len(text) {
		return len(text)
	}
	for end > start && !utf8.RuneStart(text[end]) {
		end--
	}
	const lookback = 4096
	for i := end - 1; i > end-lookback && i > start; i-- {
		if text[i] == ' ' || text[i] == '\n' {
			return i + 1
		}
	}
	return end
}

// carry — ровно те руны, что кончаются на байте start, поэтому база = руны text[:start] − руны carry.
func windowBaseRune(text string, start int, carry string) int {
	return utf8.RuneCountInString(text[:start]) - utf8.RuneCountInString(carry)
}

func tailRunes(s string, n int) string {
	if n <= 0 {
		return ""
	}
	i := len(s)
	for ; n > 0 && i > 0; n-- {
		_, size := utf8.DecodeLastRuneInString(s[:i])
		i -= size
	}
	return s[i:]
}

func chunkTexts(chunks []domain.Chunk) []string {
	out := make([]string, len(chunks))
	for i, c := range chunks {
		out[i] = c.Text
	}
	return out
}

type truncation struct {
	Truncated     bool
	OriginalBytes int
	IndexedBytes  int
}

func (t truncation) statusToken() string {
	if !t.Truncated {
		return ""
	}
	return fmt.Sprintf("[truncated orig=%d indexed=%d bytes]", t.OriginalBytes, t.IndexedBytes)
}

func (ix *Indexer) resolveText(
	ctx context.Context, evt *commonv1.DocumentParsed,
) (text string, trunc truncation, err error) {
	key := evt.GetTextObjectKey()
	if key == "" {
		return evt.GetText(), truncation{Truncated: false, OriginalBytes: 0, IndexedBytes: 0}, nil
	}
	if ix.store == nil {
		return "", truncation{}, errors.New("text_object_key set but no object store configured")
	}
	data, gerr := ix.store.GetBytes(ctx, key)
	if gerr != nil {
		return "", truncation{}, fmt.Errorf("fetch parsed text %s: %w", key, gerr)
	}
	trunc = truncation{Truncated: false, OriginalBytes: len(data), IndexedBytes: len(data)}
	if len(data) > ix.textMax {
		// Жёсткий предел против OOM: обрезка по границе руны, в поиск попадают только первые textMax байт.
		cut := ix.textMax
		for cut > 0 && !utf8.RuneStart(data[cut]) {
			cut--
		}
		data = data[:cut]
		trunc.Truncated = true
		trunc.IndexedBytes = len(data)
	}
	if !utf8.Valid(data) {
		// Текстовые источники приходят ссылкой как есть, поэтому чистятся здесь; копируются только невалидные данные.
		data = bytes.ToValidUTF8(data, []byte("�"))
	}
	return string(data), trunc, nil
}

func (ix *Indexer) fail(ctx context.Context, evt *commonv1.DocumentParsed, err error) error {
	if !contracts.FinalAttempt(ctx) {
		return err
	}
	logger.From(ctx).Error().Err(err).Str("document_id", evt.GetDocumentId()).Msg("indexing failed")
	if statusErr := ix.setStatus(ctx, evt.GetDocumentId(), contracts.StatusFailed, err.Error(), nil); statusErr != nil {
		return statusErr
	}
	ix.emit(ctx, evt, contracts.StatusFailed, err.Error())
	return err
}

func (ix *Indexer) setStatus(ctx context.Context, id, status, msg string, chunkCount *int32) error {
	if err := ix.status.UpdateDocumentStatus(ctx, id, status, msg, chunkCount); err != nil {
		return fmt.Errorf("%w: %w", contracts.ErrDurability, err)
	}
	return nil
}

func (ix *Indexer) emit(ctx context.Context, evt *commonv1.DocumentParsed, status, msg string) {
	ev := &commonv1.IngestionEvent{
		DocumentId: evt.GetDocumentId(),
		OwnerId:    evt.GetOwnerId(),
		Status:     status,
		Message:    msg,
		Timestamp:  time.Now().UTC().Format(time.RFC3339),
	}
	if err := ix.pub.PublishProto(ctx, contracts.ExchangeEvents, "", ev); err != nil {
		logger.From(ctx).Warn().Err(err).Msg("emit event failed")
	}
}

func (ix *Indexer) prepareText(
	ctx context.Context, log zerolog.Logger, evt *commonv1.DocumentParsed,
) (string, truncation, error) {
	text, trunc, err := ix.resolveText(ctx, evt)
	if err != nil {
		return "", trunc, err
	}
	text, trunc = capTextToMax(text, ix.textMax, trunc)
	if trunc.Truncated {
		log.Warn().
			Bool("truncated", true).
			Int("original_bytes", trunc.OriginalBytes).
			Int("indexed_bytes", trunc.IndexedBytes).
			Msg("text truncated to TEXT_MAX for indexing")
	}
	text, trunc = ix.normalizeDelimited(log, evt, text, trunc)

	// Склейка разреженного набора из PDF («М е т о д»), иначе он станет чанками по одной букве.
	// Склеиваются только одиночные буквы, поэтому это безопасно до разбиения и расчёта смещений.
	if ix.capture == nil {
		text = splitter.CollapseTracking(text)
	} else {
		if trunc.Truncated {
			return "", trunc, errors.New("truncated source text cannot become a ready snapshot")
		}
		ix.capture.text = text
	}

	return text, trunc, nil
}
