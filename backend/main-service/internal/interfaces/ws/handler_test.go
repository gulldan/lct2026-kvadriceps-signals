package ws_test

import (
	"bytes"
	"encoding/json"
	"github.com/alicebob/miniredis/v2"
	"github.com/example/main-service/internal/platform/valkey"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/rs/zerolog"

	wshub "github.com/example/main-service/internal/infrastructure/ws"
	wsapi "github.com/example/main-service/internal/interfaces/ws"
	"github.com/example/main-service/internal/platform/jwt"
)

const wsSecret = "ws-test-secret-32-bytes-padding!!"

func wsServer(t *testing.T) (*httptest.Server, *jwt.Manager) {
	t.Helper()
	mgr := jwt.NewManager(wsSecret, "rag-platform", time.Hour)
	hub := wshub.NewHub(zerolog.Nop())
	redis := miniredis.RunT(t)
	kv, err := valkey.New(t.Context(), redis.Addr(), "", 0)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = kv.Close() })
	handler := wsapi.New(hub, mgr, kv)
	mux := http.NewServeMux()
	mux.Handle("GET /", handler)
	mux.HandleFunc("POST /ws-ticket", handler.IssueTicket)
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv, mgr
}

func wsURL(srv *httptest.Server, query string) string {
	return "ws" + strings.TrimPrefix(srv.URL, "http") + query
}

func closeResp(resp *http.Response) {
	if resp != nil && resp.Body != nil {
		_ = resp.Body.Close()
	}
}

func TestWSHandler_NoTokenRejected(t *testing.T) {
	srv, _ := wsServer(t)
	_, resp, err := websocket.DefaultDialer.Dial(wsURL(srv, ""), nil)
	defer closeResp(resp)
	if err == nil {
		t.Fatal("dial without token should fail")
	}
	if resp == nil || resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("missing token = %v, want 401", resp)
	}
}

func TestWSHandler_InvalidTokenRejected(t *testing.T) {
	srv, _ := wsServer(t)
	hdr := http.Header{"Authorization": {"Bearer not-a-real-token"}}
	_, resp, err := websocket.DefaultDialer.Dial(wsURL(srv, ""), hdr)
	defer closeResp(resp)
	if err == nil {
		t.Fatal("dial with invalid token should fail")
	}
	if resp == nil || resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("invalid token = %v, want 401", resp)
	}
}

func TestWSHandler_HeaderTokenUpgrades(t *testing.T) {
	srv, mgr := wsServer(t)
	tok, _, err := mgr.Issue("alice", "alice", []string{"user"})
	if err != nil {
		t.Fatalf("issue: %v", err)
	}
	hdr := http.Header{"Authorization": {"Bearer " + tok}}
	conn, resp, derr := websocket.DefaultDialer.Dial(wsURL(srv, ""), hdr)
	defer closeResp(resp)
	if derr != nil {
		t.Fatalf("dial with header token failed: %v (resp %v)", derr, resp)
	}
	defer func() { _ = conn.Close() }()
	if resp.StatusCode != http.StatusSwitchingProtocols {
		t.Fatalf("status = %d, want 101", resp.StatusCode)
	}
}

// Долгоживущий токен не принимается через URL даже при корректной подписи.
func TestWSHandler_QueryTokenRejected(t *testing.T) {
	srv, mgr := wsServer(t)
	tok, _, err := mgr.Issue("bob", "bob", nil)
	if err != nil {
		t.Fatal(err)
	}
	_, resp, err := websocket.DefaultDialer.Dial(wsURL(srv, "?token="+tok), nil)
	defer closeResp(resp)
	if err == nil || resp == nil || resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("query token: %v, %v", resp, err)
	}
}

func TestWSHandler_TicketConsumedOnceAndOriginChecked(t *testing.T) {
	srv, mgr := wsServer(t)
	tok, _, err := mgr.Issue("bob", "bob", nil)
	if err != nil {
		t.Fatal(err)
	}
	req, err := http.NewRequestWithContext(t.Context(), http.MethodPost, srv.URL+"/ws-ticket", bytes.NewReader(nil))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer "+tok)
	resp, err := srv.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer closeResp(resp)
	var body struct {
		Ticket string `json:"ticket"`
	}
	if err = json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if len(body.Ticket) != 64 || resp.Header.Get("Cache-Control") != "no-store" {
		t.Fatalf("ticket: %+v", body)
	}
	hdr := http.Header{"Origin": {"https://attacker.example"}}
	_, bad, err := websocket.DefaultDialer.Dial(wsURL(srv, "?ticket="+body.Ticket), hdr)
	closeResp(bad)
	if err == nil || bad.StatusCode != http.StatusForbidden {
		t.Fatalf("origin: %v, %v", bad, err)
	}
	hdr.Set("Origin", srv.URL)
	conn, ok, err := websocket.DefaultDialer.Dial(wsURL(srv, "?ticket="+body.Ticket), hdr)
	closeResp(ok)
	if err != nil {
		t.Fatal(err)
	}
	_ = conn.Close()
	_, replay, err := websocket.DefaultDialer.Dial(wsURL(srv, "?ticket="+body.Ticket), hdr)
	closeResp(replay)
	if err == nil || replay.StatusCode != http.StatusUnauthorized {
		t.Fatalf("replay: %v, %v", replay, err)
	}
}
