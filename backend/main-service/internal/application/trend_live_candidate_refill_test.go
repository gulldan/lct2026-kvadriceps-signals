package application

import (
	"context"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"
)

// Два воспроизведения проверяют координатор целиком, сохраняя все границы допуска карточек.
func TestLiveCandidateRefillBeforeOldBarriers(t *testing.T) {
	for _, mode := range []string{"slow_qualification_tail", "possible_fifteen_fail"} {
		t.Run(mode, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				const workers = 32
				const count = workers + liveTopCandidates
				service, candidates, documents, periods := liveWaveFixture(t, count)
				service.SetWorkers(func(context.Context, string, int) int { return workers })
				activity := &livePipelineActivity{}
				attempts := make([]atomic.Int64, count)
				var firstRefill atomic.Int64
				qualify, card := service.qualify, service.card
				service.qualify = func(ctx context.Context, query, name string,
					inputs []LiveQualificationDocument,
				) (LiveQualification, error) {
					index, _ := strconv.Atoi(strings.TrimPrefix(name, "Mechanism"))
					attempts[index].Add(1)
					if index >= workers {
						firstRefill.CompareAndSwap(0, time.Now().UnixNano())
					}
					delay := 10 * time.Second
					if mode == "slow_qualification_tail" && index < workers {
						delay = 2 * time.Second
						if index == 0 {
							delay = 180 * time.Second
						}
					}
					if err := activity.wait(ctx, delay); err != nil {
						return LiveQualification{}, err
					}
					reject := index < workers && (mode == "slow_qualification_tail" || index >= liveTopCandidates)
					if reject {
						return LiveQualification{Technology: false, Model: "fixture", Method: liveQualificationMethod,
							Reason: "Это показатель, а не технический механизм"}, nil
					}
					return qualify(ctx, query, name, inputs)
				}
				service.card = func(ctx context.Context, query string, identity LiveCardIdentity,
					works []string,
				) (LiveCard, error) {
					index, _ := strconv.Atoi(strings.TrimPrefix(identity.Label, "Резонатор Mechanism"))
					delay := 20 * time.Second
					if index < workers {
						delay = 130 * time.Second
					}
					if err := activity.wait(ctx, delay); err != nil {
						return LiveCard{}, err
					}
					result, err := card(ctx, query, identity, works)
					if index < workers {
						result.Summary, result.Missing = "", []string{"описание не подтверждено"}
					}
					return result, err
				}
				service.SetClassifier(func(_ context.Context, input LiveClassifierRequest) (LiveClassifierResponse, error) {
					output := LiveClassifierResponse{FeatureSchema: LiveClassifierSchema}
					for _, candidate := range input.Candidates {
						output.Predictions = append(output.Predictions, livePipelinePrediction(candidate.ID, input.ModelVersion))
					}
					return output, nil
				}, "fixture-v1")
				ctx, cancel := context.WithTimeout(t.Context(), 4*time.Minute)
				defer cancel()
				started, umbrella := time.Now(), LiveTopic{}
				selected, rejected, _ := service.finishCandidates(ctx, "квантовое измерение", &umbrella,
					candidates, documents, nil, periods, nil)
				if len(selected)+len(rejected) != count || activity.active.Load() != 0 || activity.peak.Load() > workers {
					t.Fatalf("accounting or worker bound escaped: selected=%d rejected=%d active=%d peak=%d",
						len(selected), len(rejected), activity.active.Load(), activity.peak.Load())
				}
				for index := range attempts {
					if attempts[index].Load() > 1 {
						t.Fatalf("candidate %d qualified more than once", index)
					}
				}
				for _, candidate := range selected {
					if !liveCardComplete(candidate) || !liveVerifiedMechanism(candidate) || !liveModelDecision(candidate.ModelScore) {
						t.Fatalf("candidate bypassed admission: %s", candidate.ID)
					}
				}
				refillDelay := time.Duration(-1)
				if stamp := firstRefill.Load(); stamp != 0 {
					refillDelay = time.Unix(0, stamp).Sub(started)
				}
				t.Logf("mode=%s selected=%d rejected=%d first_refill=%s elapsed=%s peak=%d",
					mode, len(selected), len(rejected), refillDelay, time.Since(started), activity.peak.Load())
				if refillDelay < 0 || refillDelay >= 20*time.Second || len(selected) != liveTopCandidates {
					t.Fatalf("completed-group/potential-card barrier lost a usable early refill: selected=%d first_refill=%s",
						len(selected), refillDelay)
				}
			})
		})
	}
}

