package application

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

const liveMLReviewVersion = "weak-signal-2026-09-23.5"

// Настоящий обработчик Python работает через TestClient, сеть и веса не нужны.
func liveMLReviewHTTP(t *testing.T, fault ...string) (MLCandidateExtractor, *int) {
	t.Helper()
	python := os.Getenv("EXTRACTOR_TEST_PYTHON")
	if python == "" {
		python = "python3"
	}
	root, err := filepath.Abs("../../../../ml-services")
	if err != nil {
		t.Fatal(err)
	}
	// Контекст теста отменяется до cleanup: процесс убило бы раньше, чем закрытый
	// stdin завершит его штатно, и Wait вернул бы ложную ошибку.
	processCtx := context.WithoutCancel(t.Context())
	cmd := exec.CommandContext(processCtx, python, "-m", "extractor.tests.review_http_bridge")
	cmd.Env = append(os.Environ(), "PYTHONPATH="+root, "PYTHONDONTWRITEBYTECODE=1")
	if os.Getenv("EXTRACTOR_TEST_PYTHON") == "" {
		probe := exec.CommandContext(processCtx, python, "-c", "import extractor.tests.review_http_bridge")
		probe.Env = cmd.Env
		if probe.Run() != nil {
			t.Skip("для HTTP-регрессии задайте EXTRACTOR_TEST_PYTHON с зависимостями extractor и httpx")
		}
	}
	if len(fault) > 0 {
		cmd.Env = append(cmd.Env, "EXTRACTOR_REVIEW_FAULT="+fault[0])
	}
	stdin, err := cmd.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = stdin.Close()
		if err := cmd.Wait(); err != nil {
			t.Errorf("Python HTTP: %v: %s", err, stderr.String())
		}
	})
	calls := 0
	var mu sync.Mutex
	reader := bufio.NewReader(stdout)
	server, transport := liveMLTestServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Error(err)
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		request := struct {
			Path string         `json:"path"`
			Body jsontext.Value `json:"body"`
		}{r.URL.Path, body}
		raw, err := json.Marshal(request)
		if err != nil {
			t.Error(err)
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		if _, err = stdin.Write(append(raw, '\n')); err != nil {
			t.Error(err)
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		line, err := reader.ReadBytes('\n')
		if err != nil {
			t.Errorf("Python HTTP: %v", err)
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		var response struct {
			Status  int               `json:"status"`
			Body    jsontext.Value    `json:"body"`
			Headers map[string]string `json:"headers"`
			Calls   int               `json:"stage_calls"`
		}
		if err := json.Unmarshal(line, &response); err != nil {
			t.Error(err)
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		calls = response.Calls
		for key, value := range response.Headers {
			if key != "content-length" {
				w.Header().Set(key, value)
			}
		}
		w.WriteHeader(response.Status)
		_, _ = w.Write(response.Body)
	}))
	return newMLCandidateExtractor(server.URL+"/extract", 0, transport, 32<<20, 16<<20), &calls
}

func TestLiveMLReviewExtractFeedsSharedPath(t *testing.T) {
	extract, calls := liveMLReviewHTTP(t)
	service := &TrendLiveService{now: time.Now, ml: &liveMLSource{extract: extract, version: liveMLReviewVersion}}
	abstract := "Разработан прототип: Фазовый детектор прошёл испытания на полигоне."
	record := LiveSourceRecord{ID: "EP4000001A1", Title: "Фазовый детектор для радиолокации", Date: "2026-08-01",
		Language: "ru", Excerpt: abstract, Text: "Реферат. " + abstract + " Формула изобретения не приводится.",
		URL: "https://worldwide.espacenet.com/patent/search?q=pn%3DEP4000001A1", Source: "epo"}
	umbrella := LiveTopic{ID: "search:детектор", Patents: &LiveChannel{Records: []LiveSourceRecord{record}}}
	documents := []StudyPaper{
		{ID: liveRecordPrefix + record.ID, Title: record.Title, Date: record.Date, URL: record.URL, Authors: []string{"epo"}},
		{ID: "arxiv:1", Title: "Фазовый детектор в связи", Date: "2026-09-01", Authors: []string{"a"}},
		{ID: "arxiv:2", Title: "Квантовый датчик", Date: "2026-09-02", Authors: []string{"b"}},
	}
	metadata := map[string]LivePaperMetadata{
		"arxiv:1": {Language: "ru", Abstract: "Разработан Фазовый детектор: прототип."},
		"arxiv:2": {Language: "ru", Abstract: "Разработан Квантовый датчик: прототип."},
	}
	livePrepareAnswerMetadata([]LiveTopic{umbrella}, documents, metadata)
	extracting, run := service.mlStartExtraction(t.Context(), "детектор", &umbrella, documents, metadata)
	found := extracting()
	if run.Used != liveMLSourceOn || run.Coverage.Degraded || run.Coverage.DocumentsFailed != 0 ||
		run.Coverage.DocumentsFullText != 1 || *calls == 0 || len(found) != 2 {
		t.Fatalf("извлечение деградировало: %+v, технологий %d", run.Coverage, len(found))
	}
	candidates, rejected := service.extractCandidates(umbrella, found, documents, metadata, livePeriods(time.Now()))
	if len(candidates) != 1 || candidates[0].Label != "Фазовый детектор" || candidates[0].ML == nil ||
		candidates[0].DocumentsCount != 2 || len(rejected) != 1 || rejected[0].Code != liveRejectThin || rejected[0].ML == nil {
		t.Fatalf("технологии /extract не прошли общие правила: %+v, отказы %+v", candidates, rejected)
	}
}

func TestLiveMLReviewInfrastructureFallback(t *testing.T) {
	for fault, status := range map[string]int{"span_storage": 503, "cache": 422, "response": 413} {
		t.Run(fault, func(t *testing.T) {
			extract, _ := liveMLReviewHTTP(t, fault)
			service, _ := liveRetryService(t)
			service.SetMLCandidateSource(liveMLSourceOn, extract, liveMLReviewVersion)
			docs := []StudyPaper{{ID: "review", Title: "Фазовый детектор", Date: "2026-09-01"}}
			metadata := map[string]LivePaperMetadata{"review": {Language: "ru", Abstract: "Разработан Фазовый детектор: прототип."}}
			umbrella := LiveTopic{}
			extracting, run := service.mlStartExtraction(t.Context(), "детектор", &umbrella, docs, metadata)
			if found := extracting(); len(found) != 0 || run.Used != liveMLSourceOff ||
				!strings.Contains(run.FallbackReason, fmt.Sprintf("HTTP %d", status)) {
				t.Fatalf("нет контрактного отката: %+v", run)
			}
		})
	}
}
