package application

import (
	"context"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"github.com/example/main-service/internal/platform/aiclients"
)

// Пока девять карточек читаются ещё 130 секунд, свободные места проверяют следующую группу.
// Возможные пятнадцать не блокируют добор; отмена дожидается начатых работ и освобождает общий предел.
func TestLiveCandidatePipelineOverlapsWaves(t *testing.T) {
	for _, mode := range []string{"overlap", "potential_top15", "cancel"} {
		t.Run(mode, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				const workers = 16
				service, candidates, documents, periods := liveWaveFixture(t, 48)
				service.SetWorkers(func(context.Context, string, int) int { return workers })
				activity := &livePipelineActivity{}
				attempts := make([]atomic.Int64, len(candidates))
				completed := make([]atomic.Bool, len(candidates))
				var secondStarted atomic.Int64
				qualify, card := service.qualify, service.card
				service.qualify = func(ctx context.Context, query, name string,
					inputs []LiveQualificationDocument,
				) (LiveQualification, error) {
					index, _ := strconv.Atoi(strings.TrimPrefix(name, "Mechanism"))
					attempts[index].Add(1)
					if index >= workers {
						secondStarted.CompareAndSwap(0, time.Now().UnixNano())
					}
					if err := activity.wait(ctx, 10*time.Second); err != nil {
						return LiveQualification{}, err
					}
					if index >= 9 && index < workers && mode != "potential_top15" || index >= 22 {
						return LiveQualification{Technology: false, Model: "fixture", Method: liveQualificationMethod,
							Reason: "Это показатель, а не технический механизм"}, nil
					}
					return qualify(ctx, query, name, inputs)
				}
				service.card = func(ctx context.Context, query string, identity LiveCardIdentity, works []string) (LiveCard, error) {
					index, _ := strconv.Atoi(strings.TrimPrefix(identity.Label, "Резонатор Mechanism"))
					delay := 20 * time.Second
					if index < workers {
						delay = 130 * time.Second
					}
					if err := activity.wait(ctx, delay); err != nil {
						return LiveCard{}, err
					}
					completed[index].Store(true)
					return card(ctx, query, identity, works)
				}
				budget := 3 * time.Minute
				if mode == "potential_top15" {
					budget = liveSearchBudget
				}
				ctx, cancel := context.WithTimeout(t.Context(), budget)
				defer cancel()
				if mode == "cancel" {
					go func() { time.Sleep(40 * time.Second); cancel() }()
				}
				started, umbrella := time.Now(), LiveTopic{}
				selected, rejected, _ := service.finishCandidates(ctx, "квантовое измерение", &umbrella,
					candidates, documents, nil, periods, nil)
				if len(selected)+len(rejected) != len(candidates) || activity.active.Load() != 0 ||
					activity.peak.Load() > workers {
					t.Fatalf("work lost or global bound escaped: selected=%d rejected=%d active=%d peak=%d",
						len(selected), len(rejected), activity.active.Load(), activity.peak.Load())
				}
				// Добор до пятнадцати проверяется отдельно: здесь — только прошедшие все проверки.
				selected = liveWithoutTopUp(selected)
				if mode != "cancel" && len(selected) != liveTopCandidates {
					t.Fatalf("late cards held the next qualification: selected=%d elapsed=%s", len(selected), time.Since(started))
				}
				// При шестнадцати писателях все места могут быть заняты: добор ждёт место, а не возможный результат.
				if secondStarted.Load() == 0 || mode != "potential_top15" &&
					secondStarted.Load() >= started.Add(140*time.Second).UnixNano() {
					t.Fatalf("next qualification waited for old writers: started=%s",
						time.Unix(0, secondStarted.Load()).Sub(started))
				}
				for i := range candidates {
					if attempts[i].Load() > 1 {
						t.Fatalf("candidate %d repeated: %d", i, attempts[i].Load())
					}
				}
				for _, candidate := range selected {
					index, _ := strconv.Atoi(strings.TrimPrefix(candidate.Name, "Mechanism"))
					if !completed[index].Load() || !liveCardComplete(candidate) || !liveVerifiedMechanism(candidate) {
						t.Fatalf("unfinished candidate admitted: %s", candidate.ID)
					}
				}
				if mode == "overlap" && (selected[0].Name != "Mechanism00" || selected[14].Name != "Mechanism21") {
					t.Fatalf("completion speed changed the final order: first=%s last=%s", selected[0].Name, selected[14].Name)
				}
				t.Logf("selected=%d peak=%d elapsed=%s next_wave_started=%t", len(selected), activity.peak.Load(),
					time.Since(started), secondStarted.Load() != 0)
			})
		})
	}
}