// Четырнадцать готовых карточек ждут признаки без занятого шлюза: следующие тридцать две не урезаются до восемнадцати.
func TestLiveCandidateRefillPreservesFullOverlap(t *testing.T) {
	for _, cancelEarly := range []bool{false, true} {
		t.Run(strconv.FormatBool(cancelEarly), func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				const workers = 32
				service, candidates, documents, periods := liveWaveFixture(t, 64)
				service.SetWorkers(func(context.Context, string, int) int { return workers })
				activity := &livePipelineActivity{}
				var second, observed atomic.Int64
				qualify, card := service.qualify, service.card
				service.qualify = func(ctx context.Context, query, name string,
					inputs []LiveQualificationDocument,
				) (LiveQualification, error) {
					index, _ := strconv.Atoi(strings.TrimPrefix(name, "Mechanism"))
					delay := time.Second
					if index >= workers {
						second.Add(1)
						delay = 100 * time.Second
					}
					if err := activity.wait(ctx, delay); err != nil {
						return LiveQualification{}, err
					}
					if index >= 14 {
						return LiveQualification{Technology: false, Model: "fixture", Method: liveQualificationMethod,
							Reason: "Это показатель, а не технический механизм"}, nil
					}
					return qualify(ctx, query, name, inputs)
				}
				service.card = func(ctx context.Context, query string, identity LiveCardIdentity, works []string) (LiveCard, error) {
					if err := activity.wait(ctx, time.Second); err != nil {
						return LiveCard{}, err
					}
					return card(ctx, query, identity, works)
				}
				service.SetClassifier(func(ctx context.Context, input LiveClassifierRequest) (LiveClassifierResponse, error) {
					timer := time.NewTimer(80 * time.Second)
					defer timer.Stop()
					select {
					case <-timer.C:
						return liveRefillPredictions(input), nil
					case <-ctx.Done():
						return LiveClassifierResponse{}, ctx.Err()
					}
				}, "fixture-v1")
				ctx, cancel := context.WithTimeout(t.Context(), 4*time.Minute)
				defer cancel()
				checked := make(chan struct{})
				go func() {
					time.Sleep(10 * time.Second)
					observed.Store(second.Load())
					if cancelEarly {
						cancel()
					}
					close(checked)
				}()
				umbrella := LiveTopic{}
				selected, rejected, _ := service.finishCandidates(ctx, "квантовое измерение", &umbrella,
					candidates, documents, nil, periods, nil)
				<-checked
				if observed.Load() != workers || second.Load() != workers || activity.peak.Load() > workers ||
					activity.active.Load() != 0 || len(selected)+len(rejected) != len(candidates) {
					t.Fatalf("full overlap or cancellation accounting lost: observed=%d started=%d peak=%d active=%d selected=%d rejected=%d",
						observed.Load(), second.Load(), activity.peak.Load(), activity.active.Load(), len(selected), len(rejected))
				}
				want := 14
				if cancelEarly {
					want = 0
				}
				if strict := len(liveWithoutTopUp(selected)); strict != want {
					t.Fatalf("canceled scores or unfinished cards admitted: got=%d want=%d", strict, want)
				}
			})
		})
	}
}

