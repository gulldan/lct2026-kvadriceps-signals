package application

import (
	"encoding/hex"
	"encoding/json/v2"
	"testing"
	"time"

	commonv1 "github.com/example/main-service/internal/platform/genproto/common/v1"
	"github.com/example/main-service/internal/platform/indexdoc"
	"github.com/example/main-service/internal/platform/sourcedoc"
	"lukechampine.com/blake3"
)

func TestSourceReindexPreservesTextAndRejectsChangedOriginal(t *testing.T) {
	for _, tc := range []struct{ name, mime, raw, text string }{
		{"plain", "text/plain", "Точный научный текст.", "Точный научный текст."},
		{"html", "text/html", "<p>Grounded evidence.</p><script>not evidence</script>", "Grounded evidence."},
	} {
		t.Run(tc.name, func(t *testing.T) {
			source := sourcedoc.Source{Version: 1, Kind: "analysis", Provider: "manual", NativeID: "report",
				CanonicalID: "report", URL: "https://example.org/report", Revision: "v1", ObservedAt: time.Now().UTC(),
				AccessScope: "owner", HashAlgorithm: "sha256", ContentHash: indexdoc.Hash([]byte(tc.raw))}
			rawSource, err := json.Marshal(source)
			if err != nil {
				t.Fatal(err)
			}
			hash := blake3.Sum256([]byte(tc.raw))
			docs, objects := newDocs(), newObjectStore()
			docs.docs["report"] = &commonv1.Document{Id: "report", OwnerId: "alice", ObjectKey: "raw/report",
				SourceMetadata: string(rawSource), ContentHash: hex.EncodeToString(hash[:]), MimeType: tc.mime}
			objects.objects["raw/report"] = []byte(tc.raw)
			svc := &IngestionService{docs: docs, store: objects}
			if outcome, queueErr := svc.ReindexSource(t.Context(), "alice", "report", false); queueErr != nil || outcome != "queued" {
				t.Fatal(outcome, queueErr)
			}
			if len(docs.requeued) != 1 || docs.requeued[0] != tc.text {
				t.Fatal("parser provenance lost")
			}
			objects.objects["raw/report"] = []byte("changed original")
			if _, err = svc.ReindexSource(t.Context(), "alice", "report", true); err == nil || len(docs.requeued) != 1 {
				t.Fatal("tampered raw source requeued")
			}
		})
	}
}
