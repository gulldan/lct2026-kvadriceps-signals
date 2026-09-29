package ws

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"time"
)

// TicketStore обеспечивает однократное использование билета между репликами.
type TicketStore interface {
	SetNX(context.Context, string, string, time.Duration) (bool, error)
	GetDel(context.Context, string) (string, bool, error)
}

func ticketKey(ticket string) string {
	hash := sha256.Sum256([]byte(ticket))
	return "ws:ticket:" + hex.EncodeToString(hash[:])
}

func (h *Handler) IssueTicket(w http.ResponseWriter, r *http.Request) {
	raw := bearerToken(r.Header.Get("Authorization"))
	if _, err := h.jwt.Validate(r.Context(), raw); err != nil {
		http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
		return
	}
	if h.tickets == nil {
		http.Error(w, `{"error":"ticket store unavailable"}`, http.StatusServiceUnavailable)
		return
	}
	var nonce [32]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		http.Error(w, `{"error":"ticket unavailable"}`, http.StatusServiceUnavailable)
		return
	}
	ticket := hex.EncodeToString(nonce[:])
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
	defer cancel()
	if ok, err := h.tickets.SetNX(ctx, ticketKey(ticket), raw, time.Minute); err != nil || !ok {
		http.Error(w, `{"error":"ticket unavailable"}`, http.StatusServiceUnavailable)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	_, _ = fmt.Fprintf(w, `{"ticket":%q}`, ticket)
}