// Поздний более высокий вариант объединяет две ранние записи: координатор снимает паузу и снова добирает.
func TestLiveCandidateRefillResumesAfterLateDeduplication(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		// Квалификаций не больше 48: свободные вызовы позволяют всем ранним карточкам закончить до мостика.
		service, candidates, documents, periods := liveWaveFixture(t, 64)
		service.SetWorkers(func(context.Context, string, int) int { return 64 })
		activity := &livePipelineActivity{}
		started := time.Now()
		var bridgeFinished, refillStarted atomic.Int64
		qualify, card := service.qualify, service.card
		service.qualify = func(ctx context.Context, query, name string,
			inputs []LiveQualificationDocument,
		) (LiveQualification, error) {
			index, _ := strconv.Atoi(strings.TrimPrefix(name, "Mechanism"))
			delay := time.Second
			if index == 15 {
				delay = 50 * time.Second
			}
			if index >= 16 && index < 63 {
				delay = time.Until(started.Add(90 * time.Second))
			}
			if index == 63 {
				refillStarted.Store(time.Now().UnixNano())
			}
			if err := activity.wait(ctx, delay); err != nil {
				return LiveQualification{}, err
			}
			if index >= 16 && index < 63 {
				return LiveQualification{Technology: false, Model: "fixture", Method: liveQualificationMethod,
					Reason: "Это показатель, а не технический механизм"}, nil
			}
			result, err := qualify(ctx, query, name, inputs)
			switch index {
			case 0:
				result.Label = "Оптический квантовый резонатор"
			case 1:
				result.Label = "Оптический фононный резонатор"
			case 15:
				result.Label = "Оптический резонатор"
			}
			return result, err
		}
		service.card = func(ctx context.Context, query string, identity LiveCardIdentity, works []string) (LiveCard, error) {
			bridge := identity.Label == "Оптический резонатор"
			delay := time.Second
			if bridge {
				delay = 100 * time.Second
			}
			if err := activity.wait(ctx, delay); err != nil {
				return LiveCard{}, err
			}
			if bridge {
				bridgeFinished.Store(time.Now().UnixNano())
			}
			return card(ctx, query, identity, works)
		}
		service.SetClassifier(func(_ context.Context, input LiveClassifierRequest) (LiveClassifierResponse, error) {
			output := liveRefillPredictions(input)
			for i := range output.Predictions {
				if output.Predictions[i].ID == "ngram:mechanism15" {
					output.Predictions[i].Value = .99
				}
			}
			return output, nil
		}, "fixture-v1")
		ctx, cancel := context.WithTimeout(t.Context(), 6*time.Minute)
		defer cancel()
		umbrella := LiveTopic{}
		selected, rejected, _ := service.finishCandidates(ctx, "квантовое измерение", &umbrella,
			candidates, documents, nil, periods, nil)
		if len(selected) != liveTopCandidates || len(selected)+len(rejected) != len(candidates) ||
			bridgeFinished.Load() == 0 || refillStarted.Load() < bridgeFinished.Load() ||
			activity.peak.Load() > 64 || activity.active.Load() != 0 {
			t.Fatalf("late dedup did not resume refill: selected=%d rejected=%d bridge=%s refill=%s peak=%d active=%d",
				len(selected), len(rejected), time.Unix(0, bridgeFinished.Load()).Sub(started),
				time.Unix(0, refillStarted.Load()).Sub(started), activity.peak.Load(), activity.active.Load())
		}
		if selected[0].ID != "ngram:mechanism15" || selected[len(selected)-1].ID != "ngram:mechanism63" {
			t.Fatal("global ranking lost the higher bridge or the late replacement")
		}
	})
}

// Отказ подготовки освобождает кредит так же, как отказ квалификации, даже при единственном месте шлюза.
func TestLiveCandidateRefillReleasesPreparationCredits(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		service, candidates, documents, periods := liveWaveFixture(t, 32)
		service.SetWorkers(func(context.Context, string, int) int { return 1 })
		service.semantic = liveRefillEncoder{}
		activity := &livePipelineActivity{}
		qualify, card := service.qualify, service.card
		var attempts atomic.Int64
		service.qualify = func(ctx context.Context, query, name string,
			inputs []LiveQualificationDocument,
		) (LiveQualification, error) {
			attempts.Add(1)
			if err := activity.wait(ctx, time.Second); err != nil {
				return LiveQualification{}, err
			}
			return qualify(ctx, query, name, inputs)
		}
		service.card = func(ctx context.Context, query string, identity LiveCardIdentity, works []string) (LiveCard, error) {
			if err := activity.wait(ctx, time.Second); err != nil {
				return LiveCard{}, err
			}
			return card(ctx, query, identity, works)
		}
		ctx, cancel := context.WithTimeout(t.Context(), 6*time.Minute)
		defer cancel()
		umbrella := LiveTopic{}
		selected, rejected, _ := service.finishCandidates(ctx, "квантовое измерение", &umbrella,
			candidates, documents, nil, periods, nil)
		if len(selected) != liveTopCandidates || len(rejected) != 17 || attempts.Load() != 32 ||
			activity.active.Load() != 0 || activity.peak.Load() != 1 {
			t.Fatalf("preparation leaked credits: selected=%d rejected=%d attempts=%d active=%d peak=%d",
				len(selected), len(rejected), attempts.Load(), activity.active.Load(), activity.peak.Load())
		}
		for _, row := range rejected {
			if row.Code != liveRejectOffTopic {
				t.Fatalf("preparation refusal changed: %+v", row)
			}
		}
	})
}

type liveRefillEncoder struct{}

func (liveRefillEncoder) Encode(_ context.Context, texts []string) (LiveEmbeddingBatch, error) {
	batch := LiveEmbeddingBatch{Model: "fixture", Revision: "fixed"}
	for _, text := range texts {
		vector := []float64{1, 0}
		if word := strings.Fields(text)[0]; strings.HasPrefix(word, "Mechanism") {
			index, _ := strconv.Atoi(strings.TrimPrefix(word, "Mechanism"))
			if index < 17 {
				vector = []float64{0, 1}
			}
		}
		batch.Vectors = append(batch.Vectors, vector)
	}
	return batch, nil
}

func liveRefillPredictions(input LiveClassifierRequest) LiveClassifierResponse {
	output := LiveClassifierResponse{FeatureSchema: LiveClassifierSchema}
	for _, candidate := range input.Candidates {
		output.Predictions = append(output.Predictions, livePipelinePrediction(candidate.ID, input.ModelVersion))
	}
	return output
}
