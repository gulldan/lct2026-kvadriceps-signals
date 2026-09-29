package application

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"
)

// Поздние прямые сущности не теряются после шестидесятой попытки, а размер волны учитывает мощность.
func TestLiveCandidateWavesUseWorkersToFillLateDeficit(t *testing.T) {
	for _, workers := range []int{48, 16, 96} {
		t.Run(strconv.Itoa(workers), func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				service, candidates, documents, periods := liveWaveFixture(t, 80)
				service.SetWorkers(func(context.Context, string, int) int { return workers })
				for i := range candidates {
					candidates[i].ID = liveExtractPrefix + strings.ToLower(candidates[i].Name)
				}
				attempts, cardStarted := make([]atomic.Int64, len(candidates)), make([]atomic.Int64, len(candidates))
				qualificationStarted := make([]atomic.Int64, len(candidates))
				activity := &livePipelineActivity{}
				qualify, card := service.qualify, service.card
				service.qualify = func(ctx context.Context, query, name string,
					inputs []LiveQualificationDocument,
				) (LiveQualification, error) {
					index, _ := strconv.Atoi(strings.TrimPrefix(name, "Mechanism"))
					attempts[index].Add(1)
					qualificationStarted[index].Store(time.Now().UnixNano())
					if err := activity.wait(ctx, 45*time.Second); err != nil {
						return LiveQualification{}, err
					}
					return qualify(ctx, query, name, inputs)
				}
				service.card = func(ctx context.Context, query string, identity LiveCardIdentity, works []string) (LiveCard, error) {
					index, _ := strconv.Atoi(strings.TrimPrefix(identity.Label, "Резонатор Mechanism"))
					cardStarted[index].Store(time.Now().UnixNano())
					if err := activity.wait(ctx, 70*time.Second); err != nil {
						return LiveCard{}, err
					}
					result, err := card(ctx, query, identity, works)
					if index >= 6 && index < 65 || index >= 74 {
						result.Summary, result.Missing = "", []string{"описание не подтверждено"}
					}
					return result, err
				}
				var expanded atomic.Int64
				service.extract = func(context.Context, CandidateExtractionRequest) ([]LiveExtractedTechnology, error) {
					expanded.Add(1)
					return nil, nil
				}
				ctx, cancel := context.WithTimeout(t.Context(), liveSearchBudget)
				defer cancel()
				started, umbrella := time.Now(), LiveTopic{}
				input := append(slices.Clone(candidates), candidates[0], candidates[65])
				selected, rejected, _ := service.finishCandidates(ctx, "квантовое измерение", &umbrella,
					input, documents, nil, periods, nil)
				if len(selected) != liveTopCandidates || len(rejected) != 65 || expanded.Load() > liveExtractExpansionBatches ||
					time.Since(started) >= liveSearchBudget || activity.peak.Load() > int64(workers) || activity.active.Load() != 0 {
					t.Fatalf("late deficit not filled: selected=%d rejected=%d expansion=%d elapsed=%s",
						len(selected), len(rejected), expanded.Load(), time.Since(started))
				}
				for i := range candidates {
					attempted := attempts[i].Load()
					if attempted > 1 || i < 74 && attempted != 1 || attempted > 0 &&
						cardStarted[i].Load() < qualificationStarted[i].Load()+int64(45*time.Second) {
						t.Fatalf("candidate %d skipped, repeated or bypassed qualification: attempts=%d qualify=%d card=%d",
							i, attempted, qualificationStarted[i].Load(), cardStarted[i].Load())
					}
				}
				for _, candidate := range selected {
					if !liveVerifiedMechanism(candidate) || !liveCardComplete(candidate) || candidate.Shortfall != nil {
						t.Fatalf("late candidate bypassed admission: %+v", candidate)
					}
				}
			})
		})
	}
}

// Первая волна теряет карточки после квалификации; вторая проходит те же проверки и общий рейтинг.
func TestLiveCandidateWavesReplaceLateRejections(t *testing.T) {
	synctest.Test(t, testLiveCandidateWavesReplaceLateRejections)
}

