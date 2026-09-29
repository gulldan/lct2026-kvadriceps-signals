package application

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	commonv1 "github.com/example/main-service/internal/platform/genproto/common/v1"
	dbv1 "github.com/example/main-service/internal/platform/genproto/db/v1"
)

const verifyEvidenceTop = 10

const (
	verdictSupported    = "supported"
	verdictRefuted      = "refuted"
	verdictMixed        = "mixed"
	verdictInsufficient = "insufficient"
)

type verifyResult struct {
	Verdict       string        `json:"verdict"`
	Confidence    *float64      `json:"confidence"`
	Rationale     string        `json:"rationale"`
	Claims        []verifyClaim `json:"claims"`
	Supporting    []string      `json:"supporting"`
	Contradicting []string      `json:"contradicting"`
}

// Verify ищет дважды — по утверждению и встречным запросом за опровержениями: поиск одной гипотезой
// подтверждал бы сам себя.
func (s *HypothesisService) Verify(ctx context.Context, ownerID, id string) (*commonv1.Hypothesis, error) {
	settings := s.runtimeSettings(ctx, ownerID)
	llmCtx, cancel := withDeadline(ctx, time.Duration(settings.GenerationTimeoutSec)*time.Second)
	defer cancel()
	h, err := s.GetHypothesis(llmCtx, ownerID, false, id)
	if err != nil {
		return nil, err
	}
	stmt := h.GetStatement()
	// Прогноз из радара проверяется и по работам, на которых он построен.
	radarNote, radarSrc := radarVerifyContext(h.GetDetail())
	prompt := verifyPrompt(stmt) + radarNote
	// Документы, которые сами являются готовыми гипотезами («слитые ответы»), не должны стать доказательством.
	excludeDocIDs := s.answerDocIDs(llmCtx, ownerID)
	main, mainSrc, model, merr := s.judgeStatement(llmCtx, ownerID, stmt, prompt, excludeDocIDs)
	if merr != nil {
		return nil, merr
	}
	// Сбой встречного прохода намеренно не блокирует основной вердикт.
	var counter verifyResult
	var counterSrc []*commonv1.Source
	if llmCtx.Err() == nil {
		counter, counterSrc, _, _ = s.judgeStatement(llmCtx, ownerID, counterQuery(stmt), prompt, excludeDocIDs)
	}

	combined := verifyResult{
		Verdict:       main.Verdict,
		Confidence:    main.Confidence,
		Rationale:     main.Rationale,
		Claims:        main.Claims,
		Supporting:    main.Supporting,
		Contradicting: append(append([]string{}, main.Contradicting...), counter.Contradicting...),
	}
	allSrc := append(append(radarSrc, mainSrc...), counterSrc...)
	combined = groundVerification(combined, allSrc)
	h.Evidence = currentVerificationEvidence(h.GetEvidence(), allSrc, combined)

	combined.Verdict = reconcileVerdict(combined.Verdict, h.GetEvidence())

	var pubWorks []ExternalWork
	if combined.Verdict == verdictInsufficient && llmCtx.Err() == nil {
		pubWorks = s.reverifyWithPubs(llmCtx, ownerID, stmt, prompt, h, &combined, &allSrc, &model, excludeDocIDs)
	}
	cancel()
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("verify canceled before persistence: %w", err)
	}
	// Дополнительный проход может исчерпать бюджет модели, но не должен лишить основной вердикт записи.
	// Родительский контекст сохраняет отмену запроса и потерю аренды фонового задания.
	persistCtx, persistCancel := withDeadline(ctx, verifyPersistTimeout)
	defer persistCancel()

	scores := confidenceSplit(combined.Verdict, combined.Confidence, h.GetEvidence())
	h.Assessment = mergeScores(mergeCheck(h.GetAssessment(), combined, model, pubWorks), scores)
	h.ConfidenceScore = &scores.Belief
	// Рейтинг пересчитывается сразу, иначе доска ранжирует по устаревшим признакам.
	s.applyRanking(persistCtx, h)
	summary := verifySummary(combined)
	if len(pubWorks) > 0 {
		summary += " (с учётом открытых публикаций)"
	}
	rev := &commonv1.HypothesisRevision{Action: "verified", EditorId: editorSystem, Summary: summary}
	if uerr := s.cat.UpdateHypothesis(persistCtx, &dbv1.UpdateHypothesisRequest{Hypothesis: h, Revision: rev}); uerr != nil {
		return nil, uerr
	}
	return s.GetHypothesis(persistCtx, ownerID, false, id)
}

