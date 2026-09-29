package provider

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"slices"
	"strconv"
	"sync"
	"syscall"
	"time"
)

const maxResponseBytes = 8 << 20

// ErrResponseTooLarge позволяет оставить метаданные, когда необязательный полный текст больше предела загрузки.
var ErrResponseTooLarge = errors.New("source response exceeds 8 MiB")

var errTemporaryTransport = errors.New("temporary source transport failure")

// HTTPError не пускает учётные данные и тела ответов в логи; RetryAt планировщик сохраняет, а не спит всю паузу.
type HTTPError struct {
	Status  int
	RetryAt time.Time
}

// RequestError отделяет сбой запроса от битой записи: сбой обогащения останавливает страницу до сдвига курсора.
type RequestError struct{ Err error }

func (e *RequestError) Error() string { return e.Err.Error() }
func (e *RequestError) Unwrap() error { return e.Err }

func (e *HTTPError) Error() string {
	return fmt.Sprintf("source HTTP status %d; retry after %s", e.Status, e.RetryAt.UTC().Format(time.RFC3339))
}

type fetcher struct {
	http      *http.Client
	mu        sync.Mutex
	next      time.Time
	interval  time.Duration
	now       func() time.Time
	wait      func(context.Context, time.Duration) error
	userAgent string
	accept    string
	token     string
	cache     map[string]cachedResponse
	gate      Gate
}

type cachedResponse struct {
	etag string
	body []byte
}

func newFetcher(interval time.Duration) *fetcher {
	return &fetcher{
		http: &http.Client{Timeout: 30 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error {
			return errors.New("source redirects are not followed")
		}},
		interval: interval, now: time.Now, wait: waitContext, userAgent: "rag-test-source-harvester/1.0", accept: "application/json",
		cache: make(map[string]cachedResponse),
	}
}

func waitContext(ctx context.Context, delay time.Duration) error {
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return fmt.Errorf("wait for provider: %w", ctx.Err())
	case <-timer.C:
		return nil
	}
}

// get выстраивает запросы, повторы и чтение ответов всех областей провайдера в одну очередь: адаптер один на провайдер.
func (f *fetcher) get(ctx context.Context, endpoint string) ([]byte, time.Time, error) {
	raw, at, err := f.request(ctx, endpoint)
	if err != nil {
		return nil, at, &RequestError{Err: err}
	}
	return raw, at, nil
}

func (f *fetcher) request(ctx context.Context, endpoint string) ([]byte, time.Time, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for attempt := range 3 {
		delay := max(time.Duration(0), f.next.Sub(f.now()))
		if delay > 30*time.Second {
			return nil, f.now().UTC(), &HTTPError{Status: http.StatusTooManyRequests, RetryAt: f.next}
		}
		if err := f.wait(ctx, delay); err != nil {
			return nil, time.Time{}, err
		}
		if err := f.gateBefore(ctx); err != nil {
			return nil, time.Time{}, err
		}
		raw, observed, err := f.once(ctx, endpoint)
		if gateErr := f.gateAfter(ctx); gateErr != nil {
			return nil, observed, gateErr
		}
		if err == nil {
			return raw, observed, nil
		}
		retryAt, retryable := f.retryAt(err)
		if !retryable || attempt == 2 || retryAt.Sub(f.now()) > 30*time.Second {
			return nil, observed, err
		}
		f.next = maxTime(retryAt, f.now().Add(time.Duration(1<<attempt)*time.Second))
	}
	return nil, time.Time{}, errors.New("source retry budget exhausted")
}

func (f *fetcher) gateBefore(ctx context.Context) error {
	if f.gate == nil {
		return nil
	}
	return f.gate.Before(ctx)
}

func (f *fetcher) gateAfter(ctx context.Context) error {
	if f.gate == nil {
		return nil
	}
	return f.gate.After(ctx, f.next)
}

func (f *fetcher) retryAt(err error) (time.Time, bool) {
	httpErr, ok := errors.AsType[*HTTPError](err)
	if !ok {
		return f.next, errors.Is(err, errTemporaryTransport)
	}
	retryCodes := []int{http.StatusTooManyRequests, http.StatusServiceUnavailable, http.StatusNotModified,
		http.StatusInternalServerError, http.StatusBadGateway, http.StatusGatewayTimeout}
	if !slices.Contains(retryCodes, httpErr.Status) {
		return time.Time{}, false
	}
	return maxTime(f.next, httpErr.RetryAt), true
}

