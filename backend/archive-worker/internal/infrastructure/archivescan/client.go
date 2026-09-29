package archivescan

import (
	"context"
	"fmt"
	"io"
	"math"
	"mime/multipart"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/example/archive-worker/internal/domain"
	"github.com/example/archive-worker/internal/platform/jsonx"
)

type ObjectStore interface {
	Get(ctx context.Context, key string) (io.ReadCloser, error)
}

// Limits: нулевое поле отключает соответствующий лимит.
type Limits struct {
	MaxFileBytes   int64
	MaxTotalBytes  int64
	MaxRatio       float64
	ExtractTimeout time.Duration
}

type Client struct {
	base   string
	store  ObjectStore
	httpc  *http.Client
	limits Limits
}

func New(baseURL string, store ObjectStore, timeout time.Duration, limits Limits) *Client {
	if timeout <= 0 {
		timeout = 2 * time.Hour
	}
	return &Client{
		base:   strings.TrimRight(baseURL, "/"),
		store:  store,
		httpc:  &http.Client{Timeout: timeout},
		limits: limits,
	}
}

const formTrue = "true"

type wire struct {
	TotalFiles           int64 `json:"total_files"`
	StoredFiles          int64 `json:"stored_files"`
	StoredBytes          int64 `json:"stored_bytes"`
	SkippedOversizeFiles int64 `json:"skipped_oversize_files"`
	Entries              []struct {
		SanitizedPath string `json:"sanitized_path"`
		Row           struct {
			EntryKind       string `json:"entry_kind"`
			Mime            string `json:"mime"`
			IsNestedArchive bool   `json:"is_nested_archive"`
			FullB3          string `json:"full_b3"`
		} `json:"row"`
		StoredObject *struct {
			URI       string `json:"uri"`
			SizeBytes int64  `json:"size_bytes"`
		} `json:"stored_object"`
	} `json:"entries"`
}

func (c *Client) Extract(
	ctx context.Context, objectKey, filename, extractionID string,
) (*domain.Extraction, error) {
	// Свой дедлайн на случай, если защита archive-scan от CPU/IO-бомбы не сработает;
	// тот же бюджет уходит в поле extract_timeout_secs.
	if c.limits.ExtractTimeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, c.limits.ExtractTimeout)
		defer cancel()
	}

	obj, err := c.store.Get(ctx, objectKey)
	if err != nil {
		return nil, fmt.Errorf("open archive object: %w", err)
	}
	defer func() { _ = obj.Close() }()

	// Тело multipart идёт через pipe, чтобы архив не буферизовался в памяти.
	pr, pw := io.Pipe()
	defer func() { _ = pr.Close() }()
	form := multipart.NewWriter(pw)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.base+"/v1/extract/upload", pr)
	if err != nil {
		return nil, fmt.Errorf("new extract request: %w", err)
	}
	req.Header.Set("Content-Type", form.FormDataContentType())
	// Запись начинается только после проверки адреса; закрытие читателя освобождает её при раннем отказе.
	go func() { _ = pw.CloseWithError(c.writeArchiveForm(form, obj, filename, extractionID)) }()

	resp, err := c.httpc.Do(req)
	if err != nil {
		return nil, fmt.Errorf("call archive-scan: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode >= http.StatusMultipleChoices {
		raw, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
		return nil, fmt.Errorf("archive-scan: status %d: %s", resp.StatusCode, string(raw))
	}

	var w wire
	if derr := jsonx.NewDecoder(resp.Body).Decode(&w); derr != nil {
		return nil, fmt.Errorf("decode archive-scan response: %w", derr)
	}
	return mapExtraction(&w)
}

func (c *Client) writeArchiveForm(
	form *multipart.Writer, obj io.Reader, filename, extractionID string,
) error {
	part, err := form.CreateFormFile("archive", filename)
	if err != nil {
		return fmt.Errorf("create form file: %w", err)
	}
	if _, err := io.Copy(part, obj); err != nil {
		return fmt.Errorf("copy archive into form: %w", err)
	}
	if err := writeFields(form, map[string]string{
		"extraction_id":        extractionID,
		"include_entries":      formTrue,
		"fast_only":            formTrue,
		"full_hash":            formTrue,
		"max_file_bytes":       strconv.FormatInt(maxNonNeg(c.limits.MaxFileBytes), 10),
		"max_total_bytes":      strconv.FormatInt(maxNonNeg(c.limits.MaxTotalBytes), 10),
		"max_ratio":            strconv.FormatFloat(maxNonNegFloat(c.limits.MaxRatio), 'f', -1, 64),
		"extract_timeout_secs": strconv.FormatInt(int64(c.limits.ExtractTimeout/time.Second), 10),
	}); err != nil {
		return err
	}
	if err := form.Close(); err != nil {
		return fmt.Errorf("close multipart form: %w", err)
	}
	return nil
}

func maxNonNeg(v int64) int64 {
	if v < 0 {
		return 0
	}
	return v
}

func maxNonNegFloat(v float64) float64 {
	if v < 0 || math.IsNaN(v) || math.IsInf(v, 0) {
		return 0
	}
	return v
}

func mapExtraction(w *wire) (*domain.Extraction, error) {
	out := &domain.Extraction{
		Entries:         make([]domain.Entry, 0, len(w.Entries)),
		TotalFiles:      w.TotalFiles,
		StoredBytes:     w.StoredBytes,
		SkippedOversize: int(w.SkippedOversizeFiles),
		Skipped:         0,
	}
	stored := 0
	for _, e := range w.Entries {
		if e.StoredObject == nil {
			continue
		}
		stored++
		key, err := objectKeyFromURI(e.StoredObject.URI)
		if err != nil {
			out.Skipped++
			continue
		}
		out.Entries = append(out.Entries, domain.Entry{
			Path:          e.SanitizedPath,
			ObjectKey:     key,
			MIMEType:      e.Row.Mime,
			Size:          e.StoredObject.SizeBytes,
			NestedArchive: e.Row.IsNestedArchive,
			Hash:          e.Row.FullB3,
		})
	}
	if stored > 0 && out.Skipped == stored {
		return nil, fmt.Errorf("archive-scan: all %d stored entries have an unparseable stored object uri", stored)
	}
	return out, nil
}

func writeFields(form *multipart.Writer, fields map[string]string) error {
	for k, v := range fields {
		if err := form.WriteField(k, v); err != nil {
			return fmt.Errorf("write field %s: %w", k, err)
		}
	}
	return nil
}

func objectKeyFromURI(uri string) (string, error) {
	rest, ok := strings.CutPrefix(uri, "s3://")
	if !ok {
		return "", fmt.Errorf("unexpected stored object uri %q (want s3://...)", uri)
	}
	_, key, ok := strings.Cut(rest, "/")
	if !ok || key == "" {
		return "", fmt.Errorf("stored object uri %q has no key", uri)
	}
	return key, nil
}
