package postgres_test

import (
	"context"
	"encoding/json/v2"
	"errors"
	"os"
	"strings"
	"sync"
	"testing"
	"time"
	"uuid"

	"github.com/example/db-service/internal/infrastructure/postgres"
	"github.com/example/db-service/internal/platform/indexdoc"
	"github.com/example/db-service/internal/platform/sourcedoc"
	"github.com/rs/zerolog"
)

// Пишет только ID из манифеста в тестовом пространстве; проверяет лишь ограждение в БД.
func TestSourceIndexLive(t *testing.T) {
	filename := os.Getenv("TR_INDEX_MANIFEST")
	if filename == "" {
		t.Skip("bounded predeclared index manifest required")
	}
	raw, err := os.ReadFile(filename)
	if err != nil {
		t.Fatal(err)
	}
	var manifest struct {
		RunID string   `json:"test_run_id"`
		IDs   []string `json:"ids"`
	}
	if err = json.Unmarshal(raw, &manifest, json.RejectUnknownMembers(true)); err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(manifest.RunID, "tr-test-") || len(manifest.IDs) != 8 {
		t.Fatal("invalid index test manifest")
	}
	if _, err = uuid.Parse(strings.TrimPrefix(manifest.RunID, "tr-test-")); err != nil {
		t.Fatal(err)
	}
	seen := map[string]bool{}
	for _, id := range manifest.IDs {
		if _, err = uuid.Parse(id); err != nil || seen[id] {
			t.Fatal("invalid or duplicate manifest ID")
		}
		seen[id] = true
	}
	dsn := os.Getenv("DB_TEST_DSN")
	if dsn == "" {
		dsn = os.Getenv("POSTGRES_DSN")
	}
	if dsn == "" {
		t.Fatal("existing DB DSN required")
	}
	db, err := postgres.Connect(t.Context(), dsn, zerolog.Nop())
	if err != nil {
		t.Fatal("cannot connect to existing DB")
	}
	t.Cleanup(db.Close)
	var existing int
	if err = db.Pool.QueryRow(t.Context(), `SELECT count(*) FROM documents WHERE id=ANY($1)`, manifest.IDs).Scan(
		&existing,
	); err != nil || existing != 0 {
		t.Fatal("index test IDs must be unused")
	}
	owner := manifest.RunID
	t.Cleanup(
		func() {
			cleanup, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			_, cleanupErr := db.Pool.Exec(
				cleanup,
				`DELETE FROM documents WHERE id=ANY($1) AND owner_id=$2
AND source_metadata->>'test_run_id'=$2`,
				manifest.IDs[:3],
				owner,
			)
			if cleanupErr != nil {
				t.Error(cleanupErr)
			}
		},
	)
	for i, scope := range []string{"owner", "public", "shared"} {
		source := sourcedoc.Source{
			Version:       1,
			Kind:          "paper",
			Provider:      "manual",
			NativeID:      manifest.IDs[i],
			CanonicalID:   manifest.IDs[i],
			URL:           "https://example.org/control",
			ObservedAt:    time.Now().UTC(),
			Revision:      "v1",
			ContentHash:   indexdoc.Hash([]byte("original")),
			HashAlgorithm: "sha256",
			AccessScope:   scope,
			TestRunID:     owner,
		}
		sourceRaw, encodeErr := json.Marshal(source)
		if encodeErr != nil {
			t.Fatal(encodeErr)
		}
		_, insertErr := db.Pool.Exec(
			t.Context(),
			`INSERT INTO documents
(id,owner_id,filename,mime_type,size,object_key,status,status_msg,content_hash,source_metadata)
VALUES($1,$2,'source-index-control.txt','text/plain',8,$3,'parsed','',$4,$5)`,
			manifest.IDs[i],
			owner,
			"tests/"+owner+"/original",
			source.ContentHash,
			sourceRaw,
		)
		if insertErr != nil {
			t.Fatal(insertErr)
		}
	}
	repo := postgres.NewIndexRepo(db)
	lease := indexdoc.Lease{DocumentID: manifest.IDs[0], OwnerID: owner, Token: manifest.IDs[3]}
	job, err := repo.Begin(t.Context(), lease)
	if err != nil {
		t.Fatal(err)
	}
	active := indexTestManifest(job, 10)
	_, err = repo.Activate(t.Context(), indexdoc.Activation{Lease: lease, Manifest: active})
	if !errors.Is(err, indexdoc.ErrConflict) {
		t.Fatal("unprepared snapshot activated")
	}
	for range 2 {
		if _, err = repo.Prepare(t.Context(), indexdoc.Activation{Lease: lease, Manifest: active}); err != nil {
			t.Fatal(err)
		}
		if _, err = repo.Activate(t.Context(), indexdoc.Activation{Lease: lease, Manifest: active}); err != nil {
			t.Fatal(err)
		}
	}
	if _, err = repo.Finish(t.Context(), indexdoc.Finish{Lease: lease}); err != nil {
		t.Fatal(err)
	}
	if _, err = repo.Finish(t.Context(), indexdoc.Finish{Lease: lease}); err != nil {
		t.Fatal("ambiguous finish retry", err)
	}
	second := indexdoc.Lease{DocumentID: lease.DocumentID, OwnerID: owner, Token: manifest.IDs[4]}
	secondJob, err := repo.Begin(t.Context(), second)
	if err != nil || secondJob.Active == nil || *secondJob.Active != active {
		t.Fatal("previous snapshot lost")
	}
	secondManifest := indexTestManifest(secondJob, 3)
	if _, err = repo.Prepare(t.Context(), indexdoc.Activation{Lease: second, Manifest: secondManifest}); err != nil {
		t.Fatal(err)
	}
	if _, err = db.Pool.Exec(
		t.Context(),
		`UPDATE source_index_heads SET leased_until=clock_timestamp()-interval '1 second'
WHERE document_id=$1 AND lease_token=$2`,
		lease.DocumentID,
		second.Token,
	); err != nil {
		t.Fatal(err)
	}
	_, err = repo.Activate(t.Context(), indexdoc.Activation{Lease: second, Manifest: secondManifest})
	if !errors.Is(err, indexdoc.ErrConflict) {
		t.Fatal("expired index lease activated")
	}
	if _, err = repo.Renew(t.Context(), second); !errors.Is(err, indexdoc.ErrConflict) {
		t.Fatal("expired lease resurrected")
	}
	checkCompetingIndexes(t, repo, lease, manifest.IDs[5:7])
	checkIndexReaders(t, repo, owner, manifest.IDs[:3], active)
	checkIndexAccessChanges(t, repo, owner, manifest.IDs[1], manifest.IDs[7], manifest.IDs[4])
	count := 0
	if err = postgres.NewDocumentRepo(db).UpdateStatus(
		t.Context(),
		lease.DocumentID,
		"parsed",
		"late parser",
		&count,
	); err != nil {
		t.Fatal(err)
	}
	var status string
	var chunks int
	if err = db.Pool.QueryRow(
		t.Context(),
		`SELECT status,chunk_count FROM documents WHERE id=$1`,
		lease.DocumentID,
	).Scan(
		&status,
		&chunks,
	); err != nil || status != "indexed" || chunks != 3 {
		t.Fatal("late parser downgraded ready index")
	}
}

