package application

import (
	"context"
	"encoding/json/v2"
	"net/http"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
)

// Кандидат /extract проходит общий конвейер; при отказе сервиса работает LLM-извлечение.
func TestLiveMLCandidatePipeline(t *testing.T) {
	var extractRequest LiveMLExtractRequest
	var extractResponse LiveMLExtractResponse
	liveMLFixture(t, "extract_request", &extractRequest)
	liveMLFixture(t, "extract_response", &extractResponse)
	for _, mode := range []string{liveMLSourceOff, liveMLSourceOn, "unavailable"} {
		t.Run(mode, func(t *testing.T) {
			var extractCalls, llmCalls, additionalCalls, scored atomic.Int64
			var qualified atomic.Bool
			server, transport := liveMLTestServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				extractCalls.Add(1)
				var input LiveMLExtractRequest
				if err := json.UnmarshalRead(r.Body, &input); err != nil {
					t.Error(err)
				}
				if input.Query != extractRequest.Query || len(input.Documents) != 3 {
					t.Errorf("неверный пакет извлечения: %+v", input)
				}
				if mode == "unavailable" {
					w.WriteHeader(http.StatusServiceUnavailable)
					return
				}
				w.Header().Set(liveMLVersionHeader, extractRequest.ModelVersion)
				_ = json.MarshalWrite(w, extractResponse)
			}))
			service, _ := liveRetryService(t)
			service.extract = func(_ context.Context, request CandidateExtractionRequest) ([]LiveExtractedTechnology, error) {
				if request.Limit == liveExtractExpansionCandidates {
					additionalCalls.Add(1)
					if request.Query != extractRequest.Query || len(request.Excluded) == 0 || len(request.Documents) != 3 {
						t.Errorf("дополнительный проход потерял исходный запрос, исключения или документы: %+v", request)
					}
					return nil, nil
				}
				llmCalls.Add(1)
				if len(request.Excluded) != 0 {
					t.Error("первичное извлечение получило исключения дополнительного прохода")
				}
				return nil, nil
			}
			qualify := service.qualify
			service.qualify = func(ctx context.Context, query, name string, docs []LiveQualificationDocument) (LiveQualification, error) {
				result, err := qualify(ctx, query, name, docs)
				if strings.EqualFold(name, "acoustic watermark") {
					qualified.Store(true)
					result.Label = "Акустический водяной знак"
				}
				return result, err
			}
			service.SetClassifier(func(_ context.Context, input LiveClassifierRequest) (LiveClassifierResponse, error) {
				output := LiveClassifierResponse{FeatureSchema: LiveClassifierSchema}
				for _, candidate := range input.Candidates {
					scored.Add(1)
					// В фикстуре кандидат /extract делит документы со словосочетанием: выше его ставит только оценка модели.
					value := .8
					if strings.HasPrefix(candidate.ID, liveExtractPrefix) {
						value = .9
					}
					output.Predictions = append(output.Predictions, LiveClassifierPrediction{ID: candidate.ID,
						LiveModelScore: LiveModelScore{Label: liveClassifierWeak, Value: value, Threshold: .5, Model: "test",
							Version: "logreg", FeatureSchema: LiveClassifierSchema, Basis: "Встроенная модель",
							Features: []LiveModelFeature{{Name: "Документы", Value: "2"}}}})
				}
				return output, nil
			}, "logreg")
			source := mode
			if mode == "unavailable" {
				source = liveMLSourceOn
			}
			service.SetMLCandidateSource(source, newMLCandidateExtractor(server.URL+"/extract", 0, transport, 0, 0),
				extractRequest.ModelVersion)
			detail, err := service.search(t.Context(), extractRequest.Query, "", nil)
			if err != nil {
				t.Fatal(err)
			}
			if len(detail.Candidates) == 0 || scored.Load() == 0 || !slices.ContainsFunc(detail.Candidates, func(c LiveTopic) bool {
				return c.Qualification != nil && c.ModelScore != nil
			}) {
				t.Fatalf("общий путь не выполнен: %+v", detail.Candidates)
			}
			if len(detail.Candidates) >= liveTopCandidates || additionalCalls.Load() != 1 {
				t.Fatalf("недобор не привёл к одному дополнительному проходу: selected=%d calls=%d",
					len(detail.Candidates), additionalCalls.Load())
			}
			switch mode {
			case liveMLSourceOff:
				if extractCalls.Load() != 0 || llmCalls.Load() == 0 || detail.Extraction != nil {
					t.Fatal("режим по умолчанию обратился к извлечению ML или не вызвал LLM-извлечение")
				}
			case "unavailable":
				if llmCalls.Load() == 0 || detail.Extraction == nil || detail.Extraction.FallbackReason == "" ||
					!slices.ContainsFunc(detail.Topic.Limits, func(l LiveLimit) bool { return l.Code == liveLimitMLExtractFallback }) ||
					service.reusableSearch(detail, extractRequest.Query) {
					t.Fatal("откат на LLM-извлечение не выполнен, не отмечен или сохранён на сутки")
				}
			default:
				if extractCalls.Load() != 1 || llmCalls.Load() != 0 || detail.Extraction.Used != liveMLSourceOn {
					t.Fatalf("извлечение ML не заменило LLM: extract=%d llm=%d", extractCalls.Load(), llmCalls.Load())
				}
				at := slices.IndexFunc(detail.Candidates, func(c LiveTopic) bool { return c.ML != nil })
				if !qualified.Load() || at < 0 {
					t.Fatalf("кандидат /extract не прошёл проверку сущности: %+v", detail.Candidates)
				}
				candidate := detail.Candidates[at]
				if candidate.ID != liveExtractPrefix+"acoustic-watermark" || candidate.Label != "Акустический водяной знак" ||
					candidate.Qualification == nil || candidate.ModelScore == nil || candidate.ModelScore.Version != "logreg" ||
					candidate.ML.Stage.Code != 2 || candidate.DocumentsCount < 2 || !service.reusableSearch(detail, extractRequest.Query) {
					t.Fatalf("имя, проверка, оценка или происхождение подменены: %+v", candidate)
				}
			}
		})
	}
}
