package httpapi

import (
	"bytes"
	"encoding/json/v2"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/example/main-service/internal/platform/jwt"
)

func TestAppSettingsExposeOnlyDeploymentStatus(t *testing.T) {
	values := map[string]string{"VLLM_MODEL": "saved-model"}
	for _, key := range []string{"VLLM_URL", "VLLM_API_KEY", "PUBSEARCH_MAILTO"} {
		t.Setenv(key, "private-environment-value")
		values[key] = "private-stored-value"
	}
	views := appSettingViews(values)
	for _, view := range views {
		if view.EnvOnly && (!view.EnvSet || view.Source != "env" || view.HasOverride ||
			view.Override != "" || view.EnvValue != "" || view.Def != "") {
			t.Errorf("deployment setting %s exposed more than its environment status", view.Key)
		}
		if view.Key == "VLLM_MODEL" && (view.EnvOnly || view.Override != "saved-model") {
			t.Fatal("model stopped being editable")
		}
	}
	raw, err := json.Marshal(views)
	if err != nil || strings.Contains(string(raw), "private-") {
		t.Fatalf("serialized settings exposed deployment values: %v", err)
	}
	t.Setenv("VLLM_API_KEY", "")
	for _, view := range appSettingViews(values) {
		if view.Key == "VLLM_API_KEY" && (view.EnvSet || view.Source != "default" || view.HasOverride) {
			t.Fatal("legacy secret was reported as configured")
		}
	}
}

func TestAppSettingsRejectDeploymentEdits(t *testing.T) {
	api := &API{appSettings: &AppSettingsStore{}}
	manager := jwt.NewManager("test-key-for-settings-boundary-only", "settings-test", time.Hour)
	token, _, err := manager.Issue("operator", "operator", []string{"admin"})
	if err != nil {
		t.Fatal(err)
	}
	handler := manager.Middleware(http.HandlerFunc(api.putAppSettings))
	for _, key := range []string{"VLLM_URL", "VLLM_API_KEY", "PUBSEARCH_MAILTO"} {
		for _, value := range []*string{new("private-value"), nil} {
			body, err := json.Marshal(map[string]any{"revision": 0, "values": map[string]*string{
				"VLLM_MODEL": new("allowed-model"), key: value,
			}})
			if err != nil {
				t.Fatal(err)
			}
			req := httptest.NewRequestWithContext(t.Context(), http.MethodPut, "/admin/settings", bytes.NewReader(body))
			req.Header.Set("Authorization", "Bearer "+token)
			req.Header.Set("Content-Type", "application/json")
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, req)
			if response.Code != http.StatusBadRequest || strings.Contains(response.Body.String(), "private-value") {
				t.Errorf("deployment edit %s was not rejected safely: HTTP %d", key, response.Code)
			}
		}
	}
}
