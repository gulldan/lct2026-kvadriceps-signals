package application

import (
	"bytes"
	"cmp"
	"context"
	"encoding/json/v2"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"time"
)

// LiveClassifierSchema не включает правила отбора и тексты генератора: модель не должна учиться повторять ответ.
const LiveClassifierSchema = "weak-signal-features-v1"

const (
	liveClassifierTimeout = 30 * time.Second
	// Общий срок внешней модели и срок источников встроенной; ожидание зрелости использует срок поиска.
	liveClassifierStage        = 90 * time.Second
	liveClassifierBytes        = 1 << 20
	liveClassifierInputBytes   = 4 << 20
	liveLimitModelMissing      = "classifier_not_connected"
	liveLimitModelFailed       = "classifier_unavailable"
	liveRejectModel            = "classifier_rejected"
	liveClassifierFailed       = "failed"
	liveClassifierWeak         = "weak_signal"
	liveClassifierMature       = "mature"
	liveClassifierNoise        = "noise"
	liveClassifierInsufficient = "insufficient_data"
	liveClassifierUndecided    = "undecided"
)

// LiveClassifierFeatures хранит nil как «не измерено»: это не то же самое, что измеренный ноль.
type LiveClassifierFeatures struct {
	Documents       int      `json:"documents"`
	ScientificWorks int      `json:"scientific_works"`
	MarketRecords   int      `json:"market_records"`
	ActivePeriods   int      `json:"active_periods"`
	AuthorGroups    int      `json:"author_groups"`
	TopGroupShare   *float64 `json:"top_group_share"`
	ShareGrowthPP   *float64 `json:"share_growth_pp"`
	FirstObserved   string   `json:"first_observed"`
}

type LiveClassifierSource struct {
	ID       string `json:"id"`
	Title    string `json:"title"`
	Abstract string `json:"abstract,omitempty"`
	Date     string `json:"date"`
	URL      string `json:"url"`
}

type LiveClassifierCandidate struct {
	ID       string                 `json:"id"`
	Name     string                 `json:"name"`
	Features LiveClassifierFeatures `json:"features"`
	Sources  []LiveClassifierSource `json:"sources"`
	// Внешний контракт не меняется: точное имя нужно встроенному сбору признаков по фразе.
	phrase string
	// Только научные источники кандидата; патенты и новости не доказывают покрытие научной литературы.
	journals []liveJournalPaper
}

type LiveClassifierRequest struct {
	FeatureSchema string                    `json:"feature_schema"`
	ModelVersion  string                    `json:"model_version"`
	Query         string                    `json:"query"`
	Candidates    []LiveClassifierCandidate `json:"candidates"`
}

type LiveClassifierPrediction struct {
	ID string `json:"id"`
	LiveModelScore
}

// UnmarshalJSON отличает допустимый ноль от отсутствующего поля оценки.
func (p *LiveClassifierPrediction) UnmarshalJSON(raw []byte) error {
	type prediction LiveClassifierPrediction
	var decoded struct {
		prediction
		Value     *float64 `json:"value"`
		Threshold *float64 `json:"threshold"`
	}
	if err := json.Unmarshal(raw, &decoded); err != nil {
		return fmt.Errorf("decode classifier prediction: %w", err)
	}
	if decoded.Value == nil || decoded.Threshold == nil {
		return errors.New("в ответе модели отсутствует оценка или порог")
	}
	*p = LiveClassifierPrediction(decoded.prediction)
	p.Value, p.Threshold = *decoded.Value, *decoded.Threshold
	return nil
}

type LiveClassifierResponse struct {
	FeatureSchema string                     `json:"feature_schema"`
	Predictions   []LiveClassifierPrediction `json:"predictions"`
}

// Цель отбора закрывают только слабые сигналы с модельным решением по данным.
func liveModelDecision(score *LiveModelScore) bool {
	return score != nil && score.Label == liveClassifierWeak && score.Value >= score.Threshold && score.DataGap == ""
}

type CandidateClassifier func(context.Context, LiveClassifierRequest) (LiveClassifierResponse, error)

