package postgres_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json/v2"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
	"uuid"

	"github.com/example/db-service/internal/infrastructure/postgres"
	"github.com/example/db-service/internal/platform/harvest"
	"github.com/example/db-service/internal/platform/sourcedoc"
	"github.com/rs/zerolog"
)

// Пишет только шесть заранее объявленных ID и один охват; сообщений не публикует.
func TestHarvestLive(t *testing.T) {
	file := os.Getenv("TR_HARVEST_MANIFEST")
	if file == "" {
		t.Skip("TR_HARVEST_MANIFEST required for bounded DB verification")
	}
	raw, scenarioErr := os.ReadFile(file)
	if scenarioErr != nil {
		t.Fatal(scenarioErr)
	}
	var m struct {
		RunID string   `json:"test_run_id"`
		IDs   []string `json:"ids"`
	}
	if err := json.Unmarshal(raw, &m, json.RejectUnknownMembers(true)); err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(m.RunID, "tr-test-") || len(m.IDs) != 6 {
		t.Fatal("invalid test namespace or ID bound")
	}
	if _, err := uuid.Parse(strings.TrimPrefix(m.RunID, "tr-test-")); err != nil {
		t.Fatal(err)
	}
	seen := map[string]bool{}
	for _, id := range m.IDs {
		if _, err := uuid.Parse(id); err != nil || seen[id] {
			t.Fatal("invalid manifest UUID")
		}
		seen[id] = true
	}
	dsn := os.Getenv("DB_TEST_DSN")
	if dsn == "" {
		dsn = os.Getenv("POSTGRES_DSN")
	}
	if dsn == "" {
		t.Fatal("existing PostgreSQL DSN required")
	}
	db, scenarioErr := postgres.Connect(t.Context(), dsn, zerolog.Nop())
	if scenarioErr != nil {
		t.Fatal("connect to existing PostgreSQL failed")
	}
	t.Cleanup(db.Close)
	in := harvest.Start{
		OwnerID:   m.RunID,
		Provider:  "crossref",
		TestRunID: m.RunID,
		Limit:     3,
		Scope: harvest.Scope{
			Query:    m.RunID,
			From:     "2023-01-01",
			Through:  "2026-08-31",
			PageSize: 1,
			WorkType: "journal-article",
		},
		RunID: uuid.NewV4().String(),
		Token: uuid.NewV4().String(),
	}
	var existing int
	if err := db.Pool.QueryRow(t.Context(), `SELECT count(*) FROM documents WHERE id=ANY($1)`, m.IDs).Scan(
		&existing,
	); err != nil || existing != 0 {
		t.Fatal("manifest IDs must be unused")
	}
	t.Cleanup(
		func() {
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			tx, err := db.Pool.Begin(ctx)
			if err != nil {
				t.Error(err)
				return
			}
			defer func() { _ = tx.Rollback(ctx) }()
			queries := []struct {
				sql  string
				args []any
			}{
				{
					`
DELETE FROM documents WHERE id=ANY($1) AND owner_id=$2 AND source_metadata->>'test_run_id'=$2 AND
status='uploaded' AND chunk_count=0
`,
					[]any{m.IDs, m.RunID},
				},
				{`DELETE FROM source_collection_pages WHERE scope_id=$1`, []any{in.ScopeID()}},
				{
					`DELETE FROM source_collection_scopes WHERE id=$1 AND test_run_id=$2`,
					[]any{in.ScopeID(), m.RunID},
				},
				{
					`
UPDATE source_collection_limits SET leased_until='-infinity', lease_token='', scope_id='',
next_request_at=greatest(clock_timestamp()+interval '1 second', next_request_at) WHERE
provider='crossref' AND scope_id=$1
`,
					[]any{in.ScopeID()},
				},
			}
			for _, q := range queries {
				if _, err := tx.Exec(ctx, q.sql, q.args...); err != nil {
					t.Error("manifest cleanup failed", err)
					return
				}
			}
			if err := tx.Commit(ctx); err != nil {
				t.Error(err)
			}
		},
	)
	repo := postgres.NewHarvestRepo(db)
	job, scenarioErr := repo.Start(t.Context(), in)
	if scenarioErr != nil {
		t.Fatal(scenarioErr)
	}
	again, scenarioErr := repo.Start(t.Context(), in)
	if scenarioErr != nil || again.Lease != job.Lease {
		t.Fatal("Start retry was not idempotent", scenarioErr)
	}
	other := in
	other.Token = uuid.NewV4().String()
	other.RunID = uuid.NewV4().String()
	if _, err := repo.Start(t.Context(), other); !errors.Is(err, harvest.ErrConflict) {
		t.Fatal("concurrent scheduler was admitted", err)
	}
	other.Scope.Query += " another scope"
	if _, err := repo.Start(t.Context(), other); !errors.Is(err, harvest.ErrConflict) {
		t.Fatal("second scope bypassed provider lease", err)
	}
	first := harvestPage(job, 0, m.IDs[0], 0)
	job, scenarioErr = repo.CommitPage(t.Context(), first)
	if scenarioErr != nil {
		t.Fatal(scenarioErr)
	}
	if retry, err := repo.CommitPage(t.Context(), first); err != nil || retry.Received != 1 {
		t.Fatal("commit retry advanced twice", err)
	}
	second := harvestPage(job, 1, m.IDs[1], 1)
	job, scenarioErr = repo.CommitPage(t.Context(), second)
	if scenarioErr != nil {
		t.Fatal(scenarioErr)
	}
	old := job.Lease
	if _, err := db.Pool.Exec(
		t.Context(),
		`
UPDATE source_collection_limits SET leased_until=clock_timestamp()-interval '1 second' WHERE scope_id=$1
AND lease_token=$2
`,
		old.ScopeID,
		old.Token,
	); err != nil {
		t.Fatal(err)
	}
	in.RunID, in.Token = uuid.NewV4().String(), uuid.NewV4().String()
	job, scenarioErr = repo.Start(t.Context(), in)
	if scenarioErr != nil || job.Cursor != "2" {
		t.Fatal("restart lost second-page checkpoint", scenarioErr)
	}
	if _, err := repo.CommitPage(t.Context(), second); !errors.Is(err, harvest.ErrConflict) {
		t.Fatal("stale lease modified checkpoint", err)
	}
	third := harvestPage(job, 0, m.IDs[2], 2)
	job, scenarioErr = repo.CommitPage(t.Context(), third)
	if scenarioErr != nil {
		t.Fatal(scenarioErr)
	}
	if _, err := repo.Finish(
		t.Context(), harvest.Finish{Lease: job.Lease, Status: "complete"},
	); !errors.Is(err, harvest.ErrConflict) {
		t.Fatal("pending publish was called complete", err)
	}
	pending, scenarioErr := repo.Pending(t.Context(), harvest.Pending{Lease: job.Lease, Limit: 10})
	if scenarioErr != nil || len(pending.Records) != 3 {
		t.Fatal("commit/publish failure lost pending records", scenarioErr)
	}
	for _, record := range pending.Records {
		ack := harvest.Ack{Lease: job.Lease, DocumentID: record.DocumentID}
		if _, err := repo.Ack(t.Context(), ack); err != nil {
			t.Fatal(err)
		}
		if _, err := repo.Ack(t.Context(), ack); err != nil {
			t.Fatal("Ack retry failed", err)
		}
	}
	if _, err := repo.Finish(t.Context(), harvest.Finish{Lease: job.Lease, Status: "complete"}); err != nil {
		t.Fatal(err)
	}
	// Второе окно полного перекрытия сохраняет свои наблюдения, но переиспользует документы.
	in.RunID, in.Token = uuid.NewV4().String(), uuid.NewV4().String()
	job, scenarioErr = repo.Start(t.Context(), in)
	if scenarioErr != nil || job.Cursor != "" {
		t.Fatal("completed sweep did not restart overlap", scenarioErr)
	}
	for i := range 3 {
		job, scenarioErr = repo.CommitPage(t.Context(), harvestPage(job, i, m.IDs[i+3], i))
		if scenarioErr != nil {
			t.Fatal(scenarioErr)
		}
	}
	if err := db.Pool.QueryRow(t.Context(), `SELECT count(*) FROM documents WHERE id=ANY($1)`, m.IDs).Scan(
		&existing,
	); err != nil || existing != 3 {
		t.Fatal("overlap duplicated documents", existing, err)
	}
	if _, err := repo.Finish(t.Context(), harvest.Finish{Lease: job.Lease, Status: "complete"}); err != nil {
		t.Fatal(err)
	}
	// Конкурирующие старты планировщика сериализуются даже после освобождения аренды.
	var wg sync.WaitGroup
	wins := make(chan harvest.Job, 2)
	for range 2 {
		wg.Go(
			func() {
				attempt := in
				attempt.RunID, attempt.Token = uuid.NewV4().String(), uuid.NewV4().String()
				j, err := repo.Start(t.Context(), attempt)
				if err == nil {
					wins <- j
				} else if !errors.Is(err, harvest.ErrConflict) {
					t.Error(err)
				}
			},
		)
	}
	wg.Wait()
	close(wins)
	if len(wins) != 1 {
		t.Fatal("provider admitted multiple live schedulers")
	}
	job = <-wins
	// Резерв частоты запросов в БД и пауза после ответа переживают потерю процесса.
	grant, scenarioErr := repo.Quota(t.Context(), harvest.Quota{Lease: job.Lease, Reserve: true})
	if scenarioErr != nil {
		t.Fatal(scenarioErr)
	}
	if grant.Allowed {
		next, err := repo.Quota(t.Context(), harvest.Quota{Lease: job.Lease, Reserve: true})
		if err != nil || next.Allowed {
			t.Fatal("rate slot reserved twice", err)
		}
	}
	cooldown := time.Now().Add(3 * time.Second)
	if _, err := repo.Quota(t.Context(), harvest.Quota{Lease: job.Lease, NotBefore: &cooldown}); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.Finish(t.Context(), harvest.Finish{Lease: job.Lease, Status: "failed", Error: "fixture 429"}); err != nil {
		t.Fatal(err)
	}
	in.RunID, in.Token = uuid.NewV4().String(), uuid.NewV4().String()
	job, scenarioErr = repo.Start(t.Context(), in)
	if scenarioErr != nil {
		t.Fatal(scenarioErr)
	}
	grant, scenarioErr = repo.Quota(t.Context(), harvest.Quota{Lease: job.Lease, Reserve: true})
	if scenarioErr != nil || grant.Allowed || grant.NotBefore.Before(cooldown.Add(-time.Millisecond)) {
		t.Fatal("restart forgot provider cooldown", scenarioErr)
	}
	job, scenarioErr = repo.CommitPage(t.Context(), harvestPage(job, 0, m.IDs[3], 0))
	if scenarioErr != nil {
		t.Fatal(scenarioErr)
	}
	finish := harvest.Finish{Lease: job.Lease, Status: "failed", ResetCursor: true, Error: "fixture expired cursor"}
	if _, err := repo.Finish(t.Context(), finish); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.Finish(t.Context(), finish); err != nil {
		t.Fatal("Finish retry failed", err)
	}
	in.RunID, in.Token = uuid.NewV4().String(), uuid.NewV4().String()
	job, scenarioErr = repo.Start(t.Context(), in)
	if scenarioErr != nil || job.Cursor != "" || job.Exhausted {
		t.Fatal("expired cursor did not restart complete overlap", scenarioErr)
	}
	t.Log(
		"three pages; restart after page two; fenced schedulers; pending dispatch; overlap count=3; persisted cooldown",
	)
}

