package kgstore

import (
	"context"
	"time"

	"github.com/example/main-service/internal/application"
)

const graphKeyPrefix = "rag:kg:"

// Граф перезаписывается при каждой генерации, так что долгий TTL лишь убирает брошенных владельцев.
const graphTTL = 365 * 24 * time.Hour

// maxEdges держит единое JSON-значение в разумном размере; старые рёбра вытесняются первыми.
const maxEdges = 5000

type JSONStore interface {
	SetJSON(ctx context.Context, key string, v any, ttl time.Duration) error
	GetJSON(ctx context.Context, key string, dest any) (bool, error)
}

type Store struct {
	kv JSONStore
}

func New(kv JSONStore) *Store {
	return &Store{kv: kv}
}

func (s *Store) Edges(ctx context.Context, ownerID string) ([]application.KGEdge, error) {
	var edges []application.KGEdge
	if _, err := s.kv.GetJSON(ctx, graphKeyPrefix+ownerID, &edges); err != nil {
		return nil, err
	}
	if edges == nil {
		edges = []application.KGEdge{}
	}
	return edges, nil
}

func (s *Store) AddEdges(ctx context.Context, ownerID string, edges []application.KGEdge) error {
	if len(edges) == 0 {
		return nil
	}
	existing, err := s.Edges(ctx, ownerID)
	if err != nil {
		return err
	}
	seen := make(map[application.KGEdge]struct{}, len(existing)+len(edges))
	merged := make([]application.KGEdge, 0, len(existing)+len(edges))
	for _, e := range existing {
		if _, dup := seen[e]; dup {
			continue
		}
		seen[e] = struct{}{}
		merged = append(merged, e)
	}
	for _, e := range edges {
		if _, dup := seen[e]; dup {
			continue
		}
		seen[e] = struct{}{}
		merged = append(merged, e)
	}
	if len(merged) > maxEdges {
		merged = merged[len(merged)-maxEdges:]
	}
	return s.kv.SetJSON(ctx, graphKeyPrefix+ownerID, merged, graphTTL)
}
