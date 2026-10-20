package application

import (
	"bytes"
	"cmp"
	"context"
	_ "embed"
	"encoding/json/v2"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"maps"
	"math"
	"net/http"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Встроенная модель первого этапа из scripts/weak-signal/train.py подключается, только когда адрес
// внешнего сервиса ML-команды не задан; признаки фразы считаются тем же кодом, что при обучении.

//go:embed trend_live_classifier_weights.json
var liveWeakSignalWeights []byte

// Отчёт относится к обученным весам; политика покрытия источников живого радара версионируется отдельно.
//
//go:embed trend_live_classifier_evaluation.json
var liveWeakSignalEvaluation []byte

// WeakSignalArtifacts убирает из весов только проекцию эмбеддинга: четыре пятых файла, а странице нужны лишь коэффициенты.
func WeakSignalArtifacts() (weights, evaluation []byte, err error) {
	var artifact map[string]any
	if err = json.Unmarshal(liveWeakSignalWeights, &artifact); err != nil {
		return nil, nil, fmt.Errorf("weak signal model: %w", err)
	}
	if embedding, ok := artifact["embedding"].(map[string]any); ok {
		artifact["embedding"] = map[string]any{"model": embedding["model"], "coef": embedding["coef"]}
	}
	artifact["embedded_inference_version"] = fmt.Sprint(artifact["version"]) + liveSourceCoverageVersion
	if weights, err = json.Marshal(artifact); err != nil {
		return nil, nil, fmt.Errorf("weak signal model: %w", err)
	}
	return weights, liveWeakSignalEvaluation, nil
}

type WeakSignalModel struct {
	Model         string              `json:"model"`
	Version       string              `json:"version"`
	FeatureSchema string              `json:"feature_schema"`
	Threshold     float64             `json:"threshold"`
	Conformal     WeakSignalConformal `json:"conformal"`
	Intercept     float64             `json:"intercept"`
	Inputs        []string            `json:"inputs"`
	Impute        []float64           `json:"impute"`
	Mean          []float64           `json:"mean"`
	Scale         []float64           `json:"scale"`
	Coef          []float64           `json:"coef"`
	Min           []float64           `json:"min"`
	Max           []float64           `json:"max"`
	Embedding     WeakSignalEmbedding `json:"embedding"`
}

// WeakSignalConformal — квантили несоответствия по классам (Mondrian) по внепапочным оценкам обучения: метка
// «сигнал» допустима при 1 − p ≤ Weak, «не сигнал» — при p ≤ NotWeak, и каждая покрывает свой класс не реже
// 1 − Alpha. Обе допустимы — гарантия не позволяет выбрать, и модель честно говорит «не могу решить».
type WeakSignalConformal struct {
	Alpha   float64 `json:"alpha"`
	Weak    float64 `json:"weak"`
	NotWeak float64 `json:"not_weak"`
}

func (c WeakSignalConformal) undecided(value float64) bool {
	return 1-value <= c.Weak && value <= c.NotWeak
}

func (c WeakSignalConformal) basis(value float64) string {
	return fmt.Sprintf("Не могу решить: оценка %.0f %% лежит в полосе %.0f–%.0f %%, где при гарантии %.0f %% "+
		"для каждого класса допустимы и «слабый сигнал», и «не слабый сигнал». Модель не может отличить кандидата "+
		"от хайпа с этой гарантией; нужна проверка рыночного слоя.",
		value*100, (1-c.Weak)*100, c.NotWeak*100, (1-c.Alpha)*100)
}

type WeakSignalEmbedding struct {
	Model      string      `json:"model"`
	Mean       []float64   `json:"mean"`
	Scale      []float64   `json:"scale"`
	PCAMean    []float64   `json:"pca_mean"`
	Components [][]float64 `json:"components"`
	PostMean   []float64   `json:"post_mean"`
	PostScale  []float64   `json:"post_scale"`
	Coef       []float64   `json:"coef"`
}

// Порядок групп обязан совпадать с артефактом: окно arXiv, история OpenAlex, Hacker News, оценка модели языка.
func weakSignalKinds() [][]string {
	return [][]string{
		{"log_documents", "active_periods", "groups_per_document", "top_group_share", "growth_per_document",
			"months_observed", "window_empty"},
		{"history_log_works", "history_share_before", "history_growth", "history_log_recent", "history_age_years",
			"history_slope", "history_share_last3"},
		{"hn_log_total", "hn_share_before", "hn_growth", "hn_log_recent", "hn_log_recent_story", "hn_story_growth"},
		{"llm_stage", "llm_failed", "llm_confidence"},
	}
}

// WeakSignalInputNames фиксирует порядок входов: в группе сначала самая устоявшаяся фраза, затем самая узкая.
func WeakSignalInputNames() []string {
	names := []string{}
	for _, kinds := range weakSignalKinds() {
		for _, side := range []string{"broad_", "narrow_"} {
			for _, kind := range kinds {
				names = append(names, side+kind)
			}
		}
	}
	return names
}

func LoadWeakSignalModel() (WeakSignalModel, error) {
	var m WeakSignalModel
	if err := json.Unmarshal(liveWeakSignalWeights, &m); err != nil {
		return m, fmt.Errorf("weak signal model: %w", err)
	}
	n, e, c := len(m.Inputs), m.Embedding, m.Conformal
	// Порог внутри полосы «не могу решить»: вне неё решение множества совпадает с решением порога, и метка
	// не спорит с порогом, который видит читатель.
	shaped := m.Version != "" && m.FeatureSchema == LiveClassifierSchema && liveUnitInterval(m.Threshold) &&
		c.Alpha > 0 && c.Alpha < 1 && liveUnitInterval(c.Weak) && liveUnitInterval(c.NotWeak) &&
		1-c.Weak <= m.Threshold && m.Threshold <= c.NotWeak &&
		slices.Equal(m.Inputs, WeakSignalInputNames()) &&
		len(m.Impute) == n && len(m.Mean) == n && len(m.Scale) == n && len(m.Coef) == n &&
		len(m.Min) == n && len(m.Max) == n &&
		len(e.Components) > 0 && len(e.Coef) == len(e.Components) && len(e.PostMean) == len(e.Components) &&
		len(e.PostScale) == len(e.Components) && len(e.Scale) == len(e.Mean) && len(e.PCAMean) == len(e.Mean)
	for _, component := range e.Components {
		shaped = shaped && len(component) == len(e.Mean)
	}
	// Нулевой разброс — испорченный артефакт: деление дало бы бесконечность, а не ошибку.
	for _, scale := range slices.Concat(m.Scale, e.Scale, e.PostScale) {
		shaped = shaped && scale > 0
	}
	if !shaped {
		return m, errors.New("weak signal model: artifact does not match the feature schema")
	}
	return m, nil
}

// Score заменяет пропуск (NaN) медианой обучения; без эмбеддинга его вклад нулевой.
func (m WeakSignalModel) Score(inputs, embedding []float64) (value float64, contributions []float64, semantic float64) {
	logit := m.Intercept
	contributions = make([]float64, len(m.Coef))
	for i, coef := range m.Coef {
		x := inputs[i]
		if math.IsNaN(x) {
			x = m.Impute[i]
		}
		contributions[i] = coef * (x - m.Mean[i]) / m.Scale[i]
		logit += contributions[i]
	}
	if len(embedding) == len(m.Embedding.Mean) {
		semantic = m.Embedding.contribution(embedding)
		logit += semantic
	}
	return 1 / (1 + math.Exp(-logit)), contributions, semantic
}

func (e WeakSignalEmbedding) contribution(vector []float64) float64 {
	centered := make([]float64, len(vector))
	for j, v := range vector {
		centered[j] = (v-e.Mean[j])/e.Scale[j] - e.PCAMean[j]
	}
	out := 0.0
	for k, component := range e.Components {
		projection := 0.0
		for j, w := range component {
			projection += centered[j] * w
		}
		out += e.Coef[k] * (projection - e.PostMean[k]) / e.PostScale[k]
	}
	return out
}

// WeakSignalEvidence с nil — источник не ответил: вход заменяется медианой обучения и называется неизмеренным.
type WeakSignalEvidence struct {
	Window    *LivePhraseWindow
	Journals  *LiveRatingEvidence
	History   LiveTermHistory
	HN        *LiveHNCounts
	Rating    *LiveMaturityRating
	Embedding []float64
}

func (e *WeakSignalEvidence) inputs(month string) [][]float64 {
	if e == nil {
		e = &WeakSignalEvidence{}
	}
	var window *LiveClassifierFeatures
	if e.Window != nil {
		window = &e.Window.LiveClassifierFeatures
	}
	return [][]float64{liveWindowInputs(window, month), liveHistoryInputs(e.History, month), LiveHNInputs(e.HN),
		LiveRatingInputs(e.Rating)}
}

// weakSignalRow берёт самую устоявшуюся и самую узкую фразу по числу работ в OpenAlex: вместе они
// различают лучше одной, а у кандидата радара одна фраза стоит на обоих местах, как при обучении.
func weakSignalRow(phrases []string, evidence map[string]*WeakSignalEvidence, month string,
) (inputs, embedding []float64, ordered []string) {
	works := func(phrase string) int {
		if e := evidence[phrase]; e != nil {
			return liveHistoryWorks(e.History, math.MaxInt)
		}
		return 0
	}
	ordered = slices.Clone(phrases)
	slices.SortStableFunc(ordered, func(a, b string) int { return cmp.Compare(works(b), works(a)) })
	broad, narrow := evidence[ordered[0]].inputs(month), evidence[ordered[len(ordered)-1]].inputs(month)
	for group := range broad {
		inputs = slices.Concat(inputs, broad[group], narrow[group])
	}
	count := 0.0
	for _, phrase := range ordered {
		e := evidence[phrase]
		if e == nil || e.Embedding == nil {
			continue
		}
		if embedding == nil {
			embedding = make([]float64, len(e.Embedding))
		}
		for j, v := range e.Embedding {
			embedding[j] += v
		}
		count++
	}
	for j := range embedding {
		embedding[j] /= count
	}
	return inputs, embedding, ordered
}

// liveWindowInputs: рост доли делится на число документов — модели нужен темп, а не громкость; «работ нет»
// — отдельный флаг, иначе модель продолжает линию «меньше работ — новее» до нуля.
func liveWindowInputs(features *LiveClassifierFeatures, month string) []float64 {
	if features == nil {
		return []float64{math.NaN(), math.NaN(), math.NaN(), math.NaN(), math.NaN(), math.NaN(), math.NaN()}
	}
	perDocument := func(value float64) float64 {
		if features.Documents == 0 {
			return math.NaN()
		}
		return value / float64(features.Documents)
	}
	share, growth, empty := math.NaN(), math.NaN(), 0.0
	if features.TopGroupShare != nil {
		share = *features.TopGroupShare
	}
	if features.ShareGrowthPP != nil {
		growth = perDocument(*features.ShareGrowthPP * 1000)
	}
	if features.Documents == 0 {
		empty = 1
	}
	return []float64{math.Log1p(float64(features.Documents)), float64(features.ActivePeriods),
		perDocument(float64(features.AuthorGroups)), share, growth, liveMonthsBetween(features.FirstObserved, month), empty}
}

func liveMonthsBetween(from, to string) float64 {
	start, startErr := time.Parse("2006-01", from)
	end, endErr := time.Parse("2006-01", to)
	if startErr != nil || endErr != nil {
		return math.NaN()
	}
	return float64((end.Year()-start.Year())*12 + int(end.Month()) - int(start.Month()))
}

// WeakSignalFeatures считает схему weak-signal-features-v1 тем же кодом, что радар для кандидата.
func WeakSignalFeatures(periods []LivePeriod, documents []StudyPaper) LiveClassifierFeatures {
	works, records := liveSplitDocuments(documents)
	topic := LiveTopic{StudyTopic: StudyTopic{Series: liveSeries(periods, documents)},
		DocumentsCount: len(documents), PapersCount: len(works), RecordsCount: len(records)}
	liveAssess(&topic, periods, documents)
	return liveClassifierInput("", "", []LiveTopic{topic}, nil, nil).Candidates[0].Features
}

// UseEmbeddedClassifier включает собственные сроки двух стадий только вместе со встроенной моделью.
func (s *TrendLiveService) UseEmbeddedClassifier() error {
	classify, version, err := s.EmbeddedClassifier()
	if err != nil {
		return err
	}
	s.SetClassifier(classify, version)
	s.classifierOwnBudget = true
	return nil
}

func (s *TrendLiveService) EmbeddedClassifier() (CandidateClassifier, string, error) {
	model, err := LoadWeakSignalModel()
	if err != nil {
		return nil, "", err
	}
	model.Version += liveSourceCoverageVersion
	classify := func(ctx context.Context, input LiveClassifierRequest) (LiveClassifierResponse, error) {
		s.mu.RLock()
		snapshot := s.snapshot
		s.mu.RUnlock()
		if snapshot == nil || len(snapshot.Periods) == 0 {
			return LiveClassifierResponse{}, ErrLiveNotReady
		}
		periods := snapshot.Periods
		month := periods[len(periods)-1].ID
		phrases := make([]string, len(input.Candidates))
		for i, candidate := range input.Candidates {
			// Идентификатор может склеить имя с сокращением; признаки нужны по исходному термину статьи.
			if candidate.phrase != "" {
				phrases[i] = liveWithoutAcronym(candidate.phrase)
			} else if key, ok := liveCandidatePhrase(candidate.ID); ok {
				phrases[i] = liveWithoutAcronym(strings.ReplaceAll(key, "-", " "))
			}
		}
		sourceCtx, cancel := context.WithTimeout(ctx, liveClassifierStage)
		defer cancel()
		evidence := s.weakSignalCandidateEvidence(ctx, sourceCtx, input.Candidates, phrases, periods, model.Embedding.Model)
		if err := ctx.Err(); err != nil {
			return LiveClassifierResponse{}, fmt.Errorf("embedded classifier canceled: %w", err)
		}
		output := LiveClassifierResponse{FeatureSchema: LiveClassifierSchema,
			Predictions: make([]LiveClassifierPrediction, len(input.Candidates))}
		for i, candidate := range input.Candidates {
			own := map[string]*WeakSignalEvidence{phrases[i]: evidence[i]}
			if phrases[i] == "" {
				// Без фразы кандидата окно берётся по его источникам, остальное неизмерено, и оценка вне области обучения.
				own = map[string]*WeakSignalEvidence{"": {Window: &LivePhraseWindow{
					LiveClassifierFeatures: candidate.Features, Total: candidate.Features.Documents}}}
			}
			output.Predictions[i] = model.prediction(candidate.ID, phrases[i:i+1], own, month)
			// Нехватка данных важнее полосы: оценка на медианах вместо входов ни о чём не говорит, и причина одна.
			switch p, gap := &output.Predictions[i], liveDataGap(own[phrases[i]]); {
			case gap != "":
				p.Label, p.DataGap = liveClassifierInsufficient, gap
				p.Basis += " " + liveDataGapBasis(gap, own[phrases[i]])
			case p.Label == liveClassifierUndecided:
				p.Basis += " " + model.Conformal.basis(p.Value)
			}
		}
		return output, nil
	}
	return classify, model.Version, nil
}

// Причины воздержания модели: источники не ответили (наш сбой), фразы нет нигде (её называют иначе), фраза
// в одной работе (судить рано).
const (
	liveGapNotCollected = "not_collected"
	liveGapNotFound     = "not_found"
	liveGapTooEarly     = "too_early"
)

// liveDataGap: один неответивший источник заменяется медианой, два — уже не решение по данным. Фраза из
// одной работы неотличима от сигнала эталона без статей, поэтому это правило радара, а не вход модели.
func liveDataGap(e *WeakSignalEvidence) string {
	if e == nil {
		return liveGapNotCollected
	}
	if gap := liveThinEvidence(e); gap != "" {
		return gap
	}
	missing := 0
	for _, absent := range []bool{e.Window == nil, e.History == nil, e.HN == nil, e.Rating == nil, e.Embedding == nil} {
		if absent {
			missing++
		}
	}
	if missing >= 2 {
		return liveGapNotCollected
	}
	return ""
}

func liveThinEvidence(e *WeakSignalEvidence) string {
	if e.Window == nil || e.History == nil || e.HN == nil {
		return ""
	}
	works, history := max(e.Window.Total, e.Window.Documents), liveHistoryWorks(e.History, math.MaxInt)
	if e.Journals != nil {
		works = max(works, e.Journals.Total)
	}
	switch hn := e.HN.Before + e.HN.Mid + e.HN.Recent; {
	case works == 0 && history == 0 && hn == 0:
		return liveGapNotFound
	case max(works, history) == 1 && hn == 0:
		return liveGapTooEarly
	}
	return ""
}

func liveDataGapBasis(gap string, e *WeakSignalEvidence) string {
	const tail = " Оценка модели показана для справки и на данных не основана."
	switch gap {
	case liveGapNotFound:
		return "Мало данных: фраза не найдена в собранных научных материалах, истории OpenAlex " +
			"и Hacker News — возможно, технологию называют иначе." + tail
	case liveGapTooEarly:
		return "Мало данных: фраза пока встречается в одной научной работе, не больше одной в " +
			"OpenAlex за всю историю, ни одной на Hacker News. Судить о распространении рано." + tail
	}
	missing := []string{}
	if e == nil {
		e = &WeakSignalEvidence{}
	}
	for _, source := range []struct {
		absent bool
		name   string
	}{{e.Window == nil, "окно arXiv"}, {e.History == nil, "OpenAlex"}, {e.HN == nil, "Hacker News"},
		{e.Rating == nil, "оценка модели языка"}, {e.Embedding == nil, "эмбеддинг"}} {
		if source.absent {
			missing = append(missing, source.name)
		}
	}
	return "Мало данных: не собраны " + strings.Join(missing, ", ") + "." + tail
}

// SetMaturityRater с nil оставляет вход неизмеренным, и модель считает по остальным.
func (s *TrendLiveService) SetMaturityRater(rater *LiveMaturityRater) { s.rater = rater }

// История и окно общие для фразы; журнальные свидетельства и оценка зрелости принадлежат конкретному кандидату.
func (s *TrendLiveService) weakSignalEvidence(ctx, sourceCtx context.Context, phrases []string, periods []LivePeriod,
	embeddingModel string, journals map[string]*LiveRatingEvidence,
) map[string]*WeakSignalEvidence {
	phrases = slices.Compact(slices.Sorted(slices.Values(phrases)))
	out := s.weakSignalFacts(sourceCtx, phrases, periods, embeddingModel)
	rows := make([]*WeakSignalEvidence, len(phrases))
	for i, phrase := range phrases {
		out[phrase].Journals = journals[phrase]
		rows[i] = out[phrase]
	}
	s.rateWeakSignalEvidence(ctx, phrases, rows)
	return out
}

func (s *TrendLiveService) weakSignalFacts(ctx context.Context, phrases []string, periods []LivePeriod,
	embeddingModel string,
) map[string]*WeakSignalEvidence {
	month := periods[len(periods)-1].ID
	windows := s.phraseWindows(ctx, phrases, periods)
	out := make(map[string]*WeakSignalEvidence, len(phrases))
	for _, phrase := range phrases {
		out[phrase] = &WeakSignalEvidence{}
		if w, ok := windows[phrase]; ok {
			out[phrase].Window = &w
		}
	}
	var history map[string]LiveTermHistory
	var hn map[string]*LiveHNCounts
	var embeddings [][]float64
	var sources sync.WaitGroup
	sources.Go(func() { embeddings = s.phraseEmbeddings(ctx, phrases, embeddingModel) })
	sources.Go(func() { history = s.termHistory().histories(ctx, phrases, month) })
	sources.Go(func() { hn = s.hnCache.counts(ctx, phrases, month) })
	sources.Wait()
	for i, phrase := range phrases {
		out[phrase].History, out[phrase].HN = history[phrase], hn[phrase]
		if embeddings != nil {
			out[phrase].Embedding = embeddings[i]
		}
	}
	return out
}

// Ожидание места для языковой оценки ограничено исходным сроком поиска, а не сроком сбора источников.
func (s *TrendLiveService) rateWeakSignalEvidence(ctx context.Context, phrases []string, rows []*WeakSignalEvidence) {
	var wg sync.WaitGroup
	slot := make(chan struct{}, liveRatingParallel)
	for i, phrase := range phrases {
		e := rows[i]
		if s.rater == nil || e.Window == nil && !e.hasJournalEvidence() || liveThinEvidence(e) != "" {
			continue
		}
		wg.Go(func() {
			select {
			case slot <- struct{}{}:
				defer func() { <-slot }()
			case <-ctx.Done():
				return
			}
			rating, err := s.rater.Rate(ctx, phrase, e.ratingEvidence())
			if err != nil {
				slog.Warn("radar maturity rating", "phrase", phrase, "error", err)
				return
			}
			e.Rating = rating
		})
	}
	wg.Wait()
}

// Ответ не той модели, что при обучении, отбрасывается: чужое пространство дало бы уверенную бессмыслицу.
func (s *TrendLiveService) phraseEmbeddings(ctx context.Context, phrases []string, model string) [][]float64 {
	if s.semantic == nil || len(phrases) == 0 {
		return nil
	}
	batch, err := s.semantic.Encode(ctx, phrases)
	switch {
	case err != nil:
		slog.Warn("radar phrase embeddings", "phrases", len(phrases), "error", err)
		return nil
	case len(batch.Vectors) != len(phrases) || batch.Model != "" && batch.Model != model:
		slog.Warn("radar phrase embeddings: unexpected answer", "model", batch.Model, "vectors", len(batch.Vectors))
		return nil
	}
	return batch.Vectors
}

// termHistory — один кэш истории на сервис: суточный лимит OpenAlex у адреса один (1000 кредитов, фраза —
// кредит); сначала без ключа, отказ переспрашивается с ключом.
func (s *TrendLiveService) termHistory() *liveHistoryCache {
	s.historyOnce.Do(func() {
		if s.history != nil {
			s.historyCache = newLiveHistoryCache(s.history)
			return
		}
		oa := s.newSources().oa
		s.historyCache = newLiveHistoryCache(oa.With("", ""), oa)
	})
	return s.historyCache
}

func (m WeakSignalModel) prediction(id string, phrases []string, evidence map[string]*WeakSignalEvidence, month string,
) LiveClassifierPrediction {
	inputs, embedding, ordered := weakSignalRow(phrases, evidence, month)
	m.applySourceCoverage(inputs, ordered, evidence)
	outside := m.clipToSupport(inputs)
	value, contributions, semantic := m.Score(inputs, embedding)
	label := m.decide(value)
	// После прижатия оценка посчитана по ближайшему значению, которое модель видела, и ни одна метка не
	// экстраполирует. Несимметрично намеренно: «сигнал» и «не могу решить» оставляют кандидата в пятнадцати как
	// новизну, а её модель не утверждает на значении вне выборки. «Не сигнал» остаётся видимым отказом с оговоркой
	// в объяснении: воздержание вернуло бы зрелые термины в пятнадцать.
	unseen := label != liveClassifierMature && len(outside) > 0
	if unseen {
		label = liveClassifierInsufficient
	}
	basis := fmt.Sprintf("Логистическая регрессия %s. Обучена на ста сигналах эталона организаторов против "+
		"зрелых технологий, угасшего и текущего хайпа и давних малоизвестных технологий по фразе технологии в "+
		"окне arXiv, истории OpenAlex, обсуждениях Hacker News, оценке зрелости моделью языка и смыслу названия. "+
		"Вклад группы признаков — сумма коэффициентов, умноженных на стандартизованные значения, в единицах "+
		"логарифма шансов; вместе со свободным членом %.2f они дают оценку. Оценка не откалибрована как "+
		"вероятность. Ниже порога модель относит кандидата к «не слабым сигналам» и не различает зрелую "+
		"технологию и шум.", m.Version, m.Intercept)
	if slices.Contains(ordered, "") {
		basis += " У кандидата нет фразы для поиска: окно взято по его проверенным источникам, остальные " +
			"источники не спрашивались, и оценка лежит вне области обучения."
	}
	if slices.ContainsFunc(ordered, func(phrase string) bool { return evidence[phrase].journalCoverage() }) {
		basis += " При наличии научных публикаций по той же фразе вне arXiv пустое окно arXiv имеет нулевой вклад. " +
			"Проверка достаточности данных остаётся обязательной; найденные научные работы используются для оценки зрелости. " +
			"Это отдельная политика покрытия источников, " +
			"не переобучение; прежние метрики качества её не оценивают."
	}
	if len(outside) > 0 {
		basis += " В строках «" + strings.Join(outside, "», «") + "» значения вне диапазона обучающей выборки: они " +
			"прижаты к его краю, и оценка посчитана по ближайшему значению, которое модель видела."
	}
	if unseen {
		basis += " Мало данных для решения: новизну на значении вне выборки модель не утверждает, оценка показана для справки."
	}
	return LiveClassifierPrediction{ID: id, LiveModelScore: LiveModelScore{Label: label, Value: value,
		Threshold: m.Threshold, Model: m.Model, Version: m.Version, FeatureSchema: LiveClassifierSchema,
		Basis: basis, Features: weakSignalExplanation(ordered, evidence, month, contributions, semantic, embedding != nil)}}
}

// Внутри конформной полосы ни порог, ни гарантия не дают метки; вне её порог совпадает с множеством.
func (m WeakSignalModel) decide(value float64) string {
	switch {
	case m.Conformal.undecided(value):
		return liveClassifierUndecided
	case value >= m.Threshold:
		return liveClassifierWeak
	}
	return liveClassifierMature
}

// Читателю нужно, какой источник сдвинул решение, а не наклон ряда: пять строк по источникам вместо сорока шести входов.
func weakSignalExplanation(ordered []string, evidence map[string]*WeakSignalEvidence, month string,
	contributions []float64, semantic float64, embedded bool,
) []LiveModelFeature {
	year, _ := strconv.Atoi(month[:min(4, len(month))])
	shown := slices.Compact([]string{ordered[0], ordered[len(ordered)-1]})
	names := weakSignalGroupNames()
	rows := make([]LiveModelFeature, 0, len(names)+1)
	start := 0
	for group, kinds := range weakSignalKinds() {
		parts := make([]string, 0, len(shown))
		for _, phrase := range shown {
			text := evidence[phrase].describe(group, year)
			if phrase != "" {
				text = "«" + phrase + "»: " + text
			}
			parts = append(parts, text)
		}
		row := LiveModelFeature{Name: names[group], Value: strings.Join(parts, "; ")}
		end := start + 2*len(kinds)
		for _, c := range contributions[start:end] {
			row.Contribution += c
		}
		rows, start = append(rows, row), end
	}
	meaning := LiveModelFeature{Name: "Смысл названия по эмбеддингу", Value: "не получен", Contribution: semantic}
	if embedded {
		meaning.Value = "учтён: шестнадцать главных компонент эмбеддинга F2LLM"
	}
	rows = append(rows, meaning)
	slices.SortStableFunc(rows, func(a, b LiveModelFeature) int {
		return cmp.Compare(math.Abs(b.Contribution), math.Abs(a.Contribution))
	})
	return rows
}

func (e *WeakSignalEvidence) describe(group, year int) string {
	switch {
	case e == nil:
	case group == 0 && e.journalCoverage():
		return e.coverageDescription()
	case group == 0 && e.Window != nil:
		return fmt.Sprintf("работ %d, месяцев с публикациями %d из %d, авторских групп %d",
			max(e.Window.Total, e.Window.Documents), e.Window.ActivePeriods, liveMonths, e.Window.AuthorGroups)
	case group == 1 && e.History != nil:
		total := liveHistoryWorks(e.History, year-1)
		if total == 0 {
			return fmt.Sprintf("работ до %d г. нет", year)
		}
		recent := total - liveHistoryWorks(e.History, year-4)
		return fmt.Sprintf("работ %d, из них за %d–%d гг. %.0f %%", total, year-3, year-1,
			100*float64(recent)/float64(total))
	case group == 2 && e.HN != nil:
		return fmt.Sprintf("упоминаний до %d г. %d, за %d–%d гг. %d, за %d–%d гг. %d", year-6, e.HN.Before,
			year-6, year-3, e.HN.Mid, year-2, year-1, e.HN.Recent)
	case group == 3 && e.Rating != nil:
		faded := map[bool]string{true: "да", false: "нет"}[e.Rating.Failed]
		confidence := "не указана"
		if !math.IsNaN(e.Rating.Confidence) {
			confidence = fmt.Sprintf("%.0f %%", e.Rating.Confidence*100)
		}
		return fmt.Sprintf("стадия %d из 4 (%s), угасла: %s, уверенность %s", e.Rating.Stage,
			liveRatingStageName(e.Rating.Stage), faded, confidence)
	}
	return "не измерено"
}

func weakSignalGroupNames() []string {
	return []string{"Окно arXiv за 14 месяцев", "История термина в OpenAlex", "Обсуждения на Hacker News",
		"Оценка зрелости моделью языка"}
}

// clipToSupport прижимает входы к диапазону обучения: за краем линейная модель продолжает наклон без данных,
// и один выброс (уверенность 0 при обученных 0,3–1) перевешивал все источники. Возвращает группы выпавших входов.
func (m WeakSignalModel) clipToSupport(inputs []float64) []string {
	outside := []string{}
	start := 0
	for group, kinds := range weakSignalKinds() {
		end := start + 2*len(kinds)
		for i := start; i < end; i++ {
			// NaN — пропуск, Score заменит его медианой, а она внутри диапазона.
			if clipped := min(max(inputs[i], m.Min[i]), m.Max[i]); !math.IsNaN(inputs[i]) && clipped != inputs[i] {
				inputs[i] = clipped
				outside = append(outside, weakSignalGroupNames()[group])
			}
		}
		start = end
	}
	return slices.Compact(outside)
}

func liveRatingStageName(stage int) string {
	names := []string{"неизвестна модели", "исследования", "прототип или пилот", "раннее внедрение", "зрелый стандарт"}
	if stage < 0 || stage >= len(names) {
		return "вне шкалы"
	}
	return names[stage]
}

// Через дефисы сборщик ищет фразу буквально одним словом, без перевода шлюзом.
func liveCandidatePhrase(id string) (string, bool) {
	if phrase, ok := strings.CutPrefix(id, "ngram:"); ok {
		return phrase, true
	}
	return strings.CutPrefix(id, liveExtractPrefix)
}

// Повтор аббревиатуры («reinforcement learning rl») почти не пишут, а в обучающей выборке его нет.
func liveWithoutAcronym(phrase string) string {
	words := strings.Fields(phrase)
	if len(words) > liveAcronymWords {
		return phrase
	}
	short := func(tokens []string) bool {
		return !slices.ContainsFunc(tokens, func(token string) bool { return len(token) > 5 })
	}
	for k := 1; k <= 3 && k < len(words)-1; k++ {
		if head := words[:k]; short(head) && liveSpells(strings.Join(head, ""), words[k:]) {
			return strings.Join(words[k:], " ")
		}
		if tail := words[len(words)-k:]; short(tail) && liveSpells(strings.Join(tail, ""), words[:len(words)-k]) {
			return strings.Join(words[:len(words)-k], " ")
		}
	}
	return phrase
}

// Длиннее названия от извлечения не бывают, а перебор разбиений растёт с числом слов.
const liveAcronymWords = 12

func liveSpells(acronym string, words []string) bool {
	if len(words) == 0 {
		return acronym == ""
	}
	word := words[0]
	if acronym != "" && strings.HasPrefix(acronym, word) && liveSpells(acronym[len(word):], words[1:]) {
		return true
	}
	if acronym != "" && acronym[0] == word[0] && liveSpells(acronym[1:], words[1:]) {
		return true
	}
	return slices.Contains([]string{"and", "of", "for", "the", "in", "on", "with", "to", "a", "via"}, word) &&
		liveSpells(acronym, words[1:])
}

type LiveFeatureRequest struct {
	Phrases []string     `json:"phrases"`
	Periods []LivePeriod `json:"periods"`
}

// LivePhraseWindow держит признаки на верхнем уровне, чтобы старый читатель их понимал.
type LivePhraseWindow struct {
	LiveClassifierFeatures `json:",inline"`
	// Total — все работы окна с фразой; Documents ограничен выдачей поиска.
	Total  int      `json:"total"`
	Titles []string `json:"titles"`
}

// Больше кандидатов, чем проверяется в поиске, в пачке не бывает.
const liveFeatureBatch = 64

// liveRatingTitles — столько свежих заголовков видела модель языка при обучении.
const liveRatingTitles = 5

const liveRatingParallel = 8

// WindowFeatures параллелит фразы по ядрам: поиск по окну упирается в процессор, а не в сеть.
func (s *TrendLiveService) WindowFeatures(ctx context.Context, req LiveFeatureRequest,
) (map[string]LivePhraseWindow, error) {
	out := make(map[string]LivePhraseWindow, len(req.Phrases))
	failures := make([]error, len(req.Phrases))
	var mu sync.Mutex
	var wg sync.WaitGroup
	// GOMAXPROCS, а не NumCPU: у контейнера сборщика квота четыре ядра из
	// тридцати двух, и тридцать две горутины только толкались бы за них.
	slot := make(chan struct{}, runtime.GOMAXPROCS(0))
	for i, phrase := range req.Phrases {
		slot <- struct{}{}
		wg.Go(func() {
			defer func() { <-slot }()
			result, err := s.searchLiteralWindow(ctx, LiveWindowSearch{Query: phrase, Periods: req.Periods})
			if err != nil {
				failures[i] = err
				return
			}
			// Свежие первыми; при равной дате — в порядке выдачи, как при сборе.
			recent := slices.Clone(result.Papers)
			slices.SortStableFunc(recent, func(a, b StudyPaper) int { return cmp.Compare(b.Date, a.Date) })
			titles := []string{}
			for _, paper := range recent[:min(len(recent), liveRatingTitles)] {
				titles = append(titles, LiveRatingTitle(paper.Title, paper.Date))
			}
			window := LivePhraseWindow{LiveClassifierFeatures: WeakSignalFeatures(req.Periods, result.Papers),
				Total: result.Total, Titles: titles}
			mu.Lock()
			out[phrase] = window
			mu.Unlock()
		})
	}
	wg.Wait()
	return out, errors.Join(failures...)
}

func (s *TrendLiveService) phraseWindows(ctx context.Context, phrases []string, periods []LivePeriod,
) map[string]LivePhraseWindow {
	forms := make(map[string][]string, len(phrases))
	for _, phrase := range phrases {
		if form := weakSignalWindowPhrase(phrase); form != "" {
			forms[form] = append(forms[form], phrase)
		}
	}
	out := make(map[string]LivePhraseWindow, len(phrases))
	client := s.newSources().http
	for chunk := range slices.Chunk(slices.Sorted(maps.Keys(forms)), liveFeatureBatch) {
		windows, err := s.windowBatch(ctx, client, LiveFeatureRequest{Phrases: chunk, Periods: periods})
		if err != nil {
			slog.Warn("radar phrase window", "phrases", len(chunk), "answered", len(windows), "error", err)
		}
		for form, window := range windows {
			for _, phrase := range forms[form] {
				out[phrase] = window
			}
		}
	}
	return out
}

func (s *TrendLiveService) windowBatch(ctx context.Context, client *http.Client, req LiveFeatureRequest,
) (map[string]LivePhraseWindow, error) {
	s.mu.RLock()
	local := len(s.window) > 0
	s.mu.RUnlock()
	if local {
		return s.WindowFeatures(ctx, req)
	}
	if s.collectorURL == "" {
		return nil, errors.New("окно arXiv недоступно: сборщик не задан")
	}
	body, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("окно arXiv: %w", err)
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost,
		strings.TrimRight(s.collectorURL, "/")+"/v1/features", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("окно arXiv: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	resp, err := client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("окно arXiv: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("окно arXiv: сборщик ответил %d", resp.StatusCode)
	}
	var windows map[string]LivePhraseWindow
	if err := json.UnmarshalRead(io.LimitReader(resp.Body, 4<<20), &windows); err != nil {
		return nil, fmt.Errorf("окно arXiv: %w", err)
	}
	return windows, nil
}
