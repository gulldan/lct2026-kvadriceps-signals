package ws

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/gorilla/websocket"

	wsinfra "github.com/example/main-service/internal/infrastructure/ws"
	"github.com/example/main-service/internal/platform/jwt"
	"github.com/example/main-service/internal/platform/logger"
)

type Handler struct {
	hub      *wsinfra.Hub
	jwt      *jwt.Manager
	tickets  TicketStore
	upgrader websocket.Upgrader
}

// New проверяет источник соединения независимо от настроек внешнего прокси.
func New(hub *wsinfra.Hub, mgr *jwt.Manager, tickets TicketStore, allowedOrigins ...string) *Handler {
	return &Handler{
		hub:     hub,
		jwt:     mgr,
		tickets: tickets,
		upgrader: websocket.Upgrader{
			ReadBufferSize:  1024,
			WriteBufferSize: 1024,
			CheckOrigin: func(r *http.Request) bool {
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
				for _, allowed := range allowedOrigins {
					if strings.TrimSpace(allowed) == origin {
						return true
					}
				}
				return false
			},
		},
	}
}

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	log := logger.From(r.Context())
	if !h.upgrader.CheckOrigin(r) {
		http.Error(w, `{"error":"origin denied"}`, http.StatusForbidden)
		return
	}

	claims, err := h.authenticate(r)
	if err != nil {
		if errors.Is(err, jwt.ErrSessionUnavailable) {
			http.Error(w, `{"error":"session verification unavailable"}`, http.StatusServiceUnavailable)
			return
		}
		http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
		return
	}

	conn, err := h.upgrader.Upgrade(w, r, nil)
	if err != nil {
		// Upgrade сам уже ответил ошибкой.
		log.Warn().Err(err).Msg("websocket upgrade failed")
		return
	}

	client := h.hub.Register(claims.UserID, conn)
	log.Info().Str("owner_id", claims.UserID).Msg("websocket connected")

	ctx, cancel := context.WithDeadline(r.Context(), claims.ExpiresAt.Time)
	defer cancel()
	go h.watchSession(ctx, cancel, claims)
	client.ReadLoop(ctx)
	log.Info().Str("owner_id", claims.UserID).Msg("websocket disconnected")
}

// Браузер передаёт только одноразовый билет: долгоживущий токен не попадает в URL.
func (h *Handler) authenticate(r *http.Request) (*jwt.Claims, error) {
	raw := bearerToken(r.Header.Get("Authorization"))
	if raw == "" {
		ticket := r.URL.Query().Get("ticket")
		if len(ticket) != 64 || h.tickets == nil {
			return nil, errMissingToken
		}
		var err error
		raw, _, err = h.tickets.GetDel(r.Context(), ticketKey(ticket))
		if err != nil {
			return nil, errMissingToken
		}
	}
	if raw == "" {
		return nil, errMissingToken
	}
	return h.jwt.Validate(r.Context(), raw)
}

func bearerToken(header string) string {
	parts := strings.SplitN(header, " ", 2)
	if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") {
		return ""
	}
	return strings.TrimSpace(parts[1])
}

var errMissingToken = &authError{"missing token"}

type authError struct{ msg string }

func (e *authError) Error() string { return e.msg }

var _ http.Handler = (*Handler)(nil)

func (h *Handler) watchSession(ctx context.Context, cancel context.CancelFunc, claims *jwt.Claims) {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	defer cancel()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			fresh := *claims
			if err := h.jwt.Revalidate(ctx, &fresh); err != nil {
				return
			}
		}
	}
}
