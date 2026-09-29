package application

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/example/llm-service/internal/domain"
	"github.com/example/llm-service/internal/platform/indexdoc"
	"github.com/example/llm-service/internal/platform/sourcedoc"
)

type sourceReadFake struct {
	epoch   int64
	states  map[string]indexdoc.State
	revoked map[string]bool
	err     error
}

func (f *sourceReadFake) ReadIndexes(_ context.Context, in indexdoc.Read) (indexdoc.States, error) {
	if f.err != nil {
		return indexdoc.States{}, f.err
	}
	out := indexdoc.States{Epoch: f.epoch}
	for _, id := range in.DocumentIDs {
		state, ok := f.states[id]
		if !ok {
			continue
		}
		state.Allowed = indexdoc.CanRead(in.ReaderID, state.OwnerID, state.Source, in.SharedCorpus, f.revoked[id])
		out.Documents = append(out.Documents, state)
	}
	return out, nil
}

func policyFixture() *sourceReadFake {
	return &sourceReadFake{epoch: 1, revoked: map[string]bool{}, states: map[string]indexdoc.State{
		"private": {DocumentID: "private", OwnerID: "alice", Source: &sourcedoc.Source{AccessScope: "owner"}},
		"public": {DocumentID: "public", OwnerID: "alice", Source: &sourcedoc.Source{AccessScope: "public"},
			Active: &indexdoc.Manifest{SnapshotID: "ready"}},
		"pending": {DocumentID: "pending", OwnerID: "alice", Source: &sourcedoc.Source{AccessScope: "public"}},
		"legacy":  {DocumentID: "legacy", OwnerID: "alice"},
	}}
}

type checkingRanker struct{ texts []string }

func (f *checkingRanker) Rank(_ context.Context, _ string, hits []domain.RetrievedChunk) ([]float64, error) {
	scores := make([]float64, len(hits))
	for i, hit := range hits {
		f.texts = append(f.texts, hit.Text)
		scores[i] = 1
	}
	return scores, nil
}

func TestSourceReadsFilterBeforeRankingAndGeneration(t *testing.T) {
	hits := []domain.RetrievedChunk{
		{ID: "private", DocumentID: "private", Text: "SECRET private paper"},
		{ID: "old", DocumentID: "public", IndexSnapshot: "old", Text: "SECRET obsolete text"},
		{ID: "unready", DocumentID: "pending", IndexSnapshot: "writing", Text: "SECRET unready text"},
		{ID: "summary", NodeType: nodeTypeRaptorSummary, Text: "SECRET mixed summary", Members: []domain.RetrievedChunk{
			{ID: "hidden", DocumentID: "private", Text: "SECRET hidden member"},
			{ID: "ready", DocumentID: "public", IndexSnapshot: "ready", Text: "Visible verified research."},
		}},
	}
	for _, shared := range []bool{true, false} {
		t.Run(map[bool]string{true: "shared", false: "isolated"}[shared], func(t *testing.T) {
			ans, ranker := &fakeAnswerer{}, &checkingRanker{}
			svc := New(fakeEmbedder{}, fakeRetriever{chunks: hits}, fakeRetriever{chunks: hits}, ranker,
				ans, fakeCache{}, nil, nil, nil, 5, shared, 0.5, true, Tuning{})
			svc.EnableSourceReads(policyFixture(), false)
			res, err := svc.Answer(t.Context(), domain.Query{OwnerID: "bob", Text: "research"})
			if err != nil {
				t.Fatal(err)
			}
			if len(res.Sources) != 1 || res.Sources[0].IndexSnapshot != "ready" || res.Sources[0].RaptorSummaryID != "" {
				t.Fatalf("unexpected citations: %+v", res.Sources)
			}
			if strings.Contains(strings.Join(ranker.texts, " ")+ans.gotContext, "SECRET") {
				t.Fatal("unauthorized or unpublished evidence reached a model")
			}
			if res.Trace.CandidatesDense != 1 || res.Trace.CandidatesLexical != 1 || res.Trace.CandidatesFused != 1 {
				t.Fatalf("trace counts include inaccessible chunks: %+v", res.Trace)
			}
		})
	}
}

type accessCache struct {
	keys  []string
	hit   bool
	onGet func()
}

func (f *accessCache) Get(_ context.Context, key string) (domain.Result, bool, error) {
	f.keys = append(f.keys, key)
	if f.onGet != nil {
		f.onGet()
	}
	return domain.Result{Answer: "cached"}, f.hit, nil
}
func (*accessCache) Set(context.Context, string, domain.Result) error { return nil }
func (*accessCache) Epoch(context.Context, string) int64              { return 1 }

