package application

import (
	"context"
	"encoding/json/v2"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/example/main-service/internal/platform/analysisdoc"
)

func liveMLFixture(t *testing.T, name string, into any) []byte {
	t.Helper()
	raw, err := os.ReadFile("testdata/ml_candidate_source/" + name + ".json")
	if err != nil {
		t.Fatal(err)
	}
	if into != nil {
		if err := json.Unmarshal(raw, into); err != nil {
			t.Fatal(err)
		}
	}
	return raw
}

func liveMLTestServer(t *testing.T, handler http.Handler) (*httptest.Server, http.RoundTripper) {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	return server, server.Client().Transport
}

func TestLiveMLUnknownLanguageDoesNotDisableSupportedCorpus(t *testing.T) {
	service := &TrendLiveService{ml: &liveMLSource{version: "test",
		extract: func(_ context.Context, input LiveMLExtractRequest) (LiveMLExtractResponse, error) {
			if len(input.Documents) != 1 || input.Documents[0].DocID != "paper" {
				t.Fatalf("неверный корпус извлечения: %+v", input.Documents)
			}
			return LiveMLExtractResponse{Candidates: []LiveMLExtractCandidate{},
				Coverage: LiveMLExtractCoverage{DocumentsIn: 1, DocumentsProcessed: 1}}, nil
		}}}
	documents := []StudyPaper{{ID: "paper", Title: "Квантовый датчик"},
		{ID: "rec:external", Title: "Unclassified external website", URL: "https://example.com"}}
	umbrella := LiveTopic{}
	extracting, run := service.mlStartExtraction(t.Context(), "датчик", &umbrella, documents, nil)
	extracting()
	if run.Used != liveMLSourceOn || run.Coverage == nil || run.Coverage.DocumentsSkippedLanguage != 1 ||
		run.Coverage.DocumentsProcessed != 1 || len(umbrella.Limits) != 1 || !service.reusableMLExtraction(run) {
		t.Fatalf("неизвестный язык отключил модель или скрыл пропуск: %+v, %+v", run, umbrella.Limits)
	}
}

func TestLiveMLExtractionLimitsRemainSeparateFromPredict(t *testing.T) {
	var input LiveMLExtractRequest
	liveMLFixture(t, "extract_request", &input)
	raw := liveMLFixture(t, "extract_response", nil)
	largeResponse := append(slices.Clone(raw), []byte(strings.Repeat(" ", liveClassifierBytes))...)
	large := input
	large.Documents = slices.Clone(input.Documents)
	large.Documents[0].Text = strings.Repeat("x", liveClassifierInputBytes)
	predict := LiveClassifierRequest{ModelVersion: input.ModelVersion, Candidates: []LiveClassifierCandidate{
		{Name: large.Documents[0].Text}}}
	if _, err := liveClassifierRequest(t.Context(), "http://localhost/predict", predict); err == nil {
		t.Fatal("изменён прежний предел классификатора")
	}
	for _, tc := range []struct {
		name                    string
		input                   LiveMLExtractRequest
		body                    []byte
		requestBytes, respBytes int
		accepted                bool
	}{
		{"default_request", large, raw, 0, 0, false},
		{"configured_request", large, raw, 6 << 20, 0, true},
		{"default_response", input, largeResponse, 0, 0, false},
		{"configured_response", input, largeResponse, 0, 2 << 20, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server, transport := liveMLTestServer(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set(liveMLVersionHeader, input.ModelVersion)
				_, _ = w.Write(tc.body)
			}))
			extract := newMLCandidateExtractor(server.URL+"/extract", time.Second, transport, tc.requestBytes, tc.respBytes)
			_, err := extract(t.Context(), tc.input)
			if (err == nil) != tc.accepted {
				t.Fatalf("проверка отдельного лимита: принят=%v, ошибка=%v", tc.accepted, err)
			}
		})
	}
}

