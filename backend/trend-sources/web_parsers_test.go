package main

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"golang.org/x/net/html"
)

func TestPublicationFeedFormats(t *testing.T) {
	for _, tc := range []struct {
		name, body, date string
	}{
		{"rss-dublin-core", `<rss version="2.0" xmlns:dc="http://purl.org/dc/elements/1.1/"><channel><item>` +
			`<title>Tech &amp; research</title><link>https://lab.example/paper</link>` +
			`<dc:date>2026-09-09T23:30:00-04:00</dc:date></item></channel></rss>`, "2026-09-09"},
		{"atom", `<feed xmlns="http://www.w3.org/2005/Atom"><title>Lab</title><entry><id>urn:paper:1</id>` +
			`<title>Research</title><link rel="alternate" href="https://lab.example/paper"/>` +
			`<published>2026-09-08T00:00:00Z</published><updated>2026-09-10T00:00:00Z</updated></entry></feed>`, "2026-09-08"},
		{"json-feed", `{"version":"https://jsonfeed.org/version/1.1","title":"Lab","items":[{"id":"1",` +
			`"url":"https://lab.example/paper","title":"Research","date_published":"2026-09-08T00:00:00Z"}]}`, "2026-09-08"},
		{"atom-update-is-not-publication", `<feed xmlns="http://www.w3.org/2005/Atom"><entry><title>Research</title>` +
			`<link href="https://lab.example/paper"/><updated>2026-09-10T00:00:00Z</updated></entry></feed>`, ""},
		{"missing-link", `<rss><channel><item><title>Research</title>` +
			`<pubDate>Tue, 08 Sep 2026 12:00:00 GMT</pubDate></item></channel></rss>`, ""},
		{"credentials", `<rss><channel><item><title>Research</title><link>https://user:password@lab.example/paper</link>` +
			`<pubDate>Tue, 08 Sep 2026 12:00:00 GMT</pubDate></item></channel></rss>`, ""},
		{"challenge-page", `<html><body>Access denied</body></html>`, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rows, err := feedRows([]byte(tc.body), feed{URL: "https://lab.example/feed", Name: "Lab"}, "2026-09-13")
			if tc.date == "" {
				if err == nil || len(rows) != 0 {
					t.Fatalf("undated/invalid feed became evidence: %+v, %v", rows, err)
				}
				return
			}
			if err != nil || len(rows) != 1 || rows[0].Date != tc.date || rows[0].ParserVersion != webFeedParserVersion {
				t.Fatalf("feed: %+v, %v", rows, err)
			}
		})
	}
}

func TestSourceQueryWordPrefixes(t *testing.T) {
	for _, tc := range []struct {
		text, term string
		want       bool
	}{
		{"Инженерия искусственного интеллекта", "искусственн*", true},
		{"Новые материалы для сенсоров", "материал*", true},
		{"Neural network research", "neural network", true},
		{"Журналистика и материнская плата", "материал", false},
		{"Domain expertise", "ai", false},
		{"Research", "*", false},
		{"Производство твердотельных аккумуляторов выросло", "твердотельные аккумуляторы", true},
		{"Спрос на материалы для сенсоров", "материал", true},
		// Общее начало — не общее слово: основа «сет» не равна ни «сетчатк», ни «сетев».
		{"Сетчатка глаза и зрение", "сеть", false},
		{"Поставки сетевого оборудования", "сеть", false},
		{"Новая сеть связи", "сети", true},
		{"С цифрового рубля откроется новый сезон онлайн-уроков", "цифровой рубль", true},
		{"Налоговая усилит слежку за цифровым рублем", "цифровой рубль", true},
		{"Платежная инфраструктура банка", "платёжная инфраструктура", true},
		{"Цифровой рубль и платёжные сервисы", "платежные сервисы", true},
		// Латиница сравнивается слово в слово.
		{"Battery packs shipped", "battery pack", false},
		{"Battery pack shipped", "battery pack", true},
	} {
		if got := matches(tc.text, []string{tc.term}); got != tc.want {
			t.Fatalf("matches(%q, %q) = %v, want %v", tc.text, tc.term, got, tc.want)
		}
	}
}