func (s *HypothesisService) judgeStatement(
	ctx context.Context, ownerID, query, prompt string, excludeDocIDs []string,
) (verifyResult, []*commonv1.Source, string, error) {
	resp, err := s.answerer.Answer(opCtx(ctx, "verify"), &commonv1.RagRequest{
		OwnerId:            ownerID,
		Query:              query,
		Prompt:             prompt,
		TopK:               verifyEvidenceTop,
		ExcludeDocumentIds: excludeDocIDs,
	})
	if err != nil {
		return verifyResult{}, nil, "", err
	}
	res, perr := parseVerify(resp.GetAnswer())
	if perr != nil {
		return verifyResult{}, nil, "", perr
	}
	return res, resp.GetSources(), resp.GetModel(), nil
}

func verifyPrompt(statement string) string {
	var b strings.Builder
	b.WriteString("Проверяемая гипотеза: «")
	b.WriteString(statement)
	b.WriteString(`».

Ты — научный рецензент. Проверяй ВСЮ гипотезу только по контексту документов и публикаций.
Сначала разбей её на все существенные утверждения и оцени каждое в claims. Для причинной гипотезы
обязательно выдели сам эффект вмешательства: что изменилось, в какой системе или популяции,
по сравнению с чем и при каких условиях. Не пропускай этот эффект, даже если источник о нём молчит.
Отдельно проверяй предпосылки и объяснение механизма; верность предпосылки не доказывает вывод.
Тематическая близость, описание метода, сложность теста и рекомендации авторов — лишь контекст,
а не доказательство улучшения от внедрения. Изменение метрики при смене тестовых данных не доказывает
эффект вмешательства. Сохраняй направление изменения, метрику, сравниваемые условия и область применения.
Для каждого утверждения укажи supported, refuted или insufficient, дословную цитату и объяснение связи.
Если данных для утверждения нет, quote должен быть пустым, verdict — insufficient. Не подменяй
отсутствие доказательств опровержением и не достраивай причинную связь собственными знаниями.
Общий supported разрешён только если подтверждены ВСЕ существенные утверждения в заявленных условиях.
Пробел в доказательстве эффекта или перенос в непроверенную область означает общий insufficient,
даже если предпосылки подтверждены. Refuted требует прямого опровержения существенного утверждения.
Mixed означает противоречащие друг другу результаты проверки того же эффекта в сопоставимых условиях,
а не сочетание подтверждённой предпосылки с непроверенным выводом.
В supporting включай только цитаты, которые вместе доказывают всю гипотезу; при insufficient оставь [].
В contradicting включай только прямые противоречия, а не отсутствие сведений. Confidence — уверенность
в выбранном вердикте, а не вероятность будущего успеха гипотезы.`)
	b.WriteString(untrustedContextInstruction)
	b.WriteString("\nФормулируй утверждения и объяснения по-русски. Цитаты в quote, supporting и contradicting " +
		"сохраняй дословно на языке источника, без перевода и сокращения.")
	b.WriteString("\nВерни СТРОГО JSON без markdown:\n")
	b.WriteString(`{"verdict":"supported|refuted|mixed|insufficient","confidence":0..1,` +
		`"rationale":"что доказано и какие существенные части не проверены",` +
		`"claims":[{"claim":"существенное утверждение","verdict":"supported|refuted|insufficient",` +
		`"quote":"дословный фрагмент или пустая строка","reason":"как цитата доказывает именно это утверждение"}],` +
		`"supporting":["дословные фрагменты, подтверждающие всю гипотезу"],` +
		`"contradicting":["дословные фрагменты, противоречащие"]}`)
	return b.String()
}

func parseVerify(answer string) (verifyResult, error) {
	var r verifyResult
	if err := decodeJSONObject(answer, &r, json.Unmarshal); err != nil {
		if errors.Is(err, errNoJSONObject) {
			return verifyResult{}, errors.New("no JSON object in verify output")
		}
		return verifyResult{}, fmt.Errorf("parse verify result: %w", err)
	}
	return r, nil
}

