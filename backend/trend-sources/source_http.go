package main

import (
	"bytes"
	"context"
	"encoding/json/v2"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"time"
	"unicode/utf8"

	"golang.org/x/net/html/charset"
)

func (s *service) fetch(ctx context.Context, address string, data *snapshot) ([]byte, error) {
	u, err := url.Parse(address)
	if err != nil {
		return nil, fmt.Errorf("parse source URL: %w", err)
	}
	if s.kind == kindWeb {
		if err := allowedAddress(u, s.config.domains()); err != nil {
			return nil, err
		}
	}
	cooldownFile := filepath.Join(s.dir, "source-cooldown-"+hashBytes([]byte(u.Host))+".json")
	if err := sourceCooldown(cooldownFile); err != nil {
		return nil, err
	}
	for attempt := range 3 {
		raw, err := s.fetchOnce(ctx, address, data)
		failure, ok := errors.AsType[*sourceHTTPError](err)
		if !ok {
			return raw, err
		}
		if !failure.RetryAt.IsZero() {
			if saveErr := atomicSourceJSON(cooldownFile, failure.RetryAt); saveErr != nil {
				return nil, errors.Join(err, saveErr)
			}
			return nil, err
		}
		if attempt == 2 || (failure.Status != http.StatusInternalServerError && failure.Status != http.StatusBadGateway &&
			failure.Status != http.StatusGatewayTimeout) {
			return nil, err
		}
	}
	return nil, errors.New("source retry budget exhausted")
}

func sourceCooldown(filename string) error {
	raw, err := os.ReadFile(filename)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("read source cooldown: %w", err)
	}
	var until time.Time
	if json.Unmarshal(raw, &until) != nil {
		return errors.New("invalid source cooldown checkpoint")
	}
	if time.Now().Before(until) {
		return &sourceHTTPError{Status: http.StatusTooManyRequests, RetryAt: until}
	}
	return nil
}

func (s *service) fetchOnce(ctx context.Context, address string, data *snapshot) ([]byte, error) {
	if s.config.RequestBudget > 0 && s.requests >= s.config.RequestBudget {
		return nil, errBudget
	}
	if isEPAddress(address) {
		if err := s.epFairUseCheck(); err != nil {
			return nil, err
		}
	}
	s.requests++
	// Одно соединение и не меньше секунды между запросами на сервис.
	select {
	case <-ctx.Done():
		return nil, fmt.Errorf("source fetch cancelled: %w", ctx.Err())
	case <-time.After(time.Second):
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, address, http.NoBody)
	if err != nil {
		return nil, fmt.Errorf("build source request: %w", err)
	}
	req.Header.Set("User-Agent", "rag-test-signalgraph/1.0 (+public research metadata)")
	resp, err := s.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("source request failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		if err = s.epCount(address, len(raw)); err != nil {
			return nil, err
		}
		return nil, sourceFailure(resp, raw)
	}
	b, err := io.ReadAll(io.LimitReader(resp.Body, (8<<20)+1))
	// Полученные байты идут в учёт квоты, даже если чтение, проверка размера или сохранение не удались.
	if ledgerErr := s.epCount(address, len(b)); ledgerErr != nil {
		return nil, ledgerErr
	}
	var cutOff error
	if err != nil {
		if b, cutOff = cutOffFeed(ctx, b, err); b == nil {
			return nil, cutOff
		}
	}
	if len(b) > 8<<20 {
		return nil, errors.New("source page exceeds 8 MiB")
	}
	b = sourceUTF8(b, address, resp.Header.Get("Content-Type"), data)
	if err = s.storeRaw(b, address, "", data); err != nil {
		return nil, err
	}
	return b, cutOff
}

// errFeedCutOff: у части изданий сеть стенда замирает на первых 16–20 КБ ответа; дошедшие записи разобраны.
var errFeedCutOff = errors.New("feed cut off by the network")

func (s *service) epCount(address string, received int) error {
	if !isEPAddress(address) {
		return nil
	}
	return s.epFairUseAdd(received)
}

// cutOffFeed не спасает отменённый сбор: его останавливают намеренно.
func cutOffFeed(ctx context.Context, b []byte, readErr error) ([]byte, error) {
	prefix := feedPrefix(b)
	if prefix == nil || ctx.Err() != nil {
		return nil, fmt.Errorf("read source page: %w", readErr)
	}
	return prefix, fmt.Errorf("%w after %d bytes: %w", errFeedCutOff, len(b), readErr)
}

