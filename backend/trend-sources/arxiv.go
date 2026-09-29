package main

import (
	"bytes"
	"context"
	"encoding/json/v2"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
)

const arxivCacheFreshness = 24 * time.Hour

type arxivProxy struct {
	client        *http.Client
	upstream, dir string
	fulltext      arxivFulltextReader
	slot          chan struct{}
	next          time.Time
	now           func() time.Time
}
type arxivCacheEntry struct {
	FetchedAt   time.Time `json:"fetched_at"`
	SHA256      string    `json:"sha256"`
	ContentType string    `json:"content_type"`
}

func newArxivProxy(dir string) *arxivProxy {
	g := &arxivProxy{client: &http.Client{Timeout: 90 * time.Second}, upstream: "https://export.arxiv.org/api/query",
		dir: dir, slot: make(chan struct{}, 1), now: time.Now, fulltext: newArxivFulltextReader()}
	if raw, e := os.ReadFile(filepath.Join(dir, "cooldown.json")); e == nil {
		_ = json.Unmarshal(raw, &g.next)
	}
	return g
}

// arxivGateway выстраивает всех клиентов в одну очередь и хранит паузу, назначенную arXiv.
// Адрес arXiv зашит: клиенты передают только параметры запроса, не URL.
func arxivGateway(ctx context.Context) error {
	dir := os.Getenv("DATA_DIR")
	if dir == "" {
		dir = "/data"
	}
	handler, err := newArxivGatewayHandler(filepath.Join(dir, "arxiv"))
	if err != nil {
		return err
	}
	srv := &http.Server{Addr: ":8090", Handler: handler, ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 10 * time.Second,
		WriteTimeout: 120 * time.Second, IdleTimeout: time.Minute}
	go func() {
		<-ctx.Done()
		shutdown, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutdown)
	}()
	if err = srv.ListenAndServe(); err != nil {
		return fmt.Errorf("arxiv gateway listen: %w", err)
	}
	return nil
}

func newArxivGatewayHandler(dir string) (http.Handler, error) {
	if err := os.MkdirAll(dir, 0750); err != nil {
		return nil, fmt.Errorf("arxiv cache directory: %w", err)
	}
	gateway := newArxivProxy(dir)
	mux := http.NewServeMux()
	// Шлюз обслуживает запросы по требованию: для готовности ему не нужен снимок коллектора.
	ready := func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) }
	mux.HandleFunc("GET /healthz", ready)
	mux.HandleFunc("GET /readyz", ready)
	mux.HandleFunc("GET /api/query", gateway.query)
	mux.HandleFunc("GET /v1/papers/{id}/html", gateway.paperHTML)
	return mux, nil
}

// serveCache отдаёт сохранённую страницу только закрытого окна и сообщает, отдал ли.
func (g *arxivProxy) serveCache(w http.ResponseWriter, key string) bool {
	body, entry, ok := g.readCache(key)
	if !ok {
		return false
	}
	w.Header().Set("Content-Type", entry.ContentType)
	w.Header().Set("X-Radar-Cache", "fresh")
	w.Header().Set("X-Source-Observed-At", entry.FetchedAt.UTC().Format(time.RFC3339))
	_, _ = w.Write(body)
	return true
}

// awaitCooldown держит клиента на общий интервал arXiv; на долгую паузу отвечает Retry-After.
func (g *arxivProxy) awaitCooldown(w http.ResponseWriter, r *http.Request) (bool, error) {
	wait := g.next.Sub(g.now())
	if wait > 3*time.Second {
		w.Header().Set("Retry-After", strconv.Itoa(int(wait.Seconds())+1))
		http.Error(w, "arxiv shared cooldown", http.StatusTooManyRequests)
		return false, nil
	}
	if wait <= 0 {
		return true, nil
	}
	select {
	case <-r.Context().Done():
		return false, fmt.Errorf("wait for arxiv cooldown: %w", r.Context().Err())
	case <-time.After(wait):
		return true, nil
	}
}

