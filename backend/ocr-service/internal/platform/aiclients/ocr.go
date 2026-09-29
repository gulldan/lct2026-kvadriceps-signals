package aiclients

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/example/ocr-service/internal/platform/jsonx"
)

// OCRResult: Pages, если бэкенд их отдаёт, — тексты страниц в порядке чтения; из них чанки получают номер страницы.
type OCRResult struct {
	Text  string
	Pages []string
}

type OCR interface {
	Recognize(ctx context.Context, image []byte, mime string) (OCRResult, error)
}

func NewOCR(url, model string, c *http.Client) OCR {
	if url == "" {
		return &stubOCR{}
	}
	return &httpOCR{url: url, model: model, httpc: httpClient(c)}
}

// httpOCR терпит старые бэкенды, которые присылают в "pages" число страниц: страниц тогда нет.
type httpOCR struct {
	url   string
	model string
	httpc *http.Client
}

func (o *httpOCR) Recognize(ctx context.Context, image []byte, mime string) (OCRResult, error) {
	req := map[string]any{modelField: o.model, "image_b64": base64.StdEncoding.EncodeToString(image), "mime": mime}
	var resp struct {
		Text  string          `json:"text"`
		Pages json.RawMessage `json:"pages"`
	}
	if err := postJSON(ctx, o.httpc, o.url, req, &resp); err != nil {
		return OCRResult{}, err
	}
	out := OCRResult{Text: resp.Text}
	var pages []string
	if len(resp.Pages) > 0 && jsonx.Unmarshal(resp.Pages, &pages) == nil {
		out.Pages = pages
	}
	return out, nil
}

type stubOCR struct{}

func (o *stubOCR) Recognize(_ context.Context, image []byte, mime string) (OCRResult, error) {
	text := fmt.Sprintf("[stub-ocr] OCR backend is not configured (set OCR_ENGINE_URL to enable). "+
		"Received %d bytes of %s for recognition.", len(image), mime)
	return OCRResult{Text: text}, nil
}