func feedPrefix(b []byte) []byte {
	for _, end := range [...]struct{ item, document string }{{"</item>", "</channel></rss>"}, {"</entry>", "</feed>"}} {
		if at := bytes.LastIndex(b, []byte(end.item)); at >= 0 {
			at += len(end.item)
			return append(b[:at:at], end.document...)
		}
	}
	return nil
}

// sourceUTF8 чинит кодировку до хеша провенанса: encoding/json/v2 строг к UTF-8, и чужой байт ронял весь снимок.
func sourceUTF8(raw []byte, address, contentType string, data *snapshot) []byte {
	if utf8.Valid(raw) {
		return raw
	}
	// Объявленную кодировку (windows-1251) перекодируем; windows-1252 — лишь догадка библиотеки при молчании страницы.
	enc, name, declared := charset.DetermineEncoding(raw, contentType)
	// Заголовок ответа бывает испорчен (iemag.ru — charset без «;»), поэтому смотрим и пролог XML.
	if match := xmlEncodingDecl.FindSubmatch(raw); match != nil && !declared {
		if byProlog, label := charset.Lookup(string(match[2])); byProlog != nil {
			enc, name, declared = byProlog, label, true
		}
	}
	if name != "utf-8" && (declared || name != "windows-1252") {
		if decoded, err := enc.NewDecoder().Bytes(raw); err == nil && utf8.Valid(decoded) {
			slog.Info("source transcoded from its declared charset", "url", address, "charset", name)
			data.Limits = appendLimitation(data.Limits,
				"A page that declares a legacy charset is transcoded to UTF-8 before parsing; the retained page is the "+
					"transcoded one.")
			return xmlUTF8Prolog(decoded)
		}
	}
	slog.Warn("source answered with bytes outside UTF-8", "url", address)
	data.Limits = appendLimitation(data.Limits,
		"Bytes outside UTF-8 are replaced by the Unicode replacement character before parsing: such a page loses "+
			"single characters, never the whole record.")
	return xmlUTF8Prolog(bytes.ToValidUTF8(raw, []byte("�")))
}

var xmlEncodingDecl = regexp.MustCompile(`^(\s*<\?xml[^>]*?encoding\s*=\s*["'])([^"']+)(["'])`)

// xmlUTF8Prolog: разбор ленты сам читает пролог и иначе перекодировал бы байты второй раз.
func xmlUTF8Prolog(body []byte) []byte {
	return xmlEncodingDecl.ReplaceAll(body, []byte("${1}UTF-8${3}"))
}

func sourceFailure(resp *http.Response, body []byte) error {
	var detail struct {
		Code string `json:"code"`
	}
	_ = json.Unmarshal(body, &detail)
	failure := &sourceHTTPError{Status: resp.StatusCode, Code: detail.Code}
	if resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode == http.StatusServiceUnavailable ||
		resp.Header.Get("Retry-After") != "" {
		failure.RetryAt = retryTime(resp.Header.Get("Retry-After"), time.Now().UTC())
	}
	return failure
}

// storeRaw хранит байты после sourceUTF8: сохранённая страница — та, из которой прочитана запись.
func (s *service) storeRaw(b []byte, address, method string, data *snapshot) error {
	digest := hashBytes(b)
	if err := os.MkdirAll(filepath.Join(s.dir, "raw"), 0750); err != nil {
		return fmt.Errorf("raw page directory: %w", err)
	}
	if err := os.WriteFile(filepath.Join(s.dir, "raw", digest), b, 0600); err != nil {
		return fmt.Errorf("store raw page: %w", err)
	}
	data.Pages = append(data.Pages, provenance{URL: address, SHA256: digest, Method: method})
	return nil
}

type sourceHTTPError struct {
	Status  int
	Code    string
	RetryAt time.Time
}

func (e *sourceHTTPError) Error() string {
	if !e.RetryAt.IsZero() {
		return fmt.Sprintf("source returned HTTP %d; retry after %s", e.Status, e.RetryAt.Format(time.RFC3339))
	}
	return fmt.Sprintf("source returned HTTP %d (%s)", e.Status, e.Code)
}