func (s *HypothesisService) reverifyWithPubs(
	ctx context.Context, ownerID, stmt, prompt string, h *commonv1.Hypothesis,
	combined *verifyResult, allSrc *[]*commonv1.Source, model *string, excludeDocIDs []string,
) []ExternalWork {
	works, pubSrc := s.externalVerifyWorks(ctx, ownerID, stmt)
	if len(pubSrc) == 0 || ctx.Err() != nil {
		return nil
	}
	again, againSrc, pubModel, err := s.judgeStatement(ctx, ownerID, stmt, prompt+pubVerifyNote(works), excludeDocIDs)
	if err != nil {
		return nil
	}
	combined.Verdict = again.Verdict
	combined.Confidence = again.Confidence
	combined.Rationale = again.Rationale
	combined.Claims = again.Claims
	combined.Supporting = again.Supporting
	combined.Contradicting = append(combined.Contradicting, again.Contradicting...)
	*allSrc = append(append(*allSrc, againSrc...), pubSrc...)
	*combined = groundVerification(*combined, *allSrc)
	h.Evidence = currentVerificationEvidence(h.GetEvidence(), *allSrc, *combined)
	combined.Verdict = reconcileVerdict(combined.Verdict, h.GetEvidence())
	if pubModel != "" {
		*model = pubModel
	}
	return works
}

// externalVerifyWorks строит источники из аннотаций, чтобы фрагмент вердикта с цитатой публикации сохранил
// происхождение.
func (s *HypothesisService) externalVerifyWorks(
	ctx context.Context, ownerID, statement string,
) ([]ExternalWork, []*commonv1.Source) {
	if s.pubs == nil {
		return nil, nil
	}
	query := s.pubSearchQuery(ctx, ownerID, statement, "")
	if ctx.Err() != nil {
		return nil, nil
	}
	works, err := s.pubs.SearchWorks(ctx, query, statement, "", pubWorksLimit)
	if err != nil || len(works) == 0 {
		return nil, nil
	}
	if s.pubIngest != nil && ownerID != "" {
		go s.ingestWorks(context.WithoutCancel(ctx), ownerID, works)
	}
	return works, pubVerifySources(works)
}

func pubVerifySources(works []ExternalWork) []*commonv1.Source {
	out := make([]*commonv1.Source, 0, len(works))
	for _, w := range works {
		if strings.TrimSpace(w.Abstract) == "" {
			continue
		}
		out = append(out, &commonv1.Source{
			Filename: workFilename(w),
			Snippet:  strings.TrimSpace(w.Title + ". " + clipRunes(compactText(w.Abstract), pubAbstractMaxRunes)),
		})
	}
	return out
}

func pubVerifyNote(works []ExternalWork) string {
	if len(works) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("\n\nДополнительный контекст — открытые публикации (мировая практика), найденные " +
		"по теме гипотезы. Используй их наравне с документами выше, дословные фрагменты «за» и " +
		"«против» бери и из них; не выдумывай источники сверх списка:\n")
	for i, w := range works {
		fmt.Fprintf(&b, "[P%d] %s", i+1, w.Title)
		if w.Year > 0 {
			fmt.Fprintf(&b, " (%d)", w.Year)
		}
		if w.Venue != "" {
			fmt.Fprintf(&b, ", %s", w.Venue)
		}
		if w.DOI != "" {
			fmt.Fprintf(&b, ", doi:%s", w.DOI)
		}
		if a := compactText(w.Abstract); a != "" {
			b.WriteString(" — ")
			b.WriteString(clipRunes(a, pubAbstractMaxRunes))
		}
		b.WriteString("\n")
	}
	return b.String()
}

func mergeCheck(assessment string, res verifyResult, model string, works []ExternalWork) string {
	m := map[string]any{}
	if assessment != "" {
		_ = json.Unmarshal([]byte(assessment), &m)
	}
	check := map[string]any{
		"verdict": res.Verdict, "confidence": res.Confidence, keyRationale: res.Rationale,
		"claims": res.Claims, keyMethod: "whole-statement-v1",
		"supporting": res.Supporting, "contradicting": res.Contradicting,
		keyModel: model, "checked_at": time.Now().UTC().Format(time.RFC3339),
	}
	if len(works) > 0 {
		check["external_works"] = works
	}
	m["check"] = check
	b, err := json.Marshal(m)
	if err != nil {
		return assessment
	}
	return string(b)
}

type confidenceScores struct {
	Belief          float64 `json:"belief_score"`       // вероятность, что гипотеза верна
	VerdictConf     float64 `json:"verdict_confidence"` // уверенность в самом вердикте
	EvidenceQuality float64 `json:"evidence_quality"`   // сколько независимых доказательств
	Unverified      bool    `json:"unverified"`         // корпус не смог решить
}

