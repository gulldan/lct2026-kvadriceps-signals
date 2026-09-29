package application

import (
	"context"
	"fmt"
	"path"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"google.golang.org/protobuf/proto"

	"github.com/example/archive-worker/internal/domain"
	"github.com/example/archive-worker/internal/platform/contracts"
	"github.com/example/archive-worker/internal/platform/logger"

	commonv1 "github.com/example/archive-worker/internal/platform/genproto/common/v1"
	dbv1 "github.com/example/archive-worker/internal/platform/genproto/db/v1"
)

// maxDepth — защита от архивных бомб: глубже архива в архиве не распаковываем.
const maxDepth = 2

// registerConcurrency бережёт db-service и брокер: каждая запись — несколько сетевых вызовов.
const registerConcurrency = 8

func junkEntry(p string) bool {
	if strings.Contains(p, "__MACOSX/") {
		return true
	}
	base := path.Base(p)
	return base == "" || strings.HasPrefix(base, ".")
}

type DocumentCatalog interface {
	CreateDocument(ctx context.Context, req *dbv1.CreateDocumentRequest) (*commonv1.Document, error)
	FindDocumentByHash(ctx context.Context, ownerID, hash string) (*commonv1.Document, error)
	UpdateDocumentStatus(ctx context.Context, id, status, message string, chunkCount *int32) error
}

type Publisher interface {
	PublishProto(ctx context.Context, exchange, routingKey string, msg proto.Message) error
}

type Processor struct {
	extractor  domain.Extractor
	docs       DocumentCatalog
	pub        Publisher
	maxEntries int
}

// New: maxEntries < 1 означает 50000 — верхнюю границу одной массовой загрузки по ТЗ.
func New(extractor domain.Extractor, docs DocumentCatalog, pub Publisher, maxEntries int) *Processor {
	if maxEntries < 1 {
		maxEntries = 50000
	}
	return &Processor{extractor: extractor, docs: docs, pub: pub, maxEntries: maxEntries}
}

func (p *Processor) Process(ctx context.Context, evt *commonv1.DocumentUploaded) error {
	ctx = contracts.WithDocumentGeneration(ctx, evt.GetGeneration())
	log := logger.From(ctx).With().Str("document_id", evt.GetDocumentId()).Str("source", "archive").Logger()

	if evt.GetArchiveDepth() >= maxDepth {
		msg := fmt.Sprintf("вложенные архивы глубже %d уровней не распаковываются", maxDepth)
		if statusErr := p.setStatus(ctx, evt.GetDocumentId(), contracts.StatusFailed, msg, nil); statusErr != nil {
			return statusErr
		}
		p.emit(ctx, evt, contracts.StatusFailed, msg)
		log.Warn().Int32("depth", evt.GetArchiveDepth()).Msg("nested archive too deep")
		return nil // повтор не поможет, в dead-letter не отправляем
	}

	if statusErr := p.setStatus(ctx, evt.GetDocumentId(), contracts.StatusParsing, "", nil); statusErr != nil {
		return statusErr
	}
	p.emit(ctx, evt, contracts.StatusParsing, "")

	// extractionID = document_id: повторная доставка перезапишет те же ключи S3, а не создаст копии.
	res, err := p.extractor.Extract(ctx, evt.GetObjectKey(), evt.GetFilename(), evt.GetDocumentId())
	if err != nil {
		return p.fail(ctx, evt, fmt.Errorf("extract archive: %w", err))
	}
	if len(res.Entries) > p.maxEntries {
		return p.fail(ctx, evt,
			fmt.Errorf("в архиве %d файлов — больше предела %d", len(res.Entries), p.maxEntries))
	}

	// Принятые дети фиксируются независимо; ошибка любой регистрации сохраняет архив для повтора.
	tally := p.registerEntries(ctx, evt, res.Entries)
	// Контекст отменён остановкой или HandleTimeout — сообщение доставят повторно.
	if ctx.Err() != nil {
		return p.fail(ctx, evt, fmt.Errorf("archive processing interrupted: %w", ctx.Err()))
	}
	skipped := res.Skipped + tally.skipped
	registered := int(tally.registered.Load())
	duplicates := int(tally.duplicates.Load())
	failed := int(tally.failed.Load())

	msg := fmt.Sprintf("архив распакован: %d файлов отправлено в обработку", registered)
	if failed > 0 {
		msg += fmt.Sprintf(", %d не удалось зарегистрировать", failed)
	}
	if duplicates > 0 {
		msg += fmt.Sprintf(", %d дубликатов пропущено", duplicates)
	}
	if skipped > 0 {
		msg += fmt.Sprintf(", %d служебных пропущено", skipped)
	}
	if res.SkippedOversize > 0 {
		msg += fmt.Sprintf(", %d превысили лимит размера", res.SkippedOversize)
	}
	if failed > 0 {
		return p.fail(ctx, evt, fmt.Errorf("archive entries still pending: %d", failed))
	}
	count := int32(0)
	if statusErr := p.setStatus(ctx, evt.GetDocumentId(), contracts.StatusIndexed, msg, &count); statusErr != nil {
		return statusErr
	}
	p.emit(ctx, evt, contracts.StatusIndexed, msg)
	log.Info().
		Int("registered", registered).
		Int("failed", failed).
		Int("duplicates", duplicates).
		Int("skipped", skipped).
		Int("skipped_oversize", res.SkippedOversize).
		Msg("archive unpacked")
	return nil
}

