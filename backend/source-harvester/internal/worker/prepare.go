package worker

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json/v2"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"sync"
	"uuid"

	"github.com/example/source-harvester/internal/platform/harvest"
	"github.com/example/source-harvester/internal/platform/sourcedoc"
	"github.com/example/source-harvester/internal/provider"
	"lukechampine.com/blake3"
)

const mimeText = "text/plain"

type pdfSource interface {
	PDF(context.Context, sourcedoc.Source) ([]byte, error)
}

func (w *Worker) prepare(ctx context.Context, job harvest.Job, page provider.Page) (harvest.CommitPage, error) {
	result := harvest.CommitPage{
		Lease:       job.Lease,
		PageNumber:  job.PageNumber,
		Cursor:      job.Cursor,
		Next:        page.Next,
		Exhausted:   page.Exhausted,
		Received:    page.Received,
		Rejected:    len(page.Rejected),
		RawHash:     page.RawHash,
		Limitations: page.Limitations,
	}
	prefix := "sources/" + job.Provider + "/" + job.Lease.RunID + "/"
	result.RawKey = prefix + page.RawHash + ".response"
	if err := w.Objects.Put(
		ctx,
		result.RawKey,
		bytes.NewReader(page.Raw),
		int64(len(page.Raw)),
		"application/octet-stream",
	); err != nil {
		return result, preparedError(err)
	}
	prepared, err := w.prepareRecords(ctx, job, page.Records)
	if err != nil {
		return result, preparedError(err)
	}
	result.Records = append(result.Records, prepared...)
	raw, err := json.Marshal(struct {
		Page     provider.Page      `json:"page"`
		Prepared []harvest.Prepared `json:"prepared"`
	}{page, result.Records})
	if err != nil {
		return result, preparedError(err)
	}
	if len(raw) > 16<<20 {
		return result, errors.New("source page manifest exceeds 16 MiB; reduce page size")
	}
	result.ManifestHash = sha(raw)
	result.ManifestKey = prefix + result.ManifestHash + ".json"
	if err = w.Objects.Put(ctx, result.ManifestKey, bytes.NewReader(raw), int64(len(raw)), "application/json"); err != nil {
		return result, preparedError(err)
	}
	if err = result.Validate(job); err != nil {
		return result, preparedError(err)
	}
	envelope, err := json.Marshal(result)
	if err != nil {
		return result, preparedError(err)
	}
	if len(envelope) > harvest.MaxPageBytes {
		return result, errors.New("prepared page exceeds control plane bound; reduce page size")
	}
	return result, nil
}

// prepareParallel: PDF выгрузки arXiv не ограничены по частоте, а по одному страница из полусотни работ шла
// две минуты; запросы к самому arxiv.org и при параллельной подготовке идут через общий ограничитель по одному.
const prepareParallel = 8

// prepareRecords сохраняет порядок записей; первая ошибка отменяет остальные — страница коммитится целиком или никак.
func (w *Worker) prepareRecords(ctx context.Context, job harvest.Job, records []provider.Record,
) ([]harvest.Prepared, error) {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	out := make([]harvest.Prepared, len(records))
	errs := make([]error, len(records))
	slot := make(chan struct{}, prepareParallel)
	var wg sync.WaitGroup
	for i, record := range records {
		slot <- struct{}{}
		wg.Go(func() {
			defer func() { <-slot }()
			if out[i], errs[i] = w.prepareRecord(ctx, job, record); errs[i] != nil {
				cancel()
			}
		})
	}
	wg.Wait()
	if err := errors.Join(errs...); err != nil {
		return nil, err
	}
	return out, nil
}

func (w *Worker) prepareRecord(ctx context.Context, job harvest.Job, record provider.Record) (harvest.Prepared, error) {
	result := harvest.Prepared{
		DocumentID:  uuid.NewV4().String(),
		Source:      record.Source,
		Title:       record.Title,
		MIME:        mimeText,
		TextStatus:  record.TextStatus,
		Limitations: record.Limitations,
	}
	result.Source.TestRunID = job.TestRunID
	content := []byte(record.Text)
	if pdf, ok := w.Adapter.(pdfSource); ok && record.Source.Provider == "arxiv" {
		body, err := pdf.PDF(ctx, record.Source)
		if err == nil {
			content, result.MIME, result.TextStatus = body, "application/pdf", "pdf"
			// Смещения Atom описывают реферат, а не PDF; разбор PDF построит локаторы по своему тексту.
			result.Source.Sections, result.Source.Anchors = nil, nil
			result.Limitations = slices.DeleteFunc(slices.Clone(result.Limitations), func(v string) bool {
				return v == "pdf_not_fetched" || v == "abstract_unavailable"
			})
		} else if httpErr, ok := errors.AsType[*provider.HTTPError](err); ok && httpErr.Status == http.StatusNotFound {
			result.Limitations = append(result.Limitations, "arxiv_pdf_not_found; only saved abstract/title is evidence")
		} else if errors.Is(err, provider.ErrResponseTooLarge) {
			result.Limitations = append(result.Limitations, "arxiv_pdf_exceeds_8_mib; only saved abstract/title is evidence")
		} else {
			return result, fmt.Errorf("fetch pinned arXiv PDF: %w", err)
		}
	}
	if len(content) == 0 || len(content) > 8<<20 {
		return result, errors.New("source file must contain 1..8 MiB")
	}
	result.Source.TextStatus = result.TextStatus
	result.Size = int64(len(content))
	result.Source.ContentHash, result.Source.HashAlgorithm = sha(content), "sha256"
	b3 := blake3.Sum256(content)
	result.BLAKE3 = hex.EncodeToString(b3[:])
	if result.MIME == mimeText {
		result.Text = string(content)
	}
	applyLanguage(&result)
	prefix := "sources/" + job.Provider + "/"
	if job.TestRunID != "" {
		prefix += job.TestRunID + "/"
	}
	result.ObjectKey = prefix + "blobs/" + harvest.DocumentKey(job.OwnerID, result) + "/" + result.Source.ContentHash
	if err := w.Objects.Put(ctx, result.ObjectKey, bytes.NewReader(content), result.Size, result.MIME); err != nil {
		return result, preparedError(err)
	}
	return result, nil
}

func sha(raw []byte) string { h := sha256.Sum256(raw); return hex.EncodeToString(h[:]) }

func preparedError(err error) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("prepare source: %w", err)
}