func TestArticleDatesAndOriginalElementPaths(t *testing.T) {
	raw := []byte(`<html><head><title>Исследование</title>` +
		`<meta property="article:published_time" content="2026-09-08T10:00:00Z"></head><body><main><div><article>` +
		`<p>Техно<b>логия</b> лаборатории улучшает качество поиска и сохраняет ссылки на исходные исследования ` +
		`для проверки результатов.</p><p>Независимая проверка подтверждает точность извлечения текста и сохранение ` +
		`опубликованной даты без подмены датой сбора.</p></article></div></main><script type="application/ld+json">` +
		`{"@type":"NewsArticle","datePublished":"2026-09-08","comment":{"datePublished":"2026-09-12"}}</script></body></html>`)
	row, err := articleDocument(raw, record{ID: "paper", URL: "https://lab.example/paper"}, "2026-09-13T12:00:00Z")
	if err != nil || row.Date != "2026-09-08" || !strings.Contains(row.Text, "Технология") || len(row.Anchors) != 2 {
		t.Fatalf("article: %+v, %v", row, err)
	}
	doc, _ := html.Parse(bytes.NewReader(raw))
	runes := []rune(row.Text)
	for _, anchor := range row.Anchors {
		found := false
		visit(doc, func(n *html.Node) {
			if n.Type == html.ElementNode && elementPath(n) == anchor.Path {
				found = strings.Join(strings.Fields(content(n)), " ") == string(runes[anchor.Start:anchor.End])
			}
		})
		if !found {
			t.Fatalf("anchor does not identify the original element: %+v", anchor)
		}
	}
	_, conflict := articleDocument(raw, record{Date: "2026-09-07"}, "2026-09-13T12:00:00Z")
	if conflict == nil || errors.Is(conflict, errArticleBodyUnavailable) {
		t.Fatal("conflicting date must fail without entering the JavaScript render queue")
	}
	undated, err := articleDocument([]byte(webArticle(scriptedBody)),
		record{ID: "unknown", URL: "https://lab.example/unknown"}, "2026-09-13T12:00:00Z")
	if err != nil || undated.Date != "" {
		t.Fatalf("invented a publication date: %+v %v", undated, err)
	}
	data := &snapshot{From: "2026-09-01", Through: "2026-09-13", Records: []record{undated}}
	mergeRetained(data, nil, time.Date(2026, 9, 13, 12, 0, 0, 0, time.UTC), func(record) bool { return true })
	if len(data.Records) != 1 || data.Records[0].Date != "" {
		t.Fatal("undated document was lost or dated during persistence")
	}
}

func TestFeedBudgetContinuesAfterUnavailableArticle(t *testing.T) {
	t.Parallel()
	mux := http.NewServeMux()
	mux.HandleFunc("GET /robots.txt", func(w http.ResponseWriter, _ *http.Request) { fmt.Fprint(w, "User-agent: *\nAllow: /\n") })
	mux.HandleFunc("GET /feed", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, "<rss><channel>")
		for _, id := range []string{"first", "second"} {
			fmt.Fprintf(w, "<item><title>%s</title><link>https://%s/%s</link>"+
				"<pubDate>Tue, 08 Sep 2026 12:00:00 GMT</pubDate></item>", id, r.Host, id)
		}
		fmt.Fprint(w, "</channel></rss>")
	})
	mux.HandleFunc("GET /{id}", func(w http.ResponseWriter, r *http.Request) {
		if r.PathValue("id") == "first" {
			http.NotFound(w, r)
			return
		}
		fmt.Fprint(w, webArticle(scriptedBody))
	})
	server := httptest.NewTLSServer(mux)
	defer server.Close()
	source := webSource{Name: "Lab", URL: server.URL + "/feed", Kind: "feed", Pages: 1}
	s := webPanelService(t, server, []webSource{source})
	first := renderSnapshot()
	if err := s.webHistory(t.Context(), first); err != nil {
		t.Fatal(err)
	}
	if recordByURL(t, first, server.URL+"/first").TextStatus != metadataOnlyText ||
		first.Coverage[0].Cursor != server.URL+"/second" {
		t.Fatal("failed record or next unattempted page checkpoint missing")
	}
	if len(recordByURL(t, first, server.URL+"/first").Limitations) == 0 || len(first.Coverage[0].Limitations) == 0 {
		t.Fatal("article request failure was hidden by the later page-budget stop")
	}
	s.data = first
	second := renderSnapshot()
	if err := s.webHistory(t.Context(), second); err != nil {
		t.Fatal(err)
	}
	if recordByURL(t, second, server.URL+"/second").TextStatus != articleText {
		t.Fatal("the unavailable first article starved the next one")
	}
}

func TestSourceRetryAfterSurvivesRestart(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	calls := 0
	client := &http.Client{Transport: ledgerTransport(func(*http.Request) (*http.Response, error) {
		calls++
		return &http.Response{StatusCode: http.StatusTooManyRequests, Header: http.Header{"Retry-After": []string{"180"}},
			Body: io.NopCloser(strings.NewReader(""))}, nil
	})}
	for range 2 {
		s := &service{dir: dir, config: defaultConfig(), http: client}
		_, err := s.fetch(t.Context(), "https://lab.example/feed", &snapshot{})
		failure, ok := errors.AsType[*sourceHTTPError](err)
		if !ok || failure.RetryAt.IsZero() {
			t.Fatalf("cooldown was lost: %v", err)
		}
	}
	if calls != 1 {
		t.Fatalf("rate-limited provider was contacted %d times", calls)
	}
}
