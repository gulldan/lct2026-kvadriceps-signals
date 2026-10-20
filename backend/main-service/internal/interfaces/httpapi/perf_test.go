package httpapi_test

import (
	"encoding/json/v2"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestPerfDistinguishesEmptyMeasurementsFromUnavailableTelemetry(t *testing.T) {
	for _, state := range []string{"empty", "measured", "unavailable"} {
		t.Run(state, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if !strings.Contains(r.URL.Query().Get("query"), "[15m]") {
					t.Error("latency query includes stale lifetime measurements")
				}
				if state == "unavailable" {
					w.WriteHeader(http.StatusServiceUnavailable)
					return
				}
				result := `[]`
				if state == "measured" {
					result = `[{"metric":{"stage":"generate","queue":"parse.pdf"},"value":[1,"2.5"]}]`
				}
				_, _ = w.Write([]byte(`{"status":"success","data":{"result":` + result + `}}`))
			}))
			defer server.Close()
			t.Setenv("PROM_URL", server.URL)
			h := newHarness(t)
			response := h.do(t, http.MethodGet, "/admin/perf", h.token(t, "root", "admin"), "")
			type latency struct {
				P50 *float64 `json:"p50"`
			}
			var got struct {
				Pipeline   map[string]*latency `json:"pipeline"`
				Ingestion  map[string]*latency `json:"ingestion"`
				Telemetry  string              `json:"telemetry"`
				ObservedAt time.Time           `json:"observed_at"`
			}
			if response.code != http.StatusOK || json.Unmarshal([]byte(response.body), &got) != nil {
				t.Fatalf("performance response: %d %s", response.code, response.body)
			}
			want := "available"
			if state == "unavailable" {
				want = state
			}
			if got.Telemetry != want || got.ObservedAt.IsZero() {
				t.Fatalf("lost telemetry state: %+v", got)
			}
			if state == "measured" && (got.Pipeline["generate"] == nil || got.Ingestion["parse.pdf"] == nil ||
				got.Pipeline["generate"].P50 == nil || *got.Pipeline["generate"].P50 != 2.5) {
				t.Fatalf("lost measured latency: %+v", got)
			}
		})
	}
}
