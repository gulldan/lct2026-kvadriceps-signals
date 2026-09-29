package httpapi

import (
	"encoding/json/v2"
	"io"
	"net/http"

	"github.com/example/main-service/internal/platform/httpx"
	"github.com/example/main-service/internal/platform/indexdoc"
	"github.com/example/main-service/internal/platform/jwt"
)

func (a *API) sourceAccess(w http.ResponseWriter, r *http.Request) {
	claims, ok := jwt.ClaimsFromContext(r.Context())
	if !ok || claims.UserID == "" {
		httpx.Error(w, http.StatusUnauthorized, "missing claims")
		return
	}
	raw, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 1024))
	var body struct {
		RequestID string `json:"request_id"`
		Revoked   *bool  `json:"revoked"`
	}
	if err != nil || json.Unmarshal(raw, &body, json.RejectUnknownMembers(true)) != nil || body.Revoked == nil {
		httpx.Error(w, http.StatusBadRequest, "request_id and revoked are required")
		return
	}
	change := indexdoc.AccessChange{
		DocumentID: r.PathValue("id"), OwnerID: claims.UserID, RequestID: body.RequestID, Revoked: *body.Revoked,
	}
	lease := indexdoc.Lease{DocumentID: change.DocumentID, OwnerID: change.OwnerID, Token: change.RequestID}
	if lease.Validate() != nil {
		httpx.Error(w, http.StatusBadRequest, "invalid access request")
		return
	}
	if err = a.ingestion.SetSourceAccess(r.Context(), change); err != nil {
		a.fail(w, err)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusNoContent)
}

func (a *API) historicalSourceChunks(w http.ResponseWriter, r *http.Request, reader, snapshot string) {
	doc, chunks, err := a.ingestion.DocumentSnapshotChunks(r.Context(), reader, r.PathValue("id"), snapshot)
	if err != nil {
		a.fail(w, err)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	httpx.JSON(w, http.StatusOK, newDocumentChunksView(doc, chunks))
}
