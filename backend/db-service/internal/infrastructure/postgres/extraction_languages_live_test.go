package postgres

import (
	"encoding/json/v2"
	"os"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Временные таблицы на одном соединении оставляют рабочий запрос Pending только читающим.
func TestExtractionLanguagesLive(t *testing.T) {
	if os.Getenv("TR_MULTILINGUAL_INTAKE_LIVE") != "1" {
		t.Skip("explicit temporary-table PostgreSQL check required")
	}
	cfg, err := pgxpool.ParseConfig(os.Getenv("POSTGRES_DSN"))
	if err != nil {
		t.Fatal("parse PostgreSQL configuration")
	}
	cfg.MaxConns = 1
	pool, err := pgxpool.NewWithConfig(t.Context(), cfg)
	if err != nil {
		t.Fatal("connect to PostgreSQL")
	}
	t.Cleanup(pool.Close)
	_, err = pool.Exec(t.Context(), `CREATE TEMP TABLE documents(
id text PRIMARY KEY, owner_id text, source_metadata jsonb, created_at timestamptz DEFAULT now());
CREATE TEMP TABLE source_index_heads(document_id text, active_snapshot_id text, revoked bool);
CREATE TEMP TABLE source_index_snapshots(snapshot_id text, ready bool, manifest jsonb);
CREATE TEMP TABLE technology_extraction_jobs(id text, source_revision_id text, extractor_version text, schema_version text);`)
	if err != nil {
		t.Fatal(err)
	}
	for _, code := range []string{"zh", "ja", "ko", "ar", "hi", "de", "ru", "en", ""} {
		metadata, marshalErr := json.Marshal(map[string]string{"language": code, "language_basis": "provider language"})
		if marshalErr != nil {
			t.Fatal(marshalErr)
		}
		_, err = pool.Exec(t.Context(), `INSERT INTO documents(id,owner_id,source_metadata) VALUES($1,'owner',$2)`, code, metadata)
		if err != nil {
			t.Fatal(err)
		}
		_, err = pool.Exec(t.Context(), `INSERT INTO source_index_heads VALUES($1,$1,false)`, code)
		if err != nil {
			t.Fatal(err)
		}
		_, err = pool.Exec(t.Context(), `INSERT INTO source_index_snapshots VALUES($1,true,'{}')`, code)
		if err != nil {
			t.Fatal(err)
		}
	}
	repo := ExtractionRepo{pool: pool}
	rows, err := repo.Pending(t.Context(), "extractor", "schema", 50)
	if err != nil || len(rows) != 9 {
		t.Fatalf("ready original languages were excluded: %d rows, %v", len(rows), err)
	}
	for _, row := range rows {
		var metadata map[string]string
		if err = json.Unmarshal([]byte(row.SourceMetadata), &metadata); err != nil || metadata["language"] != row.DocumentID {
			t.Fatalf("source language was rewritten: %+v, %v", row, err)
		}
	}
	// Готовность, отзыв и идемпотентность действуют на любом языке.
	_, err = pool.Exec(t.Context(), `UPDATE source_index_heads SET revoked=true WHERE document_id='ja';
UPDATE source_index_snapshots SET ready=false WHERE snapshot_id='ko';
INSERT INTO technology_extraction_jobs VALUES('done','zh','extractor','schema');`)
	if err != nil {
		t.Fatal(err)
	}
	rows, err = repo.Pending(t.Context(), "extractor", "schema", 50)
	if err != nil || len(rows) != 6 {
		t.Fatalf("readiness or idempotency changed: %d rows, %v", len(rows), err)
	}
	for _, row := range rows {
		if row.DocumentID == "ja" || row.DocumentID == "ko" || row.DocumentID == "zh" {
			t.Fatalf("an ineligible revision was queued: %+v", row)
		}
	}
}
