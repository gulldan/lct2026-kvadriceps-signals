package application

import (
	"context"
	"encoding/json/v2"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// Пропуск кандидата, чужая версия или повтор ID не должны превращаться в частичный модельный ранг.
func TestLiveClassifierContract(t *testing.T) {
	version := "test-artifact-v1"
	prediction := func(id, label string, value float64) LiveClassifierPrediction {
		return LiveClassifierPrediction{ID: id, LiveModelScore: LiveModelScore{
			Value: value, Label: label, Threshold: 0.75, Model: "test-classifier", Version: version,
			FeatureSchema: LiveClassifierSchema, Basis: "Проверяем транспортный контракт; это не обученная модель.",
			Features: []LiveModelFeature{{Name: "Авторские группы", Value: "3", Contribution: 0.1}},
		}}
	}
	for _, kind := range []string{
		"valid", "partial", "duplicate", "version", "schema", "range", "explanation", "language", "unavailable",
	} {
		t.Run(kind, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var input LiveClassifierRequest
				if err := json.UnmarshalRead(r.Body, &input); err != nil {
					t.Error(err)
					w.WriteHeader(http.StatusBadRequest)
					return
				}
				if r.Method != http.MethodPost || input.ModelVersion != version || len(input.Candidates) != 2 ||
					input.Candidates[0].Features.TopGroupShare != nil {
					t.Errorf("неверный вход или подмена пропуска нулём: %+v", input)
				}
				output := LiveClassifierResponse{FeatureSchema: LiveClassifierSchema,
					Predictions: []LiveClassifierPrediction{prediction("a", "weak_signal", 0.82), prediction("b", "mature", 0.05)}}
				switch kind {
				case "partial":
					output.Predictions = output.Predictions[:1]
				case "duplicate":
					output.Predictions[1].ID = "a"
				case "version":
					output.Predictions[1].Version = "old"
				case "schema":
					output.FeatureSchema = "old"
				case "range":
					output.Predictions[1].Value = 82
				case "explanation":
					output.Predictions[1].Features = nil
				case "language":
					output.Predictions[1].Basis = "Only an English explanation"
				case "unavailable":
					w.WriteHeader(http.StatusServiceUnavailable)
					return
				}
				if err := json.MarshalWrite(w, output); err != nil {
					t.Error(err)
				}
			}))
			defer server.Close()
			service := &TrendLiveService{}
			service.SetClassifier(NewCandidateClassifier(server.URL), version)
			umbrella := LiveTopic{}
			candidates := []LiveTopic{{StudyTopic: StudyTopic{ID: "a"}}, {StudyTopic: StudyTopic{ID: "b"}}}
			kept, rejected := service.classifyCandidates(t.Context(), "Поиск технологии", &umbrella, candidates, nil, nil, nil)
			if kind == "valid" {
				if len(kept) != 1 || kept[0].ModelScore == nil || kept[0].ModelScore.Value != 0.82 ||
					len(rejected) != 1 || rejected[0].Code != liveRejectModel || rejected[0].ModelValue == nil {
					t.Fatalf("валидный ответ не применён: kept=%+v rejected=%+v", kept, rejected)
				}
				return
			}
			if len(kept) != 2 || kept[0].ModelScore != nil || kept[1].ModelScore != nil || len(rejected) != 0 ||
				len(umbrella.Limits) != 1 || umbrella.Limits[0].Code != liveLimitModelFailed {
				t.Fatalf("ошибка скрыта или применён частичный пакет: kept=%+v limits=%+v", kept, umbrella.Limits)
			}
		})
	}
}

func TestLiveClassifierVersionRescoresStoredAnswer(t *testing.T) {
	var missing LiveClassifierPrediction
	if err := json.Unmarshal([]byte(`{"id":"a","threshold":0.75}`), &missing); err == nil {
		t.Fatal("отсутствующая оценка принята за ноль")
	}
	service := storeService(t, newFakeSearchStore())
	candidate := storedCandidateFixture("c1")
	candidate.Groups = 2
	// Имя сверено с аннотацией при допуске, а сохранённый ответ держит только заголовок.
	candidate.Qualification.CanonicalName = "dual‐phase halide electrolyte"
	stored := liveScopedDetailFixture("запрос", LiveDetail{Candidates: []LiveTopic{candidate},
		Papers: []StudyPaper{{ID: "source:c1", Title: "Dual‐Phase Symbiotic Halide Solid Electrolytes"}}})
	service.saveSearch(t.Context(), "запрос", stored, time.Now().Add(time.Hour))
	phrase := ""
	service.SetClassifier(func(_ context.Context, input LiveClassifierRequest) (LiveClassifierResponse, error) {
		phrase = input.Candidates[0].phrase
		return LiveClassifierResponse{FeatureSchema: LiveClassifierSchema, Predictions: []LiveClassifierPrediction{{
			ID: "c1", LiveModelScore: LiveModelScore{Label: liveClassifierWeak, Value: 0.8, Threshold: 0.3,
				Model: "m", Version: input.ModelVersion, FeatureSchema: LiveClassifierSchema,
				Basis: "Проверочная оценка модели", Features: []LiveModelFeature{{Name: "Рост доли", Value: "1",
					Contribution: 0.5}}}}}}, nil
	}, "new-artifact")
	detail, ok := service.storedSearch(t.Context(), "запрос", nil)
	if !ok {
		t.Fatal("ответ прежней модели не открылся — клик по карте ушёл бы в новый сбор")
	}
	detail, _ = service.rescoreStored(t.Context(), "запрос", detail)
	if detail.ClassifierVersion != "new-artifact" || len(detail.Candidates) != 1 ||
		detail.Candidates[0].ModelScore == nil || detail.Counters.ModelAboveThreshold != 1 {
		t.Fatalf("ответ не получил оценку новой модели: %+v", detail)
	}
	if phrase != "dual phase halide electrolyte" {
		t.Fatalf("дооценка сменила фразу исходного поиска: %q", phrase)
	}
	raw, err := json.Marshal(liveClassifierInput("запрос", "new-artifact", []LiveTopic{{
		Confidence: &LiveConfidence{Value: 1}, Stage: "established",
	}}, nil, nil))
	if err != nil || strings.Contains(string(raw), "confidence") || strings.Contains(string(raw), "established") {
		t.Fatalf("целевая метка или эвристический ответ утекли во вход модели: %s, %v", raw, err)
	}
}