func TestSourceAccessCacheIsolationAndConcurrentRevocation(t *testing.T) {
	policy, cache := policyFixture(), &accessCache{hit: true}
	svc := New(fakeEmbedder{}, fakeRetriever{}, fakeRetriever{}, fakeRanker{}, &fakeAnswerer{},
		cache, nil, nil, nil, 5, true, 0, false, Tuning{})
	svc.EnableSourceReads(policy, true)
	for _, reader := range []string{"alice", "bob", "bob"} {
		if _, err := svc.Answer(t.Context(), domain.Query{OwnerID: reader, Text: "same", AccessRevision: "forged"}); err != nil {
			t.Fatal(err)
		}
		policy.epoch++
	}
	if cache.keys[0] == cache.keys[1] || cache.keys[1] == cache.keys[2] {
		t.Fatal("access cache keys collide")
	}
	cache.onGet = func() { policy.epoch++ }
	result, err := svc.Answer(t.Context(), domain.Query{OwnerID: "bob", Text: "same"})
	if err == nil || result.Answer != "" {
		t.Fatal("cached text returned after concurrent revocation")
	}
	policy.err = errors.New("DB unavailable")
	cache.keys = nil
	if _, err = svc.Answer(t.Context(), domain.Query{OwnerID: "bob", Text: "same"}); err == nil || len(cache.keys) > 0 {
		t.Fatal("access outage fell back to cache")
	}
}

func TestSourceChunksAndReadiness(t *testing.T) {
	policy := policyFixture()
	store := &fakeChunkSource{chunks: []domain.StoredChunk{
		{ID: "old", IndexSnapshot: "old"}, {ID: "ready", IndexSnapshot: "ready"}, {ID: "legacy"},
	}}
	svc := &RAGService{chunks: store, sharedCorpus: true}
	svc.EnableSourceReads(policy, true)
	chunks, err := svc.Chunks(t.Context(), "bob", "public")
	if err != nil || len(chunks) != 1 || chunks[0].ID != "ready" || store.gotOwner != "alice" {
		t.Fatalf("active chunk read = %+v, %v", chunks, err)
	}
	for _, id := range []string{"private", "missing"} {
		if _, err = svc.Chunks(t.Context(), "bob", id); !errors.Is(err, indexdoc.ErrNotFound) {
			t.Fatal(id, err)
		}
	}
	if chunks, err = svc.Chunks(t.Context(), "bob", "pending"); err != nil || len(chunks) != 0 {
		t.Fatal(chunks, err)
	}
	policy.revoked["public"] = true
	if _, err = svc.Chunks(t.Context(), "alice", "public"); !errors.Is(err, indexdoc.ErrNotFound) {
		t.Fatal(err)
	}
	if svc.generationAllowed(indexdoc.State{Allowed: true}, "") {
		t.Fatal("unpublished legacy corpus became visible")
	}
	svc.requireSourceReady = false
	if !svc.generationAllowed(indexdoc.State{Allowed: true}, "") {
		t.Fatal("explicit migration mode excluded legacy corpus")
	}
}

type policyChangingAnswerer struct{ change func() }

func (a policyChangingAnswerer) Answer(context.Context, string, string, bool) (string, string, error) {
	a.change()
	return "Grounded answer [S1].", "test-model", nil
}

func TestConcurrentIngestionKeepsAnswerButRevocationBlocksIt(t *testing.T) {
	for _, revoke := range []bool{false, true} {
		t.Run(map[bool]string{false: "unrelated ingestion", true: "source revoked"}[revoke], func(t *testing.T) {
			policy := policyFixture()
			hits := []domain.RetrievedChunk{{ID: "ready", DocumentID: "public", IndexSnapshot: "ready", Text: "Grounded evidence."}}
			answerer := policyChangingAnswerer{change: func() { policy.epoch++; policy.revoked["public"] = revoke }}
			svc := New(fakeEmbedder{}, fakeRetriever{chunks: hits}, fakeRetriever{}, fakeRanker{score: 1}, answerer,
				fakeCache{}, nil, nil, nil, 5, true, 0.5, true, Tuning{})
			svc.EnableSourceReads(policy, true)
			res, err := svc.Answer(t.Context(), domain.Query{OwnerID: "bob", Text: "research"})
			if revoke && (err == nil || res.Answer != "") {
				t.Fatal("revoked source returned")
			}
			if !revoke && (err != nil || res.Answer == "") {
				t.Fatal("unrelated ingestion canceled answer", err)
			}
		})
	}
}
