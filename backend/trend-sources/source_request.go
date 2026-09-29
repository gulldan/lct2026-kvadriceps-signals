package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"
)

// request отдаёт заголовки, чтобы область сама читала темп и квоту провайдера; провенанс не пишет.
func (s *service) request(ctx context.Context, method, address string, headers map[string]string,
	body []byte) ([]byte, http.Header, int, error) {
	if s.config.RequestBudget > 0 && s.requests >= s.config.RequestBudget {
		return nil, nil, 0, errBudget
	}
	s.requests++
	select {
	case <-ctx.Done():
		return nil, nil, 0, fmt.Errorf("source request cancelled: %w", ctx.Err())
	case <-time.After(time.Second):
	}
	req, err := http.NewRequestWithContext(ctx, method, address, bytes.NewReader(body))
	if err != nil {
		return nil, nil, 0, fmt.Errorf("build source request: %w", err)
	}
	req.Header.Set("User-Agent", "rag-test-signalgraph/1.0 (+public research metadata)")
	for name, value := range headers {
		req.Header.Set(name, value)
	}
	resp, err := s.http.Do(req)
	if err != nil {
		return nil, nil, 0, fmt.Errorf("source request failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	b, err := io.ReadAll(io.LimitReader(resp.Body, (8<<20)+1))
	if err != nil {
		return nil, resp.Header, resp.StatusCode, fmt.Errorf("read source response: %w", err)
	}
	if len(b) > 8<<20 {
		return nil, resp.Header, resp.StatusCode, errors.New("source response exceeds 8 MiB")
	}
	return b, resp.Header, resp.StatusCode, nil
}
