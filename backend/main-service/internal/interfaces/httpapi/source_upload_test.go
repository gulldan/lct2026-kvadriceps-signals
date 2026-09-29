package httpapi_test

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json/v2"
	"net/http"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/example/main-service/internal/platform/sourcedoc"
)

func importSource(t *testing.T, text, kind, scope string) string {
	t.Helper()
	h := sha256.Sum256([]byte(text))
	source := sourcedoc.Source{
		Version: 1, Kind: kind, Provider: "manual", NativeID: "source", CanonicalID: "source",
		URL: "https://example.org/source", Revision: "v1", ContentHash: hex.EncodeToString(h[:]),
		HashAlgorithm: "sha256", AccessScope: scope, ObservedAt: time.Now().UTC(),
		Publication: &sourcedoc.Date{Value: "2024", Precision: "year", Basis: "source cover"},
	}
	// Номер публикации указывает только патент, остальным источникам нельзя.
	if kind == "patent" {
		source.NativeID, source.CanonicalID = "RU2789123", "patent:RU2789123"
		source.Patent = &sourcedoc.Patent{PublicationNumber: "RU2789123", Jurisdiction: "RU", KindCode: "C1"}
	}
	raw, err := json.Marshal(source)
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

func TestSourceUploadPreservesTypedProvenance(t *testing.T) {
	for _, kind := range []string{"analysis", "patent", "company_material"} {
		t.Run(kind, func(t *testing.T) {
			h := newHarness(t)
			text := "An open technical description with traceable provenance."
			r := h.doMultipart(t, h.token(t, "alice", "operator"), "file", "source.txt", text,
				map[string]string{"X-Document-Source": importSource(t, text, kind, "public")})
			if r.code != http.StatusCreated || !strings.Contains(r.body, `"source_kind":"`+kind+`"`) ||
				!strings.Contains(r.body, `"precision":"year"`) {
				t.Fatalf("source upload lost provenance: %d %s", r.code, r.body)
			}
			if len(
				h.store.objects,
			) != 1 ||
				len(
					h.docs.requests,
				) != 1 ||
				!h.docs.requests[0].GetEnqueue() ||
				h.docs.requests[0].GetInlineText() == "" {
				t.Fatal("source did not enter existing text pipeline once")
			}
		})
	}
}

func TestInvalidSourceFailsBeforeStorageOrDispatch(t *testing.T) {
	cases := []string{"tampered bytes", "unknown metadata", "public nonoperator", "empty HTML", "invalid UTF-8", "blank text"}
	for _, mode := range cases {
		t.Run(mode, func(t *testing.T) {
			h := newHarness(t)
			text, filename := "technical source text", "source.txt"
			metadata := importSource(t, text, "analysis", "owner")
			switch mode {
			case "tampered bytes":
				text += " changed"
			case "unknown metadata":
				metadata = `{"unexpected":true}`
			case "public nonoperator":
				metadata = importSource(t, text, "analysis", "public")
			case "empty HTML":
				text, filename = "<!doctype html><html><body><script>fake evidence</script></body></html>", "source.html"
				metadata = importSource(t, text, "company_material", "owner")
			case "invalid UTF-8":
				text = "<!doctype html><p>Invalid text \xff</p>"
				filename = "source.html"
				metadata = importSource(t, text, "company_material", "owner")
			case "blank text":
				text = " \n\t "
				metadata = importSource(t, text, "analysis", "owner")
			}
			r := h.doMultipart(t, h.token(t, "alice"), "file", filename, text,
				map[string]string{"X-Document-Source": metadata})
			if r.code != http.StatusBadRequest || len(h.store.objects) != 0 || len(h.docs.docs) != 0 || len(h.pub.routes) != 0 {
				t.Fatalf("invalid manifest mutated pipeline: %d %s", r.code, r.body)
			}
		})
	}
}

func TestHTMLSourceUsesInertExtractionAndOriginalDownload(t *testing.T) {
	h := newHarness(t)
	text := `<!doctype html><html><body><h1>Technical method</h1><p>Measured benefit.</p>` +
		`<script>fetch('/admin')</script><iframe src="http://127.0.0.1/private"></iframe></body></html>`
	r := h.doMultipart(t, h.token(t, "alice"), "file", "source.html", text,
		map[string]string{"X-Document-Source": importSource(t, text, "company_material", "owner")})
	if r.code != http.StatusCreated ||
		len(
			h.docs.requests,
		) != 1 ||
		!h.docs.requests[0].GetEnqueue() ||
		h.docs.requests[0].GetInlineText() == "" {
		t.Fatalf("HTML did not enter the text pipeline: %d %s", r.code, r.body)
	}
	endpoint := h.server.URL + "/documents/doc-source.html/content"
	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, endpoint, http.NoBody)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer "+h.token(t, "alice"))
	response, err := h.server.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode != http.StatusOK || !strings.HasPrefix(response.Header.Get("Content-Disposition"), "attachment;") ||
		!strings.Contains(response.Header.Get("Content-Security-Policy"), "sandbox") {
		t.Fatal("active HTML was exposed as an inline document")
	}
}

func TestSourceScopeDoesNotInheritAdministrativeOrSharedBypass(t *testing.T) {
	for _, tc := range []struct {
		scope     string
		shared    bool
		otherCode int
	}{
		{"owner", true, http.StatusNotFound}, {"owner", false, http.StatusNotFound},
		{"public", false, http.StatusOK}, {"shared", true, http.StatusOK}, {"shared", false, http.StatusNotFound},
	} {
		t.Run(tc.scope+"/"+strconv.FormatBool(tc.shared), func(t *testing.T) {
			h := newHarness(t, withSharedCorpus(tc.shared))
			text := "A dated source whose visibility must be enforced by the server."
			upload := h.doMultipart(t, h.token(t, "alice", "operator"), "file", "source.txt", text,
				map[string]string{"X-Document-Source": importSource(t, text, "analysis", tc.scope)})
			if upload.code != http.StatusCreated {
				t.Fatal(upload.body)
			}
			for _, role := range []string{"user", "admin"} {
				req, err := http.NewRequestWithContext(t.Context(), http.MethodGet,
					h.server.URL+"/documents/doc-source.txt/content", http.NoBody)
				if err != nil {
					t.Fatal(err)
				}
				req.Header.Set("Authorization", "Bearer "+h.token(t, "bob", role))
				response, err := h.server.Client().Do(req)
				if err != nil {
					t.Fatal(err)
				}
				_ = response.Body.Close()
				if response.StatusCode != tc.otherCode {
					t.Fatalf("%s can bypass %s: status %d", role, tc.scope, response.StatusCode)
				}
				if response.StatusCode == http.StatusOK && response.Header.Get("Cache-Control") != "no-store" {
					t.Fatal("source content could be reused across login/revocation")
				}
			}
		})
	}
}
