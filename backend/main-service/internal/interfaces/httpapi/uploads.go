package httpapi

import (
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/gabriel-vasile/mimetype"
	"lukechampine.com/blake3"

	"github.com/example/main-service/internal/domain"
	"github.com/example/main-service/internal/platform/httpx"
	"github.com/example/main-service/internal/platform/storage"
)

// spoolUploadPart не ограничивает копию сам: тело уже урезано выше по стеку.
func spoolUploadPart(r *http.Request) (*os.File, string, int64, error) {
	mr, err := r.MultipartReader()
	if err != nil {
		return nil, "", 0, fmt.Errorf("parse multipart form: %w", err)
	}
	for {
		part, perr := mr.NextPart()
		if perr != nil {
			if errors.Is(perr, io.EOF) {
				return nil, "", 0, errors.New("missing file field")
			}
			return nil, "", 0, fmt.Errorf("parse multipart form: %w", perr)
		}
		if part.FormName() != "file" {
			_ = part.Close()
			continue
		}
		tmp, terr := os.CreateTemp("", "rag-upload-*")
		if terr != nil {
			_ = part.Close()
			return nil, "", 0, fmt.Errorf("spool upload: %w", terr)
		}
		size, cerr := io.Copy(tmp, part)
		_ = part.Close()
		if cerr != nil {
			_ = tmp.Close()
			_ = os.Remove(filepath.Clean(tmp.Name()))
			return nil, "", 0, fmt.Errorf("read file: %w", cerr)
		}
		return tmp, part.FileName(), size, nil
	}
}

func (a *API) uploadDocument(w http.ResponseWriter, r *http.Request) {
	t0 := time.Now()
	ownerID, ok := ownerFromContext(r)
	if !ok {
		httpx.Error(w, http.StatusUnauthorized, "missing claims")
		return
	}

	limit := a.maxUploadMB << 20
	// Запас в 1 МиБ сверх лимита файла — на обвязку multipart.
	r.Body = http.MaxBytesReader(w, r.Body, limit+(1<<20))
	// Часть «file» пишется во временный файл: память O(64 КиБ) на запрос при любом размере, а хэшу и S3
	// нужен перематываемый файл.
	file, filename, size, err := spoolUploadPart(r)
	if err != nil {
		httpx.Error(w, http.StatusBadRequest, err.Error())
		return
	}
	defer func() {
		_ = file.Close()
		_ = os.Remove(filepath.Clean(file.Name()))
	}()
	if size == 0 {
		httpx.Error(w, http.StatusBadRequest, "uploaded file is empty")
		return
	}
	if _, serr := file.Seek(0, io.SeekStart); serr != nil {
		httpx.Error(w, http.StatusInternalServerError, "rewind upload")
		return
	}

	head := make([]byte, 3072)
	n, err := io.ReadFull(file, head)
	if err != nil && !errors.Is(err, io.ErrUnexpectedEOF) {
		httpx.Error(w, http.StatusBadRequest, fmt.Sprintf("read file: %v", err))
		return
	}
	mimeType := mimetype.Detect(head[:n]).String()

	// BLAKE3 отдельным проходом: SDK S3 нужен перематываемый body, поэтому TeeReader не подходит.
	if _, serr := file.Seek(0, io.SeekStart); serr != nil {
		httpx.Error(w, http.StatusInternalServerError, "rewind upload")
		return
	}
	hasher := blake3.New(32, nil)
	if _, herr := io.Copy(hasher, file); herr != nil {
		httpx.Error(w, http.StatusBadRequest, fmt.Sprintf("read file: %v", herr))
		return
	}
	contentHash := hex.EncodeToString(hasher.Sum(nil))

	cmd := domain.UploadCommand{
		OwnerID:  ownerID,
		Filename: filename,
		Size:     size,
		Body:     file,
		Text:     "",
		Hash:     contentHash,
		Reuse:    r.URL.Query().Get("reuse") == "1",
	}
	if sourceErr := readUploadSource(r, file, &cmd, mimeType); sourceErr != nil {
		httpx.Error(w, http.StatusBadRequest, sourceErr.Error())
		return
	}

	// text/plain минует разборщики и едет в chunk-splitter в теле события — только эта ветка читает файл в память.
	if strings.HasPrefix(strings.ToLower(mimeType), "text/plain") && size <= 4<<20 {
		if _, serr := file.Seek(0, io.SeekStart); serr != nil {
			httpx.Error(w, http.StatusInternalServerError, "rewind upload")
			return
		}
		data, rerr := io.ReadAll(file)
		if rerr != nil {
			httpx.Error(w, http.StatusBadRequest, fmt.Sprintf("read file: %v", rerr))
			return
		}
		cmd.Text = string(data)
	}
	if _, serr := file.Seek(0, io.SeekStart); serr != nil {
		httpx.Error(w, http.StatusInternalServerError, "rewind upload")
		return
	}

	doc, existed, err := a.ingestion.Upload(r.Context(), cmd, mimeType)
	if err != nil {
		a.fail(w, err)
		return
	}
	// Стадия upload охватывает приём, хэш, S3 PUT, запись и публикацию; пишется только при успехе.
	if a.stages != nil {
		a.stages.RecordStage("upload", time.Since(t0).Seconds())
	}
	uploadView := newDocumentView(doc)
	uploadView.Duplicate = existed
	httpx.JSON(w, http.StatusCreated, uploadView)
}