func testLiveCandidateWavesReplaceLateRejections(t *testing.T) {
	service, candidates, documents, periods := liveWaveFixture(t, 48)
	// Сорок мест и запас в четырнадцать карточек впускают обе волны, не дожидаясь ни одного допуска.
	service.SetWorkers(func(context.Context, string, int) int { return 40 })
	var qualified, expanded atomic.Int64
	// Координатор останавливается на пятнадцати допущенных. Пока оценки ждут старта всех попыток, хвост
	// очереди проверяется всегда, а не только когда планировщик задержал первую волну.
	allStarted := make(chan struct{})
	service.extract = func(context.Context, CandidateExtractionRequest) ([]LiveExtractedTechnology, error) {
		expanded.Add(1)
		return nil, nil
	}
	qualify, card := service.qualify, service.card
	service.qualify = func(ctx context.Context, query, name string, docs []LiveQualificationDocument) (LiveQualification, error) {
		if qualified.Add(1) == int64(len(candidates)) {
			close(allStarted)
		}
		result, err := qualify(ctx, query, name, docs)
		index, _ := strconv.Atoi(strings.TrimPrefix(name, "Mechanism"))
		if index >= 20 && index < 26 {
			result.Label = "Повторённый резонатор"
		}
		if index == 26 {
			result.Label = "GraphRAG"
		}
		return result, err
	}
	service.name = func(context.Context, []string) (string, error) {
		return "", errors.New("naming unavailable")
	}
	service.card = func(ctx context.Context, query string, identity LiveCardIdentity, works []string) (LiveCard, error) {
		result, err := card(ctx, query, identity, works)
		if strings.Contains(works[0], "Mechanism0") || strings.Contains(works[0], "Mechanism10") ||
			strings.Contains(works[0], "Mechanism11") {
			result.Summary, result.Missing = "", []string{"описание не подтверждено"}
		}
		return result, err
	}
	service.SetClassifier(func(ctx context.Context, input LiveClassifierRequest) (LiveClassifierResponse, error) {
		select {
		case <-allStarted:
		case <-ctx.Done():
			return LiveClassifierResponse{}, ctx.Err()
		}
		output := LiveClassifierResponse{FeatureSchema: LiveClassifierSchema}
		for _, candidate := range input.Candidates {
			index, _ := strconv.Atoi(strings.TrimPrefix(candidate.ID, "ngram:mechanism"))
			label := liveClassifierWeak
			if index >= 12 && index < 20 {
				label = liveClassifierMature
			}
			output.Predictions = append(output.Predictions, LiveClassifierPrediction{ID: candidate.ID,
				LiveModelScore: LiveModelScore{Value: .5 + float64(index)/100, Threshold: .4, Label: label,
					Model: "fixture", Version: input.ModelVersion, FeatureSchema: LiveClassifierSchema,
					Basis: "Проверка признаков", Features: []LiveModelFeature{{Name: "Источники", Value: "1"}}}})
		}
		return output, nil
	}, "fixture-v1")
	umbrella := LiveTopic{}
	selected, rejected, _ := service.finishCandidates(t.Context(), "квантовое измерение", &umbrella,
		candidates, documents, nil, periods, nil)
	if qualified.Load() != 48 || expanded.Load() != liveExtractExpansionBatches ||
		len(selected) != liveTopCandidates || len(selected)+len(rejected) != 48 {
		t.Fatalf("full replacement wave did not finish: attempts=%d selected=%d rejected=%d",
			qualified.Load(), len(selected), len(rejected))
	}
	if selected[0].Name != "Mechanism47" {
		t.Fatalf("later wave was not ranked globally: %s", selected[0].Name)
	}
	for _, candidate := range selected {
		if !liveCardComplete(candidate) || !liveVerifiedMechanism(candidate) || candidate.Shortfall != nil ||
			candidate.ModelScore == nil || candidate.ModelScore.Label != liveClassifierWeak || candidate.DocumentsCount != 1 {
			t.Fatalf("an incomplete or rejected card escaped: %+v", candidate)
		}
	}
	for _, code := range []string{liveRejectCard, liveRejectModel, liveRejectDuplicate, liveRejectRank} {
		if !slices.ContainsFunc(rejected, func(row LiveRejection) bool { return row.Code == code }) {
			t.Errorf("late rejection missing: %s", code)
		}
	}
	if !slices.ContainsFunc(rejected, func(row LiveRejection) bool {
		return row.OriginalName == "Mechanism26" && row.Code == liveRejectCard && strings.Contains(row.Reason, "название")
	}) {
		t.Fatal("failed Russian naming was hidden by a complete card")
	}
}

