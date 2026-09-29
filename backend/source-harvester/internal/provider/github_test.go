package provider

import (
	"context"
	"encoding/base64"
	"encoding/json/v2"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func ghRepository(name string) map[string]any {
	return map[string]any{"id": 101, "full_name": name, "private": false, "html_url": "https://github.com/" + name,
		"description": "Research implementation", "fork": false, "archived": false, "created_at": "2024-01-02T00:00:00Z",
		"updated_at": "2026-08-30T00:00:00Z", "pushed_at": "2026-08-30T00:00:00Z", "stargazers_count": 17, "forks_count": 3}
}

func jsonResponse(t *testing.T, w http.ResponseWriter, v any) {
	t.Helper()
	raw, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.Write(raw); err != nil {
		t.Error(err)
	}
}

func ghClient(t *testing.T, handler http.HandlerFunc) *GitHub {
	t.Helper()
	s := httptest.NewServer(handler)
	t.Cleanup(s.Close)
	g := NewGitHub("test-read-token")
	g.base = s.URL
	now := testObserved()
	g.fetcher.now = func() time.Time { return now }
	g.fetcher.wait = func(ctx context.Context, d time.Duration) error { now = now.Add(d); return ctx.Err() }
	return g
}

func TestGitHubPinnedReadmeAndRename(t *testing.T) {
	name := "example/original"
	sha := strings.Repeat("a", 40)
	blob := strings.Repeat("b", 40)
	readme := "# Research\n[Paper](https://arxiv.org/abs/2501.00001)\n<script>alert(1)</script>"
	g := ghClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer test-read-token" {
			t.Error("missing token")
		}
		switch {
		case r.URL.Path == "/search/repositories":
			jsonResponse(t, w, map[string]any{"total_count": 1, "incomplete_results": false, "items": []any{ghRepository(name)}})
		case r.URL.Path == "/repositories/101":
			jsonResponse(t, w, ghRepository(name))
		case strings.HasSuffix(r.URL.Path, "/commits"):
			if r.URL.Query().Get("until") != "2026-08-31T23:59:59Z" {
				t.Error("commit cutoff missing")
			}
			jsonResponse(t, w, []any{map[string]any{"sha": sha,
				"commit": map[string]any{"committer": map[string]any{"date": "2026-08-01T00:00:00Z"}}}})
		case strings.HasSuffix(r.URL.Path, "/readme"):
			if r.URL.Query().Get("ref") != sha {
				t.Error("README not pinned")
			}
			jsonResponse(t, w, map[string]any{"sha": blob, "path": "README.md", "size": len(readme), "encoding": "base64",
				"content": base64.StdEncoding.EncodeToString([]byte(readme))})
		case strings.HasSuffix(r.URL.Path, "/releases"):
			jsonResponse(t, w, []any{
				map[string]any{"id": 3, "published_at": "2026-09-01T00:00:00Z", "draft": false},
				map[string]any{"id": 2, "published_at": "2026-07-01T00:00:00Z", "draft": true},
				map[string]any{"id": 1, "published_at": "2025-04-01T00:00:00Z",
					"html_url": "https://github.com/" + name + "/releases/tag/v1", "tag_name": "v1", "body": "Measured implementation."},
			})
		default:
			t.Errorf("unexpected path %s", r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	})
	p, err := g.Fetch(t.Context(), testScope(), "")
	if err != nil || len(p.Records) != 1 {
		t.Fatalf("GitHub source: %+v %v", p, err)
	}
	one := p.Records[0]
	if one.Text != readme || one.Source.ContentHash != digest([]byte(readme)) || one.Source.Publication != nil ||
		one.Source.GitHub.CommitSHA != sha || !strings.Contains(one.ContentURL, "/blob/"+sha+"/") || len(one.Releases) != 1 {
		t.Fatalf("lost pinned content, separate dates or cutoff: %+v", one)
	}
	name = "example/renamed"
	p, err = g.Fetch(t.Context(), testScope(), "")
	if err != nil || len(p.Records) != 1 {
		t.Fatalf("renamed source: %+v %v", p, err)
	}
	two := p.Records[0]
	if one.Source.CanonicalID != two.Source.CanonicalID || one.Source.URL == two.Source.URL ||
		one.Source.Revision == two.Source.Revision {
		t.Error("rename/observation overwrote stable identity")
	}
}

