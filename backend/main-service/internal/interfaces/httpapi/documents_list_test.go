package httpapi_test

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/example/main-service/internal/platform/contracts"
	commonv1 "github.com/example/main-service/internal/platform/genproto/common/v1"
)

func TestLegacyDocumentListReadsEveryPage(t *testing.T) {
	h := newHarness(t)
	for i := range 1001 {
		id := fmt.Sprintf("doc-%04d", i)
		h.docs.docs[id] = &commonv1.Document{Id: id, OwnerId: "alice", Filename: id + ".pdf"}
	}
	h.docs.docs["foreign"] = &commonv1.Document{Id: "foreign", OwnerId: "bob"}
	token := h.token(t, "alice")
	h.docs.pageHook = func(q contracts.DocumentQuery) error {
		if q.ReaderID != "alice" || q.AllOwners || q.Limit > 500 {
			return errors.New("unbounded or unscoped page")
		}
		return nil
	}
	full := h.do(t, http.MethodGet, "/documents", token, "")
	var docs []struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal([]byte(full.body), &docs); err != nil {
		t.Fatal(err)
	}
	if full.code != http.StatusOK || len(docs) != 1001 || full.header.Get("X-Total-Count") != "1001" ||
		full.header.Get("X-Next-Cursor") != "" {
		t.Fatalf("incomplete legacy list: status=%d count=%d headers=%v", full.code, len(docs), full.header)
	}
	seen := map[string]bool{}
	for _, doc := range docs {
		if doc.ID == "foreign" || seen[doc.ID] {
			t.Fatalf("foreign or repeated document %q", doc.ID)
		}
		seen[doc.ID] = true
	}
	first := h.do(t, http.MethodGet, "/documents?limit=100", token, "")
	if err := json.Unmarshal([]byte(first.body), &docs); err != nil || len(docs) != 100 ||
		first.header.Get("X-Next-Cursor") == "" || first.header.Get("X-Total-Count") != "1001" {
		t.Fatalf("explicit page changed: count=%d headers=%v error=%v", len(docs), first.header, err)
	}
	last := docs[len(docs)-1].ID
	next := h.do(t, http.MethodGet, "/documents?limit=100&before="+url.QueryEscape(first.header.Get("X-Next-Cursor")), token, "")
	if err := json.Unmarshal([]byte(next.body), &docs); err != nil || len(docs) != 100 || docs[0].ID >= last {
		t.Fatalf("continuation repeated a page: count=%d error=%v", len(docs), err)
	}
	for _, path := range []string{"/documents?q=doc", "/documents?limit=5000"} {
		page := h.do(t, http.MethodGet, path, token, "")
		if err := json.Unmarshal([]byte(page.body), &docs); err != nil || len(docs) > 500 ||
			page.header.Get("X-Next-Cursor") == "" {
			t.Fatalf("query lost bounded paging: path=%s count=%d error=%v", path, len(docs), err)
		}
	}
	h.docs.pageHook = nil
	empty := h.do(t, http.MethodGet, "/documents", h.token(t, "nobody"), "")
	if strings.TrimSpace(empty.body) != "[]" || empty.header.Get("X-Total-Count") != "0" {
		t.Fatalf("empty legacy list changed: %s %v", empty.body, empty.header)
	}

	h.docs.pageHook = func(q contracts.DocumentQuery) error {
		if q.BeforeID != "" {
			return errors.New("private continuation failure")
		}
		return nil
	}
	failed := h.do(t, http.MethodGet, "/documents", token, "")
	if json.Valid([]byte(failed.body)) || strings.Contains(failed.body, "private continuation failure") {
		t.Fatal("failed continuation must not become a valid incomplete list or expose storage details")
	}
}
