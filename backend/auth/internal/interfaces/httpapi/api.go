package httpapi

import (
	"context"
	"errors"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/example/auth/internal/application"
	"github.com/example/auth/internal/domain"
	"github.com/example/auth/internal/platform/httpx"
)

type API struct {
	svc *application.AuthService
}

func New(svc *application.AuthService) *API { return &API{svc: svc} }

func (a *API) Routes(mux *http.ServeMux) {
	mux.HandleFunc("POST /login", authOrigin(a.login))
	mux.HandleFunc("POST /refresh", authOrigin(a.refresh))
	mux.HandleFunc("POST /logout", authOrigin(a.logout))
	mux.HandleFunc("POST /register", authOrigin(a.register))
}

type loginRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

type refreshRequest struct {
	RefreshToken string `json:"refresh_token"`
}

type registerRequest struct {
	Username string   `json:"username"`
	Password string   `json:"password"`
	Roles    []string `json:"roles,omitempty"`
}

type userView struct {
	ID       string   `json:"id"`
	Username string   `json:"username"`
	Roles    []string `json:"roles"`
}

type tokenResponse struct {
	AccessToken  string    `json:"access_token"`
	RefreshToken string    `json:"refresh_token,omitempty"`
	TokenType    string    `json:"token_type"`
	ExpiresAt    time.Time `json:"expires_at"`
	User         userView  `json:"user"`
}

func (a *API) login(w http.ResponseWriter, r *http.Request) {
	var req loginRequest
	if err := httpx.Decode(r, &req); err != nil {
		httpx.Error(w, http.StatusBadRequest, err.Error())
		return
	}
	if req.Username == "" || req.Password == "" {
		httpx.Error(w, http.StatusBadRequest, "username and password are required")
		return
	}
	tokens, err := a.svc.Login(r.Context(), req.Username, req.Password, clientIP(r))
	if err != nil {
		a.fail(w, err)
		return
	}
	respondTokens(w, r, http.StatusOK, tokens)
}

func (a *API) refresh(w http.ResponseWriter, r *http.Request) {
	token, ok := readRefresh(w, r)
	if !ok {
		return
	}
	if token == "" {
		httpx.Error(w, http.StatusUnauthorized, "refresh session required")
		return
	}
	tokens, err := a.svc.Refresh(r.Context(), token)
	if err != nil {
		a.fail(w, err)
		return
	}
	respondTokens(w, r, http.StatusOK, tokens)
}

func (a *API) logout(w http.ResponseWriter, r *http.Request) {
	token, ok := readRefresh(w, r)
	if !ok {
		return
	}
	if err := a.svc.Logout(r.Context(), token); err != nil {
		a.fail(w, err)
		return
	}
	http.SetCookie(w, cookie("", time.Unix(1, 0)))
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusNoContent)
}

func (a *API) register(w http.ResponseWriter, r *http.Request) {
	var req registerRequest
	if err := httpx.Decode(r, &req); err != nil {
		httpx.Error(w, http.StatusBadRequest, err.Error())
		return
	}
	if req.Username == "" || req.Password == "" {
		httpx.Error(w, http.StatusBadRequest, "username and password are required")
		return
	}
	admin, _ := r.Context().Value(adminKey{}).(bool)
	var roles []string
	if admin {
		roles = req.Roles
	}
	tokens, err := a.svc.Register(r.Context(), req.Username, req.Password, roles)
	if err != nil {
		a.fail(w, err)
		return
	}
	if admin {
		// Сессию новой учётки администратору не отдаём: её cookie заменила бы его собственную.
		w.Header().Set("Cache-Control", "no-store")
		httpx.JSON(w, http.StatusCreated, tokenView(tokens).User)
		return
	}
	respondTokens(w, r, http.StatusCreated, tokens)
}

type adminKey struct{}

// ByAdmin помечает регистрацию, присланную администратором: только ему можно назначать роли.
func ByAdmin(r *http.Request) *http.Request {
	return r.WithContext(context.WithValue(r.Context(), adminKey{}, true))
}

// fail сводит нераспознанные ошибки к общему 500, чтобы тексты ошибок БД и gRPC не дошли до клиента.
func (a *API) fail(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, domain.ErrInvalidCredentials):
		httpx.Error(w, http.StatusUnauthorized, "invalid credentials")
	case errors.Is(err, domain.ErrSessionNotFound):
		httpx.Error(w, http.StatusUnauthorized, "invalid or expired refresh token")
	case errors.Is(err, domain.ErrUserExists):
		httpx.Error(w, http.StatusConflict, "username already taken")
	case errors.Is(err, domain.ErrWeakPassword):
		// Причина слабого пароля безопасна для показа.
		httpx.Error(w, http.StatusBadRequest, err.Error())
	case errors.Is(err, domain.ErrTooManyAttempts):
		var tma *domain.TooManyAttemptsError
		if errors.As(err, &tma) && tma.RetryAfter > 0 {
			secs := int(tma.RetryAfter.Seconds())
			if secs < 1 {
				secs = 1
			}
			w.Header().Set("Retry-After", strconv.Itoa(secs))
		}
		httpx.Error(w, http.StatusTooManyRequests, "too many failed login attempts; try again later")
	default:
		httpx.Error(w, http.StatusInternalServerError, "internal error")
	}
}

// clientIP доверяет заголовкам прокси, потому что сервис стоит за шлюзом; значение лишь уточняет
// ключ ограничителя по логину.
func clientIP(r *http.Request) string {
	if v := strings.TrimSpace(r.Header.Get("X-Real-IP")); v != "" {
		return v
	}
	if v := r.Header.Get("X-Forwarded-For"); v != "" {
		if first, _, found := strings.Cut(v, ","); found {
			v = first
		}
		if v = strings.TrimSpace(v); v != "" {
			return v
		}
	}
	if host, _, err := net.SplitHostPort(r.RemoteAddr); err == nil {
		return host
	}
	return r.RemoteAddr
}

func tokenView(t *application.Tokens) tokenResponse {
	return tokenResponse{
		AccessToken:  t.AccessToken,
		RefreshToken: t.RefreshToken,
		TokenType:    "Bearer",
		ExpiresAt:    t.ExpiresAt,
		User: userView{
			ID:       t.Identity.UserID,
			Username: t.Identity.Username,
			Roles:    t.Identity.Roles,
		},
	}
}
