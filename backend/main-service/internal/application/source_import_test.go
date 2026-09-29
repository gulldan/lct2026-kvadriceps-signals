package application

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json/v2"
	"strings"
	"testing"
	"time"

	"github.com/example/main-service/internal/domain"
	"github.com/example/main-service/internal/platform/sourcedoc"
)

func TestManualImportRetryHasOneDocumentObjectAndDispatch(t *testing.T) {
	text := "Independent technical source"
	h := sha256.Sum256([]byte(text))
	metadata, err := json.Marshal(sourcedoc.Source{
		Version: 1, Kind: "analysis", Provider: "manual", NativeID: "report", CanonicalID: "report",
		URL: "https://example.org/report", Revision: "v1", ContentHash: hex.EncodeToString(h[:]),
		HashAlgorithm: "sha256", AccessScope: "owner", ObservedAt: time.Now().UTC(),
	})
	if err != nil {
		t.Fatal(err)
	}
	store, docs, pub := newObjectStore(), newDocs(), &fakePublisher{}
	svc := newIngestion(store, docs, pub, &fakeChunks{})
	cmd := domain.UploadCommand{OwnerID: "alice", Filename: "report.txt", Size: int64(len(text)), Text: text,
		Hash: "verified-blake3", SourceMetadata: string(metadata)}
	for attempt := range 2 {
		cmd.Body = strings.NewReader(text)
		doc, duplicate, uploadErr := svc.Upload(t.Context(), cmd, "text/plain")
		if uploadErr != nil || duplicate != (attempt == 1) || doc.GetSourceMetadata() == "" {
			t.Fatalf("retry lost immutable provenance: %v %v", duplicate, uploadErr)
		}
	}
	if len(
		docs.docs,
	) != 1 ||
		len(
			store.objects,
		) != 1 ||
		len(
			docs.requests,
		) != 1 ||
		!docs.requests[0].GetEnqueue() ||
		pub.count() != 0 {
		t.Fatal("import retry created another domain effect")
	}
}
