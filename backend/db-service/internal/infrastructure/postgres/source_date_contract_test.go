package postgres_test

import (
	"fmt"
	"strings"
	"testing"

	"uuid"
)

func sourceMetadata(native, dates string) string {
	return fmt.Sprintf(`{"version":"1","source_kind":"paper","provider":"crossref","access_scope":"public",
"native_id":%q,"canonical_id":%q,"revision":"v1","content_hash":%q,"hash_algorithm":"sha256",
"observed_at":"2026-09-12T00:00:00Z"%s}`, native, native, strings.Repeat("a", 64), dates)
}

func TestSourceDateContract(t *testing.T) {
	db := newTestDB(t)
	ctx := t.Context()
	owner := uuid.NewV4().String()

	insert := func(dates string) error {
		id := uuid.NewV4().String()
		_, err := db.Pool.Exec(ctx, `INSERT INTO documents
(id,owner_id,filename,mime_type,size,object_key,status,chunk_count,created_at,updated_at,content_hash,source_metadata)
VALUES($1,$2,'paper.pdf','application/pdf',1,$3,'indexed',0,now(),now(),$4,$5::jsonb)`,
			id, owner, "k/"+id, id, sourceMetadata(id, dates))
		return err
	}

	accepted := map[string]string{
		"unknown publication": ``,
		"day publication":     `,"publication":{"value":"2026-02-14","precision":"day","basis":"crossref issued"}`,
		"year publication":    `,"publication":{"value":"2026","precision":"year","basis":"crossref issued"}`,
		"revision and grant": `,"publication":{"value":"2026-01-02","precision":"day","basis":"crossref issued"},
"revision_publication":{"value":"2026-03-04","precision":"day","basis":"crossref updated"},
"grant":{"value":"2026-01-02","precision":"day","basis":"USPTO issue date"}`,
		"availability with basis": `,"available_at":"2026-09-11T00:00:00Z","available_basis":"first successful source observation"`,
	}
	for name, dates := range accepted {
		t.Run(name, func(t *testing.T) {
			if err := insert(dates); err != nil {
				t.Fatalf("sourced date rejected: %v", err)
			}
		})
	}

	rejected := map[string]string{
		"publication without basis":     `,"publication":{"value":"2026-02-14","precision":"day"}`,
		"publication without precision": `,"publication":{"value":"2026-02-14","basis":"crossref issued"}`,
		"invented precision":            `,"publication":{"value":"2026","precision":"decade","basis":"crossref issued"}`,
		"empty basis":                   `,"publication":{"value":"2026-02-14","precision":"day","basis":""}`,
		"priority without basis":        `,"priority":{"value":"2021-04-02","precision":"day"}`,
		"filing without precision":      `,"filing":{"value":"2022-03-11","basis":"USPTO filing line"}`,
		"grant without basis":           `,"grant":{"value":"2026-01-02","precision":"day"}`,
		"revision without precision":    `,"revision_publication":{"value":"2026-03-04","basis":"crossref updated"}`,
		"availability without basis":    `,"available_at":"2026-09-11T00:00:00Z"`,
	}
	for name, dates := range rejected {
		t.Run(name, func(t *testing.T) {
			if err := insert(dates); err == nil {
				t.Fatal("date without precision or basis accepted")
			}
		})
	}
}
