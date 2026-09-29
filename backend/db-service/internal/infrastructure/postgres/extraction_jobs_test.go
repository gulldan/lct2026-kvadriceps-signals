package postgres_test

import (
	"strings"
	"testing"
	"uuid"

	"github.com/example/db-service/internal/infrastructure/postgres"
	"github.com/example/db-service/internal/platform/extractdoc"
	"github.com/example/db-service/internal/platform/sourcedoc"
)

const (
	parserV    = "pdf-parser/pdf-tika4-v1"
	extractorV = "technology-extractor-v1"
	schemaV    = "technology-extraction-v1"
)

// freshRevisionHash обходит задания, оставленные в тестовой базе прошлыми прогонами.
func freshRevisionHash(t *testing.T) string {
	t.Helper()
	return extractdoc.RevisionHash(sourcedoc.Source{Provider: "arxiv", NativeID: uuid.NewV4().String(),
		Revision: "v1", ContentHash: strings.Repeat("a", 64)}, strings.Repeat("b", 64))
}

func extractionKey(revisionHash, extractor string) extractdoc.Key {
	return extractdoc.Key{RevisionHash: revisionHash, ParserVersion: parserV,
		ExtractorVersion: extractor, SchemaVersion: schemaV}
}

func submitExtraction(t *testing.T, repo *postgres.ExtractionRepo, revision, owner string,
	key extractdoc.Key) extractdoc.Job {
	t.Helper()
	job, err := repo.Submit(t.Context(), extractdoc.Job{
		Key: key, SourceRevisionID: revision, OwnerID: owner,
		Input: extractdoc.Artifact{Key: extractdoc.ArtifactKey(key.ID(), "input", strings.Repeat("1", 64)),
			SHA256: strings.Repeat("1", 64)},
	})
	if err != nil {
		t.Fatalf("submit: %v", err)
	}
	return job
}

func outputArtifact(jobID string) extractdoc.Artifact {
	hash := strings.Repeat("2", 64)
	return extractdoc.Artifact{Key: extractdoc.ArtifactKey(jobID, "output", hash), SHA256: hash}
}

func TestExtractionJobIsPaidForOnce(t *testing.T) {
	db := newTestDB(t)
	repo := postgres.NewExtractionRepo(db)
	ctx := t.Context()
	owner := uuid.NewV4().String()
	revision := seedDoc(t, db, owner)
	hash := freshRevisionHash(t)
	key := extractionKey(hash, extractorV)

	first := submitExtraction(t, repo, revision, owner, key)
	if first.State != extractdoc.Queued || first.ID != key.ID() {
		t.Fatalf("a new job did not start queued under its own key: %+v", first)
	}
	if again := submitExtraction(t, repo, revision, owner, key); again.ID != first.ID ||
		again.CreatedAt != first.CreatedAt {
		t.Fatalf("the same key created a second job: %+v", again)
	}

	job, lease, err := repo.Claim(ctx, extractorV, schemaV, uuid.NewV4().String())
	if err != nil || job.ID != first.ID || job.State != extractdoc.Running || job.Attempt != 1 {
		t.Fatalf("claim: %+v %v", job, err)
	}
	done, err := repo.Finish(ctx, extractdoc.Result{Lease: lease, State: extractdoc.Completed,
		Output: outputArtifact(job.ID)})
	if err != nil || done.State != extractdoc.Completed || !done.Output.Present() {
		t.Fatalf("finish: %+v %v", done, err)
	}

	// Повторный прогон читает ответ, а не платит ещё раз.
	repeated := submitExtraction(t, repo, revision, owner, key)
	if repeated.State != extractdoc.Completed || repeated.Output != done.Output {
		t.Fatalf("a finished job was reopened by a repeated submit: %+v", repeated)
	}
	if empty, _, claimErr := repo.Claim(ctx, extractorV, schemaV, uuid.NewV4().String()); claimErr != nil ||
		empty.ID != "" {
		t.Fatalf("a finished job was handed out again: %+v %v", empty, claimErr)
	}

	// Другой извлекатель — другой вопрос к тому же материалу.
	other := submitExtraction(t, repo, revision, owner, extractionKey(hash, "technology-extractor-v2"))
	if other.ID == first.ID || other.State != extractdoc.Queued {
		t.Fatalf("a changed extractor version did not create a new job: %+v", other)
	}
}

// Ключ строится из материала, а не из момента, когда его увидели.
func TestObservingAgainDoesNotStartExtraction(t *testing.T) {
	source := sourcedoc.Source{Provider: "web", NativeID: "https://lab.example/a", Revision: "r1",
		ContentHash: strings.Repeat("c", 64)}
	before := extractdoc.RevisionHash(source, strings.Repeat("d", 64))
	source.ObservedAt = source.ObservedAt.AddDate(0, 0, 1)
	source.AvailableBasis = "first successful source observation"
	if after := extractdoc.RevisionHash(source, strings.Repeat("d", 64)); after != before {
		t.Fatal("re-observing an unchanged document changed its extraction key")
	}
	source.Revision = "r2"
	if after := extractdoc.RevisionHash(source, strings.Repeat("d", 64)); after == before {
		t.Fatal("a new revision reused the previous extraction key")
	}
	source.Revision = "r1"
	if after := extractdoc.RevisionHash(source, strings.Repeat("e", 64)); after == before {
		t.Fatal("a reparse of the same bytes reused the previous extraction key")
	}
}

