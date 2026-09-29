package contracts

import (
	"context"
	"os"
	"path/filepath"
	"strings"
)

const (
	ExchangeIngestion = "ingestion"
	ExchangeEvents    = "events"
	ExchangeDLX       = "ingestion.dlx"
	QueueDead         = "dead"
)

// Ключи маршрутизации одновременно служат именами постоянных очередей, привязанных к ExchangeIngestion.
const (
	RouteParseEmail  = "parse.email"
	RouteParseOffice = "parse.office"
	RouteParsePDF    = "parse.pdf" // текстовые PDF
	RouteParseOCR    = "parse.ocr" // сканы PDF и изображения
	RouteParseVLM    = "parse.vlm"
	RouteParseArch   = "parse.archive" // zip/rar/7z/tar -> archive-worker
	RouteParseDB     = "parse.db"      // базы SQLite -> db-parser
	RouteChunk       = "chunk"         // разобранный текст -> chunk-splitter
)

const (
	StatusUploaded = "uploaded"
	StatusQueued   = "queued"
	StatusParsing  = "parsing"
	// StatusOCR — всё ещё разбор, но медленная ветка, которую интерфейс показывает отдельным шагом.
	StatusOCR      = "ocr"
	StatusParsed   = "parsed"
	StatusChunking = "chunking"
	StatusIndexed  = "indexed"
	StatusFailed   = "failed"
)

// MaxAttempts ограничивает прикладные ошибки, а не доставки после перезапуска.
const MaxAttempts = 4

type attemptKey struct{}

// WithAttempt передаёт обработчику номер завершённой прикладной попытки.
func WithAttempt(ctx context.Context, attempt int) context.Context {
	return context.WithValue(ctx, attemptKey{}, attempt)
}

func FinalAttempt(ctx context.Context) bool {
	attempt, _ := ctx.Value(attemptKey{}).(int)
	return attempt >= MaxAttempts-1
}

func ParserQueues() map[string]string {
	return map[string]string{
		RouteParseEmail:  RouteParseEmail,
		RouteParseOffice: RouteParseOffice,
		RouteParsePDF:    RouteParsePDF,
		RouteParseOCR:    RouteParseOCR,
		RouteParseVLM:    RouteParseVLM,
		RouteParseArch:   RouteParseArch,
		RouteParseDB:     RouteParseDB,
		RouteChunk:       RouteChunk,
	}
}

func RouteForMIME(mimeType, filename string) string {
	mt := strings.ToLower(mimeType)
	ext := strings.ToLower(filepath.Ext(filename))

	switch {
	case isArchiveMIME(mt, ext):
		return RouteParseArch
	case strings.Contains(mt, "pdf"):
		return RouteParsePDF
	case mt == "message/rfc822" || ext == ".eml" || ext == ".msg":
		return RouteParseEmail
	case strings.HasPrefix(mt, "image/"):
		// По умолчанию OCR: сканы страниц встречаются чаще схем, которым нужен VLM.
		if strings.EqualFold(os.Getenv("IMAGE_PARSE_ROUTE"), "vlm") {
			return RouteParseVLM
		}
		return RouteParseOCR
	case isSQLiteMIME(mt, ext):
		return RouteParseDB
	default:
		return RouteParseOffice
	}
}

func isSQLiteMIME(mt, ext string) bool {
	if strings.Contains(mt, "sqlite") {
		return true
	}
	switch ext {
	case ".db", ".sqlite", ".sqlite3":
		return mt == "application/octet-stream" || mt == ""
	default:
		return false
	}
}

// OOXML внутри zip, но определяется своими типами, поэтому голый application/zip — действительно архив.
func isArchiveMIME(mt, ext string) bool {
	switch {
	case mt == "application/zip",
		strings.Contains(mt, "x-rar"),
		strings.Contains(mt, "x-7z"),
		strings.Contains(mt, "x-tar"),
		mt == "application/gzip":
		return true
	}
	switch ext {
	case ".zip", ".rar", ".7z", ".tar", ".gz", ".tgz":
		return mt == "application/octet-stream" || mt == ""
	default:
		return false
	}
}
