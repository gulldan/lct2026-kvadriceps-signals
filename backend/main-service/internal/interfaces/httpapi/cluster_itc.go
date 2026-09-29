package httpapi

import (
	"net/http"

	"github.com/example/main-service/internal/application"
	"github.com/example/main-service/internal/platform/httpx"
)

// storeClusterITC пишет только посчитанный индекс: списочные проекции не должны заменять кластер.
func (a *API) storeClusterITC(w http.ResponseWriter, r *http.Request) {
	ownerID, ok := ownerFromContext(r)
	if !ok {
		httpx.Error(w, http.StatusUnauthorized, "missing claims")
		return
	}
	var in application.ITCInput
	if err := decodeResource(r, &in); err != nil {
		httpx.Error(w, http.StatusBadRequest, err.Error())
		return
	}
	c, err := a.hypotheses.StoreClusterITC(r.Context(), ownerID, r.PathValue("id"), in.ITC)
	if err != nil {
		a.fail(w, err)
		return
	}
	httpx.JSON(w, http.StatusOK, newClusterView(c))
}
