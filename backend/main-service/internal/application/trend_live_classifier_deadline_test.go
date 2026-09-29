package application

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"github.com/example/main-service/internal/platform/aiclients"
	"github.com/example/main-service/internal/platform/openalex"
)

// Ожидание места после сбора признаков не превращает здоровую встроенную модель в неизмеренный вход.
func TestEmbeddedClassifierWaitsForSharedSlot(t *testing.T) {
	for _, mode := range []string{"wait_then_rate", "source_timeout", "parent_deadline", "parent_cancel", "external"} {
		t.Run(mode, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				service, candidates, documents, periods := liveWaveFixture(t, 1)
				candidates[0].ID, candidates[0].Name = "ngram:hybrid-kv", "Hybrid KV"
				service.SetWorkers(func(context.Context, string, int) int { return 1 })
				activity := &livePipelineActivity{}
				generator := &liveClassifierDeadlineGenerator{activity: activity}
				history := &liveClassifierHistoryTransport{delay: 5 * time.Second}
				if mode == "source_timeout" {
					history.delay = 120 * time.Second
				}
				prepareEmbeddedDeadline(t, service, periods, history, generator)
				umbrella := LiveTopic{}
				candidates, rejected := qualifyFixtureCandidates(t, service, "квантовое измерение", &umbrella,
					candidates, documents, periods)
				if len(candidates) != 1 || len(rejected) != 0 {
					t.Fatal("fixture qualification failed")
				}
				var externalEnd atomic.Int64
				if mode == "external" {
					service.SetClassifier(func(ctx context.Context, _ LiveClassifierRequest) (LiveClassifierResponse, error) {
						<-ctx.Done()
						externalEnd.Store(time.Now().UnixNano())
						return LiveClassifierResponse{}, fmt.Errorf("external classifier: %w", ctx.Err())
					}, "external-v1")
				}
				card := service.card
				service.card = func(ctx context.Context, query string, identity LiveCardIdentity, works []string) (LiveCard, error) {
					if err := activity.wait(ctx, 120*time.Second); err != nil {
						return LiveCard{}, err
					}
					return card(ctx, query, identity, works)
				}
				budget := liveSearchBudget
				if mode == "parent_deadline" {
					budget = 100 * time.Second
				}
				ctx, cancel := context.WithTimeout(t.Context(), budget)
				defer cancel()
				ctx = context.WithValue(ctx, liveWorkerSlotsKey{}, &liveWaveWorkers{
					calls: service.workerSlots(ctx), ratings: make(liveWorkerSlots, liveRatingParallel),
				})
				if mode == "parent_cancel" {
					go func() { time.Sleep(60 * time.Second); cancel() }()
				}
				started := time.Now()
				classify := service.classifyAsync(ctx, "квантовое измерение", &umbrella, candidates, documents, nil)
				service.writeWave(ctx, "квантовое измерение", candidates, documents, nil)
				selected, rejected := classify(candidates, rejected)
				var score *LiveModelScore
				for _, candidate := range selected {
					score = candidate.ModelScore
				}
				for _, row := range rejected {
					if row.topic != nil && row.topic.ModelScore != nil {
						score = row.topic.ModelScore
					}
				}
				wantCalls, wantTime := int64(0), 120*time.Second
				switch mode {
				case "wait_then_rate", "source_timeout":
					wantCalls, wantTime = 1, 130*time.Second
					if score == nil || !slices.ContainsFunc(score.Features, func(feature LiveModelFeature) bool {
						return strings.Contains(feature.Value, "стадия 1 из 4")
					}) {
						t.Fatalf("healthy maturity response lost while waiting: score present=%t calls=%d", score != nil,
							generator.calls.Load())
					}
				case "parent_deadline":
					wantTime = 100 * time.Second
				case "parent_cancel":
					wantTime = 60 * time.Second
				case "external":
					if time.Unix(0, externalEnd.Load()).Sub(started) != liveClassifierStage {
						t.Fatal("switching from embedded removed the external classifier deadline")
					}
				}
				if wantCalls == 0 && score != nil {
					t.Fatal("canceled classifier published a model score")
				}
				if generator.calls.Load() != wantCalls || time.Since(started) != wantTime ||
					activity.active.Load() != 0 || activity.peak.Load() > 1 {
					t.Fatalf("deadline or shared worker budget escaped: calls=%d elapsed=%s active=%d peak=%d",
						generator.calls.Load(), time.Since(started), activity.active.Load(), activity.peak.Load())
				}
				if mode == "source_timeout" && (time.Unix(0, history.finished.Load()).Sub(started) != liveClassifierStage ||
					!slices.ContainsFunc(score.Features, func(feature LiveModelFeature) bool {
						return strings.Contains(feature.Value, "не измерено")
					})) {
					t.Fatal("source timeout was extended or missing evidence was hidden")
				}
			})
		})
	}
}