func TestLiveCandidateWavesAreBoundedAndCancelable(t *testing.T) {
	for _, test := range []struct {
		mode          string
		workers, want int
	}{
		{"finite_queue", 48, 80},
		{"parent_cancel", 48, 0},
		{"wave_time_floor", 48, 48},
		{"parent_expiry_during_qualification", 48, 48},
		{"configured_workers", 4, 4},
		{"workers_floor", 0, 1},
	} {
		t.Run(test.mode, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				service, candidates, documents, periods := liveWaveFixture(t, 80)
				service.SetWorkers(func(context.Context, string, int) int { return test.workers })
				ctx, cancel := context.WithTimeout(t.Context(), 3*time.Minute)
				defer cancel()
				var qualified, expanded atomic.Int64
				service.qualify = func(stage context.Context, _, _ string, _ []LiveQualificationDocument) (LiveQualification, error) {
					qualified.Add(1)
					switch test.mode {
					case "wave_time_floor", "configured_workers", "workers_floor":
						time.Sleep(2 * time.Minute)
					case "parent_expiry_during_qualification":
						<-stage.Done()
						return LiveQualification{}, fmt.Errorf("qualification interrupted: %w", stage.Err())
					}
					return LiveQualification{Technology: false, Model: "fixture", Method: liveQualificationMethod,
						Reason: "Это показатель, а не технический механизм"}, nil
				}
				service.extract = func(context.Context, CandidateExtractionRequest) ([]LiveExtractedTechnology, error) {
					expanded.Add(1)
					return nil, nil
				}
				if test.mode == "finite_queue" {
					service.extract = nil
				}
				if test.mode == "parent_cancel" {
					cancel()
				}
				umbrella := LiveTopic{}
				selected, rejected, _ := service.finishCandidates(ctx, "запрос", &umbrella,
					candidates, documents, nil, periods, nil)
				budgetLimited := slices.ContainsFunc(umbrella.Limits, func(limit LiveLimit) bool {
					return limit.Code == liveRejectBudget
				})
				if len(liveWithoutTopUp(selected)) != 0 || len(selected)+len(rejected) != 80 ||
					qualified.Load() != int64(test.want) || expanded.Load() != 0 ||
					budgetLimited != (test.mode != "finite_queue") {
					t.Fatalf("budget/cancellation escaped: selected=%d rejected=%d qualified=%d expanded=%d limits=%+v",
						len(selected), len(rejected), qualified.Load(), expanded.Load(), umbrella.Limits)
				}
			})
		})
	}
}

func TestLiveCandidateWavesExpandOnlyAfterStrictDeficit(t *testing.T) {
	service, candidates, documents, periods := liveWaveFixture(t, 16)
	var calls atomic.Int64
	service.extract = func(_ context.Context, request CandidateExtractionRequest) ([]LiveExtractedTechnology, error) {
		calls.Add(1)
		if request.Limit != liveExtractExpansionCandidates || !slices.Contains(request.Excluded, candidates[0].Name) {
			t.Errorf("expansion forgot previous attempts: %+v", request)
		}
		return []LiveExtractedTechnology{{Name: candidates[0].Name, Documents: []string{documents[0].ID}},
			{Name: candidates[15].Name, Documents: []string{documents[15].ID}}}, nil
	}
	umbrella := LiveTopic{}
	selected, _, _ := service.finishCandidates(t.Context(), "квантовое измерение", &umbrella,
		candidates[:14], documents, nil, periods, nil)
	if calls.Load() != 1 || len(selected) != liveTopCandidates ||
		!slices.ContainsFunc(selected, func(candidate LiveTopic) bool { return candidate.Name == "Mechanism15" }) {
		t.Fatalf("extra extraction did not fill the strict deficit: calls=%d selected=%d", calls.Load(), len(selected))
	}
	for _, candidate := range selected {
		if !liveVerifiedMechanism(candidate) || !liveCardComplete(candidate) || candidate.Shortfall != nil {
			t.Fatalf("extra extraction bypassed admission: %+v", candidate)
		}
	}
}