func (f *fetcher) once(ctx context.Context, endpoint string) ([]byte, time.Time, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, http.NoBody)
	if err != nil {
		return nil, time.Time{}, errors.New("invalid source request URL")
	}
	req.Header.Set("User-Agent", f.userAgent)
	req.Header.Set("Accept", f.accept)
	if f.token != "" {
		req.Header.Set("Authorization", "Bearer "+f.token)
	}
	if cached, ok := f.cache[endpoint]; ok {
		req.Header.Set("If-None-Match", cached.etag)
	}
	f.next = f.now().Add(f.interval)
	resp, err := f.http.Do(req)
	if err != nil {
		if ctx.Err() == nil && temporaryTransport(err) {
			return nil, f.now().UTC(), errTemporaryTransport
		}
		// Ошибки net/url содержат URL, а в нём бывают учётные данные провайдера.
		return nil, f.now().UTC(), errors.New("source request failed or timed out")
	}
	defer func() { _ = resp.Body.Close() }()
	observed := f.now().UTC()
	f.applyLimits(resp.Header, observed)
	if resp.StatusCode == http.StatusNotModified {
		if cached, ok := f.cache[endpoint]; ok {
			return bytes.Clone(cached.body), observed, nil
		}
		// Локальной копии нет: вызывающий может повторить безусловный запрос в рамках бюджета; повторный 304 — не пустые данные.
		return nil, observed, &HTTPError{Status: http.StatusNotModified, RetryAt: f.next}
	}
	if resp.StatusCode == http.StatusForbidden {
		f.next = maxTime(f.next, observed.Add(time.Minute))
	}
	if resp.StatusCode != http.StatusOK {
		return nil, observed, &HTTPError{Status: resp.StatusCode, RetryAt: f.next}
	}
	raw, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes+1))
	if err != nil {
		if ctx.Err() == nil && temporaryTransport(err) {
			return nil, observed, errTemporaryTransport
		}
		return nil, observed, errors.New("source response interrupted")
	}
	if len(raw) > maxResponseBytes {
		return nil, observed, ErrResponseTooLarge
	}
	if etag := resp.Header.Get("ETag"); etag != "" {
		if len(f.cache) >= 4 {
			clear(f.cache)
		} // не больше четырёх ограниченных тел (32 МиБ)
		f.cache[endpoint] = cachedResponse{etag: etag, body: bytes.Clone(raw)}
	}
	return raw, observed, nil
}

func temporaryTransport(err error) bool {
	if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) ||
		errors.Is(err, syscall.ECONNRESET) || errors.Is(err, syscall.EPIPE) {
		return true
	}
	network, ok := errors.AsType[net.Error](err)
	return ok && network.Timeout()
}

func (f *fetcher) applyLimits(h http.Header, observed time.Time) {
	if interval, err := time.ParseDuration(h.Get("X-Rate-Limit-Interval")); err == nil && interval > 0 {
		if count, e := strconv.Atoi(h.Get("X-Rate-Limit-Limit")); e == nil && count > 0 {
			f.interval = max(f.interval, interval/time.Duration(count))
			f.next = maxTime(f.next, observed.Add(f.interval))
		}
	}
	if h.Get("X-Ratelimit-Remaining") == "0" {
		if seconds, err := strconv.ParseInt(h.Get("X-Ratelimit-Reset"), 10, 64); err == nil {
			f.next = maxTime(f.next, time.Unix(seconds, 0))
		}
	}
	if seconds, err := strconv.Atoi(h.Get("Retry-After")); err == nil && seconds >= 0 {
		f.next = maxTime(f.next, observed.Add(time.Duration(seconds)*time.Second))
	} else if at, e := http.ParseTime(h.Get("Retry-After")); e == nil {
		f.next = maxTime(f.next, at)
	}
}

func maxTime(a, b time.Time) time.Time {
	if b.After(a) {
		return b
	}
	return a
}
