package application

import (
	"bytes"
	"context"
	"encoding/json/v2"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"slices"
	"strings"
	"testing"
	"time"
)

func TestLiveMLP0ChannelText(t *testing.T) {
	var record LiveSourceRecord
	raw := `{"id":"p0","title":"Фазовый детектор","text":"Полный текст с пилотом.","text_status":"full_text"}`
	if err := json.Unmarshal([]byte(raw), &record); err != nil {
		t.Fatal(err)
	}
	topic := LiveTopic{Web: &LiveChannel{Records: []LiveSourceRecord{record}}}
	documents := []StudyPaper{{ID: "rec:p0", Title: record.Title}}
	metadata := map[string]LivePaperMetadata{}
	livePrepareAnswerMetadata([]LiveTopic{topic}, documents, metadata)
	input := liveMLExtractInput("детектор", "version", documents, metadata, liveMLRecordTexts(topic))
	// Заголовка в исходнике нет: теггер найдёт его перед текстом записи.
	if text := input.Documents[0].Text; !strings.HasPrefix(text, record.Title+"\n\n") || !strings.HasSuffix(text, record.Text) {
		t.Fatalf("текст канала не дошёл до извлечения: %q", text)
	}
	var queries []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		queries = append(queries, r.URL.RawQuery)
		text := ""
		if r.URL.Query().Get("full_text") == "1" {
			text = strings.Repeat("x", 9<<20)
		}
		_, _ = io.WriteString(w, `{"counts":[1],"records":[{"id":"r","title":"t","text":"`+text+`"}]}`)
	}))
	defer server.Close()
	src := NewTrendLiveService(nil, nil).newSources()
	for _, service := range []*TrendLiveService{{now: time.Now}, {now: time.Now, ml: &liveMLSource{}}} {
		channel, err := src.queryChannel(service.liveMLContext(t.Context()), server.URL, []byte(`{}`), 1)
		if err != nil || len(channel.Records) != 1 || (len(channel.Records[0].Text) == 9<<20) != (service.ml != nil) {
			t.Fatalf("режим %v: %v", service.ml != nil, err)
		}
	}
	if !slices.Equal(queries, []string{"", "full_text=1"}) {
		t.Fatalf("параметр полного текста: %q", queries)
	}
}

func TestLiveMLChannelFullTextFallsBackWithoutText(t *testing.T) {
	var queries []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		queries = append(queries, r.URL.RawQuery)
		if r.URL.Query().Get("full_text") == "1" {
			// Так выглядит ответ, обрезанный пределом чтения.
			_, _ = io.WriteString(w, `{"counts":[1],"records":[{"id":"r","title":"t","text":"обрыв`)
			return
		}
		_, _ = io.WriteString(w, `{"counts":[1],"records":[{"id":"r","title":"t"}]}`)
	}))
	defer server.Close()
	src := NewTrendLiveService(nil, nil).newSources()
	service := &TrendLiveService{now: time.Now, ml: &liveMLSource{}}
	channel, err := src.queryChannel(service.liveMLContext(t.Context()), server.URL, []byte(`{}`), 1)
	if err != nil || len(channel.Records) != 1 || channel.Records[0].Text != "" ||
		!slices.Equal(queries, []string{"full_text=1", ""}) {
		t.Fatalf("канал потерян или повтор не без текста: %v, %q", err, queries)
	}
	deadline := fmt.Errorf("query source channel: %w", &url.Error{Op: "Post", URL: server.URL, Err: context.DeadlineExceeded})
	if !liveMLChannelRetryable(deadline) || liveMLChannelRetryable(errors.New("HTTP 502")) {
		t.Fatal("повтор без текста не только по размеру или сроку")
	}
}

func TestLiveMLSourceWithoutURLOrVersionIsLogged(t *testing.T) {
	var logs bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&logs, nil)))
	t.Cleanup(func() { slog.SetDefault(previous) })
	extract := MLCandidateExtractor(func(context.Context, LiveMLExtractRequest) (LiveMLExtractResponse, error) {
		return LiveMLExtractResponse{}, nil
	})
	for _, tc := range []struct {
		extract MLCandidateExtractor
		version string
		logged  bool
	}{{nil, "v1", true}, {extract, " ", true}, {extract, "v1", false}} {
		logs.Reset()
		(&TrendLiveService{}).SetMLCandidateSource(liveMLSourceOn, tc.extract, tc.version)
		if got := strings.Contains(logs.String(), "level=ERROR"); got != tc.logged {
			t.Fatalf("адрес=%v версия=%q: запись в журнале %v", tc.extract != nil, tc.version, got)
		}
	}
}

func TestLiveMLTrustKeepsUnknownSeparateFromLow(t *testing.T) {
	papers := []StudyPaper{
		{ID: "missing", Title: "Prototype detector"},
		{ID: "unknown", Title: "Prototype detector", URL: "https://unlisted.test/paper"},
		{ID: "blog", Title: "Prototype detector", URL: "https://habr.com/ru/articles/1/"},
		{ID: "preprint", Title: "Prototype detector", URL: "https://arxiv.org/abs/2601.00001"},
		{ID: "explicit-low", Title: "Unreliable detector"},
	}
	metadata := map[string]LivePaperMetadata{}
	stampPaperPassports(papers, metadata)
	metadata["explicit-low"] = LivePaperMetadata{PublisherType: PublisherUnknown, Trust: "low"}
	wanted := map[string]string{"missing": "unknown", "unknown": "unknown", "blog": "low",
		"preprint": "medium", "explicit-low": "low"}
	extracted := liveMLExtractInput("detector", "version", papers, metadata, nil)
	ids := make([]string, 0, len(papers))
	for _, doc := range extracted.Documents {
		ids = append(ids, doc.DocID)
		if doc.TrustLevel != wanted[doc.DocID] {
			t.Fatalf("извлечение подменило доверие %s: %s", doc.DocID, doc.TrustLevel)
		}
	}
	if len(ids) != len(papers) {
		t.Fatalf("документы потеряны: %v", ids)
	}
}