func TestLiveCandidateExpansionPrecedesLongFallbackQueue(t *testing.T) {
	for _, mode := range []string{"complete", "incomplete", "reserve"} {
		t.Run(mode, func(t *testing.T) {
			service, candidates, documents, periods := liveWaveFixture(t, 80)
			for i := range 3 {
				candidates[i].ID = liveExtractPrefix + strings.ToLower(candidates[i].Name)
			}
			var qualified, fallback, expanded atomic.Int64
			qualify := service.qualify
			service.qualify = func(ctx context.Context, query, name string,
				inputs []LiveQualificationDocument,
			) (LiveQualification, error) {
				qualified.Add(1)
				index, _ := strconv.Atoi(strings.TrimPrefix(name, "Mechanism"))
				if index >= 3 && index < 68 {
					fallback.Add(1)
				}
				if index >= 3 && (index < 68 || mode != "complete") {
					return LiveQualification{Technology: false, Reason: "Общий термин без самостоятельного механизма"}, nil
				}
				return qualify(ctx, query, name, inputs)
			}
			service.extract = func(_ context.Context, request CandidateExtractionRequest) ([]LiveExtractedTechnology, error) {
				expanded.Add(1)
				if qualified.Load() > 3 || fallback.Load() != 0 || !slices.Contains(request.Excluded, candidates[0].Name) {
					t.Errorf("fallback consumed the budget before expansion: qualified=%d fallback=%d",
						qualified.Load(), fallback.Load())
				}
				found := make([]LiveExtractedTechnology, 0, liveExtractExpansionCandidates)
				for i := 68; i < 80; i++ {
					found = append(found, LiveExtractedTechnology{Name: candidates[i].Name, Documents: []string{documents[i].ID}})
				}
				return found, nil
			}
			budget := 10 * time.Minute
			if mode == "reserve" {
				budget = 3 * time.Minute
			}
			ctx, cancel := context.WithTimeout(t.Context(), budget)
			defer cancel()
			umbrella := LiveTopic{}
			selected, rejected, _ := service.finishCandidates(ctx, "квантовое измерение", &umbrella,
				candidates[:65], documents, nil, periods, nil)
			wantSelected, wantQualified, wantExpanded, wantTotal := 3, 77, liveExtractExpansionBatches, 77
			if mode == "complete" {
				wantSelected = liveTopCandidates
				// Начатый до фактического допуска хвост ограничен, а именованный добор уже состоялся первым.
				wantQualified = liveTopCandidates + int(fallback.Load())
			}
			if mode == "reserve" {
				wantQualified, wantExpanded, wantTotal = 65, 0, 65
			}
			if len(liveWithoutTopUp(selected)) != wantSelected || qualified.Load() != int64(wantQualified) ||
				expanded.Load() != int64(wantExpanded) || len(selected)+len(rejected) != wantTotal {
				t.Fatalf("bounded expansion failed: selected=%d qualified=%d expanded=%d total=%d",
					len(selected), qualified.Load(), expanded.Load(), len(selected)+len(rejected))
			}
		})
	}
}

func TestLiveCandidateWavesNeverMixMissingModelScores(t *testing.T) {
	for _, mode := range []string{"transport_failure", "missing_prediction"} {
		t.Run(mode, func(t *testing.T) {
			synctest.Test(t, liveNeverMixCase(mode))
		})
	}
}

func liveNeverMixCase(mode string) func(*testing.T) {
	return func(t *testing.T) {
		service, candidates, documents, periods := liveWaveFixture(t, liveCandidateWaveSize+16)
		// Пакет оценки собирается из квалификаций, готовых к этому моменту. Вторая волна ждёт, пока модель
		// не получит всю первую, иначе сбойный пакет захватывал хвост первой волны по воле планировщика.
		firstWaveScored, scored := make(chan struct{}), atomic.Int64{}
		qualify := service.qualify
		service.qualify = func(ctx context.Context, query, name string, docs []LiveQualificationDocument) (LiveQualification, error) {
			if index, _ := strconv.Atoi(strings.TrimPrefix(name, "Mechanism")); index >= liveCandidateWaveSize {
				select {
				case <-firstWaveScored:
				case <-ctx.Done():
					return LiveQualification{}, ctx.Err()
				}
			}
			return qualify(ctx, query, name, docs)
		}
		var calls atomic.Int64
		service.SetClassifier(func(_ context.Context, input LiveClassifierRequest) (LiveClassifierResponse, error) {
			calls.Add(1)
			failedWave := slices.ContainsFunc(input.Candidates, func(candidate LiveClassifierCandidate) bool {
				index, _ := strconv.Atoi(strings.TrimPrefix(candidate.ID, "ngram:mechanism"))
				return index >= liveCandidateWaveSize
			})
			for _, candidate := range input.Candidates {
				index, _ := strconv.Atoi(strings.TrimPrefix(candidate.ID, "ngram:mechanism"))
				if index < liveCandidateWaveSize && scored.Add(1) == liveCandidateWaveSize {
					close(firstWaveScored)
				}
			}
			if failedWave && mode == "transport_failure" {
				return LiveClassifierResponse{}, errors.New("classifier temporarily unavailable")
			}
			output := LiveClassifierResponse{FeatureSchema: LiveClassifierSchema}
			for i, candidate := range input.Candidates {
				if failedWave && i == len(input.Candidates)-1 {
					continue
				}
				label := liveClassifierWeak
				index, _ := strconv.Atoi(strings.TrimPrefix(candidate.ID, "ngram:mechanism"))
				if !failedWave && index >= 8 {
					label = liveClassifierMature
				}
				output.Predictions = append(output.Predictions, LiveClassifierPrediction{ID: candidate.ID,
					LiveModelScore: LiveModelScore{Value: .9, Threshold: .4, Label: label, Model: "fixture",
						Version: input.ModelVersion, FeatureSchema: LiveClassifierSchema, Basis: "Проверка признаков",
						Features: []LiveModelFeature{{Name: "Источники", Value: "1"}}}})
			}
			return output, nil
		}, "fixture-v1")
		umbrella := LiveTopic{}
		selected, rejected, _ := service.finishCandidates(t.Context(), "квантовое измерение", &umbrella,
			candidates, documents, nil, periods, nil)
		unavailable := 0
		for _, row := range rejected {
			if row.Code == liveLimitModelFailed {
				unavailable++
			}
		}
		// Без оценки кандидат идёт только добором с пометкой, а названный моделью зрелым — никогда.
		for _, candidate := range selected[min(8, len(selected)):] {
			if candidate.Shortfall == nil || candidate.Shortfall.Code != liveLimitModelFailed {
				t.Fatalf("top-up took a model rejection or lost its mark: %+v", candidate.Shortfall)
			}
			unavailable++
		}
		if calls.Load() < 2 || calls.Load() > int64(len(candidates)) || len(liveWithoutTopUp(selected)) != 8 ||
			unavailable != 16 || len(selected)+len(rejected) != len(candidates) ||
			!slices.ContainsFunc(umbrella.Limits, func(limit LiveLimit) bool { return limit.Code == liveLimitModelFailed }) {
			t.Fatalf("failed wave entered the ranking: calls=%d selected=%d unavailable=%d rejected=%d",
				calls.Load(), len(selected), unavailable, len(rejected))
		}
		for _, candidate := range selected[:8] {
			if candidate.ModelScore == nil || candidate.ModelScore.Value != .9 ||
				candidate.ModelScore.Label != liveClassifierWeak {
				t.Fatalf("an existing measurement was lost: %+v", candidate.ModelScore)
			}
		}
	}
}

