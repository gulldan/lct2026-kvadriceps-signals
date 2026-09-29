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

// Воздержание сохраняет карточку для эксперта, но не останавливает добор решений из оставшегося пула.
func TestLiveCandidateModelDeficit(t *testing.T) {
	for _, mode := range []string{"complete", "abstention", "weak_with_gap", "diagnostics_only", "reserve"} {
		t.Run(mode, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				service, candidates, documents, periods := liveWaveFixture(t, 30)
				service.SetWorkers(func(context.Context, string, int) int { return 15 })
				qualify := service.qualify
				var attempts atomic.Int64
				service.qualify = func(ctx context.Context, query, name string,
					inputs []LiveQualificationDocument,
				) (LiveQualification, error) {
					attempts.Add(1)
					delay := time.Second
					if mode == "reserve" {
						delay = 61 * time.Second
					}
					time.Sleep(delay)
					return qualify(ctx, query, name, inputs)
				}
				service.SetClassifier(func(_ context.Context, input LiveClassifierRequest) (LiveClassifierResponse, error) {
					response := LiveClassifierResponse{FeatureSchema: LiveClassifierSchema}
					for _, candidate := range input.Candidates {
						prediction := livePipelinePrediction(candidate.ID, input.ModelVersion)
						index, _ := strconv.Atoi(strings.TrimPrefix(candidate.ID, "ngram:mechanism"))
						if index < 15 && mode != "complete" || mode == "diagnostics_only" {
							prediction.DataGap, prediction.Value = liveGapNotCollected, .95
							if mode != "weak_with_gap" {
								prediction.Label = liveClassifierInsufficient
							}
						}
						response.Predictions = append(response.Predictions, prediction)
					}
					return response, nil
				}, "fixture-v1")
				budget := liveSearchBudget
				if mode == "reserve" {
					budget = 3 * time.Minute
				}
				ctx, cancel := context.WithTimeout(t.Context(), budget)
				defer cancel()
				started, umbrella := time.Now(), LiveTopic{}
				selected, rejected, _ := service.finishCandidates(ctx, "квантовое измерение", &umbrella,
					candidates, documents, nil, periods, nil)
				decisions, unattempted := 0, 0
				for _, candidate := range selected {
					if !liveCardComplete(candidate) || !liveVerifiedMechanism(candidate) {
						t.Fatal("card or entity admission was bypassed")
					}
					score := candidate.ModelScore
					if score != nil && score.Label == liveClassifierWeak && score.DataGap == "" && score.Value >= score.Threshold {
						decisions++
					}
				}
				for _, row := range rejected {
					if row.Code == liveRejectBudget {
						unattempted++
						if mode == "reserve" && strings.Contains(row.Reason, "уже отобраны") {
							t.Error("budget exhaustion was described as a completed goal")
						}
					}
					if mode != "complete" && row.ModelValue != nil {
						t.Error("unmeasured diagnostic was published as a measured rejected score")
					}
				}
				wantAttempts, wantDecisions := int64(30), 15
				if mode == "complete" {
					// Уже начатые проверки завершаются; возможные пятнадцать не останавливают ограниченный добор.
					wantAttempts = attempts.Load()
					if wantAttempts < liveTopCandidates || wantAttempts > 30 {
						t.Fatalf("completed answer escaped the finite queue: %d", wantAttempts)
					}
				}
				if mode == "reserve" {
					wantAttempts = 15
				}
				if mode == "diagnostics_only" || mode == "reserve" {
					wantDecisions = 0
				}
				if len(selected) != liveTopCandidates || decisions != wantDecisions || attempts.Load() != wantAttempts ||
					len(selected)+len(rejected) != len(candidates) {
					t.Fatalf("wrong model deficit: selected=%d decisions=%d attempted=%d unattempted=%d elapsed=%s",
						len(selected), decisions, attempts.Load(), unattempted, time.Since(started))
				}
				detail := LiveDetail{Candidates: selected, ClassifierVersion: "fixture-v1",
					Topic: LiveTopic{Limits: []LiveLimit{{Code: liveLimitModelFailed}}}}
				if liveRecoveredModelAnswer(detail) != (decisions == liveTopCandidates) ||
					liveUsableStored(detail) != (decisions == liveTopCandidates) {
					t.Fatal("cache recovery does not require fifteen model decisions")
				}
				t.Logf("selected=%d decisions=%d attempted=%d unattempted=%d elapsed=%s",
					len(selected), decisions, attempts.Load(), unattempted, time.Since(started))
			})
		})
	}
}
