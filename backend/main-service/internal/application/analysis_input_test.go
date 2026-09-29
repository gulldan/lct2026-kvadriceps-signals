package application

import (
	"context"
	"encoding/json/v2"
	"errors"
	"slices"
	"testing"
	"uuid"

	"github.com/example/main-service/internal/platform/analysisdoc"
	"github.com/example/main-service/internal/platform/indexdoc"
)

type frozenAnalysisFake struct {
	AnalysisJobs
	frozen        analysisdoc.Frozen
	freezeRequest analysisdoc.Request
	readErr       error
	job           analysisdoc.Job
	jobRead       analysisdoc.JobRead
	jobReadErr    error
}

func (f *frozenAnalysisFake) FreezeAnalysis(_ context.Context, in analysisdoc.Request) (analysisdoc.Frozen, error) {
	f.freezeRequest = in
	return f.frozen, nil
}

func (f *frozenAnalysisFake) ReadAnalysis(context.Context, analysisdoc.Read) (analysisdoc.Frozen, error) {
	return f.frozen, f.readErr
}

func (f *frozenAnalysisFake) SubmitAnalysis(context.Context, analysisdoc.Submit) (analysisdoc.Job, error) {
	return analysisdoc.Job{ID: f.job.ID, Status: f.job.Status}, nil
}

func (f *frozenAnalysisFake) ReadAnalysisJob(_ context.Context, in analysisdoc.JobRead) (analysisdoc.Job, error) {
	f.jobRead = in
	return f.job, f.jobReadErr
}

func TestFreezeAnalysisVerifiesDescriptorAndRechecksRights(t *testing.T) {
	frozen := analysisdoc.Frozen{RunID: "71c3d28e-1a3b-41ca-9bc2-03c39f3c3bed", ReaderID: "reader",
		JSON: `{"version":"analysis-input-v2"}`}
	request := analysisdoc.Request{OwnerID: frozen.ReaderID}
	frozen.SHA256 = indexdoc.Hash([]byte(frozen.JSON))
	frozen.Artifact = analysisdoc.ArtifactKey(frozen.RunID, frozen.SHA256)
	frozen.Bytes = len(frozen.JSON)
	descriptor := frozen
	descriptor.JSON = ""
	db := &frozenAnalysisFake{frozen: descriptor}
	store := newObjectStore()
	if _, err := FreezeAnalysis(t.Context(), db, store, request); err == nil {
		t.Fatal("missing object accepted")
	}
	store.objects[frozen.Artifact] = []byte(frozen.JSON)
	for range 2 {
		got, err := FreezeAnalysis(t.Context(), db, store, request)
		if err != nil || got != frozen || string(store.objects[frozen.Artifact]) != frozen.JSON {
			t.Fatal("descriptor did not recover exact frozen input", err)
		}
	}
	db.readErr = analysisdoc.ErrNotFound
	if _, err := FreezeAnalysis(t.Context(), db, store, request); !errors.Is(err, analysisdoc.ErrNotFound) {
		t.Fatal("rights revoked during S3 access must hide the result", err)
	}
	db.frozen.SHA256 = indexdoc.Hash([]byte("tampered"))
	if _, err := FreezeAnalysis(t.Context(), db, nil, request); err == nil {
		t.Fatal("tampered database bytes reached storage")
	}
}

func TestSubmitRetryReturnsFrozenRequest(t *testing.T) {
	original := analysisdoc.Request{OwnerID: uuid.NewV4().String(), RequestID: uuid.NewV4().String(),
		Query: "поисковое расширение", Start: "2022-01-01", Cutoff: "2026-09-17T00:00:00Z",
		Mode: analysisdoc.ModeAsOf, AnalysisKind: analysisdoc.KindBriefV1, SharedCorpus: true,
		SearchTerms: []string{"query expansion"}}
	raw, err := json.Marshal(analysisdoc.Input{Version: analysisdoc.Version, Request: original})
	if err != nil {
		t.Fatal(err)
	}
	frozen := analysisdoc.Frozen{RunID: uuid.NewV4().String(), SHA256: indexdoc.Hash(raw),
		Bytes: len(raw), ReaderID: original.OwnerID, SharedCorpus: original.SharedCorpus}
	frozen.Artifact = analysisdoc.ArtifactKey(frozen.RunID, frozen.SHA256)
	db := &frozenAnalysisFake{frozen: frozen,
		job: analysisdoc.Job{ID: uuid.NewV4().String(), Status: analysisdoc.Prepared, Request: &original}}
	store := newObjectStore()
	store.objects[frozen.Artifact] = raw
	service, err := NewAnalysisService(db, store, analysisdoc.DevelopmentMethod)
	if err != nil {
		t.Fatal(err)
	}
	terms := []string{"query rewriting"}
	service.SetTranslator(func(context.Context, string) ([]string, error) { return terms, nil })
	retry := original
	retry.SearchTerms = nil
	got, err := service.Submit(t.Context(), retry)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(db.freezeRequest.SearchTerms, terms) {
		t.Fatal("retry did not regenerate search terms before freezing")
	}
	if got.Request == nil || !slices.Equal(got.Request.SearchTerms, original.SearchTerms) {
		t.Fatal("retry returned regenerated terms instead of the frozen request")
	}
	if db.jobRead != (analysisdoc.JobRead{JobID: db.job.ID, ReaderID: original.OwnerID, SharedCorpus: true}) {
		t.Fatal("saved request was not read in the original reader scope")
	}
	db.jobReadErr = analysisdoc.ErrAccessChanged
	if _, err = service.Submit(t.Context(), retry); !errors.Is(err, analysisdoc.ErrAccessChanged) {
		t.Fatalf("retry must propagate the saved job's access error: %v", err)
	}
}
