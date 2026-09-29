package application

import (
	"context"
	"errors"
	"testing"
	"testing/synctest"

	commonv1 "github.com/example/main-service/internal/platform/genproto/common/v1"
	dbv1 "github.com/example/main-service/internal/platform/genproto/db/v1"
)

type gatedCreateCatalog struct {
	*fakeCatalog
	release  map[string]chan struct{}
	failures map[string]error
}

func (c *gatedCreateCatalog) CreateHypothesis(
	ctx context.Context, req *dbv1.CreateHypothesisRequest,
) (*commonv1.Hypothesis, error) {
	title := req.GetHypothesis().GetTitle()
	select {
	case <-c.release[title]:
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	if err := c.failures[title]; err != nil {
		return nil, err
	}
	return c.fakeCatalog.CreateHypothesis(ctx, req)
}

func TestPersistGeneratedResults(t *testing.T) {
	firstErr := errors.New("first item failed")
	lastErr := errors.New("last item failed")
	for _, tc := range []struct {
		name     string
		failures map[string]error
		wantErr  error
	}{
		{name: "reverse completion keeps input order"},
		{
			name:     "mixed failures wait for all writes",
			failures: map[string]error{"first": firstErr, "third": lastErr}, wantErr: firstErr,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				cat := &gatedCreateCatalog{
					fakeCatalog: newCatalog(),
					release:     make(map[string]chan struct{}),
					failures:    tc.failures,
				}
				graph := newGraph()
				svc := NewHypothesisService(cat, nil, nil, nil, nil, graph, nil)
				items := []genItem{
					{Title: "first", Statement: "first statement", TargetProperty: "first property"},
					{Title: "second", Statement: "second statement", TargetProperty: "second property"},
					{Title: "third", Statement: "third statement", TargetProperty: "third property"},
				}
				judged := make(map[int]scoreSet)
				for i, item := range items {
					cat.release[item.Title] = make(chan struct{})
					judged[i] = scoreSet{Confidence: new(float64(i+1) / 10)}
				}
				var got []*commonv1.Hypothesis
				var err error
				returned := false
				go func() {
					got, err = svc.persistGenerated(t.Context(), "alice", &commonv1.KPI{Id: "kpi1"},
						genOutcome{items: items}, judged, nil, false, "", nil, nil)
					returned = true
				}()
				synctest.Wait()
				for i := len(items) - 1; i >= 0; i-- {
					close(cat.release[items[i].Title])
					synctest.Wait()
					if i > 0 && returned {
						t.Errorf("returned before item %q completed", items[i-1].Title)
					}
				}
				if !returned {
					t.Fatal("did not return after every catalog call completed")
				}
				if !errors.Is(err, tc.wantErr) {
					t.Fatalf("error = %v, want first error by input index %v", err, tc.wantErr)
				}
				if tc.wantErr != nil {
					if got != nil || len(graph.edges["alice"]) != 0 {
						t.Fatalf("partial failure returned hypotheses or wrote graph edges: %v, %v", got, graph.edges)
					}
					if len(cat.hypotheses) != 1 || cat.hypotheses["hyp-second"] == nil {
						t.Fatalf("successful catalog write was not completed: %v", cat.hypotheses)
					}
					return
				}
				if len(got) != len(items) || len(graph.edges["alice"]) != len(items) {
					t.Fatalf("got %d hypotheses and %d edges, want %d of each", len(got), len(graph.edges["alice"]), len(items))
				}
				for i, item := range items {
					if got[i].GetTitle() != item.Title || got[i].GetConfidenceScore() != *judged[i].Confidence {
						t.Errorf("result %d lost input/score association: %v", i, got[i])
					}
					edge := graph.edges["alice"][i]
					if edge.HypothesisID != got[i].GetId() || edge.FromName != item.TargetProperty {
						t.Errorf("graph edge %d lost hypothesis provenance: %+v", i, edge)
					}
				}
			})
		})
	}
}
