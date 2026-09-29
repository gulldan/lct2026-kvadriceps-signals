// Позиция по каждому источнику и поиск контраргументов — общие для генерации и проверки,
// чтобы доска не метила каждый фрагмент «подтверждает» по умолчанию.

package application

import (
	"context"
	"encoding/json"
	"strconv"
	"strings"
	"unicode/utf8"

	commonv1 "github.com/example/main-service/internal/platform/genproto/common/v1"
)

const evidenceRelationMax = 280

func relationText(reason string) string {
	r := compactText(reason)
	if utf8.RuneCountInString(r) <= evidenceRelationMax {
		return r
	}
	return strings.TrimRight(string([]rune(r)[:evidenceRelationMax]), " ,;:") + "…"
}

const stanceSnippetMax = 600

// counterRetrievalPrompt — одноразовая инструкция: нужны только найденные источники, текст ответа выбрасывается.
const counterRetrievalPrompt = "Перечисли кратко, что в источниках ограничивает или опровергает гипотезу."

// Позиции свидетельства; они же хранятся в HypothesisEvidence.stance.
const (
	stanceSupports    = "supports"
	stanceContradicts = "contradicts"
	stanceContext     = "context"
)

type stanceVerdict struct {
	Index  int    `json:"i"`
	Stance string `json:"stance"`
	Reason string `json:"reason"`
}

func normalizeStance(s string) string {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "supports", "support", "поддерживает", "подтверждает":
		return stanceSupports
	case "contradicts", "contradict", "противоречит", "опровергает":
		return stanceContradicts
	case "context", "background", "контекст", "метод":
		return stanceContext
	default:
		return ""
	}
}

func counterQuery(statement string) string {
	return statement + " ограничения противоречия отрицательный результат нестабильность деградация неудача"
}

func snippetForStance(snippet string) string {
	runes := []rune(strings.TrimSpace(snippet))
	if len(runes) <= stanceSnippetMax {
		return string(runes)
	}
	return string(runes[:stanceSnippetMax]) + "…"
}

func buildStancePrompt(statement string, ev []*commonv1.HypothesisEvidence) string {
	var b strings.Builder
	b.WriteString("Гипотеза: «")
	b.WriteString(statement)
	b.WriteString("».\n\nНиже пронумерованные фрагменты из документов. Для КАЖДОГО определи отношение к " +
		"гипотезе: подтверждает (supports), противоречит (contradicts) или только даёт контекст/метод " +
		"без прямого вывода (context). В поле reason кратко поясни, КАК фрагмент относится к гипотезе: " +
		"при каких условиях какой эффект получен, с конкретными числами из фрагмента. " +
		"Опирайся ТОЛЬКО на текст фрагмента.\n\n")
	for i, e := range ev {
		b.WriteString(strconv.Itoa(i + 1))
		b.WriteString(") ")
		b.WriteString(snippetForStance(e.GetSnippet()))
		b.WriteByte('\n')
	}
	b.WriteString(untrustedContextInstruction)
	b.WriteString(langInstruction)
	b.WriteString("\nВерни СТРОГО JSON-массив без markdown: " +
		`[{"i":1,"stance":"supports|contradicts|context",` +
		`"reason":"при каких условиях какой эффект (с цифрами) и как относится к гипотезе"}]`)
	return b.String()
}

func parseStances(answer string) []stanceVerdict {
	start := strings.IndexByte(answer, '[')
	end := strings.LastIndexByte(answer, ']')
	if start < 0 || end <= start {
		return nil
	}
	var out []stanceVerdict
	if err := json.Unmarshal([]byte(answer[start:end+1]), &out); err != nil {
		return nil
	}
	return out
}

// classifyEvidenceStances: без ответа модели остаётся прежняя нейтральная позиция, чтобы сбой не создал поддержку.
// Выигрывает первый вердикт по фрагменту: повтор индекса в шумном ответе не перевернёт метку.
func (s *HypothesisService) classifyEvidenceStances(
	ctx context.Context, ownerID, statement string, ev []*commonv1.HypothesisEvidence,
) {
	if len(ev) == 0 {
		return
	}
	resp, err := s.answerer.Answer(ctx, &commonv1.RagRequest{
		OwnerId: ownerID,
		Query:   statement,
		Prompt:  buildStancePrompt(statement, ev),
		TopK:    1,
	})
	if err != nil {
		return
	}
	labelled := make(map[int]struct{}, len(ev))
	for _, v := range parseStances(resp.GetAnswer()) {
		idx := v.Index - 1
		if idx < 0 || idx >= len(ev) {
			continue
		}
		if _, done := labelled[idx]; done {
			continue
		}
		if st := normalizeStance(v.Stance); st != "" {
			ev[idx].Stance = st
			if r := relationText(v.Reason); r != "" {
				ev[idx].Relation = r
			}
			labelled[idx] = struct{}{}
		}
	}
}

func (s *HypothesisService) counterEvidenceSources(
	ctx context.Context, ownerID, statement string, topK int32, excludeDocIDs []string,
) []*commonv1.Source {
	resp, err := s.answerer.Answer(ctx, &commonv1.RagRequest{
		OwnerId:            ownerID,
		Query:              counterQuery(statement),
		Prompt:             counterRetrievalPrompt,
		TopK:               topK,
		ExcludeDocumentIds: excludeDocIDs,
	})
	if err != nil {
		return nil
	}
	return resp.GetSources()
}
