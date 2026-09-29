package application

import (
	"strings"

	commonv1 "github.com/example/main-service/internal/platform/genproto/common/v1"
	"google.golang.org/protobuf/proto"
)

type verifyClaim struct {
	Claim   string `json:"claim"`
	Verdict string `json:"verdict"`
	Quote   string `json:"quote"`
	Reason  string `json:"reason"`
}

// Тематическая предпосылка не подтверждает всю гипотезу: пробел по любой существенной части
// не позволяет принять положительный вердикт, даже если модель выставила высокую уверенность.
func groundVerification(r verifyResult, sources []*commonv1.Source) verifyResult {
	if r.Verdict == verdictInsufficient {
		r.Supporting = nil
	}
	if r.Verdict != verdictSupported {
		return r
	}
	quotes := make([]string, 0, len(r.Claims))
	gaps := make([]string, 0, len(r.Claims))
	for i, claim := range r.Claims {
		switch {
		case strings.TrimSpace(claim.Claim) == "":
			gaps = append(gaps, "не указано проверяемое утверждение")
		case claim.Verdict != verdictSupported:
			gaps = append(gaps, claim.Claim)
		case quoteSource(sources, claim.Quote) == nil:
			gaps = append(gaps, claim.Claim)
			r.Claims[i].Verdict = verdictInsufficient
			r.Claims[i].Quote = ""
			r.Claims[i].Reason = "Подтверждающая цитата не найдена в переданных источниках."
		default:
			quotes = append(quotes, claim.Quote)
		}
	}
	if len(r.Claims) > 0 && len(gaps) == 0 {
		r.Supporting = quotes
		return r
	}
	r.Verdict = verdictInsufficient
	r.Confidence = nil
	r.Supporting = nil
	r.Rationale = "Недостаточно данных для подтверждения всей гипотезы: "
	if len(gaps) == 0 {
		r.Rationale += "нет проверки существенных утверждений и их связи с цитатами."
	} else {
		r.Rationale += "не подтверждены существенные утверждения — " + strings.Join(gaps, "; ") + "."
	}
	return r
}

// Совпадение темы или отдельных слов не доказывает происхождение цитаты. Допускаются только
// различия пробелов и мягких переносов, которые появляются при извлечении текста документа.
func quoteSource(sources []*commonv1.Source, quote string) *commonv1.Source {
	quote = normalizeVerificationQuote(quote)
	if quote == "" {
		return nil
	}
	for _, source := range sources {
		if strings.Contains(normalizeVerificationQuote(source.GetSnippet()), quote) {
			return source
		}
	}
	return nil
}

func normalizeVerificationQuote(text string) string {
	replacer := strings.NewReplacer(
		"\u00ad\r\n", "", "\u00ad\n", "", "\u00ad", "",
		"\x02\r\n", "", "\x02\n", "", "\x02", "",
		"–", "-", "‑", "-",
	)
	return strings.Join(strings.Fields(replacer.Replace(text)), " ")
}

// Позиции предыдущей проверки не становятся доказательствами новой: оставшиеся фрагменты
// сохраняются как контекст, а метки «за» и «против» берутся только из текущего результата.
func currentVerificationEvidence(
	existing []*commonv1.HypothesisEvidence, sources []*commonv1.Source, res verifyResult,
) []*commonv1.HypothesisEvidence {
	out := verifierEvidence(nil, sources, res)
	seen := make(map[string]bool, len(out)+len(existing))
	key := func(e *commonv1.HypothesisEvidence) string {
		return e.GetDocumentId() + "|" + e.GetChunkId() + "|" + e.GetFilename()
	}
	for _, e := range out {
		seen[key(e)] = true
	}
	for _, e := range existing {
		if e == nil || seen[key(e)] {
			continue
		}
		seen[key(e)] = true
		contextEvidence := proto.CloneOf(e)
		contextEvidence.Stance = stanceContext
		contextEvidence.Relation = ""
		out = append(out, contextEvidence)
	}
	return out
}
