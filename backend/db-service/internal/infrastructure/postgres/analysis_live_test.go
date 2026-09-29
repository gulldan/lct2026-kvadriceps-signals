package postgres_test

import (
	"context"
	"encoding/json/v2"
	"errors"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/example/db-service/internal/infrastructure/postgres"
	"github.com/example/db-service/internal/platform/analysisdoc"
	"github.com/example/db-service/internal/platform/indexdoc"
	"github.com/rs/zerolog"
)

// Создаёт анализы только для заранее объявленных request ID; реальные строки не трогает.
func TestAnalysisInputLive(t *testing.T) {
	path := os.Getenv("TR_ANALYSIS_MANIFEST")
	if path == "" {
		t.Skip("explicit test request manifest required")
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var requests []analysisdoc.Request
	if err = json.Unmarshal(raw, &requests, json.RejectUnknownMembers(true)); err != nil || len(requests) != 2 {
		t.Fatal("two bounded test requests required", err)
	}
	for _, request := range requests {
		if _, err = request.Normalize(time.Now()); err != nil || request.TestRunID == "" {
			t.Fatal("invalid analysis test request", err)
		}
	}
	db, err := postgres.Connect(t.Context(), os.Getenv("POSTGRES_DSN"), zerolog.Nop())
	if err != nil {
		t.Fatal("existing database unavailable")
	}
	t.Cleanup(db.Close)
	var existing int
	ids := []string{requests[0].RequestID, requests[1].RequestID}
	if err = db.Pool.QueryRow(t.Context(), `SELECT count(*) FROM signal_analysis_inputs WHERE request_id=ANY($1)`,
		ids).Scan(&existing); err != nil || existing != 0 {
		t.Fatal("test request IDs must be unused", err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		for _, request := range requests {
			_, cleanupErr := db.Pool.Exec(ctx, `DELETE FROM signal_analysis_inputs WHERE owner_id=$1 AND request_id=$2
AND manifest::jsonb->'request'->>'test_run_id'=$3`, request.OwnerID, request.RequestID, request.TestRunID)
			if cleanupErr != nil {
				t.Error(cleanupErr)
			}
		}
	})
	repo := postgres.NewAnalysisRepo(db)
	first := analysisRace(t, repo, requests[0])
	input, err := analysisdoc.Decode[analysisdoc.Input](first.JSON)
	if err != nil || len(input.Records) == 0 || input.Model == nil {
		t.Fatal("live input lacks ready real source snapshots", err)
	}
	t.Logf("frozen real sources=%d excluded=%d pages=%d hash=%s", len(input.Records), len(input.Excluded),
		len(input.Pages), first.SHA256)
	changed := requests[0]
	changed.Query += " different"
	if _, err = repo.Freeze(t.Context(), changed); !errors.Is(err, analysisdoc.ErrConflict) {
		t.Fatal("same key accepted different query", err)
	}
	read := analysisdoc.Read{RunID: first.RunID, ReaderID: requests[1].OwnerID, SharedCorpus: true}
	if _, err = repo.Read(t.Context(), read); !errors.Is(err, analysisdoc.ErrNotFound) {
		t.Fatal("foreign reader accessed frozen counts", err)
	}
	read.ReaderID = requests[0].OwnerID
	analysisRevokedMembership(t, db, repo, read, requests[1].RequestID, first)
	second, err := repo.Freeze(t.Context(), requests[1])
	if err != nil || second.RunID == first.RunID || second.SHA256 == first.SHA256 {
		t.Fatal("different scope did not create a different input", err)
	}
}

func analysisRace(t *testing.T, repo *postgres.AnalysisRepo, request analysisdoc.Request) analysisdoc.Frozen {
	t.Helper()
	var group sync.WaitGroup
	out := make([]analysisdoc.Frozen, 3)
	errs := make([]error, 3)
	for i := range 3 {
		group.Go(func() { out[i], errs[i] = repo.Freeze(t.Context(), request) })
	}
	group.Wait()
	for i, got := range out {
		if errs[i] != nil || got != out[0] || indexdoc.Hash([]byte(got.JSON)) != got.SHA256 {
			t.Fatal("concurrent retry did not preserve one complete input", errs)
		}
	}
	return out[0]
}

func analysisRevokedMembership(t *testing.T, db *postgres.DB, repo *postgres.AnalysisRepo,
	read analysisdoc.Read, missingID string, original analysisdoc.Frozen,
) {
	t.Helper()
	// Пропавший документ блокирует старую проекцию; меняются только строки этого прогона.
	_, err := db.Pool.Exec(t.Context(), `INSERT INTO signal_analysis_sources(run_id,document_id,snapshot_id)
VALUES($1,$2,'')`, read.RunID, missingID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = repo.Read(t.Context(), read); !errors.Is(err, analysisdoc.ErrNotFound) {
		t.Fatal("missing source did not hide aggregate view", err)
	}
	_, err = db.Pool.Exec(t.Context(), `DELETE FROM signal_analysis_sources WHERE run_id=$1 AND document_id=$2`,
		read.RunID, missingID)
	if err != nil {
		t.Fatal(err)
	}
	restored, err := repo.Read(t.Context(), read)
	if err != nil || restored != original {
		t.Fatal("restoration silently replaced the immutable input", err)
	}
}
