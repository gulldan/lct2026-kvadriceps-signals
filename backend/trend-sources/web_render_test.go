package main

import (
	"encoding/json/v2"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

const scriptedBody = "Электролизёр на протонообменной мембране показал устойчивую работу в течение тысячи часов " +
	"при плотности тока выше эталонной для этого класса установок."
const staticBody = "Перовскитный модуль сохранил эффективность после термоциклирования в независимом испытании " +
	"продолжительностью один календарный год."

func newRenderPanel(t *testing.T, renderStatus int) (*httptest.Server, *httptest.Server, *[]string) {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("GET /robots.txt", func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprint(w, "User-agent: *\nDisallow:\n")
	})
	mux.HandleFunc("GET /static", func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprint(w, "<html><head><title>Статический материал</title></head><body><article><p>"+
			staticBody+"</p></article></body></html>")
	})
	for _, path := range []string{"/scripted", "/scripted-2"} {
		mux.HandleFunc("GET "+path, func(w http.ResponseWriter, _ *http.Request) {
			fmt.Fprint(w, `<html><head><title>Материал</title></head><body><div id="root"></div>`+
				`<script>document.title="loaded"</script></body></html>`)
		})
	}
	panel := httptest.NewTLSServer(mux)
	t.Cleanup(panel.Close)

	asked := []string{}
	renderer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(io.LimitReader(r.Body, 4096))
		var ask struct {
			URL string `json:"url"`
		}
		_ = json.Unmarshal(body, &ask)
		asked = append(asked, ask.URL)
		if renderStatus != http.StatusOK {
			w.WriteHeader(renderStatus)
			return
		}
		fmt.Fprint(w, "<html><head><title>Материал</title></head><body><article><p>"+
			scriptedBody+"</p></article></body></html>")
	}))
	t.Cleanup(renderer.Close)
	return panel, renderer, &asked
}

func renderService(t *testing.T, panel, renderer *httptest.Server, addresses []string, budget int) *service {
	t.Helper()
	s := webPanelService(t, panel, []webSource{{Name: "Лаборатория", URL: panel.URL + "/", Kind: kindURLs,
		Industry: "Energy", Pages: 10, URLs: addresses}})
	s.config.RenderBudget = budget
	s.renderURL, s.render = renderer.URL, renderer.Client()
	return s
}

func renderSnapshot() *snapshot {
	return &snapshot{Version: 3, SourceSet: "test", ObservedAt: "2026-09-12T00:00:00Z", From: "2025-09-01",
		Through: "2026-09-12", Records: []record{}, Pages: []provenance{}, Limits: []string{}}
}

func recordByURL(t *testing.T, data *snapshot, address string) record {
	t.Helper()
	for _, row := range data.Records {
		if row.URL == address {
			return row
		}
	}
	t.Fatalf("no record collected for %s", address)
	return record{}
}

func TestRenderQueueTakesOnlyPagesWithoutText(t *testing.T) {
	t.Parallel()
	panel, renderer, asked := newRenderPanel(t, http.StatusOK)
	s := renderService(t, panel, renderer, []string{panel.URL + "/static", panel.URL + "/scripted"}, 10)
	data := renderSnapshot()
	if err := s.webHistory(t.Context(), data); err != nil {
		t.Fatal(err)
	}

	if len(*asked) != 1 || (*asked)[0] != panel.URL+"/scripted" {
		t.Fatalf("the renderer was asked for %v, expected only the scripted page", *asked)
	}
	static := recordByURL(t, data, panel.URL+"/static")
	if static.TextStatus != articleText || !strings.Contains(static.Text, "Перовскитный модуль") {
		t.Fatalf("the static page lost its text: %+v", static)
	}
	for _, page := range static.Provenance {
		if page.Method != "" {
			t.Fatalf("the static page claims a rendered origin: %+v", page)
		}
	}

	scripted := recordByURL(t, data, panel.URL+"/scripted")
	if scripted.TextStatus != articleText || !strings.Contains(scripted.Text, "Электролизёр") {
		t.Fatalf("the scripted page produced no text: %+v", scripted)
	}
	if scripted.ContentHash != hashBytes([]byte(scripted.Text)) {
		t.Fatal("the rendered text was not hashed")
	}
	rendered := 0
	for _, page := range scripted.Provenance {
		if page.Method == renderMethod && page.SHA256 != "" {
			rendered++
		}
	}
	if rendered != 1 {
		t.Fatalf("the rendered origin is not in provenance: %+v", scripted.Provenance)
	}
	if s.renders != 1 || len(s.renderQueue) != 0 {
		t.Fatalf("the queue was not drained once: renders=%d queue=%v", s.renders, s.renderQueue)
	}
	pages := 0
	for _, page := range data.Pages {
		if page.Method == renderMethod {
			pages++
		}
	}
	if pages != 1 {
		t.Fatalf("the rendered page was not retained as a source page: %+v", data.Pages)
	}
}

