package httpapi

import (
	"encoding/json/v2"
	"io"
	"log/slog"
	"net/http"

	"github.com/example/main-service/internal/platform/httpx"
)

// clientError пишет ошибки интерфейса в журнал сервиса — мониторинг фронта без внешнего сервиса.
func (a *API) clientError(w http.ResponseWriter, r *http.Request) {
	owner, ok := ownerFromContext(r)
	if !ok {
		httpx.Error(w, http.StatusUnauthorized, "missing claims")
		return
	}
	var body struct {
		Kind      string         `json:"kind"`
		Message   string         `json:"message"`
		Stack     string         `json:"stack"`
		URL       string         `json:"url"`
		UserAgent string         `json:"user_agent"`
		Extra     map[string]any `json:"extra"`
	}
	raw, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 16<<10))
	if err != nil || json.Unmarshal(raw, &body) != nil || body.Kind == "" {
		httpx.Error(w, http.StatusBadRequest, "invalid client error")
		return
	}
	slog.Warn("client error", "kind", clip(body.Kind, 64), "message", clip(body.Message, 500),
		"url", clip(body.URL, 300), "user", owner, "user_agent", clip(body.UserAgent, 200),
		"stack", clip(body.Stack, 4000), "extra", body.Extra)
	w.WriteHeader(http.StatusNoContent)
}

func clip(value string, limit int) string {
	if len(value) <= limit {
		return value
	}
	return value[:limit]
}
