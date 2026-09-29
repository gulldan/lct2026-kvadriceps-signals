package httpapi

import (
	"net/http"
	"testing"
)

func TestAutomationScopeKeepsAdministrationOutsideWorkerPermissions(t *testing.T) {
	for _, test := range []struct {
		method, path string
		allowed      bool
	}{
		{http.MethodPost, "/admin/metrics", true}, {http.MethodPost, "/clusters/replace", true},
		{http.MethodPost, "/chats/chat/messages", true}, {http.MethodGet, "/documents/d/chunks", true},
		{http.MethodGet, "/admin/settings", false}, {http.MethodPost, "/admin/settings", false},
		{http.MethodGet, "/admin/users", false}, {http.MethodPost, "/admin/metrics/run", false},
		{http.MethodDelete, "/hypotheses/h", false}, {http.MethodPost, "/uploads", false},
	} {
		if got := automationAllowed(test.method, test.path); got != test.allowed {
			t.Errorf("%s %s=%v", test.method, test.path, got)
		}
	}
}

func TestReadOnlyBlocksDeletionEditsAndStandSettings(t *testing.T) {
	for _, test := range []struct {
		method, path string
		blocked      bool
	}{
		{http.MethodGet, "/admin/settings", false}, {http.MethodGet, "/admin/users", false},
		{http.MethodPost, "/chats", false}, {http.MethodPost, "/kpis", false},
		{http.MethodPost, "/ws-ticket", false}, {http.MethodPost, "/trends/live/signal-brief", false},
		{http.MethodDelete, "/kpis/k", true}, {http.MethodPut, "/kpis/k", true},
		{http.MethodPut, "/admin/settings", true}, {http.MethodPost, "/admin/metrics/run", true},
		{http.MethodPost, "/clusters/replace", true}, {http.MethodPatch, "/hypotheses/h", true},
	} {
		if got := readOnlyBlocked(test.method, test.path); got != test.blocked {
			t.Errorf("%s %s=%v", test.method, test.path, got)
		}
	}
}
