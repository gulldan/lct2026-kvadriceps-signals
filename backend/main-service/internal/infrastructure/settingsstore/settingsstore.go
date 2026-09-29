package settingsstore

import (
	"context"
	"time"

	"github.com/example/main-service/internal/application"
)

const settingsKeyPrefix = "rag:hypothesis:runtime_settings:"
const settingsTTL = 365 * 24 * time.Hour

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

func (s *Store) Get(ctx context.Context, ownerID string) (*application.HypothesisRuntimeSettings, error) {
	settings := application.DefaultHypothesisRuntimeSettings()
	if _, err := s.kv.GetJSON(ctx, settingsKeyPrefix+ownerID, &settings); err != nil {
		return nil, err
	}
	settings = application.NormalizeHypothesisRuntimeSettings(settings)
	return &settings, nil
}

func (s *Store) Set(ctx context.Context, ownerID string, settings application.HypothesisRuntimeSettings) error {
	return s.kv.SetJSON(ctx, settingsKeyPrefix+ownerID, application.NormalizeHypothesisRuntimeSettings(settings), settingsTTL)
}
