package application

import (
	"fmt"
	"math"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/example/main-service/internal/platform/openalex"
)

func fakeOpenAlex(t *testing.T) *openalex.Client {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		filter := r.URL.Query().Get("filter")
		if r.URL.Query().Get("group_by") != "publication_year" || !strings.HasPrefix(filter, `title_and_abstract.search:"`) {
			t.Errorf("не тот запрос истории: %s", r.URL.RawQuery)
		}
		groups := []string{}
		switch {
		case strings.Contains(filter, "broken"):
			w.WriteHeader(http.StatusInternalServerError)
			return
		case strings.Contains(filter, "keyword spotting"):
			for year := 2010; year <= 2026; year++ {
				groups = append(groups, fmt.Sprintf(`{"key":"%d","count":100}`, year))
			}
		case strings.Contains(filter, "hybrid kv"), strings.Contains(filter, "hybrid-kv"):
			groups = append(groups, `{"key":"2024","count":3}`, `{"key":"2025","count":12}`, `{"key":"2026","count":40}`)
		}
		_, _ = fmt.Fprintf(w, `{"group_by":[%s]}`, strings.Join(groups, ","))
	}))
	t.Cleanup(server.Close)
	// Одна попытка: общий клиент повторяет 5xx с паузами в секунды, а тесту нужен сам отказ.
	client, err := openalex.New(openalex.Config{BaseURL: server.URL, Mailto: "test@example.org", Attempts: 1})
	if err != nil {
		t.Fatal(err)
	}
	return client
}

func TestTermHistoryInputs(t *testing.T) {
	histories, err := TermHistories(t.Context(), fakeOpenAlex(t), []string{"keyword spotting", "hybrid-kv", "broken"})
	if err == nil {
		t.Fatal("сбой OpenAlex не попал в ошибку")
	}
	if _, ok := histories["broken"]; ok {
		t.Fatal("фраза со сбоем записана как «работ не было»")
	}
	old := liveHistoryInputs(histories["keyword spotting"], "2026-09")
	fresh := liveHistoryInputs(histories["hybrid-kv"], "2026-09")
	if math.Abs(old[0]-math.Log1p(1600)) > 1e-9 || math.Abs(old[1]-1000.0/1600) > 1e-9 || math.Abs(old[2]) > 1e-9 {
		t.Fatalf("давний термин: %v", old)
	}
	// Неполный 2026 год не считается: 15 работ, ни одной до 2020, поток вырос с нуля.
	if math.Abs(fresh[0]-math.Log1p(15)) > 1e-9 || fresh[1] != 0 || fresh[2] <= 0 {
		t.Fatalf("новый термин: %v", fresh)
	}
	if missing := liveHistoryInputs(nil, "2026-09"); !math.IsNaN(missing[0]) {
		t.Fatalf("без ответа OpenAlex вход должен быть неизмерен: %v", missing)
	}
}
