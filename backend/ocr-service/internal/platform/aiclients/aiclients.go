// Package aiclients — клиенты нейросетевых бэкендов; пустой URL даёт детерминированную заглушку,
// чтобы конвейер работал локально без GPU.
package aiclients

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/example/ocr-service/internal/platform/jsonx"
)

// defaultTimeout — только предохранитель, границу задаёт контекст: шлюз отдаёт
// 7–13 токенов в секунду на поток, и ответ в тысячу токенов идёт минутами.
const defaultTimeout = 10 * time.Minute

// gatewayIdleConns: стандартные два соединения на хост заставляли каждый
// параллельный вызов к шлюзу заново проходить TCP и TLS.
const gatewayIdleConns = 256

const modelField = "model"

func httpClient(c *http.Client) *http.Client {
	if c != nil {
		return c
	}
	transport, ok := http.DefaultTransport.(*http.Transport)
	if !ok {
		return &http.Client{Timeout: defaultTimeout}
	}
	transport = transport.Clone()
	transport.MaxIdleConns = gatewayIdleConns
	transport.MaxIdleConnsPerHost = gatewayIdleConns
	return &http.Client{Timeout: defaultTimeout, Transport: transport}
}

type reqOption func(*http.Request)

type httpStatusError struct {
	url    string
	status int
	body   string
}

func (e *httpStatusError) Error() string {
	return fmt.Sprintf("post %s: status %d: %s", e.url, e.status, e.body)
}

func postJSON(ctx context.Context, c *http.Client, url string, in, out any, opts ...reqOption) error {
	raw, err := jsonx.Marshal(in)
	if err != nil {
		return fmt.Errorf("marshal request: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(raw))
	if err != nil {
		return fmt.Errorf("new request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	for _, opt := range opts {
		opt(req)
	}
	resp, err := c.Do(req)
	if err != nil {
		return fmt.Errorf("post %s: %w", url, err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode >= http.StatusMultipleChoices {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
		return &httpStatusError{url: url, status: resp.StatusCode, body: string(body)}
	}
	if out != nil {
		if derr := jsonx.NewDecoder(resp.Body).Decode(out); derr != nil {
			return fmt.Errorf("decode response: %w", derr)
		}
	}
	return nil
}