// Медленная проверка не удерживает готовые карточки, их модельные оценки и профили общей статьи.
func TestLiveCandidatePipelineKeepsReadyCards(t *testing.T) {
	for _, mode := range []string{"complete", "deadline", "cancel", "cancel_writing", "one_worker"} {
		t.Run(mode, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				service, candidates, documents, periods := liveWaveFixture(t, 3)
				workers := 3
				if mode == "one_worker" {
					workers = 1
				}
				service.SetWorkers(func(context.Context, string, int) int { return workers })
				service.semantic = livePipelineEncoder{}
				candidates[1].PaperIDs = slices.Clone(candidates[0].PaperIDs)
				documents[0].Title += ". Mechanism01 measures the phase with an independent optical circuit."
				activity := &livePipelineActivity{}
				qualify, card := service.qualify, service.card
				service.qualify = func(ctx context.Context, query, name string,
					inputs []LiveQualificationDocument,
				) (LiveQualification, error) {
					delay := 5 * time.Second
					if name == "Mechanism02" && mode != "one_worker" {
						delay = 120 * time.Second
						if mode == "deadline" {
							delay = 240 * time.Second
						}
					}
					if err := activity.wait(ctx, delay); err != nil {
						return LiveQualification{}, err
					}
					result, err := qualify(ctx, query, name, inputs)
					quote := name + " converts quantum phase into measured displacement"
					if name == "Mechanism01" {
						quote = "Mechanism01 measures the phase with an independent optical circuit"
					}
					result.Evidence[0].Quote = quote
					result.Evidence[0].MechanismQuote = quote
					result.Evidence[0].RelevanceQuote = quote
					return result, err
				}
				var firstCard atomic.Int64
				service.card = func(ctx context.Context, query string, identity LiveCardIdentity, works []string) (LiveCard, error) {
					firstCard.CompareAndSwap(0, time.Now().UnixNano())
					delay := 20 * time.Second
					if mode == "cancel_writing" {
						delay = 120 * time.Second
					}
					if err := activity.wait(ctx, delay); err != nil {
						return LiveCard{}, err
					}
					return card(ctx, query, identity, works)
				}
				rater := NewLiveMaturityRater(livePipelineGenerator{activity}, func(context.Context) bool { return true })
				service.SetClassifier(func(ctx context.Context, input LiveClassifierRequest) (LiveClassifierResponse, error) {
					output := LiveClassifierResponse{FeatureSchema: LiveClassifierSchema}
					for _, candidate := range input.Candidates {
						if _, err := rater.Rate(ctx, candidate.ID, LiveRatingEvidence{Total: 1}); err != nil {
							return output, err
						}
						output.Predictions = append(output.Predictions, livePipelinePrediction(candidate.ID, input.ModelVersion))
					}
					return output, nil
				}, "fixture-v1")
				budget := 3 * time.Minute
				if mode == "deadline" {
					budget = 150 * time.Second
				}
				ctx, cancel := context.WithTimeout(t.Context(), budget)
				defer cancel()
				if mode == "cancel" || mode == "cancel_writing" {
					go func() { time.Sleep(60 * time.Second); cancel() }()
				}
				started, umbrella := time.Now(), LiveTopic{}
				all, rejected, _ := service.finishCandidates(ctx, "квантовое измерение", &umbrella,
					candidates, documents, map[string]LivePaperMetadata{}, periods, nil)
				selected, _ := liveSelectCandidates(liveWithoutTopUp(all), nil)
				want := 2
				if mode == "complete" || mode == "one_worker" {
					want = 3
				}
				if mode == "cancel_writing" {
					want = 0
				}
				if len(selected) != want || len(all)+len(rejected) != 3 || activity.active.Load() != 0 ||
					activity.peak.Load() > int64(workers) || firstCard.Load() == 0 ||
					firstCard.Load() >= started.Add(120*time.Second).UnixNano() {
					t.Fatalf("ready cards blocked or work leaked: selected=%d rejected=%d active=%d peak=%d first_card=%s",
						len(selected), len(rejected), activity.active.Load(), activity.peak.Load(),
						time.Unix(0, firstCard.Load()).Sub(started))
				}
				for _, candidate := range selected {
					if !liveCardComplete(candidate) || candidate.ModelScore == nil || candidate.mechanismProfile == nil {
						t.Fatalf("readiness lost a strict gate: %s", candidate.ID)
					}
				}
				if want > 0 && (selected[0].Name != "Mechanism00" || selected[1].Name != "Mechanism01") {
					t.Fatalf("completion order changed final ranking: %+v", selected)
				}
			})
		})
	}
}

type livePipelineActivity struct{ active, peak atomic.Int64 }

func (a *livePipelineActivity) wait(ctx context.Context, delay time.Duration) error {
	active := a.active.Add(1)
	defer a.active.Add(-1)
	for peak := a.peak.Load(); active > peak; peak = a.peak.Load() {
		if a.peak.CompareAndSwap(peak, active) {
			break
		}
	}
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-timer.C:
		return nil
	case <-ctx.Done():
		return fmt.Errorf("pipeline call canceled: %w", ctx.Err())
	}
}

type livePipelineGenerator struct{ activity *livePipelineActivity }

func (g livePipelineGenerator) Generate(ctx context.Context, _ aiclients.GenRequest) (aiclients.GenResponse, error) {
	if err := g.activity.wait(ctx, 10*time.Second); err != nil {
		return aiclients.GenResponse{}, err
	}
	return aiclients.GenResponse{Text: `{"stage":1,"failed_or_faded":false,"confidence":0.5}`}, nil
}

func (g livePipelineGenerator) Stream(ctx context.Context, request aiclients.GenRequest,
	_ func(string) error,
) (aiclients.GenResponse, error) {
	return g.Generate(ctx, request)
}

type livePipelineEncoder struct{}

func (livePipelineEncoder) Encode(_ context.Context, texts []string) (LiveEmbeddingBatch, error) {
	batch := LiveEmbeddingBatch{Model: "fixture", Revision: "fixed"}
	for _, text := range texts {
		vector := []float64{1, 1, 1}
		if strings.HasPrefix(text, "Преобразование квантовой фазы в смещение Mechanism") {
			vector = []float64{0, 0, 0}
			vector[int(text[len(text)-1]-'0')] = 1
		}
		batch.Vectors = append(batch.Vectors, vector)
	}
	return batch, nil
}

func livePipelinePrediction(id, version string) LiveClassifierPrediction {
	return LiveClassifierPrediction{ID: id, LiveModelScore: LiveModelScore{Value: .9, Threshold: .4,
		Label: liveClassifierWeak, Model: "fixture", Version: version, FeatureSchema: LiveClassifierSchema,
		Basis: "Проверка признаков", Features: []LiveModelFeature{{Name: "Источники", Value: "1"}}}}
}
