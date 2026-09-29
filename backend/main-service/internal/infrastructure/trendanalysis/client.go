package trendanalysis

import (
	"bytes"
	"context"
	"encoding/json/v2"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/example/main-service/internal/application"
)

type Client struct {
	URL  string
	HTTP *http.Client
}

func New(url string) *Client {
	return &Client{URL: strings.TrimRight(url, "/"), HTTP: &http.Client{Timeout: 15 * time.Minute}}
}

func (c *Client) Analyze(ctx context.Context, input application.LiveAnalysisRequest) (application.LiveAnalysisResult, error) {
	result := application.LiveAnalysisResult{}
	input = application.PrepareLiveAnalysisRequest(input)
	body, err := json.Marshal(input)
	if err != nil {
		return result, fmt.Errorf("analysis request: %w", err)
	}
	if len(body) > 512<<20 {
		return result, errors.New("analysis request exceeds 512 MiB budget")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.URL+"/v1/analyze", bytes.NewReader(body))
	if err != nil {
		return result, fmt.Errorf("build analysis request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return result, fmt.Errorf("analysis service: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		// Голый код ответа ничего не говорит тому, кто читает оговорку сбора: причина отказа
		// приходит в теле ответа.
		reason, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		detail := strings.TrimSpace(string(reason))
		if detail == "" {
			return result, fmt.Errorf("analysis service: HTTP %d", resp.StatusCode)
		}
		return result, fmt.Errorf("analysis service: HTTP %d: %s", resp.StatusCode, detail)
	}
	raw, err := io.ReadAll(io.LimitReader(resp.Body, (256<<20)+1))
	if err != nil {
		return result, fmt.Errorf("read analysis response: %w", err)
	}
	if len(raw) > 256<<20 {
		return result, errors.New("analysis response exceeds 256 MiB budget")
	}
	if err := json.Unmarshal(raw, &result); err != nil {
		return result, fmt.Errorf("analysis response: %w", err)
	}
	if result.Version != application.LiveAnalysisVersion || result.Topics == nil || result.Semantics == nil {
		return result, errors.New("analysis response has an incompatible version or missing results")
	}
	return result, nil
}
