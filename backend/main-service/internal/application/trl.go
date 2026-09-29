package application

import (
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	commonv1 "github.com/example/main-service/internal/platform/genproto/common/v1"
	dbv1 "github.com/example/main-service/internal/platform/genproto/db/v1"
)

const trlEvidenceTop = 8

//go:embed ugt_rubric.json
var ugtRubricRaw []byte

type ugtLevel struct {
	Level      int      `json:"level"`
	Name       string   `json:"name"`
	Definition string   `json:"definition"`
	Criteria   []string `json:"criteria"`
	Signals    []string `json:"signals"`
}

type ugtRubric struct {
	Standard   string     `json:"standard"`
	Scale      string     `json:"scale"`
	Method     string     `json:"method"`
	MethodNote string     `json:"method_note"`
	Levels     []ugtLevel `json:"levels"`
}

func loadUGT() ugtRubric {
	var r ugtRubric
	_ = json.Unmarshal(ugtRubricRaw, &r)
	return r
}

func TRLRubricJSON() []byte { return ugtRubricRaw }

func (s *HypothesisService) TRLRubric() []byte { return ugtRubricRaw }

func ugtLevelByNumber(n int) ugtLevel {
	for _, l := range loadUGT().Levels {
		if l.Level == n {
			return l
		}
	}
	return ugtLevel{}
}

type trlLevelVerdict struct {
	Level int    `json:"level"`
	Met   bool   `json:"met"`
	Note  string `json:"note"`
}

type trlAnketa struct {
	Levels []trlLevelVerdict `json:"levels"`
}

func (s *HypothesisService) AssessTRL(ctx context.Context, ownerID, id string) (*commonv1.Hypothesis, error) {
	ctx, cancel := withDeadline(ctx, llmSinglePassTimeout)
	defer cancel()
	h, err := s.GetHypothesis(ctx, ownerID, false, id)
	if err != nil {
		return nil, err
	}
	resp, aerr := s.answerer.Answer(opCtx(ctx, "assess_trl"), &commonv1.RagRequest{
		OwnerId: ownerID,
		Query:   h.GetStatement(),  // короткий текст — для поиска и переранжирования
		Prompt:  buildTRLPrompt(h), // длинная анкета — только модели
		TopK:    trlEvidenceTop,
	})
	if aerr != nil {
		return nil, aerr
	}
	anketa, perr := parseTRLAnketa(resp.GetAnswer())
	if perr != nil {
		return nil, perr
	}
	level := gateTRL(anketa.Levels)
	if level < 1 {
		level = 1 // сформулированная гипотеза — минимум УГТ 1, в базе ограничение 1..9
	}
	trl32 := int32(level)
	h.Trl = &trl32
	h.Assessment = mergeTRL(h.GetAssessment(), level, anketa, resp.GetModel())
	s.applyRanking(ctx, h)
	rev := &commonv1.HypothesisRevision{
		Action: actionScoreOverride, EditorId: editorSystem,
		Summary: fmt.Sprintf("Готовность по ГОСТ: УГТ %d", level),
	}
	if uerr := s.cat.UpdateHypothesis(ctx, &dbv1.UpdateHypothesisRequest{Hypothesis: h, Revision: rev}); uerr != nil {
		return nil, uerr
	}
	return s.GetHypothesis(ctx, ownerID, false, id)
}

// gateTRL применяет правило ГОСТ Р 58048-2017 (п. В.10) в коде, а не доверяет модели: уровень — наибольший L,
// при котором выполнены все уровни 1..L. Модель только заполняет анкету.
func gateTRL(verdicts []trlLevelVerdict) int {
	met := make(map[int]bool, len(verdicts))
	for _, v := range verdicts {
		met[v.Level] = v.Met
	}
	level := 0
	for l := 1; l <= 9; l++ {
		if !met[l] {
			break
		}
		level = l
	}
	return level
}