// SetClassifier вызывается до запуска: версия входит в проверку сохранённого ответа, иначе он сутки жил бы без оценки.
func (s *TrendLiveService) SetClassifier(classify CandidateClassifier, version string) {
	s.classifier, s.classifierVersion = classify, strings.TrimSpace(version)
	s.classifierOwnBudget = false
}

func NewCandidateClassifier(endpoint string) CandidateClassifier {
	if endpoint = strings.TrimSpace(endpoint); endpoint == "" {
		return nil
	}
	client := &http.Client{
		Timeout: liveClassifierTimeout,
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
	return func(ctx context.Context, input LiveClassifierRequest) (LiveClassifierResponse, error) {
		var output LiveClassifierResponse
		request, err := liveClassifierRequest(ctx, endpoint, input)
		if err != nil {
			return output, err
		}
		response, err := client.Do(request)
		if err != nil {
			return output, errors.New("классификатор не ответил")
		}
		defer func() { _ = response.Body.Close() }()
		if response.StatusCode != http.StatusOK {
			return output, fmt.Errorf("классификатор ответил HTTP %d", response.StatusCode)
		}
		raw, err := io.ReadAll(io.LimitReader(response.Body, liveClassifierBytes+1))
		if err != nil || len(raw) > liveClassifierBytes {
			return output, errors.New("ответ классификатора не прочитан или превышает 1 МиБ")
		}
		if err := json.Unmarshal(raw, &output); err != nil {
			return output, errors.New("неверный формат ответа классификатора")
		}
		return output, nil
	}
}

// liveServiceURL отвергает учётные данные в адресе: пароль ушёл бы в журнал вместе с ошибкой транспорта.
func liveServiceURL(endpoint string) bool {
	target, err := url.Parse(endpoint)
	return err == nil && target.Host != "" && target.User == nil &&
		(target.Scheme == "http" || target.Scheme == "https")
}

func liveClassifierRequest(ctx context.Context, endpoint string, input LiveClassifierRequest) (*http.Request, error) {
	if !liveServiceURL(endpoint) {
		return nil, errors.New("неверный адрес классификатора")
	}
	if input.ModelVersion == "" {
		return nil, errors.New("не задана версия классификатора")
	}
	body, err := json.Marshal(input)
	if err != nil {
		return nil, fmt.Errorf("вход классификатора: %w", err)
	}
	if len(body) > liveClassifierInputBytes {
		return nil, errors.New("вход классификатора превышает 4 МиБ")
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, errors.New("не удалось создать запрос классификатора")
	}
	request.Header.Set("Content-Type", "application/json")
	return request, nil
}

func liveClassifierInput(query, version string, candidates []LiveTopic, documents []StudyPaper,
	metadata map[string]LivePaperMetadata,
) LiveClassifierRequest {
	input := LiveClassifierRequest{FeatureSchema: LiveClassifierSchema, ModelVersion: version, Query: query}
	byID := make(map[string]StudyPaper, len(documents))
	for _, document := range documents {
		byID[document.ID] = document
	}
	for _, candidate := range candidates {
		phrase := liveClassifierCanonicalPhrase(candidate)
		// Цитата проверки называет пример внутри направления; оценивать нужно само направление.
		if liveFromDirection(candidate) {
			phrase = liveDirectionTerm(candidate)
		}
		marketRecords := candidate.RecordsCount
		if candidate.Research != nil {
			// Публикация набора данных не означает внедрение технологии на рынке.
			marketRecords = max(0, marketRecords-len(candidate.Research.Records))
		}
		item := LiveClassifierCandidate{
			ID: candidate.ID, Name: liveCandidateName(candidate), phrase: phrase,
			Features: LiveClassifierFeatures{
				Documents: candidate.DocumentsCount, ScientificWorks: candidate.PapersCount,
				MarketRecords: marketRecords, ActivePeriods: candidate.ActiveYears,
				AuthorGroups: candidate.Groups, TopGroupShare: candidate.TopGroupShare,
				ShareGrowthPP: candidate.Growth.DeltaPP, FirstObserved: candidate.FirstYear,
			},
		}
		for _, id := range slices.Concat(candidate.PaperIDs, candidate.EvidenceIDs) {
			if document, ok := byID[id]; ok {
				if liveJournalSource(document, metadata[id]) && liveJournalMechanismSource(candidate, id) {
					item.journals = append(item.journals, liveJournalPaper{paper: document,
						source: cmp.Or(liveJournalProvider(id), liveJournalProvider(metadata[id].Category))})
				}
				abstract := []rune(metadata[id].Abstract)
				item.Sources = append(item.Sources, LiveClassifierSource{
					ID: id, Title: document.Title, Date: document.Date, URL: document.URL,
					Abstract: string(abstract[:min(len(abstract), 2000)]),
				})
			}
		}
		input.Candidates = append(input.Candidates, item)
	}
	return input
}

// Пакет проверяется целиком: частичный ответ дал бы одним кандидатам модельный ранг, а другим — незаметно правила.
func liveValidatePredictions(input LiveClassifierRequest, output LiveClassifierResponse) (map[string]LiveModelScore, error) {
	if input.ModelVersion == "" || output.FeatureSchema != LiveClassifierSchema || len(output.Predictions) != len(input.Candidates) {
		return nil, errors.New("не совпадает схема или число предсказаний")
	}
	wanted := make(map[string]bool, len(input.Candidates))
	for _, candidate := range input.Candidates {
		wanted[candidate.ID] = true
	}
	scores := make(map[string]LiveModelScore, len(wanted))
	for _, prediction := range output.Predictions {
		score := prediction.LiveModelScore
		_, duplicate := scores[prediction.ID]
		if !wanted[prediction.ID] || duplicate || score.Model == "" || score.Version != input.ModelVersion ||
			score.FeatureSchema != LiveClassifierSchema || liveRussianText(score.Basis) == "" || len(score.Features) == 0 ||
			!liveUnitInterval(score.Value) || !liveUnitInterval(score.Threshold) ||
			!slices.Contains([]string{liveClassifierWeak, liveClassifierMature, liveClassifierNoise, liveClassifierFailed,
				liveClassifierInsufficient, liveClassifierUndecided}, score.Label) {
			return nil, errors.New("предсказание без согласованной версии, объяснения или допустимого значения")
		}
		for _, feature := range score.Features {
			if liveRussianText(feature.Name) == "" || feature.Value == "" ||
				math.IsNaN(feature.Contribution) || math.IsInf(feature.Contribution, 0) {
				return nil, errors.New("неверное объяснение признака классификатора")
			}
		}
		scores[prediction.ID] = score
	}
	return scores, nil
}

func liveUnitInterval(value float64) bool {
	return !math.IsNaN(value) && !math.IsInf(value, 0) && value >= 0 && value <= 1
}

func (s *TrendLiveService) classifyCandidates(ctx context.Context, query string, umbrella *LiveTopic,
	candidates []LiveTopic, documents []StudyPaper, metadata map[string]LivePaperMetadata,
	rejected []LiveRejection,
) ([]LiveTopic, []LiveRejection) {
	return s.classifyAsync(ctx, query, umbrella, candidates, documents, metadata)(candidates, rejected)
}

// classifyAsync запускает модель сразу: встроенная считает признаки по окну, а не по карточке. Вход
// собирается синхронно — пересказы дописывают метаданные, и чтение из горутины было бы гонкой.
func (s *TrendLiveService) classifyAsync(ctx context.Context, query string, umbrella *LiveTopic,
	candidates []LiveTopic, documents []StudyPaper, metadata map[string]LivePaperMetadata,
) func([]LiveTopic, []LiveRejection) ([]LiveTopic, []LiveRejection) {
	input := liveClassifierInput(query, s.classifierVersion, candidates, documents, metadata)
	if s.classifier == nil {
		// Объём входа считается и без модели: по нему ML-коллега оценивает размер пакета и отведённое время.
		liveTallyOf(ctx).addClassifier(input, false)
		return func(kept []LiveTopic, rejected []LiveRejection) ([]LiveTopic, []LiveRejection) {
			umbrella.Limits = append(umbrella.Limits, liveLimitOf(liveLimitModelMissing,
				"Обученная модель не подключена; показаны кандидаты для экспертной проверки, без модельной оценки.", nil))
			return kept, rejected
		}
	}
	if len(candidates) == 0 {
		return func(kept []LiveTopic, rejected []LiveRejection) ([]LiveTopic, []LiveRejection) { return kept, rejected }
	}
	liveTallyOf(ctx).addClassifier(input, true)
	type result struct {
		scores map[string]LiveModelScore
		err    error
	}
	done := make(chan result, 1)
	go func() {
		stage := ctx
		if !s.classifierOwnBudget {
			var cancel context.CancelFunc
			stage, cancel = context.WithTimeout(ctx, liveClassifierStage)
			defer cancel()
		}
		output, err := s.classifier(stage, input)
		var scores map[string]LiveModelScore
		if err == nil {
			scores, err = liveValidatePredictions(input, output)
		}
		done <- result{scores, err}
	}()
	return func(admitted []LiveTopic, rejected []LiveRejection) ([]LiveTopic, []LiveRejection) {
		got := <-done
		if got.err != nil {
			umbrella.Limits = append(umbrella.Limits, liveLimitFail(liveLimitModelFailed, "classifier", got.err))
			return admitted, rejected
		}
		kept, rejected := liveApplyScores(admitted, rejected, got.scores)
		return kept, liveScoreRejected(rejected, got.scores)
	}
}

// Оценка посчитана на всю партию: отказ по карточке её не отменяет, а добранной до пятнадцати ТЗ требует
// показать уверенность модели. Раньше посчитанную оценку отклонённых выбрасывали.
func liveScoreRejected(rejected []LiveRejection, scores map[string]LiveModelScore) []LiveRejection {
	for i, rejection := range rejected {
		if rejection.topic == nil || rejection.topic.ModelScore != nil {
			continue
		}
		if score, ok := scores[rejection.topic.ID]; ok {
			scored := *rejection.topic
			scored.ModelScore = &score
			rejected[i].topic, rejected[i].ModelValue = &scored, new(score.Value)
		}
	}
	return rejected
}

func liveApplyScores(candidates []LiveTopic, rejected []LiveRejection,
	scores map[string]LiveModelScore,
) ([]LiveTopic, []LiveRejection) {
	kept := make([]LiveTopic, 0, len(candidates))
	for _, candidate := range candidates {
		score, ok := scores[candidate.ID]
		if !ok {
			kept = append(kept, candidate)
			continue
		}
		candidate.ModelScore = &score
		// Воздержание модели — не отказ: иначе незнание выдавалось бы за вердикт. Добранная при дооценке
		// уже помечена непройденной и остаётся в пятнадцати, если модель не назвала её зрелой.
		if score.Label == liveClassifierInsufficient || score.Label == liveClassifierUndecided || score.DataGap != "" ||
			candidate.Shortfall != nil && score.Label != liveClassifierMature {
			kept = append(kept, candidate)
			continue
		}
		if !liveModelDecision(&score) {
			rejected = append(rejected, liveRejection(candidate, liveRejectModel,
				fmt.Sprintf("обученная модель %s, версия %s: класс «%s», оценка %.1f %%, порог %.1f %%",
					score.Model, score.Version, liveModelLabel(score.Label), score.Value*100, score.Threshold*100)))
			continue
		}
		kept = append(kept, candidate)
	}
	return kept, rejected
}

func liveModelLabel(label string) string {
	switch label {
	case "weak_signal":
		return "слабый сигнал"
	case liveClassifierMature:
		return "зрелая технология"
	case liveClassifierFailed:
		return "неподтверждённое направление"
	case liveClassifierInsufficient:
		return "мало данных"
	case liveClassifierUndecided:
		return "не могу решить"
	default:
		return "информационный шум"
	}
}
