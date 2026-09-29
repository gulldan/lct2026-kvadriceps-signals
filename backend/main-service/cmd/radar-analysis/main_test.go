package main

import (
	"bytes"
	"context"
	"encoding/json/v2"
	"github.com/example/main-service/internal/platform/openalex"
	"net"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/example/main-service/internal/application"
	"github.com/example/main-service/internal/infrastructure/trendsemantic"
)

func TestAnalysisExternalCloseStopsWithLiveParent(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	started := make(chan struct{})
	server := &http.Server{
		Addr: "127.0.0.1:0", ReadHeaderTimeout: time.Second,
		BaseContext: func(net.Listener) context.Context {
			close(started)
			return ctx
		},
	}
	defer func() { _ = server.Close() }()
	done := make(chan error, 1)
	go func() { done <- serveAnalysis(ctx, server) }()
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("analysis server did not start")
	}
	if err := server.Close(); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-done:
		if err != nil || ctx.Err() != nil {
			t.Fatalf("external close must stop without canceling parent: serve=%v parent=%v", err, ctx.Err())
		}
	case <-time.After(time.Second):
		t.Fatal("external close left shutdown waiting for parent cancellation")
	}
}

func TestAnalysisGuardExcludesOtherWorkAndReleasesSlot(t *testing.T) {
	busy := make(chan struct{}, 1)
	entered, unblock := make(chan struct{}), make(chan struct{})
	release := sync.OnceFunc(func() { close(unblock) })
	defer release()
	first := exclusiveAnalysis(busy, func(w http.ResponseWriter, _ *http.Request) {
		close(entered)
		<-unblock
		w.WriteHeader(http.StatusNoContent)
	})
	second := exclusiveAnalysis(busy, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusAccepted)
	})
	var group sync.WaitGroup
	group.Go(func() {
		first.ServeHTTP(httptest.NewRecorder(), httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/v1/analyze", nil))
	})
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("first operation did not acquire the slot")
	}
	response := httptest.NewRecorder()
	second.ServeHTTP(response, httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/v1/evaluate", nil))
	release()
	group.Wait()
	if response.Code != http.StatusConflict {
		t.Fatalf("concurrent evaluation = %d, want conflict", response.Code)
	}
	response = httptest.NewRecorder()
	second.ServeHTTP(response, httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/v1/evaluate", nil))
	if response.Code != http.StatusAccepted {
		t.Fatalf("completed analysis retained the slot: %d", response.Code)
	}
}

func TestAnalysisHTTPEnrichesWithoutInventingUnavailableModel(t *testing.T) {
	input := application.LiveAnalysisRequest{Snapshot: application.LiveSnapshot{
		Periods: []application.LivePeriod{{Start: "2026-08-01", EndExclusive: "2026-09-11"}},
		Topics:  []application.LiveTopic{{ID: "one"}},
	}}
	body, _ := json.Marshal(input)
	server := analysisHandler(nil)
	response := httptest.NewRecorder()
	server.ServeHTTP(response, httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/v1/analyze", bytes.NewReader(body)))
	var result application.LiveAnalysisResult
	if response.Code != http.StatusOK || json.Unmarshal(response.Body.Bytes(), &result) != nil ||
		result.Version != application.LiveAnalysisVersion || result.Topics["one"].Evidence.IndependentImplementations != nil {
		t.Fatalf("invalid response %d %s", response.Code, response.Body.String())
	}
	input.Discover = true
	body, _ = json.Marshal(input)
	response = httptest.NewRecorder()
	server.ServeHTTP(response, httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/v1/analyze", bytes.NewReader(body)))
	if response.Code != http.StatusBadGateway {
		t.Fatal("discovery silently used a nonsemantic fallback")
	}
}

func TestEvidenceOnlyHTTPDoesNotCallOpenAlexOrDiscovery(t *testing.T) {
	calls := 0
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls++
		http.Error(w, "unexpected external call", http.StatusInternalServerError)
	}))
	defer provider.Close()
	input := application.LiveAnalysisRequest{EvidenceOnly: true, EnrichMetadata: true, Discover: true,
		Snapshot: application.LiveSnapshot{
			Periods: []application.LivePeriod{{Start: "2026-08-01", EndExclusive: "2099-01-01"}},
			Papers:  []application.StudyPaper{{ID: "p", Title: "Paper", Date: "2026-08-01"}},
			Topics:  []application.LiveTopic{{ID: "a", PaperIDs: []string{"p"}}},
		},
		Metadata: map[string]application.LivePaperMetadata{"p": {DOI: "10.1/paper"}}}
	body, _ := json.Marshal(input)
	response := httptest.NewRecorder()
	request := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/v1/analyze", bytes.NewReader(body))
	analysisHandler(nil, trendsemantic.NewOpenAlex(testOpenAlex(t, provider.URL))).ServeHTTP(response, request)
	var result application.LiveAnalysisResult
	if response.Code != http.StatusOK || json.Unmarshal(response.Body.Bytes(), &result) != nil ||
		calls != 0 || result.Metadata != nil || len(result.Evidence) != 1 {
		t.Fatalf("not evidence-only: %d %s", response.Code, response.Body.String())
	}
}