func indexTestManifest(job indexdoc.Job, chunks int) indexdoc.Manifest {
	prefix := indexdoc.Prefix(job.Lease.DocumentID, job.Lease.Token)
	textHash, dataHash := indexdoc.Hash([]byte("test extracted text")), indexdoc.Hash([]byte("test snapshot"))
	return indexdoc.Manifest{
		Version:          1,
		DocumentID:       job.Lease.DocumentID,
		OwnerID:          job.Lease.OwnerID,
		SnapshotID:       job.Lease.Token,
		SourceRevision:   job.Source.Revision,
		RawHash:          job.Source.ContentHash,
		RawKey:           job.ObjectKey,
		Text:             indexdoc.Object{Key: prefix + textHash + ".txt", SHA256: textHash},
		Data:             indexdoc.Object{Key: prefix + dataHash + ".json", SHA256: dataHash},
		ChunkCount:       chunks,
		Model:            indexdoc.Model{Name: "control", Revision: "test-only", Dimension: 1},
		PipelineRevision: "test-v1",
		ParserRevision:   "test-v1",
	}
}

func checkCompetingIndexes(t *testing.T, repo *postgres.IndexRepo, previous indexdoc.Lease, tokens []string) {
	t.Helper()
	var wait sync.WaitGroup
	jobs := make(chan indexdoc.Job, 2)
	failures := make(chan error, 2)
	for _, token := range tokens {
		wait.Go(
			func() {
				lease := previous
				lease.Token = token
				job, err := repo.Begin(t.Context(), lease)
				if err == nil {
					jobs <- job
				} else {
					failures <- err
				}
			},
		)
	}
	wait.Wait()
	if len(jobs) != 1 || len(failures) != 1 {
		t.Fatal("two workers acquired the same document")
	}
	if err := <-failures; !errors.Is(err, indexdoc.ErrConflict) {
		t.Fatal(err)
	}
	job := <-jobs
	next := indexTestManifest(job, 3)
	if _, err := repo.Prepare(t.Context(), indexdoc.Activation{Lease: job.Lease, Manifest: next}); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.Activate(t.Context(), indexdoc.Activation{Lease: job.Lease, Manifest: next}); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.Finish(t.Context(), indexdoc.Finish{Lease: job.Lease}); err != nil {
		t.Fatal(err)
	}
}

