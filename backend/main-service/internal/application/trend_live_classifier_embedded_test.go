package application

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"encoding/json/v2"
	"fmt"
	"math"
	"net/http"
	"net/http/httptest"
	"os"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/example/main-service/internal/platform/aiclients"
)

// Векторы пишет scripts/weak-signal/train.py: расчёт в Go обязан совпасть с обучением на Python.
func TestWeakSignalParity(t *testing.T) {
	raw, err := os.ReadFile("testdata/weak_signal_parity.json")
	if err != nil {
		t.Fatal(err)
	}
	var rows []struct {
		Phrases  []string `json:"phrases"`
		Evidence map[string]struct {
			Window  *LiveClassifierFeatures `json:"window"`
			History LiveTermHistory         `json:"history"`
			HN      *LiveHNCounts           `json:"hn"`
			LLM     *LiveMaturityRating     `json:"llm"`
		} `json:"evidence"`
		Inputs      []*float64 `json:"inputs"`
		Embedding   string     `json:"embedding"`
		Probability float64    `json:"probability"`
	}
	if err = json.Unmarshal(raw, &rows); err != nil {
		t.Fatal(err)
	}
	model, err := LoadWeakSignalModel()
	if err != nil {
		t.Fatal(err)
	}
	// Входы llm_* обучены на ответах этого промпта: правка промпта без переобучения тихо сдвинула бы признак.
	var artifact struct {
		Training struct {
			LLMRating struct {
				PromptSHA256 string `json:"prompt_sha256"`
			} `json:"llm_rating"`
		} `json:"training"`
	}
	if err = json.Unmarshal(liveWeakSignalWeights, &artifact); err != nil {
		t.Fatal(err)
	}
	if sum := sha256.Sum256([]byte(liveRatingSystem)); hex.EncodeToString(sum[:]) != artifact.Training.LLMRating.PromptSHA256 {
		t.Fatalf("промпт оценки зрелости не тот, на котором обучена модель: %x", sum)
	}
	for i, row := range rows {
		vector := decodeHalfFloats(t, row.Embedding)
		evidence := map[string]*WeakSignalEvidence{}
		for phrase, e := range row.Evidence {
			// Сверочный эмбеддинг — уже среднее по фразам: одинаковый у всех, он даёт то же среднее.
			item := &WeakSignalEvidence{History: e.History, HN: e.HN, Rating: e.LLM, Embedding: vector}
			if e.Window != nil {
				item.Window = &LivePhraseWindow{LiveClassifierFeatures: *e.Window}
			}
			evidence[phrase] = item
		}
		inputs, embedding, _ := weakSignalRow(row.Phrases, evidence, "2026-09")
		for j, want := range row.Inputs {
			if want == nil && !math.IsNaN(inputs[j]) || want != nil && math.Abs(inputs[j]-*want) > 1e-9 {
				t.Fatalf("строка %d, вход %s: %v, в Python %v", i, model.Inputs[j], inputs[j], want)
			}
		}
		if value, _, _ := model.Score(inputs, embedding); math.Abs(value-row.Probability) > 1e-9 {
			t.Fatalf("строка %d: оценка %v, в Python %v", i, value, row.Probability)
		}
	}
}

func decodeHalfFloats(t *testing.T, packed string) []float64 {
	t.Helper()
	raw, err := base64.StdEncoding.DecodeString(packed)
	if err != nil {
		t.Fatal(err)
	}
	out := make([]float64, len(raw)/2)
	for i := range out {
		h := binary.LittleEndian.Uint16(raw[2*i:])
		sign, exponent, fraction := 1.0, int(h>>10)&0x1f, float64(h&0x3ff)
		if h>>15 == 1 {
			sign = -1
		}
		switch exponent {
		case 0:
			out[i] = sign * math.Ldexp(fraction, -24)
		case 0x1f:
			t.Fatal("в сверочном эмбеддинге бесконечность")
		default:
			out[i] = sign * math.Ldexp(1+fraction/1024, exponent-15)
		}
	}
	return out
}

