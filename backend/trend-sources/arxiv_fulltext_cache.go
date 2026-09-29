package main

import (
	"bytes"
	"encoding/json/v2"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type arxivHTMLCacheEntry struct {
	ArxivID    string    `json:"arxiv_id"`
	ObservedAt time.Time `json:"observed_at"`
	SHA256     string    `json:"sha256"`
}

func serveArxivHTML(w http.ResponseWriter, r *http.Request, body []byte, entry arxivHTMLCacheEntry) {
	// Сырьё передаётся как данные: браузер не должен исполнять содержимое источника.
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Disposition", `attachment; filename="source.html"`)
	w.Header().Set("X-Source-Media-Type", "text/html")
	w.Header().Set("Content-Security-Policy", "sandbox; default-src 'none'")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("X-Source-Url", arxivHTMLOrigin+"/html/"+entry.ArxivID)
	w.Header().Set("X-Arxiv-Id", entry.ArxivID)
	w.Header().Set("X-Source-Observed-At", entry.ObservedAt.Format(time.RFC3339Nano))
	w.Header().Set("X-Source-Sha256", entry.SHA256)
	http.ServeContent(w, r, "source.html", entry.ObservedAt, bytes.NewReader(body))
}

func (g *arxivProxy) serveHTMLCache(w http.ResponseWriter, r *http.Request, id string) bool {
	key := hashBytes([]byte("html:" + id))
	raw, err := os.ReadFile(filepath.Join(g.dir, key+".html.json"))
	var entry arxivHTMLCacheEntry
	if err != nil || len(raw) > 4096 || json.Unmarshal(raw, &entry) != nil || len(entry.SHA256) != 64 {
		return false
	}
	ttl := arxivCacheFreshness
	if !strings.Contains(id, "v") {
		ttl = time.Hour
	}
	if g.now().Before(entry.ObservedAt) || g.now().Sub(entry.ObservedAt) > ttl {
		return false
	}
	file, err := os.Open(filepath.Join(g.dir, key+".html"))
	if err != nil {
		return false
	}
	defer func() { _ = file.Close() }()
	body, err := io.ReadAll(io.LimitReader(file, arxivHTMLMaxBody+1))
	if err != nil || len(body) > arxivHTMLMaxBody || hashBytes(body) != entry.SHA256 {
		return false
	}
	resolved, err := arxivHTMLVersion(body, id)
	if err != nil || resolved != entry.ArxivID {
		return false
	}
	w.Header().Set("X-Radar-Cache", "fresh")
	serveArxivHTML(w, r, body, entry)
	return true
}

func (g *arxivProxy) saveHTMLCache(id string, body []byte, entry arxivHTMLCacheEntry) {
	key := hashBytes([]byte("html:" + id))
	raw, err := json.Marshal(entry)
	if err == nil {
		err = atomicSourceFile(g.dir, key+".html", body)
	}
	if err == nil {
		err = atomicSourceFile(g.dir, key+".html.json", raw)
	}
	if err != nil {
		slog.Warn("save arxiv HTML cache", "error", err)
	}
}

func (g *arxivProxy) persistHTMLCooldown() {
	raw, err := json.Marshal(g.next)
	if err == nil {
		err = atomicSourceFile(g.dir, "cooldown.json", raw)
	}
	if err != nil {
		slog.Warn("persist arxiv HTML cooldown", "error", err)
	}
}
