package httpapi

import (
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/example/auth/internal/application"
	"github.com/example/auth/internal/platform/config"
	"github.com/example/auth/internal/platform/httpx"
)

const refreshCookie = "rag_refresh"

func browserRequest(r *http.Request) bool { return r.Header.Get("X-Auth-Mode") == "browser" }

// Браузерные мутации требуют собственного заголовка и допустимого Origin.
// Произвольные сайты не могут выставить заголовок без разрешённого CORS.
func trustedOrigin(r *http.Request) bool {
	origin := r.Header.Get("Origin")
	if origin == "" {
		return true
	}
	u, err := url.Parse(origin)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.User != nil || u.Path != "" {
		return false
	}
	if strings.EqualFold(u.Host, r.Host) {
		return true
	}
	for _, allowed := range strings.Split(config.Get("AUTH_ALLOWED_ORIGINS", ""), ",") {
		if strings.TrimSpace(allowed) == origin {
			return true
		}
	}
	return false
}

func authOrigin(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !trustedOrigin(r) {
			httpx.Error(w, http.StatusForbidden, "origin denied")
			return
		}
		next(w, r)
	}
}

func cookie(token string, expires time.Time) *http.Cookie {
	c := &http.Cookie{Name: refreshCookie, Value: token, Path: "/api/v1/auth/", HttpOnly: true,
		Secure: true, SameSite: http.SameSiteStrictMode, Expires: expires}
	if config.Get("APP_ENV", "") != "production" {
		c.Secure = config.GetBool("AUTH_COOKIE_SECURE", true)
	}
	if token == "" {
		c.MaxAge = -1
	}
	return c
}

func respondTokens(w http.ResponseWriter, r *http.Request, status int, tokens *application.Tokens) {
	w.Header().Set("Cache-Control", "no-store")
	view := tokenView(tokens)
	if browserRequest(r) {
		http.SetCookie(w, cookie(tokens.RefreshToken, tokens.Identity.SessionExpiresAt))
		view.RefreshToken = ""
	}
	httpx.JSON(w, status, view)
}

func readRefresh(w http.ResponseWriter, r *http.Request) (string, bool) {
	var request refreshRequest
	if err := httpx.Decode(r, &request); err != nil {
		httpx.Error(w, http.StatusBadRequest, "invalid request")
		return "", false
	}
	if browserRequest(r) {
		value, err := r.Cookie(refreshCookie)
		if err != nil {
			return "", true
		}
		return value.Value, true
	}
	return request.RefreshToken, true
}