func harvestPage(job harvest.Job, page int, id string, record int) harvest.CommitPage {
	text := fmt.Sprintf("Recorded DB fixture %d", record)
	hash := func(s string) string { h := sha256.Sum256([]byte(s)); return hex.EncodeToString(h[:]) }
	s := sourcedoc.Source{
		Version:       1,
		Kind:          "paper",
		Provider:      "crossref",
		NativeID:      fmt.Sprintf("10.5555/%s-%d", job.TestRunID, record),
		CanonicalID:   fmt.Sprintf("fixture:%s:%d", job.TestRunID, record),
		URL:           "https://example.org/fixture",
		ObservedAt:    time.Now().UTC(),
		Revision:      "fixture-v1",
		ContentHash:   hash(text),
		HashAlgorithm: "sha256",
		AccessScope:   "public",
		TestRunID:     job.TestRunID,
	}
	next := strconv.Itoa(record + 1)
	if record == 2 {
		next = ""
	}
	prefix := "sources/crossref/" + job.Lease.RunID + "/"
	return harvest.CommitPage{
		Lease:        job.Lease,
		PageNumber:   page,
		Cursor:       job.Cursor,
		Next:         next,
		Exhausted:    record == 2,
		Received:     1,
		RawKey:       prefix + strconv.Itoa(record) + ".raw",
		RawHash:      hash("raw " + text),
		ManifestKey:  prefix + id + ".json",
		ManifestHash: hash(id),
		Records: []harvest.Prepared{
			{
				DocumentID: id,
				Source:     s,
				Title:      "DB fixture",
				ObjectKey:  "sources/crossref/" + job.TestRunID + "/" + id,
				Size:       int64(len(text)),
				BLAKE3:     hash("BLAKE3 placeholder for metadata-only DB test"),
				MIME:       "text/plain",
				Text:       text,
				TextStatus: "title_and_abstract",
			},
		},
	}
}