func TestLiveMLExtractionFailuresAndCache(t *testing.T) {
	var input LiveMLExtractRequest
	raw := liveMLFixture(t, "extract_response", nil)
	liveMLFixture(t, "extract_request", &input)
	for _, mode := range []string{
		"timeout", "version", "body_version", "malformed", "oversize", "input_oversize", "redirect", "closed",
	} {
		t.Run(mode, func(t *testing.T) {
			var calls atomic.Int64
			server, transport := liveMLTestServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				w.Header().Set(liveMLVersionHeader, input.ModelVersion)
				switch mode {
				case "timeout":
					select {
					case <-r.Context().Done():
					case <-time.After(100 * time.Millisecond):
					}
				case "version":
					w.Header().Set(liveMLVersionHeader, "wrong")
					_, _ = w.Write(raw)
				case "body_version":
					_, _ = w.Write([]byte(strings.ReplaceAll(string(raw), input.ModelVersion, "wrong")))
				case "malformed":
					_, _ = w.Write([]byte(`{}`))
				case "oversize":
					_, _ = w.Write([]byte(strings.Repeat(" ", liveClassifierBytes+1)))
				case "redirect":
					http.Redirect(w, r, "/other", http.StatusTemporaryRedirect)
				}
			}))
			if mode == "closed" {
				server.Close()
			}
			extract := newMLCandidateExtractor(server.URL+"/extract", 20*time.Millisecond, transport, 0, 0)
			request := input
			if mode == "input_oversize" {
				request.Documents = slices.Clone(input.Documents)
				request.Documents[0].Text = strings.Repeat("x", liveClassifierInputBytes)
			}
			if _, err := extract(t.Context(), request); err == nil {
				t.Fatal("отказ извлечения принят как успешный пакет")
			}
			if mode == "redirect" && calls.Load() != 1 {
				t.Fatal("клиент последовал перенаправлению")
			}
			if mode == "input_oversize" && calls.Load() != 0 {
				t.Fatal("слишком большой пакет отправлен сервису")
			}
		})
	}
	service := &TrendLiveService{}
	service.SetMLCandidateSource("ml", nil, "v1")
	if service.SetMLCandidateSource("other", nil, ""); service.ml != nil {
		t.Fatal("неизвестный режим не вернул n-граммы")
	}
	service.SetMLCandidateSource("ml", nil, "v1")
	run := &LiveMLExtractionRun{Requested: "ml", Used: "ml", Version: "v1", Coverage: &LiveMLExtractCoverage{}}
	if service.reusableMLExtraction(nil) || !service.reusableMLExtraction(run) {
		t.Fatal("смешан кэш разных источников")
	}
	run.Version = "v2"
	if service.reusableMLExtraction(run) {
		t.Fatal("смешан кэш версий извлечения")
	}
	service.SetMLCandidateSource("ngrams", nil, "")
	if service.reusableMLExtraction(run) || !service.reusableMLExtraction(nil) {
		t.Fatal("старый режим читает результат модели")
	}
	if NewMLCandidateExtractor("", 0, 0, 0) != nil {
		t.Fatal("пустой адрес включил извлечение")
	}
	_, err := newMLCandidateExtractor("invalid", 0, nil, 0, 0)(t.Context(), input)
	if err == nil || errors.Is(err, context.DeadlineExceeded) {
		t.Fatal("не проверяется адрес извлечения")
	}
}

func liveMLFitServer(t *testing.T, limit int) (MLCandidateExtractor, *[]LiveMLExtractRequest) {
	t.Helper()
	var sent []LiveMLExtractRequest
	server, transport := liveMLTestServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var input LiveMLExtractRequest
		if err := json.UnmarshalRead(r.Body, &input); err != nil {
			t.Error(err)
		}
		sent = append(sent, input)
		w.Header().Set(liveMLVersionHeader, input.ModelVersion)
		_ = json.MarshalWrite(w, LiveMLExtractResponse{ModelVersions: map[string]string{"service": input.ModelVersion},
			Candidates: []LiveMLExtractCandidate{{ID: "ml-id", Name: "Quantum sensor", CanonicalRU: "quantum sensor",
				DocIDs: []string{input.Documents[0].DocID}, Stage: LiveMLExtractStage{Code: 3}}},
			Coverage: LiveMLExtractCoverage{DocumentsIn: len(input.Documents), DocumentsProcessed: len(input.Documents)}})
	}))
	return newMLCandidateExtractor(server.URL+"/extract", time.Second, transport, limit, 0), &sent
}

