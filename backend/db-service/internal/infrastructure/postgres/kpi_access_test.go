package postgres_test

import (
	"encoding/json/v2"
	"errors"
	"testing"
	"time"
	"uuid"

	"github.com/example/db-service/internal/domain"
	"github.com/example/db-service/internal/infrastructure/postgres"
	"github.com/example/db-service/internal/platform/indexdoc"
	"github.com/example/db-service/internal/platform/sourcedoc"
)

func TestKPIDocumentAccessAndAtomicBatch(t *testing.T) {
	db := newTestDB(t)
	ctx := t.Context()
	owner, other := uuid.NewV4().String(), uuid.NewV4().String()
	repo := postgres.NewKPIRepo(db).WithSharedCorpus(true)
	kpi := &domain.KPI{ID: uuid.NewV4().String(), OwnerID: owner, Title: "Проверка доступа",
		Direction: "decrease", Status: "active", CreatedAt: time.Now(), UpdatedAt: time.Now()}
	if err := repo.Create(ctx, kpi); err != nil {
		t.Fatal(err)
	}
	ids := make([]string, 0, 4)
	for _, scope := range []string{"", "owner", "public", "shared"} {
		id := seedDoc(t, db, other)
		ids = append(ids, id)
		var source *sourcedoc.Source
		var raw []byte
		if scope != "" {
			source = &sourcedoc.Source{Version: 1, Kind: "paper", Provider: "manual", NativeID: id,
				CanonicalID: id, URL: "https://example.org/access", ObservedAt: time.Now().UTC(),
				Revision: "v1", ContentHash: indexdoc.Hash([]byte(id)), HashAlgorithm: "sha256", AccessScope: scope}
			var err error
			raw, err = json.Marshal(source)
			if err != nil {
				t.Fatal(err)
			}
			if _, err = db.Pool.Exec(ctx, `UPDATE documents SET source_metadata=$2 WHERE id=$1`, id, raw); err != nil {
				t.Fatal(err)
			}
		}
		// Одна политика должна действовать и в SQL, и при чтении полного снимка.
		for _, reader := range []string{"", owner, other} {
			for _, shared := range []bool{false, true} {
				for _, revoked := range []bool{false, true} {
					var allowed bool
					if err := db.Pool.QueryRow(ctx, `SELECT document_readable($1,$2,$3,$4,$5)`,
						reader, other, raw, shared, revoked).Scan(&allowed); err != nil {
						t.Fatal(err)
					}
					if allowed != indexdoc.CanRead(reader, other, source, shared, revoked) {
						t.Fatalf("policy mismatch: scope=%q reader=%q shared=%t revoked=%t", scope, reader, shared, revoked)
					}
				}
			}
		}
	}
	if err := repo.AttachDocuments(ctx, owner, kpi.ID, []string{ids[2], ids[1]}, "input"); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("partially readable batch accepted: %v", err)
	}
	if docs, err := repo.ListDocuments(ctx, owner, kpi.ID); err != nil || len(docs) != 0 {
		t.Fatalf("failed batch left bindings: %v %v", docs, err)
	}
	if err := repo.AttachDocuments(ctx, other, kpi.ID, []string{ids[2]}, "input"); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("foreign KPI accepted: %v", err)
	}
	for range 2 {
		if err := repo.AttachDocuments(ctx, owner, kpi.ID, []string{ids[2], ids[2]}, "input"); err != nil {
			t.Fatal(err)
		}
	}
	if docs, err := repo.ListDocuments(ctx, owner, kpi.ID); err != nil || len(docs) != 1 {
		t.Fatalf("binding retry was not idempotent: %v %v", docs, err)
	}
	if docs, err := repo.ListDocuments(ctx, other, kpi.ID); err != nil || len(docs) != 0 {
		t.Fatalf("foreign KPI leaked bindings: %v %v", docs, err)
	}
	if _, err := db.Pool.Exec(ctx, `INSERT INTO source_index_heads(document_id,revoked) VALUES($1,true)`, ids[2]); err != nil {
		t.Fatal(err)
	}
	if docs, err := repo.ListDocuments(ctx, owner, kpi.ID); err != nil || len(docs) != 0 {
		t.Fatalf("revoked source remained visible: %v %v", docs, err)
	}
	if err := repo.AttachDocuments(ctx, owner, kpi.ID, []string{ids[2]}, "reference"); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("revoked source accepted: %v", err)
	}
	states, err := postgres.NewIndexRepo(db).Read(ctx, indexdoc.Read{
		ReaderID: owner, DocumentIDs: ids, SharedCorpus: true, AccessOnly: true})
	if err != nil || len(states.Documents) != len(ids) {
		t.Fatalf("compact ACL: %v %v", states, err)
	}
	for _, state := range states.Documents {
		want := state.DocumentID == ids[0] || state.DocumentID == ids[3]
		if state.Allowed != want || state.OwnerID != "" || state.Source != nil || state.Active != nil {
			t.Fatalf("incorrect or noncompact ACL response: %+v", state)
		}
	}
}
