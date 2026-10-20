package httpapi_test

import (
	"crypto/sha256"
	"encoding/json/v2"
	"fmt"
	"net/http"
	"testing"
)

func TestMaintenanceJournalSupersedesStaleWorkerSnapshot(t *testing.T) {
	for _, state := range []string{"succeeded", "failed", "running", "orphaned", "older", "running_without_history"} {
		t.Run(state, func(t *testing.T) {
			h := newHarness(t)
			h.metrics.data["rag:worker:itc:status"] = `{"state":"idle","updated_at":"2026-09-20T00:00:00Z",
				"last_success_at":"2026-09-20T00:00:00Z","last_run_seconds":24.507}`
			h.metrics.data["rag:itc:last_status"] = state + " request=manual-fixture"
			previous := fmt.Sprintf("rag:work:{itc}:%x", sha256.Sum256([]byte("previous-fixture")))
			h.metrics.data["rag:work:{itc}:last_success"] = previous
			h.metrics.hashes[previous] = map[string]string{"request": "previous-fixture", "state": "succeeded",
				"started": "1792252700", "finished": "1792252720"}
			record := map[string]string{"request": "manual-fixture", "state": state,
				"started": "1792252800", "finished": "1792252825"}
			want := "idle"
			switch state {
			case "failed":
				want = "error"
				record["error"] = "child_exit_1"
			case "running", "running_without_history":
				want = "running"
				record["state"] = "running"
				h.metrics.data["rag:work:{itc}:lease"] = "active-owner"
				record["finished"] = ""
				if state == "running_without_history" {
					delete(h.metrics.data, "rag:work:{itc}:last_success")
				}
			case "orphaned":
				want = "unknown"
				record["state"], record["finished"] = "running", ""
			case "older":
				record["state"], record["started"], record["finished"] = "failed", "1700000000", "1700000025"
			}
			key := fmt.Sprintf("rag:work:{itc}:%x", sha256.Sum256([]byte("manual-fixture")))
			h.metrics.hashes[key] = record
			response := h.do(t, http.MethodGet, "/system/activity", h.token(t, "alice"), "")
			var got struct {
				Workers []struct {
					Name           string  `json:"name"`
					State          string  `json:"state"`
					UpdatedAt      string  `json:"updated_at"`
					LastSuccessAt  string  `json:"last_success_at"`
					LastRunSeconds float64 `json:"last_run_seconds"`
				} `json:"workers"`
			}
			if response.code != http.StatusOK || json.Unmarshal([]byte(response.body), &got) != nil {
				t.Fatalf("activity response: %d %s", response.code, response.body)
			}
			for _, worker := range got.Workers {
				if worker.Name != "itc" {
					continue
				}
				if worker.State != want || (worker.UpdatedAt == "2026-09-20T00:00:00Z") != (state == "older") {
					t.Fatalf("stale or lost maintenance state: %+v", worker)
				}
				if state == "succeeded" && (worker.LastSuccessAt == "" || worker.LastRunSeconds != 25) {
					t.Fatalf("missing actual completion: %+v", worker)
				}
				wantSuccess := "2026-10-17T15:58:40Z"
				switch state {
				case "succeeded":
					wantSuccess = "2026-10-17T16:00:25Z"
				case "older":
					wantSuccess = "2026-09-20T00:00:00Z"
				case "running_without_history":
					wantSuccess = ""
				}
				if worker.LastSuccessAt != wantSuccess {
					t.Fatalf("last successful attempt lost or stale: %+v, want %s", worker, wantSuccess)
				}
				if (state == "running" || state == "orphaned") && worker.LastRunSeconds != 20 {
					t.Fatalf("stale duration during new attempt: %+v", worker)
				}
				if state == "running_without_history" && worker.LastRunSeconds != 0 {
					t.Fatalf("legacy duration without journal evidence: %+v", worker)
				}
			}
		})
	}
}

func TestFactoryRestartKeepsLastSuccessfulRun(t *testing.T) {
	h := newHarness(t)
	for _, name := range []string{"clusters", "discovery", "raptor"} {
		prefix := "rag:work:{" + name + "}"
		key := fmt.Sprintf("%s:%x", prefix, sha256.Sum256([]byte("previous-fixture")))
		h.metrics.data["rag:worker:"+name+":status"] = `{"state":"running","updated_at":"2026-10-17T16:00:00Z"}`
		h.metrics.data[prefix+":last_success"] = key
		h.metrics.hashes[key] = map[string]string{"request": "previous-fixture", "state": "succeeded",
			"started": "1792252700", "finished": "1792252720"}
	}
	response := h.do(t, http.MethodGet, "/system/activity", h.token(t, "alice"), "")
	var got struct {
		Workers []struct {
			Name          string `json:"name"`
			State         string `json:"state"`
			LastSuccessAt string `json:"last_success_at"`
		} `json:"workers"`
	}
	if response.code != http.StatusOK || json.Unmarshal([]byte(response.body), &got) != nil {
		t.Fatalf("activity response: %d %s", response.code, response.body)
	}
	for _, worker := range got.Workers {
		if worker.Name == "itc" || worker.Name == "eval" {
			continue
		}
		if worker.State != "running" || worker.LastSuccessAt != "2026-10-17T15:58:40Z" {
			t.Fatalf("worker restart lost successful history: %+v", worker)
		}
	}
}
