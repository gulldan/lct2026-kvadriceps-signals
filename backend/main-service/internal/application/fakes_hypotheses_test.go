package application

import (
	"context"
	"sync"

	commonv1 "github.com/example/main-service/internal/platform/genproto/common/v1"
)

type answer struct {
	resp *commonv1.RagResponse
	err  error
}

// scriptedAnswerer отдаёт ответы по очереди, а исчерпав очередь, повторяет последний.
type scriptedAnswerer struct {
	mu       sync.Mutex
	replies  []answer
	calls    int
	requests []*commonv1.RagRequest
}

func newAnswerer(replies ...answer) *scriptedAnswerer {
	return &scriptedAnswerer{replies: replies}
}

func reply(text string, sources ...*commonv1.Source) answer {
	return answer{resp: &commonv1.RagResponse{Answer: text, Sources: sources, Model: "fake-model"}, err: nil}
}

func failReply(err error) answer { return answer{resp: nil, err: err} }

func (s *scriptedAnswerer) Answer(_ context.Context, req *commonv1.RagRequest) (*commonv1.RagResponse, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.requests = append(s.requests, req)
	idx := s.calls
	s.calls++
	if len(s.replies) == 0 {
		return &commonv1.RagResponse{Answer: "", Model: "fake-model"}, nil
	}
	if idx >= len(s.replies) {
		idx = len(s.replies) - 1
	}
	a := s.replies[idx]
	return a.resp, a.err
}

func (s *scriptedAnswerer) callCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.calls
}

type fakeWeights struct {
	w      map[string]*ScoringWeights
	getErr error
	setErr error
}

func newWeights() *fakeWeights { return &fakeWeights{w: map[string]*ScoringWeights{}} }

func (f *fakeWeights) Get(_ context.Context, ownerID string) (*ScoringWeights, error) {
	if f.getErr != nil {
		return nil, f.getErr
	}
	return f.w[ownerID], nil
}

func (f *fakeWeights) Set(_ context.Context, ownerID string, w ScoringWeights) error {
	if f.setErr != nil {
		return f.setErr
	}
	f.w[ownerID] = &w
	return nil
}

type fakeRuntimeSettings struct {
	settings map[string]*HypothesisRuntimeSettings
	getErr   error
	setErr   error
}

func newRuntimeSettings() *fakeRuntimeSettings {
	return &fakeRuntimeSettings{settings: map[string]*HypothesisRuntimeSettings{}}
}

func (f *fakeRuntimeSettings) Get(_ context.Context, ownerID string) (*HypothesisRuntimeSettings, error) {
	if f.getErr != nil {
		return nil, f.getErr
	}
	return f.settings[ownerID], nil
}

func (f *fakeRuntimeSettings) Set(_ context.Context, ownerID string, settings HypothesisRuntimeSettings) error {
	if f.setErr != nil {
		return f.setErr
	}
	f.settings[ownerID] = &settings
	return nil
}

type fakeGraph struct {
	edges    map[string][]KGEdge
	addErr   error
	edgesErr error
}

func newGraph() *fakeGraph { return &fakeGraph{edges: map[string][]KGEdge{}} }

func (f *fakeGraph) AddEdges(_ context.Context, ownerID string, edges []KGEdge) error {
	if f.addErr != nil {
		return f.addErr
	}
	f.edges[ownerID] = append(f.edges[ownerID], edges...)
	return nil
}

func (f *fakeGraph) Edges(_ context.Context, ownerID string) ([]KGEdge, error) {
	if f.edgesErr != nil {
		return nil, f.edgesErr
	}
	return f.edges[ownerID], nil
}