func TestEmbeddedWeakSignalClassifier(t *testing.T) {
	const canonicalID = "extract:misnamed-hybrid-kv"
	service := NewTrendLiveService(nil, nil)
	periods := livePeriods(time.Date(2026, 9, 17, 12, 0, 0, 0, time.UTC))
	for i := range periods {
		periods[i].Population = 25000
	}
	service.snapshot = &LiveSnapshot{Periods: periods}
	service.history = fakeOpenAlex(t)
	hn := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits := 2
		if q := r.URL.Query().Get("query"); strings.Contains(q, "vloc") || strings.Contains(q, "nowhere") {
			hits = 0
		}
		_, _ = fmt.Fprintf(w, `{"nbHits":%d}`, hits)
	}))
	t.Cleanup(hn.Close)
	service.hnCache = newLiveHNCache(hn.URL)
	model, err := LoadWeakSignalModel()
	if err != nil {
		t.Fatal(err)
	}
	encoder := &semanticTestEncoder{batch: LiveEmbeddingBatch{Model: model.Embedding.Model,
		Vectors: [][]float64{model.Embedding.Mean, model.Embedding.Mean, model.Embedding.Mean}}}
	service.semantic = encoder
	rated := aiclients.GenResponse{Text: `{"stage": 1, "failed_or_faded": false, "known_since": null, "confidence": 0.8}`}
	gen := &qualificationGenerator{replies: []aiclients.GenResponse{rated}}
	service.SetMaturityRater(NewLiveMaturityRater(gen, func(context.Context) bool { return true }))
	window := make([]StudyPaper, 0, 7)
	for i := range 6 {
		window = append(window, StudyPaper{ID: fmt.Sprintf("w%d", i), Title: fmt.Sprintf("Hybrid KV cache, part %d", i),
			Date: periods[8+i].Start, Authors: []string{fmt.Sprintf("author %d", i)}})
	}
	window = append(window, StudyPaper{ID: "other", Title: "Unrelated survey", Date: periods[0].Start},
		StudyPaper{ID: "one", Title: "VLoc bench for visual localization", Date: periods[12].Start})
	service.setWindow(window, map[string]LivePaperMetadata{})
	classify, version, err := service.EmbeddedClassifier()
	if err != nil {
		t.Fatal(err)
	}
	input := LiveClassifierRequest{FeatureSchema: LiveClassifierSchema, ModelVersion: version, Candidates: []LiveClassifierCandidate{
		{ID: canonicalID, Name: "Гибридный KV-кэш", phrase: "hybrid kv", Features: LiveClassifierFeatures{Documents: 3}},
		{ID: "semantic:1", Name: "Без фразы", Features: LiveClassifierFeatures{Documents: 4, AuthorGroups: 4}},
		{ID: "ngram:vloc-bench", Name: "VLoc Bench", Features: LiveClassifierFeatures{Documents: 1}},
		{ID: "ngram:nowhere-tech", Name: "Нигде", Features: LiveClassifierFeatures{Documents: 1}},
	}}
	output, err := classify(t.Context(), input)
	if err != nil {
		t.Fatal(err)
	}
	scores, err := liveValidatePredictions(input, output)
	if err != nil {
		t.Fatal(err)
	}
	score := scores[canonicalID]
	logit, lines := model.Intercept, make([]string, 0, len(score.Features))
	for _, feature := range score.Features {
		logit += feature.Contribution
		lines = append(lines, feature.Name+": "+feature.Value)
	}
	text := strings.Join(lines, "\n")
	for _, want := range []string{"«hybrid kv»: работ 6,", "«hybrid kv»: работ 15,", "за 2024–2025 гг. 2",
		"стадия 1 из 4", "учтён"} {
		if !strings.Contains(text, want) {
			t.Fatalf("в объяснении нет %q:\n%s", want, text)
		}
	}
	if math.Abs(logit-math.Log(score.Value/(1-score.Value))) > 1e-9 {
		t.Fatalf("вклады не складываются в оценку: %+v", score)
	}
	asked := slices.ContainsFunc(gen.requests, func(r aiclients.GenRequest) bool {
		return strings.Contains(r.Query, "Technology: hybrid kv\nEvidence: 6 matching papers in the window.\n- Hybrid KV cache, part 5")
	})
	// Фразам, которых нигде нет или которые в одной работе, шлюз не нужен.
	if strings.Join(encoder.texts, "|") != "hybrid kv|nowhere tech|vloc bench" || !asked || len(gen.requests) != 1 ||
		score.DataGap != "" {
		t.Fatalf("фраза ушла к эмбеддеру или модели языка не в той форме: %q %+v", encoder.texts, gen.requests)
	}
	// Мало данных — по трём разным причинам, и модель тогда не выносит решения.
	for id, gap := range map[string]string{"ngram:vloc-bench": liveGapTooEarly, "ngram:nowhere-tech": liveGapNotFound,
		"semantic:1": liveGapNotCollected} {
		got := scores[id]
		if got.Label != liveClassifierInsufficient || got.DataGap != gap || !strings.Contains(got.Basis, "Мало данных") {
			t.Fatalf("%s: ждали «мало данных» (%s), получили %s %q", id, gap, got.Label, got.DataGap)
		}
	}
}

func TestLiveWithoutAcronym(t *testing.T) {
	for phrase, want := range map[string]string{
		"reinforcement learning rl":                           "reinforcement learning",
		"regret weighted payoff sampling rwps":                "regret weighted payoff sampling",
		"security orchestration automation and response soar": "security orchestration automation and response",
		"grpo group relative policy optimization":             "group relative policy optimization",
		"ra vla retrieval augmented vla":                      "retrieval augmented vla",
		"llm agent":                                           "llm agent",
		"hybrid kv cache":                                     "hybrid kv cache",
		"mist mid trained security transformer":               "mist mid trained security transformer",
	} {
		if got := liveWithoutAcronym(phrase); got != want {
			t.Fatalf("%q: %q, ждали %q", phrase, got, want)
		}
	}
}

func TestLiveCandidateOrderAbstained(t *testing.T) {
	// Промежуточная уверенность без модели раньше замыкала цикл сравнения трёх кандидатов.
	ordered := []LiveTopic{
		{Name: "решение", Confidence: &LiveConfidence{Value: 0.1},
			ModelScore: &LiveModelScore{Label: liveClassifierWeak, Value: 0.5, Threshold: 0.4}},
		{Name: "воздержание", Confidence: &LiveConfidence{Value: 0.9},
			ModelScore: &LiveModelScore{Label: liveClassifierInsufficient, Value: 0.99, Threshold: 0.4}},
		{Name: "без оценки", Confidence: &LiveConfidence{Value: 0.5}},
	}
	for i, earlier := range ordered {
		for _, later := range ordered[i+1:] {
			if liveCandidateOrder(earlier, later) >= 0 || liveCandidateOrder(later, earlier) <= 0 {
				t.Errorf("нарушен порядок решений и транзитивность: %s должен быть выше %s", earlier.Name, later.Name)
			}
		}
	}
}