func TestLiveMLExtractFitsTransportBudget(t *testing.T) {
	const sentence = "Квантовый датчик прошёл пилот в банке."
	filler := func(n int) string { return strings.Repeat("lorem ipsum ", n/12) }
	documents := []StudyPaper{
		{ID: "arxiv:1", Title: "Квантовый датчик для связи", Date: "2026-05-10"},
		{ID: "rec:r1", Title: "Испытания датчика", Date: "2026-07-10"},
		{ID: "rec:r2", Title: "Обзор датчиков", Date: "2026-06-10"},
	}
	// У r2 аннотация стоит глубже 20 000 знаков: рез не может быть раньше её конца.
	texts := map[string]string{
		"r1": "Испытания датчика\n\nАннотация испытаний.\n\n" + sentence + " " + filler(60000),
		"r2": filler(30000) + "Обзор датчиков\n\nАннотация обзора. " + filler(30000),
	}
	for _, tc := range []struct {
		name                 string
		limit                int
		used                 string
		truncated, dropped   int
		fullText, onlyTitles int
	}{
		{"truncate", 60000, liveMLSourceOn, 2, 0, 2, 1},
		{"abstract_only", 40000, liveMLSourceOn, 1, 1, 1, 2},
		{"too_small", 200, liveMLSourceOff, 0, 0, 0, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			metadata := map[string]LivePaperMetadata{
				"arxiv:1": {Language: "ru", Abstract: "Аннотация статьи."},
				"rec:r1":  {Language: "ru", Abstract: "Аннотация испытаний."},
				"rec:r2":  {Language: "ru", Abstract: "Аннотация обзора."},
			}
			extract, sent := liveMLFitServer(t, tc.limit)
			umbrella := LiveTopic{Web: &LiveChannel{Records: []LiveSourceRecord{
				{ID: "r1", Title: "Испытания датчика", Text: texts["r1"]}, {ID: "r2", Title: "Обзор датчиков", Text: texts["r2"]},
			}}}
			service := &TrendLiveService{ml: &liveMLSource{version: "test", extract: extract}}
			extracting, run := service.mlStartExtraction(t.Context(), "датчик", &umbrella, documents, metadata)
			if umbrella.Web.Records[0].Text != "" || umbrella.Web.Records[1].Text != "" {
				t.Fatal("исходные тексты остались в записях после сборки пакета")
			}
			technologies := extracting()
			if run.Used != tc.used {
				t.Fatalf("режим %s, откат: %q", run.Used, run.FallbackReason)
			}
			if tc.used != liveMLSourceOn {
				if len(*sent) != 0 || technologies != nil {
					t.Fatal("непомещающийся пакет отправлен")
				}
				return
			}
			got := (*sent)[0]
			raw, err := json.Marshal(got)
			if err != nil || len(*sent) != 1 || len(raw) > tc.limit {
				t.Fatalf("пакет не урезан под предел: %d байт, %d запросов, %v", len(raw), len(*sent), err)
			}
			for _, document := range got.Documents {
				full, abstract := texts[strings.TrimPrefix(document.DocID, "rec:")], metadata[document.DocID].Abstract
				if full != "" && !strings.HasPrefix(full, document.Text) && document.Text != document.Title+"\n\n"+abstract ||
					!strings.Contains(document.Text, abstract) {
					t.Fatalf("%s: текст не префикс исходного или потерял аннотацию: %d знаков",
						document.DocID, utf8.RuneCountInString(document.Text))
				}
				if document.DocID == "rec:r1" && utf8.RuneCountInString(document.Text) > liveMLExtractTextChars {
					t.Fatalf("полный текст r1 не сокращён до предела: %d", utf8.RuneCountInString(document.Text))
				}
			}
			coverage := run.Coverage
			if coverage.DocumentsTextTruncated != tc.truncated || coverage.DocumentsTextDropped != tc.dropped ||
				coverage.DocumentsFullText != tc.fullText || coverage.DocumentsAbstractOnly != tc.onlyTitles ||
				!slices.ContainsFunc(coverage.Notes, func(note string) bool { return strings.Contains(note, "превышал предел") }) ||
				len(technologies) != 1 || technologies[0].ML == nil {
				t.Fatalf("урезание не отмечено в охвате или кандидат потерян: %+v", coverage)
			}
		})
	}
}