func TestLostLeaseCannotPublish(t *testing.T) {
	db := newTestDB(t)
	repo := postgres.NewExtractionRepo(db)
	ctx := t.Context()
	owner := uuid.NewV4().String()
	revision := seedDoc(t, db, owner)
	key := extractionKey(freshRevisionHash(t), extractorV)
	submitExtraction(t, repo, revision, owner, key)

	job, lease, err := repo.Claim(ctx, extractorV, schemaV, uuid.NewV4().String())
	if err != nil {
		t.Fatal(err)
	}
	if _, err = repo.Renew(ctx, lease); err != nil {
		t.Fatalf("the holder could not renew its own lease: %v", err)
	}
	stale := lease
	stale.Token = uuid.NewV4().String()
	if _, err = repo.Renew(ctx, stale); err == nil {
		t.Fatal("a second worker renewed a lease it does not hold")
	}
	foreign := lease
	foreign.OwnerID = uuid.NewV4().String()
	if _, err = repo.Renew(ctx, foreign); err == nil {
		t.Fatal("the lease was renewed without its owner")
	}
	if _, err = repo.Finish(ctx, extractdoc.Result{Lease: stale, State: extractdoc.Completed,
		Output: outputArtifact(job.ID)}); err == nil {
		t.Fatal("a worker that lost its lease published a result")
	}

	cancelled, err := repo.Cancel(ctx, job.ID, owner, "operator stopped the run")
	if err != nil || cancelled.State != extractdoc.Cancelled || cancelled.Output.Present() {
		t.Fatalf("cancel: %+v %v", cancelled, err)
	}
	if _, err = repo.Finish(ctx, extractdoc.Result{Lease: lease, State: extractdoc.Completed,
		Output: outputArtifact(job.ID)}); err == nil {
		t.Fatal("a cancelled job was published as complete")
	}
	if _, err = repo.Cancel(ctx, job.ID, owner, "again"); err == nil {
		t.Fatal("a terminal job was cancelled twice")
	}
}

func TestAnswerNamesItsArtifact(t *testing.T) {
	db := newTestDB(t)
	repo := postgres.NewExtractionRepo(db)
	ctx := t.Context()
	owner := uuid.NewV4().String()
	revision := seedDoc(t, db, owner)
	key := extractionKey(freshRevisionHash(t), extractorV)
	submitExtraction(t, repo, revision, owner, key)
	job, lease, err := repo.Claim(ctx, extractorV, schemaV, uuid.NewV4().String())
	if err != nil {
		t.Fatal(err)
	}
	for name, result := range map[string]extractdoc.Result{
		"complete without an output":  {Lease: lease, State: extractdoc.Completed},
		"partial without an output":   {Lease: lease, State: extractdoc.Partial},
		"failed without a reason":     {Lease: lease, State: extractdoc.Failed},
		"still running":               {Lease: lease, State: extractdoc.Running},
		"cancelled through finish":    {Lease: lease, State: extractdoc.Cancelled},
		"output key without its hash": {Lease: lease, State: extractdoc.Completed, Output: extractdoc.Artifact{Key: "x"}},
	} {
		if _, err = repo.Finish(ctx, result); err == nil {
			t.Fatalf("a result that cannot be read back was published: %s", name)
		}
	}
	partial, err := repo.Finish(ctx, extractdoc.Result{Lease: lease, State: extractdoc.Partial,
		ReasonCode: "budget_exhausted", Output: outputArtifact(job.ID)})
	if err != nil || partial.State != extractdoc.Partial || partial.ReasonCode != "budget_exhausted" {
		t.Fatalf("a bounded answer was refused: %+v %v", partial, err)
	}
}

// Язык — часть происхождения и не решает готовность.
func TestPendingPostsOnlyAfterTheTextIsReady(t *testing.T) {
	db := newTestDB(t)
	repo := postgres.NewExtractionRepo(db)
	ctx := t.Context()
	owner := uuid.NewV4().String()
	noIndex := seedDoc(t, db, owner)

	pending, err := repo.Pending(ctx, extractorV, schemaV, 50)
	if err != nil {
		t.Fatal(err)
	}
	for _, candidate := range pending {
		if candidate.DocumentID == noIndex {
			t.Fatal("a revision whose text is not ready was queued for extraction")
		}
	}
	if _, err = repo.Pending(ctx, extractorV, schemaV, 0); err == nil {
		t.Fatal("an unbounded page of candidates was accepted")
	}
	if _, err = repo.Pending(ctx, "", schemaV, 10); err == nil {
		t.Fatal("candidates were listed without naming the extractor")
	}
}
