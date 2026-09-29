package application

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"unicode"

	commonv1 "github.com/example/main-service/internal/platform/genproto/common/v1"
)

// MaterialsRef может быть nil или без ключа: тогда любая формула считается неизвестной.
type MaterialsRef interface {
	Known(ctx context.Context, formula string) (bool, error)
}

const (
	noveltyNeutral     = 0.5
	noveltyPriorArtTop = 12
	// noveltyCloseOverlap — сколько общих токенов (от 4 рун) делает источник близкой работой.
	noveltyCloseOverlap = 3
	noveltyKnownPenalty = 0.2
)

type noveltyResult struct {
	Score         float64
	Rationale     string
	CloseMatches  int
	Retrieved     int
	MaterialKnown bool
}

// computeNovelty считает новизну детерминированно вместо самооценки модели: чем плотнее близкие работы
// в корпусе и если материал уже есть в Materials Project — тем ниже. Без сигнала — нейтральные 0,5.
func (s *HypothesisService) computeNovelty(
	ctx context.Context, ownerID string, item genItem,
) noveltyResult {
	density, retrieved, closeMatches := s.priorArtDensity(ctx, ownerID, item)

	score := noveltyNeutral
	if retrieved > 0 {
		score = clamp01(1 - density)
	}

	formula := materialFormula(item)
	known := false
	if formula != "" && s.materials != nil {
		if k, err := s.materials.Known(ctx, formula); err == nil && k {
			known = true
			score = clamp01(score - noveltyKnownPenalty)
		}
	}

	return noveltyResult{
		Score:         round3(score),
		Rationale:     noveltyRationale(retrieved, closeMatches, formula, known),
		CloseMatches:  closeMatches,
		Retrieved:     retrieved,
		MaterialKnown: known,
	}
}

func (s *HypothesisService) priorArtDensity(
	ctx context.Context, ownerID string, item genItem,
) (density float64, retrieved, closeMatches int) {
	resp, err := s.answerer.Answer(ctx, &commonv1.RagRequest{
		OwnerId: ownerID,
		Query:   noveltyQuery(item),
		Prompt:  noveltyRetrievalPrompt,
		TopK:    noveltyPriorArtTop,
	})
	if err != nil {
		return 0, 0, 0
	}
	hyp := tokenSet(noveltyQuery(item))
	if len(hyp) == 0 {
		return 0, 0, 0
	}
	seen := make(map[string]struct{})
	for _, source := range resp.GetSources() {
		key := noveltySourceKey(source)
		if key == "" {
			continue
		}
		if _, dup := seen[key]; dup {
			continue
		}
		seen[key] = struct{}{}
		if overlap(hyp, tokenSet(source.GetSnippet())) >= noveltyCloseOverlap {
			closeMatches++
		}
	}
	retrieved = len(seen)
	if retrieved == 0 {
		return 0, 0, 0
	}
	return float64(closeMatches) / float64(retrieved), retrieved, closeMatches
}

func noveltyQuery(item genItem) string {
	parts := make([]string, 0, 5)
	for _, p := range []string{
		item.MaterialSystem, item.CompositionChange, item.ProcessChange, item.TargetProperty,
	} {
		if v := compactText(p); v != "" {
			parts = append(parts, v)
		}
	}
	if len(parts) == 0 {
		return compactText(item.Statement)
	}
	return strings.Join(parts, " ")
}

// noveltyRetrievalPrompt нужен только ради поиска: считаются найденные источники, текст модели не используется.
const noveltyRetrievalPrompt = "Перечисли кратко уже известные из источников работы по этой теме."

func noveltySourceKey(src *commonv1.Source) string {
	if src.GetDocumentId() != "" {
		return "doc:" + src.GetDocumentId()
	}
	if src.GetFilename() != "" {
		return "file:" + strings.ToLower(strings.TrimSpace(src.GetFilename()))
	}
	if src.GetChunkId() != "" {
		return "chunk:" + src.GetChunkId()
	}
	return ""
}

var formulaPattern = regexp.MustCompile(`^(?:[A-Z][a-z]?\d*){2,}$`)

func materialFormula(item genItem) string {
	for _, field := range []string{item.MaterialSystem, item.CompositionChange} {
		if f := firstFormula(field); f != "" {
			return f
		}
	}
	return ""
}

func firstFormula(s string) string {
	for _, tok := range strings.FieldsFunc(s, func(r rune) bool {
		return unicode.IsSpace(r) || strings.ContainsRune(",;:()[]{}«»\"'", r)
	}) {
		tok = strings.TrimSpace(tok)
		if looksLikeFormula(tok) {
			return tok
		}
	}
	return ""
}

func looksLikeFormula(tok string) bool {
	if len(tok) < 2 || len(tok) > 24 {
		return false
	}
	if !formulaPattern.MatchString(tok) {
		return false
	}
	// Аббревиатуры вроде SEM и XRD шаблон элементов пропустил бы: у формулы есть строчная буква или цифра.
	hasLowerOrDigit := false
	for _, r := range tok {
		if unicode.IsLower(r) || unicode.IsDigit(r) {
			hasLowerOrDigit = true
			break
		}
	}
	return hasLowerOrDigit
}

func noveltyRationale(retrieved, closeMatches int, formula string, known bool) string {
	var b strings.Builder
	switch {
	case retrieved == 0:
		b.WriteString("Близких работ в корпусе не найдено")
	case closeMatches == 0:
		fmt.Fprintf(&b, "В корпусе %d источников по теме, близких совпадений нет", retrieved)
	default:
		fmt.Fprintf(&b, "%d близких работ в корпусе из %d по теме", closeMatches, retrieved)
	}
	switch {
	case formula == "":
	case known:
		fmt.Fprintf(&b, "; материал %s известен в Materials Project", formula)
	default:
		fmt.Fprintf(&b, "; материал %s не найден в Materials Project", formula)
	}
	b.WriteByte('.')
	return b.String()
}
