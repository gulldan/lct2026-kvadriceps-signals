package httpapi

import (
	"net/http"

	"github.com/example/main-service/internal/application"
	"github.com/example/main-service/internal/platform/httpx"
)

func (a *API) getScoringWeights(w http.ResponseWriter, r *http.Request) {
	ownerID, ok := ownerFromContext(r)
	if !ok {
		httpx.Error(w, http.StatusUnauthorized, "missing claims")
		return
	}
	httpx.JSON(w, http.StatusOK, a.hypotheses.GetScoringWeights(r.Context(), ownerID))
}

// updateScoringWeights нормирует веса к сумме 1, чтобы объяснённая сводная оценка оставалась сравнимой.
func (a *API) updateScoringWeights(w http.ResponseWriter, r *http.Request) {
	ownerID, ok := ownerFromContext(r)
	if !ok {
		httpx.Error(w, http.StatusUnauthorized, "missing claims")
		return
	}
	var in application.ScoringWeights
	if err := decodeResource(r, &in); err != nil {
		httpx.Error(w, http.StatusBadRequest, err.Error())
		return
	}
	if msg, valid := validateScoringWeights(in); !valid {
		httpx.Error(w, http.StatusBadRequest, msg)
		return
	}
	saved := normalizeScoringWeights(in)
	if err := a.hypotheses.SetScoringWeights(r.Context(), ownerID, saved); err != nil {
		a.fail(w, err)
		return
	}
	httpx.JSON(w, http.StatusOK, saved)
}

func scoringWeightValues(w application.ScoringWeights) []float64 {
	return []float64{w.KPIFit, w.Evidence, w.Novelty, w.Value, w.RiskInv, w.TRLFit}
}

// validateScoringWeights возвращает сообщение для пользователя, поэтому по-русски.
func validateScoringWeights(w application.ScoringWeights) (string, bool) {
	sum := 0.0
	for _, v := range scoringWeightValues(w) {
		if v < 0 || v > 1 {
			return "каждый вес должен быть в диапазоне от 0 до 1", false
		}
		sum += v
	}
	if sum <= 0 {
		return "хотя бы один вес должен быть больше нуля", false
	}
	return "", true
}

func normalizeScoringWeights(w application.ScoringWeights) application.ScoringWeights {
	sum := 0.0
	for _, v := range scoringWeightValues(w) {
		sum += v
	}
	if sum == 0 {
		return w
	}
	return application.ScoringWeights{
		KPIFit:   w.KPIFit / sum,
		Evidence: w.Evidence / sum,
		Novelty:  w.Novelty / sum,
		Value:    w.Value / sum,
		RiskInv:  w.RiskInv / sum,
		TRLFit:   w.TRLFit / sum,
	}
}

func (a *API) getHypothesisRuntimeSettings(w http.ResponseWriter, r *http.Request) {
	ownerID, ok := ownerFromContext(r)
	if !ok {
		httpx.Error(w, http.StatusUnauthorized, "missing claims")
		return
	}
	httpx.JSON(w, http.StatusOK, a.hypotheses.GetRuntimeSettings(r.Context(), ownerID))
}

func (a *API) updateHypothesisRuntimeSettings(w http.ResponseWriter, r *http.Request) {
	ownerID, ok := ownerFromContext(r)
	if !ok {
		httpx.Error(w, http.StatusUnauthorized, "missing claims")
		return
	}
	var in application.HypothesisRuntimeSettings
	if err := decodeResource(r, &in); err != nil {
		httpx.Error(w, http.StatusBadRequest, err.Error())
		return
	}
	saved := application.NormalizeHypothesisRuntimeSettings(in)
	if err := a.hypotheses.SetRuntimeSettings(r.Context(), ownerID, saved); err != nil {
		a.fail(w, err)
		return
	}
	httpx.JSON(w, http.StatusOK, saved)
}
