package application

import (
	"bytes"
	"errors"
	"testing"

	"github.com/example/main-service/internal/platform/indexdoc"
)

func TestSourceOriginalPinsBytesAndFailsClosed(t *testing.T) {
	svc, manifest, store := evidenceFixture(t)
	want := bytes.Clone(store.objects[manifest.Text.Key])
	store.objects[manifest.RawKey] = want
	got, snapshot, err := svc.SourceOriginal(t.Context(), "alice", manifest.DocumentID, manifest.SnapshotID)
	if err != nil || !bytes.Equal(got, want) || snapshot.SnapshotID != manifest.SnapshotID {
		t.Fatal("saved original was not returned", err)
	}
	store.objects[manifest.RawKey] = []byte("a newer or corrupted original")
	raw, _, readErr := svc.SourceOriginal(t.Context(), "alice", manifest.DocumentID, manifest.SnapshotID)
	if readErr == nil || raw != nil {
		t.Fatal("replacement bytes leaked")
	}
	if _, _, readErr := svc.SourceOriginal(t.Context(), "alice", manifest.DocumentID, "unknown"); !errors.Is(readErr, ErrNotFound) {
		t.Fatal("missing snapshot was replaced", readErr)
	}
	if _, _, readErr := svc.SourceOriginal(t.Context(), "alice", manifest.DocumentID, ""); !errors.Is(readErr, ErrInvalidArgument) {
		t.Fatal("unpinned original accepted", readErr)
	}
}

func TestSourceOriginalRechecksRevocationBeforeReturningBytes(t *testing.T) {
	svc, manifest, store := evidenceFixture(t)
	svc.store = nil
	if _, _, err := svc.SourceOriginal(t.Context(), "bob", manifest.DocumentID, manifest.SnapshotID); !errors.Is(err, ErrNotFound) {
		t.Fatal("foreign reader allowed", err)
	}
	svc.store = store
	store.objects[manifest.RawKey] = bytes.Clone(store.objects[manifest.Text.Key])
	calls := 0
	svc.sourceIndexes = evidencePolicy(func(_ indexdoc.Read) indexdoc.States {
		calls++
		return indexdoc.States{Documents: []indexdoc.State{{DocumentID: manifest.DocumentID,
			Active: manifest, Allowed: calls == 1}}}
	})
	raw, _, err := svc.SourceOriginal(t.Context(), "alice", manifest.DocumentID, manifest.SnapshotID)
	if !errors.Is(err, ErrNotFound) || raw != nil {
		t.Fatal("revocation while reading raw object did not hide bytes", err)
	}
}
