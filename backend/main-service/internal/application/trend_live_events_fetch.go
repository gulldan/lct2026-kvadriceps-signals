package application

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	// Сеть стенда через раз замирает на 16–20 КБ: без сторожа куска запрос висел бы до общего срока поиска.
	liveMarketStall    = 3 * time.Second
	liveMarketAttempts = 8
	// Длиннее — уже таблица или склеенный без точек раздел отчёта: показывать его как основание нельзя.
	liveSentenceMax   = 700
	liveMarketMaxJSON = 8 << 20
)

type liveMarketState struct {
	sbir   liveGrantIndex
	cordis liveGrantIndex
	edgar  livePacer
}

// livePacer общий для всех поисков процесса: правило источника считает запросы с адреса, а не с поиска.
type livePacer struct {
	mu   sync.Mutex
	next time.Time
}

func (p *livePacer) wait(ctx context.Context, every time.Duration) error {
	p.mu.Lock()
	at := time.Now()
	if p.next.After(at) {
		at = p.next
	}
	p.next = at.Add(every)
	p.mu.Unlock()
	return liveSleep(ctx, time.Until(at))
}

type liveMarketStatusError struct{ status int }

func (e *liveMarketStatusError) Error() string { return "HTTP " + strconv.Itoa(e.status) }

// liveMarketRead продолжает с места обрыва и считает попытками только пустые чтения: иначе сотни мегабайт не дошли бы.
func liveMarketRead(ctx context.Context, client *http.Client, request func(context.Context) (*http.Request, error),
	out io.Writer, restart func() error, limit int64) error {
	if client == nil || client.Transport == nil {
		client = &http.Client{Transport: liveMarketTransport()}
	}
	var offset int64
	var last error
	for idle := 0; idle < liveMarketAttempts; {
		reached, err := liveMarketReadOnce(ctx, client, request, offset, out, restart, limit)
		var status *liveMarketStatusError
		switch {
		case err == nil:
			return nil
		case ctx.Err() != nil:
			return fmt.Errorf("events: %w", ctx.Err())
		case errors.As(err, &status) && status.status != http.StatusTooManyRequests && status.status < 500:
			return err
		case reached <= offset:
			idle++
		default:
			idle = 0
		}
		offset, last = reached, err
	}
	return fmt.Errorf("events: %d attempts without progress: %w", liveMarketAttempts, last)
}

// liveMarketTransport: в HTTP/2 повторы после замирания сети стенда висели бы в том же соединении.
func liveMarketTransport() *http.Transport {
	protocols := new(http.Protocols)
	protocols.SetHTTP1(true)
	return &http.Transport{Proxy: http.ProxyFromEnvironment, Protocols: protocols, DisableKeepAlives: true,
		TLSHandshakeTimeout: 10 * time.Second}
}

func liveMarketReadOnce(ctx context.Context, client *http.Client, request func(context.Context) (*http.Request, error),
	offset int64, out io.Writer, restart func() error, limit int64) (int64, error) {
	attempt, cancel := context.WithCancel(ctx)
	defer cancel()
	watchdog := time.AfterFunc(liveMarketStall, cancel)
	defer watchdog.Stop()
	req, err := request(attempt)
	if err != nil {
		return offset, err
	}
	if offset > 0 {
		req.Header.Set("Range", "bytes="+strconv.FormatInt(offset, 10)+"-")
	}
	resp, err := client.Do(req)
	if err != nil {
		return offset, fmt.Errorf("events: get: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	switch {
	case resp.StatusCode == http.StatusPartialContent && offset > 0:
	case resp.StatusCode == http.StatusOK:
		if offset > 0 {
			if err := restart(); err != nil {
				return 0, err
			}
			offset = 0
		}
	default:
		return offset, &liveMarketStatusError{status: resp.StatusCode}
	}
	buf := make([]byte, 64<<10)
	for offset < limit {
		n, err := resp.Body.Read(buf[:min(int64(len(buf)), limit-offset)])
		if n > 0 {
			if _, werr := out.Write(buf[:n]); werr != nil {
				return offset, fmt.Errorf("events: write: %w", werr)
			}
			offset += int64(n)
			watchdog.Reset(liveMarketStall)
		}
		if errors.Is(err, io.EOF) {
			return offset, nil
		}
		if err != nil {
			return offset, fmt.Errorf("events: read: %w", err)
		}
	}
	return offset, nil
}

func liveMarketFetch(ctx context.Context, client *http.Client, method, address string, header http.Header,
	body []byte, limit int64) ([]byte, error) {
	var buf bytes.Buffer
	request := func(ctx context.Context) (*http.Request, error) {
		req, err := http.NewRequestWithContext(ctx, method, address, bytes.NewReader(body))
		if err != nil {
			return nil, fmt.Errorf("events: build request: %w", err)
		}
		req.Header = header.Clone()
		if req.Header == nil {
			req.Header = http.Header{}
		}
		if req.Header.Get("User-Agent") == "" {
			req.Header.Set("User-Agent", liveEventAgent)
		}
		return req, nil
	}
	restart := func() error { buf.Reset(); return nil }
	if err := liveMarketRead(ctx, client, request, &buf, restart, limit); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func liveMarketRecord(id, title, date, address, source, dateBasis string) LiveSourceRecord {
	records := []LiveSourceRecord{{
		ID: id, Title: strings.Join(strings.Fields(title), " "), Date: date, URL: address, Source: source,
		Language: "en", LanguageBasis: "источник публикует записи на английском",
		PublishedAt: date, ObservedAt: time.Now().UTC().Format(time.RFC3339), DateBasis: dateBasis,
	}}
	stampPassports(records)
	return records[0]
}

// liveTermSentence требует слова термина подряд: набора слов мало («solid state lighting, battery anodes»).
func liveTermSentence(text, term string) string {
	tokens := liveWords(term)
	stems := liveStems(liveWords(term))
	fallback := ""
	for _, sentence := range liveSourceExcerpts(text) {
		sentence = strings.Join(strings.Fields(sentence), " ")
		if len(sentence) > liveSentenceMax || len(tokens) == 0 {
			continue
		}
		words := liveWords(sentence)
		if livePhraseIn(words, tokens) {
			return sentence
		}
		if fallback == "" && livePhraseIn(liveStems(words), stems) {
			fallback = sentence
		}
	}
	return fallback
}
