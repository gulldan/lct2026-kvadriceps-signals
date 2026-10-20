package httpapi

import (
	"cmp"
	"net/http"
	"slices"
	"strconv"

	"github.com/example/main-service/internal/application"
	"github.com/example/main-service/internal/platform/httpx"
)

// Слой ранних денег читается из памяти реплики: запрос ничего не качает и не зовёт модель.

func (a *API) trendLiveMoney(w http.ResponseWriter, r *http.Request) {
	if _, ok := ownerFromContext(r); !ok {
		httpx.Error(w, http.StatusUnauthorized, "missing claims")
		return
	}
	if a.live == nil {
		httpx.Error(w, http.StatusServiceUnavailable, "live trends disabled")
		return
	}
	region, kind := r.URL.Query().Get("region"), r.URL.Query().Get("kind")
	if !slices.Contains([]string{"", "americas", "europe", "latam", "mena", "africa", "asia", "cis", "unknown"}, region) ||
		!slices.Contains([]string{"", "round", "grant", "accelerator", "development", "research"}, kind) {
		httpx.Error(w, http.StatusBadRequest, "invalid money region or kind")
		return
	}
	w.Header().Set("Cache-Control", "private, max-age=300")
	analysisJSON(w, http.StatusOK, a.live.MoneySummary(region, kind))
}

func (a *API) trendLiveMap(w http.ResponseWriter, r *http.Request) {
	if _, ok := ownerFromContext(r); !ok {
		httpx.Error(w, http.StatusUnauthorized, "missing claims")
		return
	}
	if a.live == nil {
		httpx.Error(w, http.StatusServiceUnavailable, "live trends disabled")
		return
	}
	w.Header().Set("Cache-Control", "private, max-age=300")
	analysisJSON(w, http.StatusOK, a.live.MapSummary())
}

func (a *API) trendLiveMoneyMatch(w http.ResponseWriter, r *http.Request) {
	if _, ok := ownerFromContext(r); !ok {
		httpx.Error(w, http.StatusUnauthorized, "missing claims")
		return
	}
	if a.live == nil {
		httpx.Error(w, http.StatusServiceUnavailable, "live trends disabled")
		return
	}
	terms := r.URL.Query()["term"]
	if len(terms) == 0 || len(terms) > 12 {
		httpx.Error(w, http.StatusBadRequest, "term: from 1 to 12 values required")
		return
	}
	for _, term := range terms {
		if len(term) > 160 {
			httpx.Error(w, http.StatusBadRequest, "term is longer than 160 bytes")
			return
		}
	}
	w.Header().Set("Cache-Control", "private, max-age=300")
	analysisJSON(w, http.StatusOK, a.live.MoneyMatch(terms))
}

// trendLiveInvestors читает вшитый справочник: неверный фильтр — 400, чтобы интерфейс не принял его за пустой список.
func (a *API) trendLiveInvestors(w http.ResponseWriter, r *http.Request) {
	if _, ok := ownerFromContext(r); !ok {
		httpx.Error(w, http.StatusUnauthorized, "missing claims")
		return
	}
	if a.live == nil {
		httpx.Error(w, http.StatusServiceUnavailable, "live trends disabled")
		return
	}
	values := r.URL.Query()
	offset, errOffset := strconv.Atoi(cmp.Or(values.Get("offset"), "0"))
	limit, errLimit := strconv.Atoi(cmp.Or(values.Get("limit"), "0"))
	page, err := a.live.Investors(application.LiveInvestorQuery{Region: values.Get("region"), Country: values.Get("country"),
		Type: values.Get("type"), Stage: values.Get("stage"), Rubric: values.Get("rubric"), Q: values.Get("q"),
		Sort: values.Get("sort"), Offset: offset, Limit: limit})
	if err != nil || errOffset != nil || errLimit != nil {
		httpx.Error(w, http.StatusBadRequest, "invalid investor filter")
		return
	}
	w.Header().Set("Cache-Control", "private, max-age=300")
	analysisJSON(w, http.StatusOK, page)
}
