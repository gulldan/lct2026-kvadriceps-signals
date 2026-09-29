package httpapi

import (
	"net/http"
	"strings"

	"github.com/example/main-service/internal/platform/httpx"
	"github.com/example/main-service/internal/platform/jwt"
)

const (
	keyClusters   = "clusters"
	keyHypotheses = "hypotheses"
)

// RestrictAutomation ограничивает техническую учётную запись явными продуктовыми операциями.
func RestrictAutomation(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		claims, ok := jwt.ClaimsFromContext(r.Context())
		if ok && hasAnyRole(claims, "automation") && !automationAllowed(r.Method, r.URL.Path) {
			httpx.Error(w, http.StatusForbidden, "operation unavailable to automation account")
			return
		}
		next.ServeHTTP(w, r)
	})
}

func automationAllowed(method, path string) bool {
	parts := strings.Split(strings.Trim(path, "/"), "/")
	if method == http.MethodGet {
		switch parts[0] {
		case "documents":
			return len(parts) == 1 || len(parts) == 3 && parts[2] == "chunks"
		case "kpis", keyClusters, keyHypotheses, "chats":
			return len(parts) <= 3
		default:
			return false
		}
	}
	if method == http.MethodPost {
		switch path {
		case "/admin/metrics", "/kpis", "/clusters/replace", "/hypotheses", "/hypotheses/generate", "/chats":
			return true
		}
		return len(
			parts,
		) == 3 &&
			(parts[0] == keyClusters &&
				parts[2] == "itc" ||
				parts[0] == keyHypotheses &&
					parts[2] == "itc" ||
				parts[0] == "chats" &&
					parts[2] == "messages")
	}
	return method == http.MethodPut && len(parts) == 2 && parts[0] == keyHypotheses
}

// RestrictReadOnly держит демо-учётку с ролью readonly в режиме просмотра: она видит всё, что
// позволяют остальные роли, запускает анализ и создаёт новое, но не удаляет, не правит
// существующие записи (архивация цели идёт через PUT) и не меняет настройки стенда.
func RestrictReadOnly(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		claims, ok := jwt.ClaimsFromContext(r.Context())
		if ok && hasAnyRole(claims, "readonly") && readOnlyBlocked(r.Method, r.URL.Path) {
			httpx.Error(w, http.StatusForbidden, "Демо-доступ: удалять и изменять данные нельзя")
			return
		}
		next.ServeHTTP(w, r)
	})
}

func readOnlyBlocked(method, path string) bool {
	switch method {
	case http.MethodGet, http.MethodHead, http.MethodOptions:
		return false
	case http.MethodPost:
		return strings.HasPrefix(path, "/admin/") || path == "/clusters/replace"
	}
	return true
}