type beginUploadRequest struct {
	Filename string `json:"filename"`
	Size     int64  `json:"size"`
	MIMEType string `json:"mime_type"`
	Reuse    bool   `json:"reuse"`
}

type uploadSessionView struct {
	UploadID  string `json:"upload_id"`
	PartSize  int64  `json:"part_size"`
	PartCount int32  `json:"part_count"`
}

func (a *API) beginUpload(w http.ResponseWriter, r *http.Request) {
	ownerID, ok := ownerFromContext(r)
	if !ok {
		httpx.Error(w, http.StatusUnauthorized, "missing claims")
		return
	}
	var req beginUploadRequest
	if err := httpx.Decode(r, &req); err != nil {
		httpx.Error(w, http.StatusBadRequest, err.Error())
		return
	}
	if req.Filename == "" || req.Size <= 0 {
		httpx.Error(w, http.StatusBadRequest, "filename and positive size are required")
		return
	}
	sess, err := a.uploads.Begin(r.Context(), ownerID, req.Filename, req.MIMEType, req.Size, req.Reuse)
	if err != nil {
		a.fail(w, err)
		return
	}
	httpx.JSON(w, http.StatusCreated, uploadSessionView{
		UploadID: sess.ID, PartSize: sess.PartSize, PartCount: sess.PartCount,
	})
}

type partURLView struct {
	PartNumber int32  `json:"part_number"`
	URL        string `json:"url"`
}

func (a *API) uploadPartURLs(w http.ResponseWriter, r *http.Request) {
	ownerID, ok := ownerFromContext(r)
	if !ok {
		httpx.Error(w, http.StatusUnauthorized, "missing claims")
		return
	}
	from := int32(queryInt(r, "from", 1))
	count := int32(queryInt(r, "count", 100))
	urls, err := a.uploads.PartURLs(r.Context(), ownerID, r.PathValue("id"), from, count)
	if err != nil {
		a.fail(w, err)
		return
	}
	out := make([]partURLView, 0, len(urls))
	for _, u := range urls {
		out = append(out, partURLView{PartNumber: u.PartNumber, URL: u.URL})
	}
	httpx.JSON(w, http.StatusOK, map[string][]partURLView{"urls": out})
}

type completeUploadRequest struct {
	Parts []struct {
		PartNumber int32  `json:"part_number"`
		ETag       string `json:"etag"`
	} `json:"parts"`
}

func (a *API) completeUpload(w http.ResponseWriter, r *http.Request) {
	ownerID, ok := ownerFromContext(r)
	if !ok {
		httpx.Error(w, http.StatusUnauthorized, "missing claims")
		return
	}
	var req completeUploadRequest
	if err := httpx.Decode(r, &req); err != nil {
		httpx.Error(w, http.StatusBadRequest, err.Error())
		return
	}
	parts := make([]storage.MultipartPart, 0, len(req.Parts))
	for _, p := range req.Parts {
		parts = append(parts, storage.MultipartPart{PartNumber: p.PartNumber, ETag: p.ETag})
	}
	doc, existed, err := a.uploads.Complete(r.Context(), ownerID, r.PathValue("id"), parts)
	if err != nil {
		a.fail(w, err)
		return
	}
	view := newDocumentView(doc)
	view.Duplicate = existed
	httpx.JSON(w, http.StatusCreated, view)
}

func (a *API) abortUpload(w http.ResponseWriter, r *http.Request) {
	ownerID, ok := ownerFromContext(r)
	if !ok {
		httpx.Error(w, http.StatusUnauthorized, "missing claims")
		return
	}
	if err := a.uploads.Abort(r.Context(), ownerID, r.PathValue("id")); err != nil {
		a.fail(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
