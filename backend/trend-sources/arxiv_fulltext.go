package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/url"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"golang.org/x/net/html"
)

const (
	arxivHTMLOrigin  = "https://arxiv.org"
	arxivHTMLMaxBody = 8 << 20
	arxivHTMLRequest = 4 * time.Second
	arxivHTMLBudget  = 8 * time.Second
	arxivRobotsPath  = "/robots.txt"
)

var arxivHTMLID = regexp.MustCompile(`^[0-9]{4}\.[0-9]{4,5}(?:v[1-9][0-9]{0,3})?$`)

type arxivFulltextReader struct {
	client   *http.Client
	robots   robotsRules
	robotsAt time.Time
}

func newArxivFulltextReader() arxivFulltextReader {
	client := newWebClient(arxivHTMLRequest, []string{"arxiv.org"})
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	return arxivFulltextReader{client: client}
}

// HTML не может переадресовать чтение в API, PDF, соседний домен или иной порт.
func arxivHTMLAddress(u *url.URL) error {
	if u == nil || u.Scheme != schemeHTTPS || u.Host != "arxiv.org" || u.User != nil || u.RawQuery != "" ||
		u.Fragment != "" || u.RawPath != "" {
		return errors.New("arxiv HTML requires its exact public HTTPS origin")
	}
	if u.Path != arxivRobotsPath && !arxivHTMLID.MatchString(strings.TrimPrefix(u.Path, "/html/")) {
		return errors.New("arxiv HTML path is not a paper identifier")
	}
	return nil
}

// Очередь и пауза общие с Atom: отдельный маршрут не обходит ограничение источника.
func (g *arxivProxy) paperHTML(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if !arxivHTMLID.MatchString(id) || r.URL.RawQuery != "" {
		http.Error(w, "invalid arxiv paper identifier", http.StatusBadRequest)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), arxivHTMLBudget)
	defer cancel()
	r = r.WithContext(ctx)
	// Готовый проверенный текст не должен ждать очередь обращений к источнику.
	if g.serveHTMLCache(w, r, id) {
		return
	}
	select {
	case g.slot <- struct{}{}:
	case <-ctx.Done():
		http.Error(w, "arxiv HTML wait interrupted", http.StatusGatewayTimeout)
		return
	}
	defer func() { <-g.slot }()
	if g.serveHTMLCache(w, r, id) {
		return
	}
	address, _ := url.Parse(arxivHTMLOrigin + "/html/" + id)
	body, resolved := g.loadPaperHTML(w, r, address, id)
	if resolved == "" {
		return
	}
	entry := arxivHTMLCacheEntry{ArxivID: resolved, ObservedAt: g.now().UTC(), SHA256: hashBytes(body)}
	g.saveHTMLCache(id, body, entry)
	serveArxivHTML(w, r, body, entry)
}

// Переадресация тоже расходует паузу и проверяет robots: автоматический переход обошёл бы обе границы.
func (g *arxivProxy) loadPaperHTML(w http.ResponseWriter, r *http.Request, address *url.URL, id string) ([]byte, string) {
	for range 4 {
		if !g.htmlRobots(w, r, address) || !g.awaitHTMLCooldown(w, r) {
			return nil, ""
		}
		body, err := g.getHTML(r.Context(), address.String(), arxivHTMLMaxBody)
		if redirect, ok := errors.AsType[*arxivHTMLRedirectError](err); ok {
			if !arxivHTMLSamePaper(redirect.address.Path, id) {
				http.Error(w, "arxiv HTML redirect changed paper or version", http.StatusBadGateway)
				return nil, ""
			}
			address = redirect.address
			id = strings.TrimPrefix(address.Path, "/html/")
			continue
		}
		if err != nil {
			g.htmlFailure(w, err)
			return nil, ""
		}
		resolved, err := arxivHTMLVersion(body, id)
		if err != nil {
			g.htmlFailure(w, err)
			return nil, ""
		}
		return body, resolved
	}
	http.Error(w, "arxiv HTML redirect limit exceeded", http.StatusBadGateway)
	return nil, ""
}

func arxivHTMLSamePaper(path, requested string) bool {
	id, ok := strings.CutPrefix(path, "/html/")
	if !ok || !arxivHTMLID.MatchString(id) {
		return false
	}
	base, _, versioned := strings.Cut(requested, "v")
	if versioned {
		return id == requested
	}
	nextBase, _, _ := strings.Cut(id, "v")
	return base == nextBase
}

type arxivHTMLRedirectError struct{ address *url.URL }

func (*arxivHTMLRedirectError) Error() string { return "arxiv HTML redirect" }

func (g *arxivProxy) awaitHTMLCooldown(w http.ResponseWriter, r *http.Request) bool {
	ready, err := g.awaitCooldown(w, r)
	if err != nil {
		http.Error(w, "arxiv HTML wait interrupted", http.StatusGatewayTimeout)
	}
	return ready
}

