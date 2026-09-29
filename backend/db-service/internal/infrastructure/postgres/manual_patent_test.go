package postgres_test

import (
	"fmt"
	"strings"
	"testing"

	"uuid"
)

func patentMetadata(publication, jurisdiction, translationOf, native string) string {
	translation := ""
	if translationOf != "" {
		translation = fmt.Sprintf(`,"translation_of":%q`, translationOf)
	}
	return fmt.Sprintf(`{"version":"1","source_kind":"patent","provider":"manual","access_scope":"owner",
"native_id":%q,"canonical_id":"patent:%s","revision":%q,"content_hash":%q,"hash_algorithm":"sha256",
"observed_at":"2026-09-12T00:00:00Z",
"patent":{"publication_number":%q,"jurisdiction":%q,"kind_code":"C1"%s}}`,
		native, publication, uuid.NewV4().String(), strings.Repeat("a", 64),
		publication, jurisdiction, translation)
}

func TestManualPatentIdentity(t *testing.T) {
	db := newTestDB(t)
	ctx := t.Context()
	owner := uuid.NewV4().String()

	insert := func(metadata string) error {
		id := uuid.NewV4().String()
		_, err := db.Pool.Exec(ctx, `INSERT INTO documents
(id,owner_id,filename,mime_type,size,object_key,status,chunk_count,created_at,updated_at,content_hash,source_metadata)
VALUES($1,$2,'patent.pdf','application/pdf',1,$3,'indexed',0,now(),now(),$4,$5::jsonb)`,
			id, owner, "k/"+id, id, metadata)
		return err
	}

	if err := insert(patentMetadata("RU2789123", "RU", "", "RU2789123")); err != nil {
		t.Fatalf("import a patent: %v", err)
	}
	if insert(patentMetadata("RU2789123", "RU", "", "RU2789123")) == nil {
		t.Fatal("a second file of the same publication became a second patent")
	}
	if err := insert(patentMetadata("RU2789123", "EA", "", "RU2789123")); err != nil {
		t.Fatalf("another jurisdiction is another publication: %v", err)
	}
	if err := insert(patentMetadata("EP3456789", "EP", "RU2789123", "EP3456789")); err != nil {
		t.Fatalf("a translation of another publication: %v", err)
	}
	if insert(patentMetadata("RU2789124", "RU", "RU2789124", "RU2789124")) == nil {
		t.Fatal("a publication was accepted as a translation of itself")
	}
	if insert(patentMetadata("RU2789125", "ru", "", "RU2789125")) == nil {
		t.Fatal("a jurisdiction that is not a country code was accepted")
	}

	var records, canonical int
	err := db.Pool.QueryRow(ctx, `SELECT count(*), count(DISTINCT source_metadata->>'canonical_id')
FROM documents WHERE owner_id=$1`, owner).Scan(&records, &canonical)
	if err != nil {
		t.Fatalf("read imports: %v", err)
	}
	// Три записи, но перевод разделяет идентичность пересказываемого патента,
	// поэтому в корпусе два изобретения, а не три.
	if records != 3 || canonical != 2 {
		t.Fatalf("expected three records over two identities, got %d over %d", records, canonical)
	}
}