type entryTally struct {
	registered, duplicates, failed atomic.Int64
	skipped                        int
}

// registerEntries занимает хеши до раздачи по воркерам, иначе одинаковые файлы одного архива
// параллельно проскочат дедупликацию.
func (p *Processor) registerEntries(
	ctx context.Context, evt *commonv1.DocumentUploaded, entries []domain.Entry,
) *entryTally {
	tally := &entryTally{}
	seenHashes := make(map[string]struct{}, len(entries))
	sem := make(chan struct{}, registerConcurrency)
	var wg sync.WaitGroup
	for _, entry := range entries {
		if ctx.Err() != nil {
			break
		}
		if junkEntry(entry.Path) {
			tally.skipped++
			continue
		}
		if entry.Hash != "" {
			if _, ok := seenHashes[entry.Hash]; ok {
				tally.duplicates.Add(1)
				continue
			}
			seenHashes[entry.Hash] = struct{}{}
		}
		select {
		case sem <- struct{}{}:
		case <-ctx.Done():
			wg.Wait()
			return tally
		}
		wg.Go(func() {
			defer func() { <-sem }()
			p.registerOne(ctx, evt, entry, tally)
		})
	}
	wg.Wait()
	return tally
}

func (p *Processor) registerOne(
	ctx context.Context, evt *commonv1.DocumentUploaded, entry domain.Entry, tally *entryTally,
) {
	if p.isDuplicate(ctx, evt.GetOwnerId(), entry) {
		tally.duplicates.Add(1)
		return
	}
	if rerr := p.registerEntry(ctx, evt, entry); rerr != nil {
		tally.failed.Add(1)
		logger.From(ctx).Warn().Err(rerr).Str("entry", entry.Path).Msg("entry registration failed")
		return
	}
	tally.registered.Add(1)
}

func (p *Processor) registerEntry(
	ctx context.Context, evt *commonv1.DocumentUploaded, entry domain.Entry,
) error {
	_, err := p.docs.CreateDocument(ctx, &dbv1.CreateDocumentRequest{
		OwnerId:     evt.GetOwnerId(),
		Filename:    path.Base(entry.Path),
		MimeType:    entry.MIMEType,
		Size:        entry.Size,
		ObjectKey:   entry.ObjectKey,
		ContentHash: entry.Hash, Enqueue: true, ArchiveDepth: evt.GetArchiveDepth() + 1,
		RequestId: evt.GetDocumentId() + "/" + entry.Path,
	})
	if err != nil {
		return fmt.Errorf("create document for %s: %w", entry.Path, err)
	}

	return nil
}

// isDuplicate при ошибке поиска пропускает запись: дедупликация — лишь оптимизация. Поиск идёт
// в пределах владельца, чтобы корпуса разных пользователей не дедуплицировались друг о друга.
func (p *Processor) isDuplicate(ctx context.Context, ownerID string, entry domain.Entry) bool {
	if entry.Hash == "" {
		return false
	}
	if _, err := p.docs.FindDocumentByHash(ctx, ownerID, entry.Hash); err == nil {
		return true
	}
	return false
}

func (p *Processor) fail(ctx context.Context, evt *commonv1.DocumentUploaded, err error) error {
	if !contracts.FinalAttempt(ctx) {
		return err
	}
	logger.From(ctx).Error().Err(err).Str("document_id", evt.GetDocumentId()).Msg("archive processing failed")
	if statusErr := p.setStatus(ctx, evt.GetDocumentId(), contracts.StatusFailed, err.Error(), nil); statusErr != nil {
		return statusErr
	}
	p.emit(ctx, evt, contracts.StatusFailed, err.Error())
	return err
}

func (p *Processor) setStatus(ctx context.Context, id, status, msg string, chunkCount *int32) error {
	if err := p.docs.UpdateDocumentStatus(ctx, id, status, msg, chunkCount); err != nil {
		return fmt.Errorf("%w: %w", contracts.ErrDurability, err)
	}
	return nil
}

func (p *Processor) emit(ctx context.Context, evt *commonv1.DocumentUploaded, status, msg string) {
	ev := &commonv1.IngestionEvent{
		DocumentId: evt.GetDocumentId(), OwnerId: evt.GetOwnerId(),
		Status: status, Message: msg, Timestamp: time.Now().UTC().Format(time.RFC3339),
	}
	if err := p.pub.PublishProto(ctx, contracts.ExchangeEvents, "", ev); err != nil {
		logger.From(ctx).Warn().Err(err).Msg("emit event failed")
	}
}
