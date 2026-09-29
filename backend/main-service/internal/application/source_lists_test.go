package application

import (
	"encoding/json/v2"
	"slices"
	"strings"
	"testing"
	"time"

	commonv1 "github.com/example/main-service/internal/platform/genproto/common/v1"
	"github.com/example/main-service/internal/platform/indexdoc"
	"github.com/example/main-service/internal/platform/sourcedoc"
)

func TestPaperListsDeduplicateAfterAccessChecks(t *testing.T) {
	docs := newDocs()
	for i, status := range []string{"full_text", "abstract_only", "title_only", "full_text", "", "title_only"} {
		id := []string{"full", "abstract", "metadata", "revoked", "legacy-pdf", "legacy-title"}[i]
		source := sourcedoc.Source{Version: 1, Kind: "paper", Provider: "crossref", NativeID: "10.1234/paper",
			URL: "https://doi.org/10.1234/paper", CanonicalID: "doi:10.1234/paper", Revision: id,
			TextStatus: status, ObservedAt: time.Date(2026, 9, i+1, 0, 0, 0, 0, time.UTC),
			ContentHash: strings.Repeat("a", 64), HashAlgorithm: "sha256", AccessScope: "public"}
		if i >= 4 {
			source.NativeID, source.CanonicalID = "10.1234/legacy", "doi:10.1234/legacy"
		}
		raw, err := json.Marshal(source)
		if err != nil {
			t.Fatal(err)
		}
		docs.docs[id] = &commonv1.Document{Id: id, OwnerId: "owner", SourceMetadata: string(raw)}
	}
	docs.docs["upload"] = &commonv1.Document{Id: "upload", OwnerId: "owner"}
	svc := &IngestionService{docs: docs, sourceIndexes: evidencePolicy(func(in indexdoc.Read) indexdoc.States {
		states := indexdoc.States{}
		for _, id := range in.DocumentIDs {
			states.Documents = append(states.Documents, indexdoc.State{DocumentID: id, Allowed: id != "revoked"})
		}
		return states
	})}
	visible, err := svc.ListAllDocuments(t.Context(), "reader")
	if err != nil {
		t.Fatal(err)
	}
	ids := make([]string, 0, len(visible))
	for _, doc := range visible {
		ids = append(ids, doc.GetId())
	}
	slices.Sort(ids)
	if !slices.Equal(ids, []string{"full", "legacy-pdf", "upload"}) {
		t.Fatalf("duplicate or inaccessible paper in list: %v", ids)
	}
	if len(docs.docs) != 7 {
		t.Fatal("list removed immutable history")
	}
}