func (g *arxivProxy) query(w http.ResponseWriter, r *http.Request) {
	if len(r.URL.RawQuery) > 8192 {
		http.Error(w, "query too large", http.StatusBadRequest)
		return
	}
	select {
	case g.slot <- struct{}{}:
	case <-r.Context().Done():
		return
	}
	defer func() { <-g.slot }()
	key := hashBytes([]byte(r.URL.Query().Encode()))
	cacheable := closedArxivWindow(r.URL.Query(), g.now())
	if cacheable && g.serveCache(w, key) {
		return
	}
	if ready, _ := g.awaitCooldown(w, r); !ready {
		return
	}
	req, err := http.NewRequestWithContext(r.Context(), http.MethodGet, g.upstream+"?"+r.URL.RawQuery, http.NoBody)
	if err != nil {
		http.Error(w, "invalid query", http.StatusBadRequest)
		return
	}
	req.Header.Set("User-Agent", "rag-test-signalgraph/1.0")
	resp, err := g.client.Do(req)
	g.next = g.now().Add(3 * time.Second)
	if err != nil {
		http.Error(w, "arxiv unavailable", http.StatusBadGateway)
		return
	}
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(io.LimitReader(resp.Body, (64<<20)+1))
	g.next = g.now().Add(3 * time.Second)
	if err != nil || len(body) > 64<<20 {
		http.Error(w, "invalid arxiv response", http.StatusBadGateway)
		return
	}
	if resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode == http.StatusServiceUnavailable {
		g.next = retryTime(resp.Header.Get("Retry-After"), g.now())
		raw, _ := json.Marshal(g.next)
		if err := atomicSourceFile(g.dir, "cooldown.json", raw); err != nil {
			slog.Error("persist arxiv cooldown", "error", err)
		}
		w.Header().Set("Retry-After", strconv.Itoa(int(g.next.Sub(g.now()).Seconds())+1))
	} else if retry := resp.Header.Get("Retry-After"); retry != "" {
		w.Header().Set("Retry-After", retry)
	}
	if cacheable && resp.StatusCode == http.StatusOK && validArxivCachePage(body, r.URL.Query()) {
		if err := g.saveCache(key, body, resp.Header.Get("Content-Type")); err != nil {
			slog.Warn("save arxiv page cache", "error", err)
		}
	}
	w.Header().Set("Content-Type", resp.Header.Get("Content-Type"))
	w.WriteHeader(resp.StatusCode)
	_, _ = w.Write(body)
}
func retryTime(value string, now time.Time) time.Time {
	if seconds, e := strconv.Atoi(value); e == nil && seconds > 0 {
		return now.Add(time.Duration(seconds) * time.Second)
	}
	if at, e := http.ParseTime(value); e == nil && at.After(now) {
		return at
	}
	return now.Add(time.Minute)
}

var arxivDateWindow = regexp.MustCompile(`submittedDate:\[([0-9]{12}) TO ([0-9]{12})\]`)

func closedArxivWindow(q url.Values, now time.Time) bool {
	dates := arxivDateWindow.FindAllStringSubmatch(q.Get("search_query"), -1)
	if len(dates) != 1 {
		return false
	}
	from, e := time.Parse("200601021504", dates[0][1])
	through, f := time.Parse("200601021504", dates[0][2])
	month := time.Date(now.UTC().Year(), now.UTC().Month(), 1, 0, 0, 0, 0, time.UTC)
	return e == nil && f == nil && !through.Before(from) && through.Before(month)
}
func validArxivCachePage(body []byte, q url.Values) bool {
	decoder := xml.NewDecoder(bytes.NewReader(body))
	for {
		token, e := decoder.Token()
		if errors.Is(e, io.EOF) {
			break
		}
		if e != nil {
			return false
		}
		if _, ok := token.(xml.Directive); ok {
			return false
		}
	}
	var feed struct {
		XMLName xml.Name `xml:"http://www.w3.org/2005/Atom feed"`
		Total   *int     `xml:"http://a9.com/-/spec/opensearch/1.1/ totalResults"`
		Start   *int     `xml:"http://a9.com/-/spec/opensearch/1.1/ startIndex"`
		Entries []struct {
			ID string `xml:"id"`
		} `xml:"entry"`
	}
	start, e := strconv.Atoi(q.Get("start"))
	size, f := strconv.Atoi(q.Get("max_results"))
	if e != nil || f != nil || start < 0 || size < 1 || xml.Unmarshal(body, &feed) != nil || feed.Total == nil ||
		feed.Start == nil || *feed.Start != start || *feed.Total < 0 || len(feed.Entries) > size {
		return false
	}
	for _, entry := range feed.Entries {
		if entry.ID == "" || strings.Contains(entry.ID, "/api/errors") {
			return false
		}
	}
	return start+len(feed.Entries) >= *feed.Total || len(feed.Entries) == size
}
func (g *arxivProxy) readCache(key string) ([]byte, arxivCacheEntry, bool) {
	var entry arxivCacheEntry
	raw, e := os.ReadFile(filepath.Join(g.dir, key+".json"))
	if e != nil || len(raw) > 4096 || json.Unmarshal(raw, &entry) != nil || g.now().Before(entry.FetchedAt) ||
		g.now().Sub(entry.FetchedAt) > arxivCacheFreshness || len(entry.SHA256) != 64 {
		return nil, entry, false
	}
	body, e := os.ReadFile(filepath.Join(g.dir, entry.SHA256+".atom"))
	if e != nil || hashBytes(body) != entry.SHA256 {
		return nil, entry, false
	}
	return body, entry, true
}
func (g *arxivProxy) saveCache(key string, body []byte, contentType string) error {
	entry := arxivCacheEntry{FetchedAt: g.now(), SHA256: hashBytes(body), ContentType: contentType}
	if err := atomicSourceFile(g.dir, entry.SHA256+".atom", body); err != nil {
		return err
	}
	raw, err := json.Marshal(entry)
	if err != nil {
		return fmt.Errorf("encode arxiv cache entry: %w", err)
	}
	return atomicSourceFile(g.dir, key+".json", raw)
}
func atomicSourceFile(dir, name string, raw []byte) error {
	f, err := os.CreateTemp(dir, ".source-")
	if err != nil {
		return fmt.Errorf("create temporary source file: %w", err)
	}
	defer func() { _ = os.Remove(f.Name()) }()
	_, writeErr := f.Write(raw)
	closeErr := f.Close()
	if err = errors.Join(writeErr, closeErr); err != nil {
		return fmt.Errorf("write temporary source file: %w", err)
	}
	if err = os.Rename(f.Name(), filepath.Join(dir, name)); err != nil {
		return fmt.Errorf("publish source file: %w", err)
	}
	return nil
}