func buildTRLPrompt(h *commonv1.Hypothesis) string {
	var b strings.Builder
	b.WriteString("Гипотеза: «")
	b.WriteString(h.GetStatement())
	b.WriteString("».")
	if r := h.GetRationale(); r != "" {
		b.WriteString(" Обоснование: ")
		b.WriteString(r)
	}
	if evs := h.GetEvidence(); len(evs) > 0 {
		b.WriteString(" Доказательства из базы знаний: ")
		for i, e := range evs {
			if i >= 4 {
				break
			}
			b.WriteString("• ")
			b.WriteString(e.GetSnippet())
			b.WriteString(" ")
		}
	}
	b.WriteString("\n\nТы — эксперт по оценке уровня готовности технологии (УГТ) по ГОСТ Р 58048-2017. " +
		"Правило: уровень считается достигнутым, ТОЛЬКО если выполнены ВСЕ его критерии и все критерии " +
		"предыдущих уровней. Опираясь на гипотезу и приведённый выше контекст из документов, для КАЖДОГО " +
		"уровня 1–9 строго определи, выполнены ли ВСЕ его критерии. Будь консервативен: при недостатке " +
		"данных или отсутствии доказательств — НЕ выполнено (false). Уровни и их критерии:\n")
	for _, l := range loadUGT().Levels {
		fmt.Fprintf(&b, "УГТ %d — %s:\n", l.Level, l.Name)
		for _, c := range l.Criteria {
			b.WriteString("  - ")
			b.WriteString(c)
			b.WriteString("\n")
		}
	}
	b.WriteString(untrustedContextInstruction)
	b.WriteString(langInstruction)
	b.WriteString("\nВерни СТРОГО JSON без markdown по всем 9 уровням: " +
		`{"levels":[{"level":1,"met":true,"note":"кратко почему"}, ... ,{"level":9,"met":false,"note":""}]}`)
	return b.String()
}

func parseTRLAnketa(answer string) (trlAnketa, error) {
	var a trlAnketa
	if err := decodeJSONObject(answer, &a, json.Unmarshal); err != nil {
		if errors.Is(err, errNoJSONObject) {
			return trlAnketa{}, errors.New("no JSON object in TRL output")
		}
		return trlAnketa{}, fmt.Errorf("parse TRL anketa: %w", err)
	}
	if len(a.Levels) == 0 {
		return trlAnketa{}, errors.New("TRL anketa has no levels")
	}
	return a, nil
}

func mergeTRL(assessment string, level int, anketa trlAnketa, model string) string {
	m := map[string]any{}
	if assessment != "" {
		_ = json.Unmarshal([]byte(assessment), &m)
	}
	lvl := ugtLevelByNumber(level)
	levels := make([]map[string]any, 0, len(anketa.Levels))
	noteByLevel := make(map[int]string, len(anketa.Levels))
	for _, v := range anketa.Levels {
		levels = append(levels, map[string]any{keyLevel: v.Level, "met": v.Met, "note": v.Note})
		noteByLevel[v.Level] = v.Note
	}
	m["trl"] = map[string]any{
		keyLevel:      level,
		keyName:       lvl.Name,
		keyRationale:  trlRationale(level, noteByLevel),
		keyMethod:     "ugt-gating",
		"standard":    loadUGT().Standard,
		"assessed_at": time.Now().UTC().Format(time.RFC3339),
		keyModel:      model,
		"levels":      levels,
	}
	b, err := json.Marshal(m)
	if err != nil {
		return assessment
	}
	return string(b)
}

func trlRationale(level int, noteByLevel map[int]string) string {
	lvl := ugtLevelByNumber(level)
	var b strings.Builder
	fmt.Fprintf(&b, "Достигнут УГТ %d: %s.", level, lvl.Name)
	if n := strings.TrimSpace(noteByLevel[level]); n != "" {
		b.WriteString(" ")
		b.WriteString(n)
	}
	if level < 9 {
		next := ugtLevelByNumber(level + 1)
		if n := strings.TrimSpace(noteByLevel[level+1]); n != "" {
			fmt.Fprintf(&b, " Для УГТ %d (%s) не хватает: %s", level+1, next.Name, n)
		}
	}
	return b.String()
}
