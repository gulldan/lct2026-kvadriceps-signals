package application_test

import (
	"encoding/json/v2"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/example/db-service/internal/application"
	"github.com/example/db-service/internal/domain"
	"github.com/example/db-service/internal/platform/sourcedoc"
)

func TestSetDocumentSourceBoundary(t *testing.T) {
	source := sourcedoc.Source{Version: 1, Kind: "paper", Provider: "manual", NativeID: "native", CanonicalID: "canonical",
		URL: "https://example.org/source", Revision: "v1", ContentHash: strings.Repeat("a", 64),
		HashAlgorithm: "blake3", AccessScope: "owner", ObservedAt: time.Now().UTC()}
	raw, err := json.Marshal(source)
	if err != nil {
		t.Fatal(err)
	}
	docs := &fakeDocs{got: &domain.Document{ID: "doc", OwnerID: "owner", ContentHash: source.ContentHash}}
	svc := application.New(nil, docs, nil, nil, nil, nil, nil, nil, nil)
	if err := svc.SetDocumentSource(t.Context(), "doc", "other", raw); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("foreign owner: %v", err)
	}
	if docs.source != nil {
		t.Fatal("foreign owner mutated document")
	}
	if err := svc.SetDocumentSource(t.Context(), "doc", "owner", []byte(`{}`)); !errors.Is(err, domain.ErrInvalidArgument) {
		t.Fatalf("invalid contract: %v", err)
	}
	docs.got.ContentHash = strings.Repeat("b", 64)
	if err := svc.SetDocumentSource(t.Context(), "doc", "owner", raw); !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("hash mismatch: %v", err)
	}
	docs.got.ContentHash = source.ContentHash
	docs.got.Kind = "hypotheses"
	if err := svc.SetDocumentSource(t.Context(), "doc", "owner", raw); !errors.Is(err, domain.ErrInvalidArgument) {
		t.Fatalf("hypothesis accepted: %v", err)
	}
	docs.got.Kind = ""
	if err := svc.SetDocumentSource(t.Context(), "doc", "owner", raw); err != nil {
		t.Fatal(err)
	}
	if len(docs.source) == 0 {
		t.Fatal("valid source not persisted")
	}
}

func TestCreateDocumentValidatesSourceBeforeWrite(t *testing.T) {
	source := sourcedoc.Source{Version: 1, Kind: "analysis", Provider: "manual", NativeID: "report", CanonicalID: "report",
		URL: "https://example.org/report", Revision: "v1", ContentHash: strings.Repeat("a", 64),
		HashAlgorithm: "blake3", AccessScope: "owner", ObservedAt: time.Now().UTC()}
	raw, marshalErr := json.Marshal(source)
	if marshalErr != nil {
		t.Fatal(marshalErr)
	}
	r := newRepos()
	_, badErr := r.svc().CreateDocument(t.Context(), "owner", "report.txt", "text/plain", "key", "wrong", 5, raw)
	if !errors.Is(badErr, domain.ErrInvalidArgument) {
		t.Fatalf("invalid source hash accepted: %v", badErr)
	}
	if r.docs.created != nil {
		t.Fatal("invalid source created a row")
	}
	d, createErr := r.svc().CreateDocument(t.Context(), "owner", "report.txt", "text/plain", "key", source.ContentHash, 5, raw)
	if createErr != nil || len(d.SourceMetadata) == 0 {
		t.Fatalf("document creation lost provenance: %v", createErr)
	}
}