func TestLiveMetadataEnrichmentRecordsItsOwnObservationTime(t *testing.T) {
	var calls atomic.Int64
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		_, _ = w.Write([]byte(`{"id":"https://openalex.org/W1","doi":"https://doi.org/10.1/paper",` +
			`"authorships":[{"author":{"id":"https://openalex.org/A1"},` +
			`"institutions":[{"id":"https://openalex.org/I1"}]}]}`))
	}))
	defer provider.Close()
	now := time.Now().UTC()
	input := application.LiveAnalysisRequest{EnrichMetadata: true,
		Snapshot: application.LiveSnapshot{ObservedAt: now.Add(-time.Minute).Format(time.RFC3339Nano),
			Periods: []application.LivePeriod{{
				Start: now.AddDate(0, -1, 0).Format(time.DateOnly), EndExclusive: now.Format(time.DateOnly),
			}},
			Papers: []application.StudyPaper{{ID: "p", Title: "Paper", Date: now.AddDate(0, 0, -2).Format(time.DateOnly)}},
			Topics: []application.LiveTopic{{ID: "a", PaperIDs: []string{"p"}}}},
		Metadata: map[string]application.LivePaperMetadata{"p": {DOI: "10.1/paper"}}}
	handler := analysisHandler(nil, trendsemantic.NewOpenAlex(testOpenAlex(t, provider.URL)))
	for _, strict := range []bool{false, true} {
		input.StrictAvailability = strict
		body, _ := json.Marshal(input)
		response := httptest.NewRecorder()
		request := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/v1/analyze", bytes.NewReader(body))
		handler.ServeHTTP(response, request)
		var result application.LiveAnalysisResult
		if response.Code != http.StatusOK || json.Unmarshal(response.Body.Bytes(), &result) != nil {
			t.Fatalf("failed evidence enrichment: %d %s", response.Code, response.Body.String())
		}
		evidence := result.Topics["a"].Evidence
		observation, err := time.Parse(time.RFC3339Nano, evidence.ObservedAt)
		if err != nil || calls.Load() != 1 {
			t.Fatal("missing observation time or strict history contacted the current provider")
		}
		if strict {
			if evidence.AttributedPapers != 0 || !observation.Before(now) {
				t.Fatal("strict historical analysis admitted current provider metadata")
			}
		} else if evidence.AttributedPapers != 1 || !observation.After(now) || len(result.Metadata["p"].Authorships) != 1 {
			t.Fatal("live analysis excluded freshly collected evidence")
		}
	}
}

func testOpenAlex(t *testing.T, base string) *openalex.Client {
	t.Helper()
	client, err := openalex.New(openalex.Config{BaseURL: base})
	if err != nil {
		t.Fatal(err)
	}
	client.Sleep = func(context.Context, time.Duration) error { return nil }
	return client
}
