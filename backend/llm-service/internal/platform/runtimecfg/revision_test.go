package runtimecfg_test

import (
	"testing"

	"github.com/alicebob/miniredis/v2"
	"github.com/example/llm-service/internal/platform/runtimecfg"
	"github.com/example/llm-service/internal/platform/valkey"
)

func TestRuntimePublicationCannotRegress(t *testing.T) {
	r := miniredis.RunT(t)
	kv, err := valkey.New(t.Context(), r.Addr(), "", 0)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = kv.Close() })
	for _, values := range []map[string]string{
		{"RAG_SETTINGS_REVISION": "2", "VLLM_MODEL": "new"},
		{"RAG_SETTINGS_REVISION": "1", "VLLM_MODEL": "old"},
	} {
		if err := runtimecfg.Publish(t.Context(), kv, values); err != nil {
			t.Fatal(err)
		}
	}
	o := runtimecfg.New(kv)
	if o.Get(t.Context(), "VLLM_MODEL", "") != "new" || o.Get(t.Context(), "RAG_SETTINGS_REVISION", "") != "2" {
		t.Fatal("stale publisher replaced settings")
	}
}

func TestDeploymentSettingsStayInEnvironment(t *testing.T) {
	r := miniredis.RunT(t)
	kv, err := valkey.New(t.Context(), r.Addr(), "", 0)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = kv.Close() })
	values := map[string]string{
		"RAG_SETTINGS_REVISION": "3", "VLLM_MODEL": "saved-model",
		"VLLM_REASONING_EFFORT": "high", "VLLM_RPM": "12", "LLM_PROVIDERS": "model=local:Local::free",
		"LLM_BUDGET_TOTAL": "20", "LLM_PRICE_PROMPT_PER_1M": "0.5", "LLM_PRICE_COMPLETION_PER_1M": "1.5",
		"EMBEDDINGS_MODEL": "embedding-model", "EMBEDDING_DIM": "768", "RERANKER_MODEL": "reranker-model",
		"RAG_PIPELINE_REVISION": "pipeline-v2", "PUBSEARCH_ENABLED": "false", "RAG_GEN_QUALITY_FLOOR": "0.7",
	}
	privateKeys := []string{"VLLM_URL", "VLLM_API_KEY", "PUBSEARCH_MAILTO", "GITHUB_TOKEN",
		"OPENALEX_API_KEY", "EMBEDDINGS_URL", "RERANKER_URL", "FUTURE_PRIVATE_PATH"}
	for _, key := range privateKeys {
		t.Setenv(key, "environment-value")
		values[key] = "legacy-stored-value"
	}
	if err := kv.SetJSON(t.Context(), runtimecfg.ValkeyKey, values, 0); err != nil {
		t.Fatal(err)
	}
	reader := runtimecfg.New(kv)
	if reader.Get(t.Context(), "VLLM_MODEL", "") != "saved-model" {
		t.Fatal("model override was lost")
	}
	for _, key := range privateKeys {
		if _, ok := reader.Lookup(t.Context(), key); ok || reader.Get(t.Context(), key, "") != "environment-value" {
			t.Errorf("stored deployment value was accepted for %s", key)
		}
	}
	if err := runtimecfg.Publish(t.Context(), kv, values); err != nil {
		t.Fatal(err)
	}
	var published map[string]string
	if ok, err := kv.GetJSON(t.Context(), runtimecfg.ValkeyKey, &published); err != nil || !ok {
		t.Fatalf("read published settings: %v", err)
	}
	for _, key := range privateKeys {
		if _, ok := published[key]; ok || values[key] != "legacy-stored-value" {
			t.Errorf("publication exposed %s or changed the caller's snapshot", key)
		}
		delete(values, key)
	}
	for key, want := range values {
		if published[key] != want {
			t.Errorf("publication lost runtime setting %s", key)
		}
	}
}
