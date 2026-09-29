package scoringstore

import (
	"context"
	"time"

	"github.com/example/main-service/internal/application"
)

const weightsKeyPrefix = "rag:scoring:weights:"

// Оверрайды малы и перезаписываются при каждой правке, так что долгий TTL лишь убирает брошенных владельцев.
const weightsTTL = 365 * 24 * time.Hour

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

func (s *Store) Get(ctx context.Context, ownerID string) (*application.ScoringWeights, error) {
	w := application.DefaultWeights()
	if _, err := s.kv.GetJSON(ctx, weightsKeyPrefix+ownerID, &w); err != nil {
		return nil, err
	}
	return &w, nil
}

func (s *Store) Set(ctx context.Context, ownerID string, w application.ScoringWeights) error {
	return s.kv.SetJSON(ctx, weightsKeyPrefix+ownerID, w, weightsTTL)
}
