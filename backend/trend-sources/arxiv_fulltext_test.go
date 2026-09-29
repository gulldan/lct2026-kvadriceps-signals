package main

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"
)

func arxivHTMLTestPage(id string) string {
	return `<html><header><a class="header-button" href="/abs/` + id +
		`">Back to abstract</a></header><article><h1>Research</h1><p>Public laboratory results.</p></article></html>`
}

func TestArxivFulltextPerimeterAndExactVersion(t *testing.T) {
	t.Setenv("SOURCE_PROXY_URL", "")
	reader := newArxivFulltextReader()
	previous, _ := http.NewRequestWithContext(t.Context(), http.MethodGet,
		"https://arxiv.org/html/2511.12739v1", http.NoBody)
	for _, address := range []string{
		"http://arxiv.org/html/2511.12739v1", "https://arxiv.org:8443/html/2511.12739v1",
		"https://arxiv.org.evil.example/html/2511.12739v1", "https://sub.arxiv.org/html/2511.12739v1",
		"https://user@arxiv.org/html/2511.12739v1", "https://arxiv.org/pdf/2511.12739v1",
		"https://arxiv.org/html/2511.12739v1?url=http://127.0.0.1", "https://127.0.0.1/html/2511.12739v1",
		"https://arxiv.org/html/%32%35%31%31.12739v1", "https://arxiv.org/html/../api/query",
	} {
		next, err := http.NewRequestWithContext(t.Context(), http.MethodGet, address, http.NoBody)
		if err == nil && arxivHTMLAddress(next.URL) == nil {
			t.Fatalf("unsafe redirect accepted: %s", address)
		}
	}
	if !errors.Is(reader.client.CheckRedirect(previous, []*http.Request{previous}), http.ErrUseLastResponse) {
		t.Fatal("automatic redirects bypass robots and pacing")
	}
	transport, ok := reader.client.Transport.(*http.Transport)
	if !ok {
		t.Fatal("public-address transport missing")
	}
	connection, err := transport.DialContext(t.Context(), "tcp", "127.0.0.1:443")
	if connection != nil {
		_ = connection.Close()
	}
	if err == nil || !strings.Contains(err.Error(), "blocked internal source") {
		t.Fatalf("actual connection skipped the public-address check: %v", err)
	}
	for _, scenario := range []struct {
		requested, returned string
		want                bool
	}{
		{"2511.12739", "2511.12739v1", true}, {"2511.12739v1", "2511.12739v1", true},
		{"2511.12739v1", "2511.12739v2", false}, {"2511.12739", "2511.02645v1", false},
		{"2511.12739", "2511.12739", false},
	} {
		got, err := arxivHTMLVersion([]byte(arxivHTMLTestPage(scenario.returned)), scenario.requested)
		if (err == nil) != scenario.want || scenario.want && got != scenario.returned {
			t.Fatalf("version binding failed for %+v: id=%q err=%v", scenario, got, err)
		}
	}
	if _, err := arxivHTMLVersion([]byte(`<article><a href="/abs/2511.12739v1">citation</a></article>`),
		"2511.12739"); err == nil {
		t.Fatal("a paper citation was mistaken for the page identity")
	}
}

