package httpapi_test

import (
	"net/http"
	"strings"
	"testing"

	commonv1 "github.com/example/main-service/internal/platform/genproto/common/v1"
)

func TestKPIs_Lifecycle(t *testing.T) {
	h := newHarness(t)
	tok := h.token(t, "alice")
	r := h.do(t, http.MethodPost, "/kpis", tok, `{"title":"Hardness","metric":"HV"}`)
	if r.code != http.StatusCreated {
		t.Fatalf("create kpi = %d: %s", r.code, r.body)
	}
	_ = r.body
	id := "kpi-Hardness"

	if r := h.do(t, http.MethodGet, "/kpis", tok, ""); r.code != http.StatusOK {
		t.Fatalf("list kpis = %d", r.code)
	} else {
		_ = r.body
	}
	if r := h.do(t, http.MethodGet, "/kpis/"+id, tok, ""); r.code != http.StatusOK {
		t.Fatalf("get kpi = %d", r.code)
	} else {
		_ = r.body
	}
	if r := h.do(t, http.MethodPut, "/kpis/"+id, tok, `{"title":"Hardness v2","metric":"HV"}`); r.code != http.StatusOK {
		t.Fatalf("update kpi = %d: %s", r.code, r.body)
	} else {
		_ = r.body
	}
	if r := h.do(t, http.MethodGet, "/kpis/"+id, h.token(t, "bob"), ""); r.code != http.StatusNotFound {
		t.Fatalf("foreign kpi = %d, want 404", r.code)
	}
	h.users.users = []*commonv1.User{{Id: "alice"}, {Id: "bob"}}
	admin := h.token(t, "root", "admin")
	if r := h.do(t, http.MethodGet, "/kpis", admin, ""); !strings.Contains(r.body, id) {
		t.Fatalf("admin list misses foreign kpi: %d %s", r.code, r.body)
	}
	if r := h.do(t, http.MethodGet, "/kpis/"+id, admin, ""); r.code != http.StatusOK {
		t.Fatalf("admin read foreign kpi = %d", r.code)
	}
	if r := h.do(t, http.MethodPut, "/kpis/"+id, admin, `{"title":"x","metric":"HV"}`); r.code == http.StatusOK {
		t.Fatal("admin must not edit a foreign kpi")
	}
}

func TestKPIs_CreateValidation(t *testing.T) {
	h := newHarness(t)
	r := h.do(t, http.MethodPost, "/kpis", h.token(t, "alice"), `{"metric":"HV"}`)
	if r.code != http.StatusBadRequest {
		t.Fatalf("missing title = %d, want 400", r.code)
	}
	_ = r.body
}

func TestClusters_Lifecycle(t *testing.T) {
	h := newHarness(t)
	tok := h.token(t, "alice")
	r := h.do(t, http.MethodPost, "/clusters", tok, `{"label":"Coatings"}`)
	if r.code != http.StatusCreated {
		t.Fatalf("create cluster = %d: %s", r.code, r.body)
	}
	_ = r.body
	id := "cl-Coatings"

	if r := h.do(t, http.MethodGet, "/clusters", tok, ""); r.code != http.StatusOK {
		t.Fatalf("list clusters = %d", r.code)
	} else {
		_ = r.body
	}
	if r := h.do(t, http.MethodGet, "/clusters/"+id, tok, ""); r.code != http.StatusOK {
		t.Fatalf("get cluster = %d", r.code)
	} else {
		_ = r.body
	}
	if r := h.do(t, http.MethodPut, "/clusters/"+id, tok, `{"label":"Coatings v2"}`); r.code != http.StatusOK {
		t.Fatalf("update cluster = %d: %s", r.code, r.body)
	} else {
		_ = r.body
	}
	if r := h.do(t, http.MethodDelete, "/clusters", tok, ""); r.code != http.StatusNoContent {
		t.Fatalf("delete clusters = %d, want 204", r.code)
	}
}

func TestClusters_CreateValidation(t *testing.T) {
	h := newHarness(t)
	r := h.do(t, http.MethodPost, "/clusters", h.token(t, "alice"), `{"summary":"x"}`)
	if r.code != http.StatusBadRequest {
		t.Fatalf("missing label = %d, want 400", r.code)
	}
	_ = r.body
}

func TestClusters_ReplaceAndPublishVersionFilter(t *testing.T) {
	h := newHarness(t)
	tok := h.token(t, "alice")
	h.cat.clusters["old"] = &commonv1.Cluster{Id: "old", OwnerId: "alice", Label: "Old", Params: `{"publish_version":"stale"}`}

	body := `{"request_id":"91f5a038-dbd2-4d23-997a-beb654f5a100","clusters":[{"label":"Fresh A"},{"label":"Fresh B"}]}`
	r := h.do(t, http.MethodPost, "/clusters/replace", tok, body)
	if r.code != http.StatusCreated {
		t.Fatalf("replace = %d: %s", r.code, r.body)
	}
	_ = r.body

	list := h.do(t, http.MethodGet, "/clusters", tok, "").body
	if strings.Contains(list, "\"Old\"") {
		t.Fatalf("stale cluster must be filtered out, got %s", list)
	}
	if !strings.Contains(list, "Fresh A") {
		t.Fatalf("fresh clusters must be listed, got %s", list)
	}
}

// Публикация требует устойчивый ключ для повторов после потери ответа.
func TestClusters_ReplaceEmpty(t *testing.T) {
	h := newHarness(t)
	r := h.do(t, http.MethodPost, "/clusters/replace", h.token(t, "alice"), `{"clusters":[]}`)
	if r.code != http.StatusBadRequest {
		t.Fatalf("empty replace = %d, want 400", r.code)
	}
	_ = r.body
}

func TestHypotheses_GraphHypotheses(t *testing.T) {
	h := newHarness(t)
	h.cat.kpis["kpi1"] = &commonv1.KPI{Id: "kpi1", OwnerId: "alice", Title: "T"}
	r := h.do(t, http.MethodPost, "/kpis/kpi1/graph-hypotheses", h.token(t, "alice"), "")
	if r.code != http.StatusOK || !strings.Contains(r.body, "candidates") {
		t.Fatal("graph-hypotheses must return a candidates list")
	}
}

func TestClusters_AdminWithoutBoardReadsStandBoard(t *testing.T) {
	h := newHarness(t)
	h.cat.clusters["c1"] = &commonv1.Cluster{Id: "c1", OwnerId: "worker", Label: "Board"}
	h.users.users = []*commonv1.User{{Id: "root"}, {Id: "worker"}}
	admin := h.token(t, "root", "admin")
	if r := h.do(t, http.MethodGet, "/clusters", admin, ""); !strings.Contains(r.body, "c1") {
		t.Fatalf("admin misses the stand board: %d %s", r.code, r.body)
	}
	if r := h.do(t, http.MethodGet, "/clusters/c1", admin, ""); r.code != http.StatusOK {
		t.Fatalf("admin read stand theme = %d", r.code)
	}
	if r := h.do(t, http.MethodGet, "/clusters", h.token(t, "bob"), ""); strings.Contains(r.body, "c1") {
		t.Fatal("regular user must not see a foreign board")
	}
}