func (g *arxivProxy) htmlRobots(w http.ResponseWriter, r *http.Request, address *url.URL) bool {
	if g.fulltext.robotsAt.IsZero() || g.now().Sub(g.fulltext.robotsAt) > time.Hour {
		if !g.awaitHTMLCooldown(w, r) {
			return false
		}
		body, err := g.getHTML(r.Context(), arxivHTMLOrigin+arxivRobotsPath, 512<<10)
		rules := robotsRules{deny: true}
		if err == nil {
			rules = parseRobots(string(body))
		} else if status, ok := errors.AsType[*sourceHTTPError](err); ok &&
			status.Status >= 400 && status.Status < 500 && status.Status != http.StatusTooManyRequests {
			rules = robotsRules{}
		}
		g.fulltext.robots = rules
		if !rules.deny {
			g.fulltext.robotsAt = g.now()
		}
	}
	if !g.fulltext.robots.allows(address) {
		http.Error(w, "arxiv robots permission unavailable", http.StatusForbidden)
		return false
	}
	return true
}

func (g *arxivProxy) getHTML(ctx context.Context, address string, limit int64) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, address, http.NoBody)
	if err != nil {
		return nil, fmt.Errorf("build arxiv HTML request: %w", err)
	}
	if err = arxivHTMLAddress(req.URL); err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "rag-test-signalgraph/1.0")
	req.Header.Set("Accept", "text/html, application/xhtml+xml")
	resp, err := g.fulltext.client.Do(req)
	g.next = g.now().Add(3 * time.Second)
	if err != nil {
		return nil, fmt.Errorf("fetch arxiv HTML: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if slices.Contains([]int{http.StatusMovedPermanently, http.StatusFound, http.StatusSeeOther,
		http.StatusTemporaryRedirect, http.StatusPermanentRedirect}, resp.StatusCode) && req.URL.Path != arxivRobotsPath {
		destination, locationErr := resp.Location()
		if locationErr != nil || arxivHTMLAddress(destination) != nil {
			return nil, errors.New("arxiv HTML redirect leaves the allowed origin or path")
		}
		return nil, &arxivHTMLRedirectError{address: destination}
	}
	if resp.StatusCode != http.StatusOK {
		if resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode == http.StatusServiceUnavailable {
			g.next = retryTime(resp.Header.Get("Retry-After"), g.now())
			g.persistHTMLCooldown()
		}
		return nil, &sourceHTTPError{Status: resp.StatusCode}
	}
	if req.URL.Path != arxivRobotsPath && !arxivHTMLMIME(resp.Header.Get("Content-Type")) {
		return nil, errors.New("arxiv did not return HTML")
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, limit+1))
	if err != nil {
		return nil, fmt.Errorf("read arxiv HTML: %w", err)
	}
	if int64(len(body)) > limit || len(body) == 0 || !utf8.Valid(body) {
		return nil, errors.New("arxiv HTML is empty, oversized or not UTF-8")
	}
	return body, nil
}

func arxivHTMLMIME(value string) bool {
	kind, params, err := mime.ParseMediaType(value)
	return err == nil && (kind == "text/html" || kind == "application/xhtml+xml") &&
		(params["charset"] == "" || strings.EqualFold(params["charset"], "utf-8"))
}

func (g *arxivProxy) htmlFailure(w http.ResponseWriter, err error) {
	if status, ok := errors.AsType[*sourceHTTPError](err); ok &&
		(status.Status == http.StatusTooManyRequests || status.Status == http.StatusServiceUnavailable) {
		w.Header().Set("Retry-After", strconv.Itoa(max(1, int(g.next.Sub(g.now()).Seconds())+1)))
		http.Error(w, "arxiv shared cooldown", http.StatusTooManyRequests)
		return
	}
	http.Error(w, "arxiv HTML unavailable", http.StatusBadGateway)
}

// Версия берётся из служебной ссылки страницы, не из цитируемой в статье другой работы.
func arxivHTMLVersion(body []byte, requested string) (string, error) {
	decoder := html.NewTokenizer(bytes.NewReader(body))
	resolved := ""
	for {
		switch decoder.Next() {
		case html.ErrorToken:
			if !errors.Is(decoder.Err(), io.EOF) {
				return "", fmt.Errorf("parse arxiv identity: %w", decoder.Err())
			}
			if resolved == "" {
				return "", errors.New("arxiv HTML has no exact version")
			}
			return resolved, nil
		case html.StartTagToken:
			token := decoder.Token()
			if token.Data != "a" || !slices.Contains(strings.Fields(arxivTokenAttr(token, "class")), "header-button") {
				continue
			}
			id, ok := strings.CutPrefix(arxivTokenAttr(token, "href"), "/abs/")
			if !ok {
				continue
			}
			if !arxivHTMLBoundVersion(id, requested, resolved) {
				return "", errors.New("arxiv HTML changed paper or version")
			}
			resolved = id
		case html.TextToken, html.EndTagToken, html.SelfClosingTagToken, html.CommentToken, html.DoctypeToken:
			continue
		}
	}
}

func arxivHTMLBoundVersion(id, requested, resolved string) bool {
	base, _, versioned := strings.Cut(id, "v")
	requestBase, _, requestVersioned := strings.Cut(requested, "v")
	return arxivHTMLID.MatchString(id) && versioned && base == requestBase &&
		(!requestVersioned || id == requested) && (resolved == "" || resolved == id)
}

func arxivTokenAttr(token html.Token, name string) string {
	for _, attribute := range token.Attr {
		if attribute.Key == name {
			return attribute.Val
		}
	}
	return ""
}
