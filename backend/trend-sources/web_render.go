package main

import (
	"bytes"
	"cmp"
	"context"
	"encoding/json/v2"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"
)

// Страница, чей текст появляется только после скриптов, читается ещё раз изолированным рендером:
// код страницы выполняется в его контейнере, не в этом процессе, в сети без сервисов платформы.
const renderMethod = "javascript_render"
const renderTimeout = 90 * time.Second

// renderPending оставляет курсор при исчерпании бюджета: страницы за ним ждут следующего прохода.
func (s *service) renderPending(ctx context.Context, data *snapshot, c *coverage) {
	queue := s.renderQueue
	s.renderQueue = nil
	byID := map[string]int{}
	for i, row := range data.Records {
		byID[row.ID] = i
	}
	for _, address := range queue {
		i, ok := byID[address]
		if !ok || data.Records[i].TextStatus == articleText {
			continue
		}
		if s.render == nil {
			c.Limitations = append(c.Limitations, "main text not extracted: "+address)
			continue
		}
		if s.config.RenderBudget > 0 && s.renders >= s.config.RenderBudget {
			c.Cursor = address
			c.StopReason = "javascript render budget reached; the pages behind this cursor stay metadata only"
			return
		}
		s.renders++
		raw, err := s.renderPage(ctx, address, data)
		if err != nil {
			// Отказ рендера сужает охват этой страницы; уже собранная запись и её текст не меняются.
			c.Limitations = append(c.Limitations, "javascript render failed: "+address+": "+err.Error())
			continue
		}
		enriched, parseErr := articleDocument(raw, data.Records[i], data.ObservedAt)
		if parseErr != nil {
			c.Limitations = append(c.Limitations, "main text not extracted after rendering: "+address)
			continue
		}
		enriched.Title = cmp.Or(enriched.Title, pageTitle(raw), address)
		enriched.Provenance = append(enriched.Provenance, provenance{URL: address, SHA256: hashBytes(raw), Method: renderMethod})
		data.Records[i] = enriched
	}
}

// renderPage пишет метод в провенанс, чтобы отрендеренный текст не приняли за ответ обычного запроса.
func (s *service) renderPage(ctx context.Context, address string, data *snapshot) ([]byte, error) {
	body, err := json.Marshal(map[string]any{"url": address, "gotoOptions": map[string]any{"waitUntil": "networkidle2"}})
	if err != nil {
		return nil, fmt.Errorf("build render request: %w", err)
	}
	endpoint, err := url.Parse(s.renderURL)
	if err != nil || endpoint.Host == "" || (endpoint.Scheme != "http" && endpoint.Scheme != "https") || endpoint.User != nil {
		return nil, errors.New("invalid renderer endpoint configuration")
	}
	// Токен берётся отдельно из окружения: старый параметр адреса не может его переопределить.
	query := endpoint.Query()
	query.Del("token")
	if s.renderToken != "" {
		query.Set("token", s.renderToken)
	}
	endpoint.RawQuery = query.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint.String(), bytes.NewReader(body))
	if err != nil {
		return nil, errors.New("invalid renderer request configuration")
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := s.render.Do(req)
	if err != nil {
		// Транспортная ошибка содержит адрес с токеном и затем попадает в публичный отчёт покрытия.
		return nil, errors.New("render request failed")
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return nil, &sourceHTTPError{Status: resp.StatusCode}
	}
	raw, err := io.ReadAll(io.LimitReader(resp.Body, (8<<20)+1))
	if err != nil {
		return nil, errors.New("read rendered page failed")
	}
	if len(raw) > 8<<20 {
		return nil, errors.New("rendered page exceeds 8 MiB")
	}
	raw = sourceUTF8(raw, address, resp.Header.Get("Content-Type"), data)
	if err = s.storeRaw(raw, address, renderMethod, data); err != nil {
		return nil, err
	}
	return raw, nil
}
