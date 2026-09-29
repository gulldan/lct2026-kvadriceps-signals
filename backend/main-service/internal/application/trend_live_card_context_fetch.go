package application

import (
	"context"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"
)

const (
	liveCardContextRequest    = 9 * time.Second
	liveCardContextBudgetTime = 45 * time.Second
	liveCardContextDocuments  = 32
	liveCardContextMaxHTML    = 8 << 20
)

var errCardContextBudget = errors.New("card source context budget exhausted")

type liveCardContextKey struct{}

type liveCardContextResult struct {
	done     chan struct{}
	document liveCardContextDocument
	err      error
}

// Единственный владелец бюджета — весь отбор: волны и одновременно готовые карточки делят остаток.
type liveCardContextBudget struct {
	mu             sync.Mutex
	entries        map[string]*liveCardContextResult
	slots          chan struct{}
	remaining      time.Duration
	maxDocuments   int
	requestTimeout time.Duration
	fetch          func(context.Context, string) (liveCardContextDocument, error)
}

func liveWithCardContext(ctx context.Context) context.Context {
	if _, ok := ctx.Value(liveCardContextKey{}).(*liveCardContextBudget); ok {
		return ctx
	}
	budget := &liveCardContextBudget{entries: map[string]*liveCardContextResult{}, slots: make(chan struct{}, 2),
		remaining: liveCardContextBudgetTime, maxDocuments: liveCardContextDocuments, requestTimeout: liveCardContextRequest,
		fetch: liveCardHTMLFetcher(liveCardGatewayURL())}
	return context.WithValue(ctx, liveCardContextKey{}, budget)
}

func (b *liveCardContextBudget) get(ctx context.Context, id string) (liveCardContextDocument, error) {
	b.mu.Lock()
	if pending, ok := b.entries[id]; ok {
		b.mu.Unlock()
		select {
		case <-pending.done:
			return pending.document, pending.err
		case <-ctx.Done():
			return liveCardContextDocument{}, fmt.Errorf("wait for source context: %w", ctx.Err())
		}
	}
	if len(b.entries) >= b.maxDocuments {
		b.mu.Unlock()
		return liveCardContextDocument{}, errCardContextBudget
	}
	result := &liveCardContextResult{done: make(chan struct{})}
	b.entries[id] = result
	b.mu.Unlock()
	result.document, result.err = b.read(ctx, id)
	close(result.done)
	return result.document, result.err
}

func (b *liveCardContextBudget) read(ctx context.Context, id string) (liveCardContextDocument, error) {
	select {
	case b.slots <- struct{}{}:
	case <-ctx.Done():
		return liveCardContextDocument{}, fmt.Errorf("queue source context: %w", ctx.Err())
	}
	defer func() { <-b.slots }()
	b.mu.Lock()
	allowance := min(b.remaining, b.requestTimeout)
	if deadline, ok := ctx.Deadline(); ok {
		allowance = min(allowance, max(0, time.Until(deadline)-liveCandidateWaveReserve))
	}
	b.remaining -= allowance
	b.mu.Unlock()
	if allowance <= 0 {
		return liveCardContextDocument{}, errCardContextBudget
	}
	start := time.Now()
	readCtx, cancel := context.WithTimeout(ctx, allowance)
	defer cancel()
	document, err := b.fetch(readCtx, id)
	b.mu.Lock()
	b.remaining += allowance - min(allowance, time.Since(start))
	b.mu.Unlock()
	return document, err
}

func liveCardBudgetFailure(err error) string {
	switch {
	case errors.Is(err, errCardContextBudget):
		return "исчерпан общий бюджет чтения источников"
	case errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
		return "истёк срок или отменён поиск"
	default:
		return ""
	}
}

// Только внутренний шлюз выполняет внешний запрос. Адрес источника не становится адресом клиента main-service.
func liveCardGatewayURL() string {
	if configured := os.Getenv("ARXIV_FULLTEXT_URL"); configured != "" {
		return strings.TrimRight(configured, "/")
	}
	api, err := url.Parse(os.Getenv("ARXIV_API_URL"))
	if err != nil || api.Host == "" || api.Hostname() == "export.arxiv.org" || api.Path != "/api/query" {
		return ""
	}
	api.Path, api.RawQuery, api.Fragment = "", "", ""
	return api.String()
}

func liveCardHTMLFetcher(gateway string) func(context.Context, string) (liveCardContextDocument, error) {
	client := &http.Client{Timeout: liveCardContextRequest,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	return func(ctx context.Context, id string) (liveCardContextDocument, error) {
		if gateway == "" || !liveCardArxivPattern.MatchString(id) {
			return liveCardContextDocument{}, errors.New("arxiv fulltext gateway or identifier unavailable")
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, gateway+"/v1/papers/"+id+"/html", http.NoBody)
		if err != nil {
			return liveCardContextDocument{}, fmt.Errorf("build arxiv context request: %w", err)
		}
		resp, err := client.Do(req)
		if err != nil {
			return liveCardContextDocument{}, fmt.Errorf("read arxiv context: %w", err)
		}
		defer func() { _ = resp.Body.Close() }()
		if resp.StatusCode != http.StatusOK {
			return liveCardContextDocument{}, fmt.Errorf("arxiv context HTTP %d", resp.StatusCode)
		}
		return liveReadCardHTML(ctx, resp, id)
	}
}

func liveReadCardHTML(ctx context.Context, resp *http.Response, requested string) (liveCardContextDocument, error) {
	kind, _, mimeErr := mime.ParseMediaType(resp.Header.Get("Content-Type"))
	id := resp.Header.Get("X-Arxiv-Id")
	base, _, _ := strings.Cut(id, "v")
	wanted, _, versioned := strings.Cut(requested, "v")
	address := "https://arxiv.org/html/" + id
	observed, dateErr := time.Parse(time.RFC3339Nano, resp.Header.Get("X-Source-Observed-At"))
	if mimeErr != nil || kind != "application/octet-stream" || resp.Header.Get("X-Source-Media-Type") != "text/html" ||
		!liveCardArxivPattern.MatchString(id) || !strings.Contains(id, "v") ||
		base != wanted || versioned && requested != id || address != resp.Header.Get("X-Source-Url") || dateErr != nil {
		return liveCardContextDocument{}, errors.New("arxiv context provenance mismatch")
	}
	raw, err := io.ReadAll(io.LimitReader(resp.Body, liveCardContextMaxHTML+1))
	if err != nil {
		return liveCardContextDocument{}, fmt.Errorf("read arxiv context HTML: %w", err)
	}
	if len(raw) > liveCardContextMaxHTML || liveCardTextHash(string(raw)) != resp.Header.Get("X-Source-Sha256") {
		return liveCardContextDocument{}, errors.New("arxiv context size or hash mismatch")
	}
	document, err := liveParseCardHTML(ctx, raw)
	if err != nil {
		return liveCardContextDocument{}, err
	}
	document.arxivID, document.address = id, address
	document.rawHash, document.observed = resp.Header.Get("X-Source-Sha256"), observed
	return document, nil
}