func TestArxivFulltextBoundsCacheAndCooldown(t *testing.T) {
	for _, mode := range []string{"valid", "mime", "overflow", "broken_body", "cancel", "wrong_version",
		"robots_denied", "cooldown", "invalid_id"} {
		t.Run(mode, func(t *testing.T) {
			g := newArxivProxy(t.TempDir())
			g.fulltext.robotsAt = time.Now()
			var calls atomic.Int64
			entered := make(chan struct{})
			g.fulltext.client.Transport = ledgerTransport(func(req *http.Request) (*http.Response, error) {
				calls.Add(1)
				if mode == "cancel" {
					close(entered)
					<-req.Context().Done()
					return nil, req.Context().Err()
				}
				body, contentType := arxivHTMLTestPage("2511.12739v1"), "text/html; charset=utf-8"
				switch mode {
				case "mime":
					contentType = "application/pdf"
				case "overflow":
					body = strings.Repeat("x", arxivHTMLMaxBody+1)
				case "wrong_version":
					body = arxivHTMLTestPage("2511.12739v2")
				}
				reader := io.NopCloser(strings.NewReader(body))
				if mode == "broken_body" {
					reader = io.NopCloser(arxivBrokenReader{})
				}
				return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": {contentType}},
					Body: reader, Request: req}, nil
			})
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			id, want := "2511.12739v1", http.StatusBadGateway
			switch mode {
			case "valid":
				want = http.StatusOK
			case "robots_denied":
				g.fulltext.robots = parseRobots("User-agent: *\nDisallow: /html/\n")
				want = http.StatusForbidden
			case "cooldown":
				g.next = time.Now().Add(time.Minute)
				want = http.StatusTooManyRequests
			case "invalid_id":
				id, want = "https://127.0.0.1", http.StatusBadRequest
			case "cancel":
				go func() {
					select {
					case <-entered:
						cancel()
					case <-ctx.Done():
					}
				}()
			}
			req := httptest.NewRequestWithContext(ctx, http.MethodGet, "/v1/papers/paper/html", http.NoBody)
			req.SetPathValue("id", id)
			out := httptest.NewRecorder()
			g.paperHTML(out, req)
			if out.Code != want {
				t.Fatalf("status=%d want=%d body=%s", out.Code, want, out.Body.String())
			}
			if mode != "valid" {
				if out.Header().Get("X-Source-Sha256") != "" {
					t.Fatal("failed read published proof headers")
				}
				return
			}
			if out.Header().Get("X-Arxiv-Id") != id || out.Header().Get("X-Source-Sha256") != hashBytes(out.Body.Bytes()) ||
				out.Header().Get("Content-Type") != "application/octet-stream" || out.Header().Get("X-Source-Media-Type") != "text/html" {
				t.Fatal("response lost exact identity, hash or inert transport type")
			}
			cached := httptest.NewRecorder()
			g.paperHTML(cached, req)
			if calls.Load() != 1 || cached.Header().Get("X-Radar-Cache") != "fresh" ||
				cached.Body.String() != out.Body.String() {
				t.Fatal("cached immutable body was fetched again or changed")
			}
		})
	}
}

type arxivBrokenReader struct{}

func (arxivBrokenReader) Read([]byte) (int, error) { return 0, io.ErrUnexpectedEOF }

func TestArxivHTMLWaitingDeadline(t *testing.T) {
	for _, mode := range []string{"slot", "robots_cooldown", "paper_cooldown", "cached"} {
		t.Run(mode, func(t *testing.T) {
			dir := t.TempDir()
			synctest.Test(t, func(t *testing.T) {
				g := newArxivProxy(dir)
				g.slot <- struct{}{}
				cached := mode == "cached"
				body := []byte(arxivHTMLTestPage("2511.02645v1"))
				if cached {
					g.saveHTMLCache("2511.02645v1", body, arxivHTMLCacheEntry{
						ArxivID: "2511.02645v1", ObservedAt: g.now(), SHA256: hashBytes(body),
					})
				}
				remainingSlots := 1
				if mode != "slot" && !cached {
					remainingSlots = 0
					g.next = time.Now().Add(9 * time.Second)
					go func() {
						time.Sleep(6 * time.Second)
						<-g.slot
					}()
				}
				if mode == "paper_cooldown" {
					g.fulltext.robotsAt = time.Now()
				}
				var calls atomic.Int64
				g.fulltext.client.Transport = ledgerTransport(func(*http.Request) (*http.Response, error) {
					calls.Add(1)
					return nil, errors.New("waiting request must not reach arxiv")
				})
				req := httptest.NewRequestWithContext(t.Context(), http.MethodGet,
					"/v1/papers/2511.02645v1/html", http.NoBody)
				req.SetPathValue("id", "2511.02645v1")
				out := httptest.NewRecorder()
				started := time.Now()
				g.paperHTML(out, req)
				if cached {
					if out.Code != http.StatusOK || out.Body.String() != string(body) ||
						out.Header().Get("X-Source-Sha256") != hashBytes(body) || time.Since(started) != 0 ||
						calls.Load() != 0 || len(g.slot) != remainingSlots {
						t.Fatal("verified cache waited for the network queue or lost its source proof")
					}
					return
				}
				if out.Code != http.StatusGatewayTimeout || out.Body.Len() == 0 {
					t.Fatalf("waiting deadline returned status=%d body=%q", out.Code, out.Body.String())
				}
				if time.Since(started) != arxivHTMLBudget || calls.Load() != 0 || len(g.slot) != remainingSlots {
					t.Fatalf("wait changed budget, source access or slot ownership: duration=%v calls=%d slots=%d",
						time.Since(started), calls.Load(), len(g.slot))
				}
				if out.Header().Get("X-Arxiv-Id") != "" || out.Header().Get("X-Source-Sha256") != "" {
					t.Fatal("waiting deadline published source proof")
				}
			})
		})
	}
}