func reconcileVerdict(main string, evidence []*commonv1.HypothesisEvidence) string {
	contra := false
	for _, e := range evidence {
		if e.GetStance() == stanceContradicts {
			contra = true
			break
		}
	}
	if !contra {
		return main
	}
	switch main {
	case verdictSupported:
		return verdictMixed
	default:
		return main
	}
}

// confidenceSplit: у опровергнутой гипотезы уверенность в вердикте высокая, а вера низкая; при нехватке
// данных вера падает и карточка помечается непроверенной, прежняя высокая уверенность не остаётся.
func confidenceSplit(verdict string, verdictConf *float64, evidence []*commonv1.HypothesisEvidence) confidenceScores {
	vc := 0.5
	if verdictConf != nil {
		vc = clamp01(*verdictConf)
	}
	sup, con := evidenceCounts(evidence)
	eq := evidenceQuality(sup, con)
	var belief float64
	unverified := false
	switch verdict {
	case verdictSupported:
		belief = vc
	case verdictRefuted:
		belief = 1 - vc
	case verdictMixed:
		belief = 0.4
	default: // verdictInsufficient
		belief = 0.3 * eq
		unverified = true
	}
	return confidenceScores{
		Belief: round3(clamp01(belief)), VerdictConf: round3(vc),
		EvidenceQuality: round3(eq), Unverified: unverified,
	}
}

// evidenceQuality: наличие обеих позиций значит, что вопрос действительно исследовали, — это ценится выше.
func evidenceQuality(sup, con int) float64 {
	total := sup + con
	if total == 0 {
		return 0
	}
	volume := clamp01(float64(total) / 5.0)
	balance := 0.6
	if sup > 0 && con > 0 {
		balance = 1.0
	}
	return clamp01(0.7*volume + 0.3*balance)
}

func mergeScores(assessment string, sc confidenceScores) string {
	m := map[string]any{}
	if assessment != "" {
		_ = json.Unmarshal([]byte(assessment), &m)
	}
	m["scores"] = sc
	b, err := json.Marshal(m)
	if err != nil {
		return assessment
	}
	return string(b)
}

const verifierMaxEvidence = 6

// verifierEvidence пропускает фрагменты без совпавшего источника, чтобы не выдумывать происхождение, и
// сверяется с имеющимися по (чанк, позиция), чтобы повторная проверка не плодила дубли.
func verifierEvidence(
	existing []*commonv1.HypothesisEvidence, sources []*commonv1.Source, res verifyResult,
) []*commonv1.HypothesisEvidence {
	seen := make(map[string]bool, len(existing))
	for _, e := range existing {
		seen[e.GetChunkId()+"|"+e.GetFilename()+"|"+e.GetStance()] = true
	}
	out := make([]*commonv1.HypothesisEvidence, 0, verifierMaxEvidence)
	add := func(fragments []string, stance string) {
		for _, frag := range fragments {
			if len(out) >= verifierMaxEvidence {
				return
			}
			src := quoteSource(sources, frag)
			if src == nil {
				continue
			}
			key := src.GetChunkId() + "|" + src.GetFilename() + "|" + stance
			if seen[key] {
				continue
			}
			seen[key] = true
			score := src.GetScore()
			ev := &commonv1.HypothesisEvidence{
				ChunkId: src.GetChunkId(), Filename: src.GetFilename(), Snippet: frag,
				Stance: stance, Score: &score, Ord: int32(100 + len(out)),
				PageStart: src.GetPageStart(), PageEnd: src.GetPageEnd(), SectionHeading: src.GetSectionHeading(),
			}
			if docID := src.GetDocumentId(); docID != "" {
				ev.DocumentId = &docID
			}
			out = append(out, ev)
		}
	}
	add(res.Supporting, stanceSupports)
	add(res.Contradicting, stanceContradicts)
	return out
}

func verifySummary(res verifyResult) string {
	labels := map[string]string{
		verdictSupported: "подтверждается", verdictRefuted: "опровергается",
		verdictMixed: "частично подтверждается", verdictInsufficient: "недостаточно данных",
	}
	label := labels[res.Verdict]
	if label == "" {
		label = res.Verdict
	}
	return "Проверка по корпусу: " + label
}
