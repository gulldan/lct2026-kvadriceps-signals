package postgres

import (
	"encoding/json/v2"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/example/db-service/internal/platform/analysisdoc"
	"github.com/example/db-service/internal/platform/harvest"
	"github.com/example/db-service/internal/platform/sourcedoc"
	"github.com/jackc/pgx/v5"
)

// Использует временные таблицы транзакции; рабочие строки не трогает.
func TestAnalysisCollectionScopeLive(t *testing.T) {
	if os.Getenv("TR_COLLECTION_SCOPE_LIVE") != "1" {
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
	_, err = tx.Exec(t.Context(), `
CREATE TEMP TABLE documents(id text PRIMARY KEY, title text, mime_type text, source_metadata jsonb, owner_id text);
CREATE TEMP TABLE source_index_heads(document_id text, active_snapshot_id text, revoked boolean);
CREATE TEMP TABLE source_index_snapshots(document_id text, snapshot_id text, ready boolean, manifest jsonb);
CREATE TEMP TABLE source_collection_scopes(id text PRIMARY KEY, owner_id text, test_run_id text);
CREATE TEMP TABLE source_collection_documents(record_key text PRIMARY KEY, document_id text);
CREATE TEMP TABLE source_collection_membership(scope_id text, document_id text, first_seen_at timestamptz DEFAULT now(),
PRIMARY KEY(scope_id,document_id));`)
	if err != nil {
		t.Fatal(err)
	}
	a, b := strings.Repeat("a", 64), strings.Repeat("b", 64)
	_, err = tx.Exec(t.Context(), `INSERT INTO source_collection_scopes VALUES($1,'owner',''),($2,'other','')`, a, b)
	if err != nil {
		t.Fatal(err)
	}
	request := analysisdoc.Request{OwnerID: "owner", Start: "2020-01-01", Cutoff: "2026-09-11T23:59:59Z",
		Mode: "as_observed", CollectionScopeIDs: []string{a}}
	if err = analysisCollectionScopes(t.Context(), tx, request); err != nil {
		t.Fatal(err)
	}
	request.CollectionScopeIDs = []string{b}
	if err = analysisCollectionScopes(t.Context(), tx, request); !errors.Is(err, analysisdoc.ErrNotFound) {
		t.Fatal("another owner's scope was exposed", err)
	}
	request.CollectionScopeIDs = []string{strings.Repeat("c", 64)}
	if err = analysisCollectionScopes(t.Context(), tx, request); !errors.Is(err, analysisdoc.ErrNotFound) {
		t.Fatal("unknown scope produced a different access outcome", err)
	}
	request.CollectionScopeIDs = []string{a}
	at := time.Date(2026, 9, 10, 0, 0, 0, 0, time.UTC)
	source := sourcedoc.Source{Version: 1, Kind: "paper", Provider: "crossref", NativeID: "10.1234/check",
		URL: "https://doi.org/10.1234/check", CanonicalID: "doi:10.1234/check", Revision: "revision-1",
		ContentHash: strings.Repeat("d", 64), HashAlgorithm: "sha256", AccessScope: "public", ObservedAt: at,
		Publication: &sourcedoc.Date{Value: "2026-09-09", Precision: "day", Basis: "test primary metadata"}}
	metadata, err := json.Marshal(source)
	if err != nil {
		t.Fatal(err)
	}
	_, err = tx.Exec(t.Context(), `INSERT INTO documents VALUES
('selected','Selected','text/plain',$1,'owner'),('late','Later encounter','text/plain',$1,'owner'),
('unselected','Unrelated','text/plain',$1,'owner');`, metadata)
	if err != nil {
		t.Fatal(err)
	}
	_, err = tx.Exec(t.Context(), `INSERT INTO source_collection_membership VALUES
($1,'selected','2026-09-10'),($1,'late','2026-09-12')`, a)
	if err != nil {
		t.Fatal(err)
	}
	_, err = tx.Exec(t.Context(), `INSERT INTO documents
SELECT 'unrelated-'||n,'Unrelated','text/plain',$1,'owner' FROM generate_series(1,$2) n`,
		metadata, analysisdoc.MaxDocuments+1)
	if err != nil {
		t.Fatal(err)
	}
	input := analysisdoc.Input{Request: request}
	if err = analysisRecords(t.Context(), tx, &input); err != nil || len(input.Excluded) != 1 ||
		input.Excluded[0].DocumentID != "selected" || input.Excluded[0].Reason != "index_not_ready" {
		t.Fatalf("scope/cutoff filtering or index readiness was bypassed: %+v, %v", input.Excluded, err)
	}
	if len(input.Limitations) != 0 {
		t.Fatalf("unrelated documents truncated the selected scope: %v", input.Limitations)
	}
	unbounded := analysisdoc.Input{Request: request}
	unbounded.Request.CollectionScopeIDs = nil
	if err = analysisRecords(t.Context(), tx, &unbounded); err != nil {
		t.Fatal("a large corpus must remain available with explicit truncation", err)
	}
	if got := len(unbounded.Records) + len(unbounded.Excluded); got != analysisdoc.MaxDocuments {
		t.Fatalf("corpus cap: got %d documents, want %d", got, analysisdoc.MaxDocuments)
	}
	if len(unbounded.Limitations) != 1 || !strings.HasPrefix(unbounded.Limitations[0], "corpus_truncated:") {
		t.Fatalf("corpus truncation was not disclosed: %v", unbounded.Limitations)
	}
	// Повторная загрузка должна записать в новый охват реальный существующий ID,
	// не вставляя предложенный новый и не отправляя сообщение ещё раз.
	record := harvest.Prepared{DocumentID: "new-proposed-id", Source: source, MIME: "text/plain"}
	_, err = tx.Exec(t.Context(), `INSERT INTO source_collection_documents VALUES($1,'selected')`,
		harvest.DocumentKey("owner", record))
	if err != nil {
		t.Fatal(err)
	}
	job := harvest.Job{OwnerID: "owner", Lease: harvest.Lease{ScopeID: b}}
	if err := saveHarvestDocument(t.Context(), tx, job, record); err != nil {
		t.Fatal(err)
	}
	var id string
	var first time.Time
	if err := tx.QueryRow(t.Context(), `SELECT document_id,first_seen_at FROM source_collection_membership WHERE scope_id=$1`, b).
		Scan(&id, &first); err != nil || id != "selected" {
		t.Fatal("duplicate encounter lost its actual source identity", err)
	}
	if err := saveHarvestDocument(t.Context(), tx, job, record); err != nil {
		t.Fatal(err)
	}
	var count int
	var repeated time.Time
	if err := tx.QueryRow(t.Context(), `SELECT count(*),min(first_seen_at) FROM source_collection_membership WHERE scope_id=$1`, b).
		Scan(&count, &repeated); err != nil || count != 1 || !first.Equal(repeated) {
		t.Fatal("repeat ingestion changed first encounter or duplicated membership", err)
	}
}