func prepareEmbeddedDeadline(t *testing.T, service *TrendLiveService, periods []LivePeriod,
	history *liveClassifierHistoryTransport, generator *liveClassifierDeadlineGenerator,
) {
	t.Helper()
	model, err := LoadWeakSignalModel()
	if err != nil {
		t.Fatal(err)
	}
	service.snapshot = &LiveSnapshot{Periods: periods}
	client, err := openalex.New(openalex.Config{BaseURL: "https://history.invalid", Attempts: 1, Timeout: 3 * time.Minute})
	if err != nil {
		t.Fatal(err)
	}
	client.HTTP.Transport = history
	service.history = client
	month := periods[len(periods)-1].ID
	service.hnCache.known[month+"|hybrid kv"] = &LiveHNCounts{Recent: 2}
	service.semantic = liveClassifierDeadlineEncoder{model.Embedding.Model, model.Embedding.Mean}
	service.SetMaturityRater(NewLiveMaturityRater(generator, func(context.Context) bool { return true }))
	window := make([]StudyPaper, 6)
	for i := range window {
		window[i] = StudyPaper{ID: fmt.Sprintf("window-%d", i), Title: "Hybrid KV cache for phase estimation",
			Date: periods[8+i].Start, Authors: []string{fmt.Sprintf("Lab %d", i)}}
	}
	service.setWindow(window, map[string]LivePaperMetadata{})
	if err := service.UseEmbeddedClassifier(); err != nil {
		t.Fatal(err)
	}
}

type liveClassifierHistoryTransport struct {
	delay    time.Duration
	finished atomic.Int64
}

func (h *liveClassifierHistoryTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	defer func() { h.finished.Store(time.Now().UnixNano()) }()
	timer := time.NewTimer(h.delay)
	defer timer.Stop()
	select {
	case <-timer.C:
		return &http.Response{StatusCode: http.StatusOK, Header: http.Header{}, Request: request,
			Body: io.NopCloser(strings.NewReader(`{"group_by":[{"key":"2025","count":6}]}`))}, nil
	case <-request.Context().Done():
		return nil, fmt.Errorf("history canceled: %w", request.Context().Err())
	}
}

type liveClassifierDeadlineGenerator struct {
	activity *livePipelineActivity
	calls    atomic.Int64
}

func (g *liveClassifierDeadlineGenerator) Generate(ctx context.Context, _ aiclients.GenRequest) (aiclients.GenResponse, error) {
	g.calls.Add(1)
	if err := g.activity.wait(ctx, 10*time.Second); err != nil {
		return aiclients.GenResponse{}, err
	}
	return aiclients.GenResponse{Text: `{"stage":1,"failed_or_faded":false,"confidence":0.8}`}, nil
}

func (g *liveClassifierDeadlineGenerator) Stream(ctx context.Context, request aiclients.GenRequest,
	_ func(string) error,
) (aiclients.GenResponse, error) {
	return g.Generate(ctx, request)
}

type liveClassifierDeadlineEncoder struct {
	model  string
	vector []float64
}

func (e liveClassifierDeadlineEncoder) Encode(_ context.Context, texts []string) (LiveEmbeddingBatch, error) {
	batch := LiveEmbeddingBatch{Model: e.model, Revision: "fixed"}
	for range texts {
		batch.Vectors = append(batch.Vectors, slices.Clone(e.vector))
	}
	return batch, nil
}