func TestGitHubCollectionOrderIsExplicit(t *testing.T) {
	for _, order := range []string{"", "updated", "stars"} {
		t.Run(order, func(t *testing.T) {
			g := ghClient(t, func(w http.ResponseWriter, r *http.Request) {
				want := order
				if want == "" {
					want = "updated"
				}
				if r.URL.Query().Get("sort") != want || r.URL.Query().Get("order") != "desc" {
					t.Error("query order differs from the persisted scope")
				}
				jsonResponse(t, w, map[string]any{"total_count": 0, "items": []any{}})
			})
			scope := testScope()
			scope.Sort = order
			if _, err := g.Fetch(t.Context(), scope, ""); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestGitHubPrivateOrRateLimitedDoesNotAdvance(t *testing.T) {
	for _, mode := range []string{"private", "missing_visibility", "unauthorized", "primary_rate_limit", "secondary_rate_limit"} {
		t.Run(mode, func(t *testing.T) {
			g := ghClient(t, func(w http.ResponseWriter, r *http.Request) {
				repo := ghRepository("example/project")
				if r.URL.Path == "/search/repositories" {
					jsonResponse(t, w, map[string]any{"total_count": 1, "items": []any{repo}})
					return
				}
				switch mode {
				case "private":
					repo["private"] = true
					jsonResponse(t, w, repo)
				case "missing_visibility":
					delete(repo, "private")
					jsonResponse(t, w, repo)
				case "unauthorized":
					w.WriteHeader(http.StatusUnauthorized)
				case "primary_rate_limit":
					w.Header().Set("X-Ratelimit-Remaining", "0")
					w.Header().Set("X-Ratelimit-Reset", "1788825720")
					w.WriteHeader(http.StatusForbidden)
				case "secondary_rate_limit":
					w.Header().Set("Retry-After", "120")
					w.WriteHeader(http.StatusTooManyRequests)
				}
			})
			saved := 0
			result, err := Collect(t.Context(), g, testScope(), 2, func(p Page) error {
				saved++
				if len(p.Records) != 0 {
					t.Error("private record exposed")
				}
				return nil
			})
			if strings.Contains(mode, "visibility") || mode == "private" {
				if err != nil || result.Rejected != 1 {
					t.Fatalf("private/unknown visibility accepted: %+v %v", result, err)
				}
			} else if err == nil || saved != 0 || result.Cursor != "" {
				t.Fatalf("failed request skipped: %+v %v", result, err)
			}
		})
	}
}

func TestGitHubIncompleteCoverage(t *testing.T) {
	for _, raw := range []string{`{"total_count":1001,"incomplete_results":false,"items":[]}`,
		`{"total_count":0,"incomplete_results":true,"items":[]}`} {
		g := ghClient(t, func(w http.ResponseWriter, _ *http.Request) { fmt.Fprint(w, raw) })
		r, err := Collect(t.Context(), g, testScope(), 2, func(Page) error { return nil })
		if err != nil || r.Status != "provider_incomplete" || len(r.Limitations) == 0 {
			t.Fatalf("false full coverage: %+v %v", r, err)
		}
	}
}

func TestGitHubMissingAndInvalidReadme(t *testing.T) {
	for _, mode := range []string{"missing", "too_large", "bad_base64", "invalid_utf8"} {
		t.Run(mode, func(t *testing.T) {
			g := ghClient(t, func(w http.ResponseWriter, _ *http.Request) {
				if mode == "missing" {
					w.WriteHeader(http.StatusNotFound)
					return
				}
				raw := map[string]any{"sha": strings.Repeat("b", 40), "path": "README.md", "encoding": "base64", "size": 1, "content": "AA=="}
				switch mode {
				case "too_large":
					raw["size"] = 1<<20 + 1
				case "bad_base64":
					raw["content"] = "?"
				case "invalid_utf8":
					raw["content"] = "/w=="
				}
				jsonResponse(t, w, raw)
			})
			readme, _, _, err := g.readme(t.Context(), g.base, strings.Repeat("a", 40))
			if mode == "missing" {
				if err != nil || readme != nil {
					t.Error("missing README fabricated")
				}
			} else if err == nil {
				t.Error("invalid README accepted")
			}
		})
	}
}

func TestConditionalBodyAndMissingCache(t *testing.T) {
	for _, missing := range []bool{false, true} {
		calls := 0
		c := fakeClient(t, func(w http.ResponseWriter, r *http.Request) {
			calls++
			if (missing && calls == 1) || (!missing && calls == 2) {
				w.WriteHeader(http.StatusNotModified)
				return
			}
			if missing && calls == 2 && r.Header.Get("If-None-Match") != "" {
				t.Error("conditional request without a body")
			}
			w.Header().Set("ETag", "test-etag")
			fmt.Fprint(w, "saved body")
		})
		for range 2 {
			raw, _, err := c.fetcher.get(t.Context(), c.base)
			if err != nil || string(raw) != "saved body" {
				t.Fatalf("lost conditional body: %s %v", raw, err)
			}
			raw[0] = 'X'
		}
	}
}
