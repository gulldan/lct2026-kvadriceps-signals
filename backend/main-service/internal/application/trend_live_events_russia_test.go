package application

import (
	"bytes"
	"encoding/json/v2"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"unicode/utf8"
)

func TestLiveRussianFeedEventsFitsCollectorTermBudget(t *testing.T) {
	query := LiveEventQuery{
		Term: "Operando Synchrotron X-ray Fluorescence and Diffraction with High Resolution Cryogenic " +
			"Electron and Ion Multi Model Microscopy",
		Russian: "операндная синхротронная рентгеновская флуоресценция и дифракция с криогенной " +
			"электронной и ионной мультимодельной микроскопией",
	}
	wantTerm := "операндная синхротронная рентгеновская флуоресценция и дифракция с криогенной электронной и ионной"
	var accepted, rejected atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request struct {
			Terms   []string     `json:"terms"`
			Periods []LivePeriod `json:"periods"`
			Titles  bool         `json:"titles"`
		}
		body, err := io.ReadAll(r.Body)
		if err != nil || json.Unmarshal(body, &request) != nil {
			t.Error("invalid collector request")
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		// Та же граница, что validQuery в trend-sources: один длинный термин отклоняет весь пакет.
		for _, term := range request.Terms {
			if len(term) > 200 {
				rejected.Add(1)
				w.WriteHeader(http.StatusBadRequest)
				return
			}
		}
		accepted.Add(1)
		if len(request.Terms) != 2 || request.Terms[0] != wantTerm || !utf8.ValidString(request.Terms[0]) ||
			request.Terms[1] != query.Term || len(request.Periods) != 1 || !request.Titles {
			t.Errorf("lookup changed identity or calendar: %+v", request)
		}
		channel := LiveChannel{Counts: []*int{nil}, Records: []LiveSourceRecord{
			{ID: "whole", Title: query.Russian, URL: "https://example.org/whole", Language: "ru"},
			{ID: "prefix", Title: wantTerm, URL: "https://example.org/prefix", Language: "ru"},
		}}
		response, err := json.Marshal(channel)
		if err != nil {
			t.Error(err)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(response)
	}))
	defer server.Close()
	t.Setenv("WEB_COLLECTOR_URL", server.URL)
	oldPayload, err := json.Marshal(struct {
		Terms   []string     `json:"terms"`
		Periods []LivePeriod `json:"periods"`
		Titles  bool         `json:"titles"`
	}{[]string{query.Russian, query.Term},
		[]LivePeriod{{ID: "year", Start: "2025-09-26", EndExclusive: "2026-09-27"}}, true})
	if err != nil {
		t.Fatal(err)
	}
	oldRequest, err := http.NewRequestWithContext(t.Context(), http.MethodPost, server.URL+"/v1/query",
		bytes.NewReader(oldPayload))
	if err != nil {
		t.Fatal(err)
	}
	oldResponse, err := server.Client().Do(oldRequest)
	if err != nil {
		t.Fatal(err)
	}
	_ = oldResponse.Body.Close()
	if oldResponse.StatusCode != http.StatusBadRequest {
		t.Fatal("original payload did not reproduce the collector rejection")
	}
	events, err := liveRussianFeedEvents(t.Context(), server.Client(), query)
	if err != nil || len(events) != 1 || events[0].Record.ID != "whole" || accepted.Load() != 1 || rejected.Load() != 1 {
		t.Fatalf("lookup must succeed and still reject partial identities: events=%+v err=%v accepted=%d rejected=%d",
			events, err, accepted.Load(), rejected.Load())
	}
	if strings.Join(strings.Fields(query.Russian)[:len(strings.Fields(wantTerm))], " ") != wantTerm {
		t.Fatal("lookup cut a word instead of shortening the whole-word prefix")
	}
}