func TestLiveMLExtractSelectsClosestDocuments(t *testing.T) {
	abstract := strings.Repeat("Measurements of field noise in laboratory conditions. ", 25)
	documents := make([]StudyPaper, 0, 3621)
	metadata := map[string]LivePaperMetadata{}
	closeness := map[string]int{}
	for i := range 3621 {
		paper := StudyPaper{ID: fmt.Sprintf("arxiv:%d", i), Title: fmt.Sprintf("Unrelated study %d", i),
			Date: fmt.Sprintf("2026-%02d-10", 1+i%9)}
		meta := LivePaperMetadata{Language: "en", Abstract: abstract}
		if i%30 == 0 {
			paper.Title, closeness[paper.ID] = fmt.Sprintf("Quantum sensor pilot %d", i), 2
		}
		if i%3 == 0 {
			meta.Abstract, closeness[paper.ID] = abstract+" A quantum sensor was used.", closeness[paper.ID]+1
		}
		documents, metadata[paper.ID] = append(documents, paper), meta
	}
	umbrella := LiveTopic{Query: LiveQuery{Terms: []string{"quantum sensor"}}}
	for _, limit := range []int{liveClassifierInputBytes, 300 << 10} {
		extract, sent := liveMLFitServer(t, limit)
		service := &TrendLiveService{ml: &liveMLSource{version: "test", extract: extract}}
		extracting, run := service.mlStartExtraction(t.Context(), "квантовые датчики", &umbrella, documents, metadata)
		if extracting(); run.Used != liveMLSourceOn || run.FallbackReason != "" || len(*sent) == 0 {
			t.Fatalf("предел %d: широкий запрос откатился: %q", limit, run.FallbackReason)
		}
		got := (*sent)[len(*sent)-1].Documents
		if len(got) > liveExtractDocuments || run.Coverage.DocumentsOmitted != len(documents)-len(got) {
			t.Fatalf("предел %d: отправлено %d, не отправлено %d", limit, len(got), run.Coverage.DocumentsOmitted)
		}
		for i, document := range got {
			if closeness[document.DocID] == 0 || i > 0 && closeness[document.DocID] > closeness[got[i-1].DocID] {
				t.Fatalf("предел %d: документ %s дальше от запроса, чем неотправленные", limit, document.DocID)
			}
		}
	}
}

func TestLiveMLSearchStoreSeparatesModes(t *testing.T) {
	store := newFakeSearchStore()
	ngrams, ml := storeService(t, store), storeService(t, store)
	ml.SetMLCandidateSource(liveMLSourceOn, nil, "v1")
	pinned, err := normalizeLiveQuery(livePinnedSearches()[0])
	if err != nil {
		t.Fatal(err)
	}
	run := &LiveMLExtractionRun{Requested: liveMLSourceOn, Used: liveMLSourceOn, Version: "v1",
		Coverage: &LiveMLExtractCoverage{}}
	expires := time.Now().Add(time.Hour)
	ngrams.saveSearch(t.Context(), pinned, LiveDetail{Scope: liveFixtureQueryScope(pinned), Topic: LiveTopic{ID: "ngrams"}}, expires)
	ml.saveSearch(t.Context(), pinned, LiveDetail{Scope: liveFixtureQueryScope(pinned), Topic: LiveTopic{ID: "ml"},
		Extraction: run}, expires)
	if _, ok := store.stored[liveMLStoreKey(liveStoreKey(pinned))]; !ok || len(store.stored) != 2 {
		t.Fatalf("режимы затёрли друг друга: %d ключей", len(store.stored))
	}
	for _, service := range []*TrendLiveService{ngrams, ml} {
		want := map[bool]string{false: "ngrams", true: "ml"}[service.ml != nil]
		detail, ok := service.storedSearch(t.Context(), pinned, nil)
		history, err := service.SearchHistory(t.Context(), 0)
		if !ok || detail.Topic.ID != want || err != nil || len(history.Entries) != 1 || history.Entries[0].Query != pinned {
			t.Fatalf("режим %s читает чужой ответ: %+v, история %+v", want, detail.Topic, history.Entries)
		}
	}
	delete(store.stored, liveMLStoreKey(liveStoreKey(pinned)))
	snapshotAt := time.Now().Add(-time.Hour)
	for _, service := range []*TrendLiveService{ngrams, ml} {
		history, err := service.searchStoreRef().ListLiveSearches(t.Context(), analysisdoc.LiveSearchList{})
		if err != nil {
			t.Fatal(err)
		}
		refresh := slices.Contains(liveSearchesToRefresh(history.Entries, snapshotAt, time.Now(), nil), pinned)
		if refresh != (service.ml != nil) {
			t.Fatalf("режим ml=%v: пересборка закреплённого запроса %v", service.ml != nil, refresh)
		}
	}
	ml.SetMLCandidateSource(liveMLSourceOff, nil, "")
	if _, view := ml.searchStoreRef().(liveMLSearchStore); view {
		t.Fatal("возврат к ngrams оставил ключи режима ml")
	}
}