func TestArxivHTMLRobotsAndAtomShareRetryAfter(t *testing.T) {
	g := newArxivProxy(t.TempDir())
	g.fulltext.client.Transport = ledgerTransport(func(req *http.Request) (*http.Response, error) {
		if req.URL.Path != "/robots.txt" {
			t.Errorf("page fetched without robots permission: %s", req.URL)
		}
		return &http.Response{StatusCode: http.StatusTooManyRequests, Body: io.NopCloser(strings.NewReader("")),
			Header: http.Header{"Retry-After": {"180"}}, Request: req}, nil
	})
	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/v1/papers/2511.12739v1/html", http.NoBody)
	req.SetPathValue("id", "2511.12739v1")
	g.paperHTML(httptest.NewRecorder(), req)
	restarted := newArxivProxy(g.dir)
	restarted.client.Transport = ledgerTransport(func(*http.Request) (*http.Response, error) {
		return nil, errors.New("Atom must not be requested during shared cooldown")
	})
	out := httptest.NewRecorder()
	restarted.query(out, httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/api/query?search_query=test", nil))
	if out.Code != http.StatusTooManyRequests || out.Header().Get("Retry-After") == "" {
		t.Fatalf("HTML Retry-After not shared with Atom after restart: %d", out.Code)
	}
}

func TestArxivHTMLRedirectRechecksRobotsIdentityAndPacing(t *testing.T) {
	t.Parallel()
	for _, mode := range []string{"allowed", "robots_denied", "wrong_version", "cross_origin"} {
		t.Run(mode, func(t *testing.T) {
			t.Parallel()
			g := newArxivProxy(t.TempDir())
			g.fulltext.robotsAt = time.Now()
			if mode == "robots_denied" {
				g.fulltext.robots = parseRobots("User-agent: *\nDisallow: /html/2511.12739v1\n")
			}
			var paths []string
			first := time.Now()
			g.fulltext.client.Transport = ledgerTransport(func(req *http.Request) (*http.Response, error) {
				paths = append(paths, req.URL.Path)
				if len(paths) == 1 {
					first = time.Now()
					location := "/html/2511.12739v1"
					if mode == "wrong_version" {
						location = "/html/2511.12739v2"
					}
					if mode == "cross_origin" {
						location = "https://127.0.0.1/html/2511.12739v1"
					}
					return &http.Response{StatusCode: http.StatusFound, Header: http.Header{"Location": {location}},
						Body: io.NopCloser(strings.NewReader("")), Request: req}, nil
				}
				if time.Since(first) < 3*time.Second {
					t.Error("redirect performed another GET before the shared source pause")
				}
				return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": {"text/html"}},
					Body: io.NopCloser(strings.NewReader(arxivHTMLTestPage("2511.12739v1"))), Request: req}, nil
			})
			id := "2511.12739"
			if mode == "wrong_version" {
				id += "v1"
			}
			req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/v1/papers/"+id+"/html", http.NoBody)
			req.SetPathValue("id", id)
			out := httptest.NewRecorder()
			g.paperHTML(out, req)
			if mode == "allowed" {
				if len(paths) != 2 || out.Code != http.StatusOK || out.Header().Get("X-Arxiv-Id") != "2511.12739v1" {
					t.Fatalf("safe version-pinning redirect failed: paths=%v status=%d", paths, out.Code)
				}
			} else if len(paths) != 1 || out.Header().Get("X-Arxiv-Id") != "" || out.Code == http.StatusOK {
				t.Fatalf("redirect bypassed source policy: paths=%v status=%d", paths, out.Code)
			}
		})
	}
}