func TestRenderBudgetAndRefusalKeepTheRecord(t *testing.T) {
	t.Parallel()
	panel, renderer, asked := newRenderPanel(t, http.StatusInternalServerError)
	s := renderService(t, panel, renderer, []string{panel.URL + "/scripted", panel.URL + "/scripted-2"}, 1)
	data := renderSnapshot()
	if err := s.webHistory(t.Context(), data); err != nil {
		t.Fatal(err)
	}

	if len(*asked) != 1 {
		t.Fatalf("the render budget of one was exceeded: %v", *asked)
	}
	if len(data.Records) != 2 {
		t.Fatalf("a refused render dropped a collected record: %+v", data.Records)
	}
	for _, address := range []string{panel.URL + "/scripted", panel.URL + "/scripted-2"} {
		row := recordByURL(t, data, address)
		if row.TextStatus != metadataOnlyText || row.Text != "" || len(row.Provenance) == 0 {
			t.Fatalf("%s did not stay a metadata-only record with its page: %+v", address, row)
		}
	}
	c := data.Coverage[0]
	if c.Cursor != panel.URL+"/scripted-2" || !strings.Contains(c.StopReason, "render budget") {
		t.Fatalf("the exhausted budget left no cursor to continue from: %+v", c)
	}
	if c.Exhausted {
		t.Fatal("a scope stopped by the render budget is not exhausted")
	}
	if !strings.Contains(strings.Join(c.Limitations, " | "), "javascript render failed") {
		t.Fatalf("the renderer refusal was not recorded: %+v", c.Limitations)
	}
}

func TestRenderStaysOffWithoutARenderer(t *testing.T) {
	t.Parallel()
	panel, renderer, asked := newRenderPanel(t, http.StatusOK)
	s := renderService(t, panel, renderer, []string{panel.URL + "/scripted"}, 10)
	s.renderURL, s.render = "", nil
	data := renderSnapshot()
	if err := s.webHistory(t.Context(), data); err != nil {
		t.Fatal(err)
	}
	if len(*asked) != 0 {
		t.Fatalf("a renderer that is not configured was called: %v", *asked)
	}
	if !strings.Contains(strings.Join(data.Coverage[0].Limitations, " | "), "main text not extracted") {
		t.Fatalf("the page without text was omitted silently: %+v", data.Coverage[0])
	}
}

func TestScheduledCollectionRenewsTheRenderBudget(t *testing.T) {
	t.Parallel()
	panel, renderer, asked := newRenderPanel(t, http.StatusServiceUnavailable)
	s := renderService(t, panel, renderer, []string{panel.URL + "/scripted"}, 1)
	for range 2 {
		if err := s.collect(t.Context(), time.Date(2026, 9, 13, 12, 0, 0, 0, time.UTC)); err != nil {
			t.Fatal(err)
		}
	}
	if len(*asked) != 2 {
		t.Fatalf("a completed pass permanently consumed the render budget: %v", *asked)
	}
}

func TestRendererCredentialsStayOutOfCoverage(t *testing.T) {
	t.Parallel()
	panel, renderer, _ := newRenderPanel(t, http.StatusOK)
	s := renderService(t, panel, renderer, []string{panel.URL + "/scripted"}, 10)
	s.renderURL += "?token=stale-test-value"
	s.renderToken = "test-only&+#?"
	transport := s.render.Transport
	s.render.Transport = ledgerTransport(func(r *http.Request) (*http.Response, error) {
		if got := r.URL.Query().Get("token"); got != s.renderToken {
			t.Error("renderer did not receive the separately configured token")
		}
		return transport.RoundTrip(r)
	})
	data := renderSnapshot()
	if err := s.webHistory(t.Context(), data); err != nil {
		t.Fatal(err)
	}
	if row := recordByURL(t, data, panel.URL+"/scripted"); row.TextStatus != articleText {
		t.Fatal("authenticated rendering lost the article")
	}

	// Ошибка транспорта обычно содержит весь адрес запроса, включая параметр авторизации.
	s.render.Transport = ledgerTransport(func(r *http.Request) (*http.Response, error) {
		return nil, errors.New("transport failed at " + r.URL.String())
	})
	data = renderSnapshot()
	if err := s.webHistory(t.Context(), data); err != nil {
		t.Fatal(err)
	}
	limitations := strings.Join(data.Coverage[0].Limitations, " | ")
	if !strings.Contains(limitations, "render request failed") {
		t.Fatal("renderer failure was not recorded")
	}
	for _, private := range []string{renderer.URL, s.renderToken, "stale-test-value", "token="} {
		if strings.Contains(limitations, private) {
			t.Fatal("renderer configuration leaked into coverage")
		}
	}
}
