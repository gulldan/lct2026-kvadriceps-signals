package application

import (
	"context"
	"crypto/sha256"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"fmt"
	"math"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/example/main-service/internal/platform/aiclients"
)

// Подсказка и разбор дословно повторяют исследование (rate.py): перефразировка сдвинула бы распределение стадий.

type LiveMaturityRating struct {
	Stage      int     `json:"stage"`
	Failed     bool    `json:"failed"`
	Confidence float64 `json:"confidence"`
}

// LiveRatingEvidence заполняется как в исследовании: до пяти заголовков через LiveRatingTitle, от свежих к давним.
type LiveRatingEvidence struct {
	Total  int
	Titles []string
}

const (
	liveRatingWait   = 120 * time.Second
	liveRatingTokens = 80
)

const liveRatingSystem = "Today is September 2026. You are a technology-foresight analyst. " +
	"Rate the maturity of the technology exactly as named, not the broader field it belongs to.\n" +
	"Reply with strict JSON only, no prose:\n" +
	`{"stage": 0-4, "failed_or_faded": true|false, "known_since": year|null, "confidence": 0-1}` + "\n" +
	"stage: 0 = unknown to you, or appeared after your knowledge; 1 = research (papers, lab results); " +
	"2 = prototype or pilot; 3 = early adoption (first commercial products, growing use); " +
	"4 = mainstream, mature standard.\n" +
	"failed_or_faded: true if it was hyped or tried but then faded, was abandoned or superseded.\n" +
	"known_since: year the technology or term first appeared, null if you do not know.\n" +
	"confidence: how sure you are about the stage, 0 to 1.\n" +
	"You also get evidence: how many arXiv papers from Aug 2025 to Sep 2026 mention the exact phrase " +
	"(window of 380k papers) and a few recent titles. " +
	"Use the evidence together with what you know; a rarely mentioned term can still be mature."

// Запасной разбор: модель иногда отвечает списком «**stage**: 3» вместо JSON.
var (
	liveRatingStageRe      = regexp.MustCompile(`(?i)stage\W*([0-4])\b`)
	liveRatingFailedRe     = regexp.MustCompile(`(?i)failed_or_faded\W*(true|false)`)
	liveRatingConfidenceRe = regexp.MustCompile(`(?i)confidence\W*([01](?:\.\d+)?)`)
)

// LiveRatingTitle повторяет evidence.py: 160 символов, а не байтов.
func LiveRatingTitle(title, date string) string {
	return truncateRunes(title, 160) + " (" + date[:min(7, len(date))] + ")"
}

type LiveMaturityRater struct {
	gen        aiclients.Generator
	configured func(context.Context) bool
	mu         sync.Mutex
	// known не ограничен: это десятки байт на ответ; неудача не запоминается.
	known map[[sha256.Size]byte]LiveMaturityRating
}

// NewLiveMaturityRater полагается на VLLM_REASONING_EFFORT шлюза: рассуждение выключено, как в исследовании.
func NewLiveMaturityRater(gen aiclients.Generator, configured func(context.Context) bool) *LiveMaturityRater {
	return &LiveMaturityRater{gen: gen, configured: configured, known: map[[sha256.Size]byte]LiveMaturityRating{}}
}

func (r *LiveMaturityRater) Rate(ctx context.Context, phrase string, evidence LiveRatingEvidence,
) (*LiveMaturityRating, error) {
	user := liveRatingPrompt(phrase, evidence)
	key := sha256.Sum256([]byte(liveRatingSystem + "\x00" + user))
	r.mu.Lock()
	known, ok := r.known[key]
	r.mu.Unlock()
	if ok {
		return &known, nil
	}
	if r.configured == nil || !r.configured(ctx) {
		return nil, errLLMNotConfigured
	}
	// Классификатор работает вне очереди карточек, но не должен занимать дополнительные места шлюза.
	if workers, shared := ctx.Value(liveWorkerSlotsKey{}).(*liveWaveWorkers); shared {
		if !workers.acquireRating(ctx) {
			return nil, fmt.Errorf("maturity rating wait: %w", ctx.Err())
		}
		defer workers.releaseRating()
	}
	out, err := liveGenerateWithin(ctx, r.gen, liveRatingWait, aiclients.GenRequest{
		System: liveRatingSystem, Query: user, MaxTokens: liveRatingTokens, Temperature: 0, JSONMode: true})
	if err != nil {
		return nil, fmt.Errorf("maturity rating: %w", err)
	}
	rating, ok := liveReadRating(out.Text)
	if !ok {
		return nil, fmt.Errorf("maturity rating: unreadable answer %q", truncateRunes(out.Text, 200))
	}
	r.mu.Lock()
	r.known[key] = rating
	r.mu.Unlock()
	return &rating, nil
}