func checkIndexReaders(t *testing.T, repo *postgres.IndexRepo, owner string, ids []string, old indexdoc.Manifest) {
	t.Helper()
	for _, test := range []struct {
		reader  string
		shared  bool
		allowed int
	}{
		{owner, false, 3},
		{"another-user", false, 1},
		{"another-user", true, 2},
	} {
		state, err := repo.Read(
			t.Context(),
			indexdoc.Read{ReaderID: test.reader, SharedCorpus: test.shared, DocumentIDs: ids},
		)
		if err != nil {
			t.Fatal(err)
		}
		allowed := 0
		for _, d := range state.Documents {
			if d.Allowed {
				allowed++
			} else if d.Source != nil || d.Active != nil || d.OwnerID != "" {
				t.Fatal("private metadata leaked")
			}
		}
		if allowed != test.allowed {
			t.Fatalf("allowed %d, want %d", allowed, test.allowed)
		}
	}
	historic, err := repo.Read(
		t.Context(),
		indexdoc.Read{ReaderID: owner, DocumentIDs: ids[:1], SnapshotID: old.SnapshotID},
	)
	if err != nil || historic.Documents[0].Active == nil || *historic.Documents[0].Active != old {
		t.Fatal("old ready snapshot changed")
	}
	denied, err := repo.Read(
		t.Context(),
		indexdoc.Read{
			ReaderID:     "another-user",
			SharedCorpus: true,
			DocumentIDs:  ids[:1],
			SnapshotID:   old.SnapshotID,
		},
	)
	if err != nil || denied.Documents[0].Allowed || denied.Documents[0].Active != nil {
		t.Fatal("private historical snapshot leaked")
	}
}

func checkIndexAccessChanges(t *testing.T, repo *postgres.IndexRepo, owner, doc, revokeID, restoreID string) {
	t.Helper()
	read := indexdoc.Read{ReaderID: "another-reader", SharedCorpus: true, DocumentIDs: []string{doc}}
	before, err := repo.Read(t.Context(), read)
	if err != nil || len(before.Documents) != 1 || !before.Documents[0].Allowed {
		t.Fatal("public source unavailable", err)
	}
	change := indexdoc.AccessChange{DocumentID: doc, OwnerID: "another-reader", RequestID: revokeID, Revoked: true}
	if _, err = repo.SetAccess(t.Context(), change); !errors.Is(err, indexdoc.ErrNotFound) {
		t.Fatal("foreign access write", err)
	}
	change.OwnerID = owner
	for range 2 {
		if _, err = repo.SetAccess(t.Context(), change); err != nil {
			t.Fatal(err)
		}
	}
	after, err := repo.Read(t.Context(), read)
	if err != nil || after.Documents[0].Allowed || after.Documents[0].Source != nil || after.Epoch != before.Epoch+1 {
		t.Fatal("revocation leak or retry advanced epoch", err)
	}
	change.Revoked = false
	if _, err = repo.SetAccess(t.Context(), change); !errors.Is(err, indexdoc.ErrConflict) {
		t.Fatal("retry identity reused", err)
	}
	change.RequestID = restoreID
	if _, err = repo.SetAccess(t.Context(), change); err != nil {
		t.Fatal(err)
	}
	restored, err := repo.Read(t.Context(), read)
	if err != nil || !restored.Documents[0].Allowed || restored.Epoch != after.Epoch+1 {
		t.Fatal("restore failed", err)
	}
}
