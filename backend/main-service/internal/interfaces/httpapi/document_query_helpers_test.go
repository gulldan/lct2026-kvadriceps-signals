package httpapi_test

import (
	"context"
	"github.com/example/main-service/internal/platform/contracts"
	commonv1 "github.com/example/main-service/internal/platform/genproto/common/v1"
	dbv1 "github.com/example/main-service/internal/platform/genproto/db/v1"
	"net/url"
	"slices"
	"sort"
	"strings"
	"time"
)

func filterAdminDocuments(docs []*commonv1.Document, q url.Values) []*commonv1.Document {
	needle := strings.ToLower(strings.TrimSpace(q.Get("q")))
	status := q.Get("status")
	ftype := q.Get("type")
	if needle == "" && status == "" && ftype == "" {
		return docs
	}
	out := make([]*commonv1.Document, 0, len(docs))
	for _, d := range docs {
		if needle != "" &&
			!strings.Contains(strings.ToLower(d.GetFilename()), needle) &&
			!strings.Contains(strings.ToLower(d.GetTitle()), needle) {
			continue
		}
		if status == "processing" && (d.GetStatus() == "indexed" || d.GetStatus() == "failed") {
			continue
		}
		if status != "" && status != "processing" && d.GetStatus() != status {
			continue
		}
		if ftype != "" && docFileType(d.GetFilename(), d.GetMimeType()) != ftype {
			continue
		}
		out = append(out, d)
	}
	return out
}

func topByChunks(docs []*commonv1.Document, n int) []topDocView {
	sorted := make([]*commonv1.Document, len(docs))
	copy(sorted, docs)
	sort.Slice(sorted, func(i, j int) bool {
		if sorted[i].GetChunkCount() != sorted[j].GetChunkCount() {
			return sorted[i].GetChunkCount() > sorted[j].GetChunkCount()
		}
		return sorted[i].GetId() < sorted[j].GetId()
	})
	out := make([]topDocView, 0, min(n, len(sorted)))
	for _, d := range sorted[:min(n, len(sorted))] {
		out = append(out, topDocView{
			ID:         d.GetId(),
			Filename:   d.GetFilename(),
			Title:      d.GetTitle(),
			ChunkCount: d.GetChunkCount(),
		})
	}
	return out
}

// lastDays — n календарных дней UTC по сегодняшний включительно, счётчики по нулям.
func lastDays(now time.Time, n int) []dayCountView {
	out := make([]dayCountView, 0, n)
	for i := n - 1; i >= 0; i-- {
		out = append(out, dayCountView{Day: now.AddDate(0, 0, -i).Format(time.DateOnly)})
	}
	return out
}

// docFileType зеркалит фронтовый docAdapter.fileTypeOf: категория бейджа/фильтра
// по расширению, затем по MIME.
func docFileType(filename, mime string) string {
	name := strings.ToLower(filename)
	ext := ""
	if dot := strings.LastIndexByte(name, '.'); dot >= 0 {
		ext = name[dot+1:]
	}
	switch ext {
	case "pdf":
		return typePDF
	case "doc", "docx":
		return typeDocx
	case "eml", "msg":
		return typeEml
	case "xls", "xlsx":
		return typeXlsx
	case "ppt", "pptx":
		return typePptx
	case "txt", "md":
		return typeTxt
	case "png", "jpg", "jpeg", "webp", "tiff":
		return typeImage
	case "db", "sqlite", "sqlite3":
		return typeDB
	}
	mt := strings.ToLower(mime)
	switch {
	case strings.Contains(mt, "pdf"):
		return typePDF
	case strings.HasPrefix(mt, "image/"):
		return typeImage
	case strings.Contains(mt, "wordprocessingml"), strings.Contains(mt, "msword"):
		return typeDocx
	case strings.Contains(mt, "spreadsheetml"), strings.Contains(mt, "excel"):
		return typeXlsx
	case strings.Contains(mt, "presentationml"), strings.Contains(mt, "powerpoint"):
		return typePptx
	case mt == "message/rfc822":
		return typeEml
	case strings.Contains(mt, "sqlite"):
		return typeDB
	case strings.HasPrefix(mt, "text/"):
		return typeTxt
	}
	return typeOther
}

func (f *fakeDocs) DocumentPage(ctx context.Context, q contracts.DocumentQuery) (*dbv1.DocumentPageResponse, error) {
	if f.pageHook != nil {
		if err := f.pageHook(q); err != nil {
			return nil, err
		}
	}
	owner := q.ReaderID
	if q.AllOwners {
		owner = ""
	}
	docs, err := f.ListDocuments(ctx, owner)
	if err != nil {
		return nil, err
	}
	docs = filterAdminDocuments(docs, url.Values{"q": {q.Query}, "status": {q.Status}, "type": {q.FileType}})
	sort.Slice(docs, func(i, j int) bool {
		if docs[i].GetCreatedAt() != docs[j].GetCreatedAt() {
			return docs[i].GetCreatedAt() > docs[j].GetCreatedAt()
		}
		return docs[i].GetId() > docs[j].GetId()
	})
	out := &dbv1.DocumentPageResponse{Total: int64(len(docs))}
	if q.BeforeID != "" {
		cursor := slices.IndexFunc(docs, func(doc *commonv1.Document) bool { return doc.GetId() == q.BeforeID })
		if cursor < 0 {
			return out, nil
		}
		docs = docs[cursor+1:]
	}
	start := min(q.Offset, len(docs))
	end := min(start+q.Limit, len(docs))
	out.Documents = docs[start:end]
	if end < len(docs) {
		out.NextCursor = docs[end-1].GetId()
	}
	return out, nil
}
func (f *fakeDocs) DocumentStats(ctx context.Context, q contracts.DocumentQuery) (contracts.DocumentStats, error) {
	owner := q.ReaderID
	if q.AllOwners {
		owner = ""
	}
	docs, err := f.ListDocuments(ctx, owner)
	if err != nil {
		return contracts.DocumentStats{}, err
	}
	stats := contracts.DocumentStats{
		Total: len(
			docs,
		),
		ByStatus:   map[string]int{},
		ByFileType: map[string]int{},
		UploadsByDay: lastDays(
			time.Now().UTC(),
			14,
		),
		TopByChunks: topByChunks(
			docs,
			10,
		),
	}
	for _, d := range docs {
		stats.ByStatus[d.GetStatus()]++
		stats.ByFileType[docFileType(d.GetFilename(), d.GetMimeType())]++
		stats.TotalSizeBytes += d.GetSize()
		stats.TotalChunks += int64(d.GetChunkCount())
		for i, day := range stats.UploadsByDay {
			if strings.HasPrefix(d.GetCreatedAt(), day.Day) {
				stats.UploadsByDay[i].Count++
			}
		}
	}
	if stats.Total > 0 {
		stats.IndexedPct = (stats.ByStatus["indexed"]*100 + stats.Total/2) / stats.Total
	}
	return stats, nil
}

const (
	typePDF   = "pdf"
	typeDocx  = "docx"
	typeEml   = "eml"
	typeXlsx  = "xlsx"
	typePptx  = "pptx"
	typeTxt   = "txt"
	typeImage = "image"
	typeDB    = "db"
	typeOther = "other"
)

type dayCountView = contracts.DocumentDay
type topDocView = contracts.DocumentTop