func liveRatingPrompt(phrase string, evidence LiveRatingEvidence) string {
	var user strings.Builder
	fmt.Fprintf(&user, "Technology: %s\nEvidence: %d matching papers in the window.", phrase, evidence.Total)
	for _, title := range evidence.Titles {
		user.WriteString("\n- " + title)
	}
	if len(evidence.Titles) == 0 {
		user.WriteString("\n(no titles)")
	}
	return user.String()
}

// LiveRatingInputs — три входа в порядке обучения; нет оценки — NaN: ноль читался бы как «не угасла».
func LiveRatingInputs(rating *LiveMaturityRating) []float64 {
	if rating == nil {
		return []float64{math.NaN(), math.NaN(), math.NaN()}
	}
	failed := 0.0
	if rating.Failed {
		failed = 1
	}
	return []float64{float64(rating.Stage), failed, rating.Confidence}
}

func liveReadRating(text string) (LiveMaturityRating, bool) {
	if rating, ok := liveRatingFromJSON(text); ok {
		return rating, true
	}
	stage := liveRatingStageRe.FindStringSubmatch(text)
	if stage == nil {
		return LiveMaturityRating{}, false
	}
	rating := LiveMaturityRating{Stage: int(stage[1][0] - '0')}
	if failed := liveRatingFailedRe.FindStringSubmatch(text); failed != nil {
		rating.Failed = strings.EqualFold(failed[1], "true")
	}
	if confidence := liveRatingConfidenceRe.FindStringSubmatch(text); confidence != nil {
		rating.Confidence, _ = strconv.ParseFloat(confidence[1], 64)
	}
	return rating, true
}

func liveRatingFromJSON(text string) (LiveMaturityRating, bool) {
	_, rest, opened := strings.Cut(text, "{")
	object, _, closed := strings.Cut(rest, "}")
	var answer map[string]any
	if !opened || !closed || json.Unmarshal([]byte("{"+object+"}"), &answer,
		jsontext.AllowDuplicateNames(true), jsontext.AllowInvalidUTF8(true)) != nil {
		return LiveMaturityRating{}, false
	}
	stage, ok := liveRatingNumber(answer["stage"], true)
	if !ok {
		return LiveMaturityRating{}, false
	}
	confidence := 0.0
	if liveRatingTruthy(answer["confidence"]) {
		if confidence, ok = liveRatingNumber(answer["confidence"], false); !ok {
			return LiveMaturityRating{}, false
		}
	}
	return LiveMaturityRating{Stage: int(stage), Failed: liveRatingTruthy(answer["failed_or_faded"]),
		Confidence: confidence}, true
}

// liveRatingNumber — int() или float() Python: число, строка с числом, логическое.
func liveRatingNumber(value any, integer bool) (float64, bool) {
	switch v := value.(type) {
	case float64:
		if integer {
			return math.Trunc(v), true
		}
		return v, true
	case bool:
		if v {
			return 1, true
		}
		return 0, true
	case string:
		if integer {
			n, err := strconv.Atoi(strings.TrimSpace(v))
			return float64(n), err == nil
		}
		f, err := strconv.ParseFloat(strings.TrimSpace(v), 64)
		return f, err == nil
	default:
		return 0, false
	}
}

// liveRatingTruthy — истинность значения JSON по правилам Python.
func liveRatingTruthy(value any) bool {
	switch v := value.(type) {
	case nil:
		return false
	case bool:
		return v
	case float64:
		return v != 0
	case string:
		return v != ""
	case []any:
		return len(v) > 0
	case map[string]any:
		return len(v) > 0
	default:
		return true
	}
}
