package postgres

import (
	"encoding/json/v2"
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/example/db-service/internal/platform/harvest"
	"github.com/example/db-service/internal/platform/sourcedoc"
	"github.com/jackc/pgx/v5"
)

// Таблицы транзакции; ни один рабочий документ не записывается.
func TestHarvestIncrementalLive(t *testing.T) {
	if os.Getenv("TR_HARVEST_INCREMENTAL_LIVE") != "1" {
		t.Skip("explicit temporary-table PostgreSQL check required")
	}
	conn, err := pgx.Connect(t.Context(), os.Getenv("POSTGRES_DSN"))
	if err != nil {
		t.Fatal("connect to PostgreSQL")
	}
	t.Cleanup(func() { _ = conn.Close(t.Context()) })
	tx, err := conn.Begin(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback(t.Context()) }()
	_, err = tx.Exec(t.Context(), `CREATE TEMP TABLE documents(id text PRIMARY KEY, owner_id text,
mime_type text, source_metadata jsonb, status text, created_at timestamptz DEFAULT now());
CREATE TEMP TABLE source_collection_documents(record_key text PRIMARY KEY, document_id text, source_hash text);
CREATE TEMP TABLE collection_manifests (LIKE public.collection_manifests INCLUDING ALL) ON COMMIT DROP;`)
	if err != nil {
		t.Fatal(err)
	}
	in := harvest.Prepared{MIME: "text/plain", Source: sourcedoc.Source{Provider: "crossref", NativeID: "10.1234/paper",
		ContentHash: strings.Repeat("a", 64), AccessScope: "public", Revision: "metadata-jats-v2:old",
		Publication: &sourcedoc.Date{Value: "2026-09-01", Precision: "day", Basis: "Crossref published.date-parts"}}}
	raw, err := json.Marshal(in.Source)
	if err != nil {
		t.Fatal(err)
	}
	_, err = tx.Exec(t.Context(), `INSERT INTO documents(id,owner_id,mime_type,source_metadata,status)
VALUES('existing','owner','text/plain',$1,'indexed')`, raw)
	if err != nil {
		t.Fatal(err)
	}
	in.Source.Revision = "paper-text-v3:new"
	key := harvest.DocumentKey("owner", in)
	for range 2 {
		id, lookupErr := existingHarvestDocument(t.Context(), tx, "owner", key, in)
		if lookupErr != nil || id != "existing" {
			t.Fatalf("unchanged paper was not reused: %s, %v", id, lookupErr)
		}
	}
	for _, change := range []string{"text", "date", "owner", "namespace"} {
		candidate := in
		owner := "owner"
		switch change {
		case "text":
			candidate.Source.ContentHash = strings.Repeat("b", 64)
		case "date":
			candidate.Source.Publication = &sourcedoc.Date{Value: "2026-09-02", Precision: "day", Basis: "Crossref published.date-parts"}
			candidate.Source.Revision += ":date-correction"
		case "owner":
			owner = "other"
		case "namespace":
			candidate.Source.TestRunID = "tr-test-isolated"
		}
		_, err = existingHarvestDocument(t.Context(), tx, owner, harvest.DocumentKey(owner, candidate), candidate)
		if !errors.Is(err, pgx.ErrNoRows) {
			t.Fatalf("%s was incorrectly treated as identical: %v", change, err)
		}
	}
	job := harvest.Job{OwnerID: "owner", Provider: "crossref", Limit: 4}
	page := harvest.CommitPage{Lease: harvest.Lease{ScopeID: "temporary-scope", RunID: "temporary-run"},
		Received: 2, Rejected: 1, Records: []harvest.Prepared{in}}
	for range 2 {
		if err = saveCollectionManifest(t.Context(), tx, job, page); err != nil {
			t.Fatal(err)
		}
	}
	var counts [4]int
	err = tx.QueryRow(t.Context(), `SELECT listed, received, rejected, processed FROM collection_manifests
WHERE scope_id=$1 AND run_id=$2`, page.Lease.ScopeID, page.Lease.RunID).
		Scan(&counts[0], &counts[1], &counts[2], &counts[3])
	if err != nil || counts != [4]int{4, 4, 2, 2} {
		t.Fatalf("rejected entries were counted twice: %v, %v", counts, err)
	}
}
