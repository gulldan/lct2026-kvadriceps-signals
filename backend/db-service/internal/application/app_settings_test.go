package application_test

import (
	"context"
	"errors"
	"testing"
	"uuid"

	"github.com/example/db-service/internal/application"
	"github.com/example/db-service/internal/domain"
)

type settingsBoundaryRepo struct {
	snapshot domain.AppSettingsSnapshot
	writes   int
}

func (r *settingsBoundaryRepo) Snapshot(context.Context) (domain.AppSettingsSnapshot, error) {
	return r.snapshot, nil
}

func (r *settingsBoundaryRepo) Apply(_ context.Context, revision int64, _ string, _ map[string]*string) (int64, error) {
	r.writes++
	return revision + 1, nil
}

func TestAppSettingsRejectDeploymentValuesBeforeWriting(t *testing.T) {
	repo := &settingsBoundaryRepo{snapshot: domain.AppSettingsSnapshot{Revision: 4, Values: map[string]string{
		"VLLM_API_KEY": "legacy-value", "VLLM_URL": "legacy-endpoint", "PUBSEARCH_MAILTO": "legacy-contact",
		"GITHUB_TOKEN": "legacy-token", "FUTURE_PRIVATE_PATH": "legacy-path", "VLLM_MODEL": "saved-model",
	}}}
	svc := application.New(nil, nil, nil, nil, nil, nil, nil, nil, repo)
	actor := uuid.NewV4().String()
	for _, key := range []string{"VLLM_API_KEY", "VLLM_URL", "PUBSEARCH_MAILTO", "GITHUB_TOKEN",
		"OPENALEX_API_KEY", "FUTURE_PRIVATE_PATH", "RAG_SETTINGS_REVISION"} {
		for _, value := range []*string{new("value"), nil} {
			_, err := svc.ApplyAppSettings(t.Context(), 4, actor, map[string]*string{
				"VLLM_MODEL": new("changed-model"), key: value,
			})
			if !errors.Is(err, domain.ErrInvalidArgument) || repo.writes != 0 {
				t.Fatalf("forbidden %s reached the repository: %v", key, err)
			}
		}
	}
	snapshot, err := svc.AppSettings(t.Context())
	if err != nil || snapshot.Revision != 4 || len(snapshot.Values) != 1 || snapshot.Values["VLLM_MODEL"] != "saved-model" {
		t.Fatalf("legacy settings were not filtered: %v", err)
	}
	if len(repo.snapshot.Values) != 6 {
		t.Fatal("reading settings changed the stored legacy values")
	}
	revision, err := svc.ApplyAppSettings(t.Context(), 4, actor, map[string]*string{
		"VLLM_MODEL": new("changed-model"), "VLLM_RPM": new("20"), "LLM_BUDGET_TOTAL": nil,
	})
	if err != nil || revision != 5 || repo.writes != 1 {
		t.Fatalf("working parameters were rejected: %v", err)
	}
}
