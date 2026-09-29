// Package tokenizer обходится стандартной библиотекой, чтобы не трогать вендоренный platform/aiclients.
package tokenizer

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// Зависшая модель не должна блокировать цикл разбиения; при сбое вызывающий меряет в рунах.
const defaultTimeout = 30 * time.Second

const tokenizePath = "/tokenize"

// LlamaCPP не выставляет add_special (на сервере false): счёт идёт по куску без служебных токенов.
type LlamaCPP struct {
	url   string
	httpc *http.Client
}

// New принимает базовый URL или уже с /tokenize; для пустого возвращает nil — токенизатора нет.
func New(base string, c *http.Client) *LlamaCPP {
	base = strings.TrimSpace(base)
	if base == "" {
		return nil
	}
	url := strings.TrimRight(base, "/")
	if !strings.HasSuffix(url, tokenizePath) {
		url += tokenizePath
	}
	if c == nil {
		c = &http.Client{Timeout: defaultTimeout}
	}
	return &LlamaCPP{url: url, httpc: c}
}

type tokenizeRequest struct {
	Content string `json:"content"`
}

type tokenizeResponse struct {
	Tokens []int `json:"tokens"`
}

func (t *LlamaCPP) CountTokens(ctx context.Context, text string) (int, error) {
	if text == "" {
		return 0, nil
	}
	raw, err := json.Marshal(tokenizeRequest{Content: text})
	if err != nil {
		return 0, fmt.Errorf("marshal tokenize request: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, t.url, bytes.NewReader(raw))
	if err != nil {
		return 0, fmt.Errorf("new tokenize request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := t.httpc.Do(req)
	if err != nil {
		return 0, fmt.Errorf("post %s: %w", t.url, err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode >= http.StatusMultipleChoices {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
		return 0, fmt.Errorf("post %s: status %d: %s", t.url, resp.StatusCode, string(body))
	}

	var out tokenizeResponse
	if derr := json.NewDecoder(resp.Body).Decode(&out); derr != nil {
		return 0, fmt.Errorf("decode tokenize response: %w", derr)
	}
	return len(out.Tokens), nil
}
