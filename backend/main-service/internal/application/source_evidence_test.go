package application

import (
	"context"
	"encoding/json/v2"
	"errors"
	"testing"
	"time"
	"uuid"

	"github.com/example/main-service/internal/platform/indexdoc"
	"github.com/example/main-service/internal/platform/sourcedoc"
)

type evidencePolicy func(indexdoc.Read) indexdoc.States

func (p evidencePolicy) ReadIndexes(_ context.Context, in indexdoc.Read) (indexdoc.States, error) {
	return p(in), nil
}

func evidenceFixture(t *testing.T) (*IngestionService, *indexdoc.Manifest, *fakeObjectStore) {
	t.Helper()
	doc, snap := uuid.NewV4().String(), uuid.NewV4().String()
	text := "Проверяемый фрагмент первичного источника."
	hash := indexdoc.Hash([]byte(text))
	source := sourcedoc.Source{Version: 1, Kind: "analysis", Provider: "manual", NativeID: "report",
		CanonicalID: "report", URL: "https://example.org/report", ObservedAt: time.Now().UTC(), Revision: "v1",
		ContentHash: hash, HashAlgorithm: "sha256", AccessScope: "owner"}
	model := indexdoc.Model{Name: "f2", Revision: "pinned", Dimension: 2}
	snapshot := indexdoc.Snapshot{Version: 1, DocumentID: doc, OwnerID: "alice", SnapshotID: snap,
		Filename: "report.txt", MIMEType: "text/plain", Source: source, TextHash: hash, Model: model,
		PipelineRevision: "source-v1", ParserRevision: "text-v1", Chunks: []indexdoc.Chunk{{
			ID: uuid.NewV4().String(), Text: text, CharEnd: len([]rune(text)), LocatorKind: "text_span",
		}}}
	raw, err := json.Marshal(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	manifest := &indexdoc.Manifest{Version: 1, DocumentID: doc, OwnerID: "alice", SnapshotID: snap,
		SourceRevision: "v1", RawHash: hash, RawKey: "original", Model: model, ChunkCount: 1,
		PipelineRevision: "source-v1", ParserRevision: "text-v1",
		Data: indexdoc.Object{Key: indexdoc.Prefix(doc, snap) + indexdoc.Hash(raw) + ".json", SHA256: indexdoc.Hash(raw)},
		Text: indexdoc.Object{Key: indexdoc.Prefix(doc, snap) + hash + ".txt", SHA256: hash}}
	store := newObjectStore()
	store.objects[manifest.Data.Key], store.objects[manifest.Text.Key] = raw, []byte(text)
	policy := evidencePolicy(func(in indexdoc.Read) indexdoc.States {
		return indexdoc.States{Documents: []indexdoc.State{{DocumentID: doc, Allowed: in.ReaderID == "alice", Active: manifest}}}
	})
	return &IngestionService{sourceIndexes: policy, store: store}, manifest, store
}

func TestSourceEvidenceRetainsRevisionAndRejectsTamperedText(t *testing.T) {
	svc, m, store := evidenceFixture(t)
	snapshot, err := svc.SourceEvidence(t.Context(), "alice", m.DocumentID, m.SnapshotID)
	if err != nil || snapshot.SnapshotID != m.SnapshotID || len(snapshot.Chunks) != 1 {
		t.Fatal("historical evidence not returned", err)
	}
	store.objects[m.Text.Key] = []byte("different source text")
	if _, err = svc.SourceEvidence(t.Context(), "alice", m.DocumentID, m.SnapshotID); err == nil {
		t.Fatal("text tamper accepted")
	}
}

func TestSourceEvidenceDeniesBeforeStorageAndRechecksAfterLoading(t *testing.T) {
	svc, m, store := evidenceFixture(t)
	svc.store = nil
	if _, err := svc.SourceEvidence(t.Context(), "bob", m.DocumentID, m.SnapshotID); !errors.Is(err, ErrNotFound) {
		t.Fatal("foreign reader allowed", err)
	}
	svc.store = store
	calls := 0
	svc.sourceIndexes = evidencePolicy(func(_ indexdoc.Read) indexdoc.States {
		calls++
		return indexdoc.States{Documents: []indexdoc.State{{DocumentID: m.DocumentID, Active: m, Allowed: calls == 1}}}
	})
	if _, err := svc.SourceEvidence(t.Context(), "alice", m.DocumentID, m.SnapshotID); !errors.Is(err, ErrNotFound) || calls != 2 {
		t.Fatal("revocation while loading did not hide the response", err)
	}
}