func liveWaveFixture(t *testing.T, count int) (*TrendLiveService, []LiveTopic, []StudyPaper, []LivePeriod) {
	t.Helper()
	t.Setenv("WEB_COLLECTOR_URL", "")
	service := NewTrendLiveService(nil, nil)
	periods := livePeriods(time.Date(2026, 9, 17, 12, 0, 0, 0, time.UTC))
	candidates, documents := make([]LiveTopic, 0, count), make([]StudyPaper, 0, count)
	for i := range count {
		name, id := fmt.Sprintf("Mechanism%02d", i), fmt.Sprintf("paper-%02d", i)
		candidates = append(candidates, LiveTopic{StudyTopic: StudyTopic{ID: fmt.Sprintf("ngram:mechanism%02d", i),
			PaperIDs: []string{id}}, Name: name, DocumentsCount: 1, EvidenceLevel: liveEvidenceThin})
		documents = append(documents, StudyPaper{ID: id, Title: name + " converts quantum phase into measured displacement",
			Date: "2026-08-01", Authors: []string{"One laboratory"}})
	}
	service.qualify = func(_ context.Context, query, name string, docs []LiveQualificationDocument) (LiveQualification, error) {
		return LiveQualification{Scope: liveFixtureQueryScope(query), Label: "Резонатор " + name,
			Mechanism:  "Преобразование квантовой фазы в смещение " + name,
			Technology: true, Relevant: true, Coherent: true, Verified: true, Model: "fixture", VerifierModel: "fixture",
			Method: liveQualificationMethod, Reason: "Механизм подтверждён прямо", Evidence: []LiveQualificationEvidence{
				{ScopeCriteria: []string{"b1"}, SourceID: docs[0].ID, Quote: docs[0].Text, SameTechnology: true, Role: "introduces",
					IdentityReason: "Прямое техническое описание", MechanismQuote: docs[0].Text,
					RelevanceQuote: docs[0].Text, RelevanceReason: "Прямо описана требуемая задача"}}}, nil
	}
	service.card = func(_ context.Context, _ string, _ LiveCardIdentity, works []string) (LiveCard, error) {
		card := LiveCard{Summary: "Преобразование квантовой фазы в смещение", Advantage: "Измерение малых возмущений",
			Example: "Лабораторное измерение смещения", VerificationModel: "fixture"}
		for _, field := range []string{liveClaimSummary, liveClaimAdvantage, liveClaimExample} {
			card.Evidence = append(card.Evidence, LiveClaimEvidence{Field: field, SourceID: "1", Quote: works[0], Verified: true})
		}
		return card, nil
	}
	return service, candidates, documents, periods
}
