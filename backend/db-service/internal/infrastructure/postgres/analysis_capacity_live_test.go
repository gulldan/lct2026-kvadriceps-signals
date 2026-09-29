package postgres_test

import (
	"context"
	"encoding/json/v2"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/example/db-service/internal/infrastructure/postgres"
	"github.com/example/db-service/internal/platform/analysisdoc"
	"github.com/rs/zerolog"
)

func TestAnalysisAdmissionLive(t *testing.T) {
	s := newJobLive(t)
	first := s.submit(t, 0, s.method)
	lease := s.claim(t, first.MethodSHA256)
	second := s.submit(t, 1, s.method)
	// Повтор полезен, даже когда обе ячейки очереди владельца заняты.
	if replay := s.submit(t, 0, s.method); replay.ID != first.ID {
		t.Fatal("full queue broke idempotent retry")
	}
	in := analysisdoc.Submit{RunID: first.InputRunID, ReaderID: s.requests[0].OwnerID,
		SharedCorpus: true, MethodJSON: strings.Replace(s.method, "publication-test-v1", "admission-new", 1)}
	if _, err := s.repo.Submit(t.Context(), in); !errors.Is(err, analysisdoc.ErrCapacity) {
		t.Fatal("third unfinished analysis was admitted", err)
	}
	if lease.Job.ID != first.ID {
		t.Fatal("wrong test job claimed")
	}
	if _, err := s.repo.Fail(t.Context(), analysisdoc.Failure{Lease: *lease.Lease,
		Code: "invalid_input", Retryable: false}); err != nil {
		t.Fatal(err)
	}
	var group sync.WaitGroup
	errs := make([]error, 8)
	for i := range errs {
		group.Go(func() {
			request := in
			request.MethodJSON = strings.Replace(s.method, "publication-test-v1", fmt.Sprintf("admission-%d", i), 1)
			_, errs[i] = s.repo.Submit(t.Context(), request)
		})
	}
	group.Wait()
	accepted := 0
	for _, err := range errs {
		if err == nil {
			accepted++
		} else if !errors.Is(err, analysisdoc.ErrCapacity) {
			t.Fatal("unexpected admission error", err)
		}
	}
	if accepted != 1 {
		t.Fatal("one freed queue slot must admit exactly one contender", accepted)
	}
	if replay := s.submit(t, 1, s.method); replay.ID != second.ID {
		t.Fatal("accepted job changed under pressure")
	}
	t.Log("two owner slots, idempotent replay, terminal release, eight competing submissions: PASS")
}

// Запуск по явному флагу; создаёт только неизменяемые входы для 34 заранее объявленных request ID.
func TestAnalysisHistoryCapacityLive(t *testing.T) {
	path := os.Getenv("TR_HISTORY_MANIFEST")
	if path == "" {
		t.Skip("explicit history test manifest required")
	}
	raw, err := os.ReadFile(path)
	var requests []analysisdoc.Request
	if err != nil || json.Unmarshal(raw, &requests, json.RejectUnknownMembers(true)) != nil || len(requests) != 34 {
		t.Fatal("34 predeclared test requests required")
	}
	ids := make([]string, len(requests))
	seen := map[string]bool{}
	for i, request := range requests {
		if _, err = request.Normalize(time.Now()); err != nil || request.TestRunID == "" ||
			request.TestRunID != requests[0].TestRunID || request.OwnerID != requests[0].OwnerID || seen[request.RequestID] {
			t.Fatal("invalid or mixed test namespace")
		}
		ids[i], seen[request.RequestID] = request.RequestID, true
	}
	db, err := postgres.Connect(t.Context(), os.Getenv("POSTGRES_DSN"), zerolog.Nop())
	if err != nil {
		t.Fatal("current PostgreSQL unavailable")
	}
	t.Cleanup(db.Close)
	var existing int
	if err = db.Pool.QueryRow(t.Context(), `SELECT count(*) FROM signal_analysis_inputs WHERE request_id=ANY($1)`, ids).
		Scan(&existing); err != nil || existing != 0 {
		t.Fatal("history test IDs already used")
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		_, cleanupErr := db.Pool.Exec(ctx, `DELETE FROM signal_analysis_inputs WHERE request_id=ANY($1)
AND owner_id=$2 AND manifest::jsonb->'request'->>'test_run_id'=$3`, ids, requests[0].OwnerID, requests[0].TestRunID)
		if cleanupErr != nil {
			t.Error(cleanupErr)
		}
	})
	repo := postgres.NewAnalysisRepo(db)
	var first analysisdoc.Frozen
	for i := range 30 {
		frozen, freezeErr := repo.Freeze(t.Context(), requests[i])
		if freezeErr != nil {
			t.Fatal(freezeErr)
		}
		if i == 0 {
			first = frozen
		}
	}
	var group sync.WaitGroup
	errs := make([]error, 4)
	for i := range errs {
		group.Go(func() { _, errs[i] = repo.Freeze(t.Context(), requests[30+i]) })
	}
	group.Wait()
	accepted := 0
	for _, freezeErr := range errs {
		if freezeErr == nil {
			accepted++
		} else if !errors.Is(freezeErr, analysisdoc.ErrHistoryFull) {
			t.Fatal("unexpected concurrent history error", freezeErr)
		}
	}
	if accepted != 2 {
		t.Fatal("history ceiling did not fence concurrent freezes", accepted)
	}
	replay, err := repo.Freeze(t.Context(), requests[0])
	if err != nil || replay.JSON != first.JSON || replay.RunID != first.RunID {
		t.Fatal("history ceiling changed a saved request", err)
	}
	t.Log("32 saved inputs, concurrent admission at ceiling, immutable replay and addressed cleanup: PASS")
}
