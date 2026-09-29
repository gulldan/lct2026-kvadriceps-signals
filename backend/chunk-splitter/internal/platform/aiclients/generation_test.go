package aiclients_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/example/chunk-splitter/internal/platform/aiclients"
)

func TestDynamicGeneratorReasoningPolicy(t *testing.T) {
	bodies := make(chan map[string]any, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		bodies <- decodeBody(t, r)
		jsonHandler(t, http.StatusOK,
			`{"choices":[{"message":{"content":"structured answer"}}]}`)(w, r)
	}))
	defer srv.Close()
	target := aiclients.GenTarget{URL: srv.URL, Model: "corporate-model"}
	g := aiclients.NewDynamicGenerator(func(context.Context) aiclients.GenTarget { return target }, srv.Client())

	// Смена одной лишь политики рассуждения обязана сбросить закэшированный генератор, а её очистка —
	// снова убрать необязательное поле.
	for _, effort := range []string{"", "none", "high", ""} {
		target.ReasoningEffort = effort
		response, err := g.Generate(t.Context(), aiclients.GenRequest{Query: "extract a goal"})
		if err != nil || response.Text != "structured answer" {
			t.Fatalf("Generate with effort %q: response=%+v, error=%v", effort, response, err)
		}
		body := <-bodies
		got, present := body["reasoning_effort"]
		if effort == "" && present || effort != "" && got != effort {
			t.Errorf("effort %q: request has reasoning_effort=%v (present=%v)", effort, got, present)
		}
	}
}

func TestQwenNonThinkingTemplatePolicy(t *testing.T) {
	bodies := make(chan map[string]any, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		bodies <- decodeBody(t, r)
		jsonHandler(t, http.StatusOK, `{"choices":[{"message":{"content":"{\"result\":null}"}}]}`)(w, r)
	}))
	defer srv.Close()
	var target aiclients.GenTarget
	g := aiclients.NewDynamicGenerator(func(context.Context) aiclients.GenTarget { return target }, srv.Client())
	for _, test := range []struct {
		model, effort string
		disable       bool
	}{
		{"corp/qwen3.8-27b-nvfp4", "none", true},
		{"Qwen/Qwen3-8B", "none", true},
		{"corp/qwen3.8-27b-nvfp4", "high", false},
		{"corp/qwen3.8-27b-nvfp4", "", false},
		{"corporate-model", "none", false},
	} {
		target = aiclients.GenTarget{URL: srv.URL, Model: test.model, ReasoningEffort: test.effort}
		if _, err := g.Generate(t.Context(), aiclients.GenRequest{Query: "select source sentences"}); err != nil {
			t.Fatal(err)
		}
		body := <-bodies
		kwargs, present := body["chat_template_kwargs"]
		if present != test.disable {
			t.Fatalf("model %s, effort %q: template override present=%v", test.model, test.effort, present)
		}
		values, ok := kwargs.(map[string]any)
		if test.disable && (!ok || values["enable_thinking"] != false) {
			t.Fatalf("model %s: thinking was not disabled", test.model)
		}
	}
}

func TestGeneratorJSONModeIsExplicit(t *testing.T) {
	bodies := make(chan map[string]any, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		bodies <- decodeBody(t, r)
		jsonHandler(t, http.StatusOK, `{"choices":[{"message":{"content":"{}"}}]}`)(w, r)
	}))
	defer srv.Close()
	g := aiclients.NewGenerator(srv.URL, "current-model", "", 0, srv.Client())
	for _, enabled := range []bool{false, true} {
		if _, err := g.Generate(t.Context(), aiclients.GenRequest{Query: "return JSON facts", JSONMode: enabled}); err != nil {
			t.Fatal(err)
		}
		body := <-bodies
		format, present := body["response_format"]
		if present != enabled {
			t.Fatalf("JSONMode=%v: response_format present=%v", enabled, present)
		}
		values, ok := format.(map[string]any)
		if enabled && (!ok || values["type"] != "json_object") {
			t.Fatal("JSON object format was not requested")
		}
	}
}

func TestQwen38ThinkingEffortReachesTemplate(t *testing.T) {
	bodies := make(chan map[string]any, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		bodies <- decodeBody(t, r)
		jsonHandler(t, http.StatusOK, `{"choices":[{"message":{"content":"answer"}}]}`)(w, r)
	}))
	defer srv.Close()
	var target aiclients.GenTarget
	g := aiclients.NewDynamicGenerator(func(context.Context) aiclients.GenTarget { return target }, srv.Client())
	for _, test := range []struct {
		model, effort string
		wantTemplate  bool
	}{
		{"corp/qwen3.8-27b-nvfp4", "low", true},
		{"Qwen/Qwen3.8-27B", "medium", true},
		{"corp/qwen3.8-27b-nvfp4", "xhigh", true},
		{"Qwen/Qwen3-8B", "low", false},
		{"corporate-model", "medium", false},
		{"corp/qwen3.8-27b-nvfp4", "", false},
	} {
		target = aiclients.GenTarget{URL: srv.URL, Model: test.model, ReasoningEffort: test.effort}
		if _, err := g.Generate(t.Context(), aiclients.GenRequest{Query: "compare source evidence"}); err != nil {
			t.Fatal(err)
		}
		body := <-bodies
		kwargs, present := body["chat_template_kwargs"]
		if present != test.wantTemplate {
			t.Fatalf("model %s, effort %q: template override present=%v", test.model, test.effort, present)
		}
		values, ok := kwargs.(map[string]any)
		if test.wantTemplate && (!ok || values["enable_thinking"] != true || values["reasoning_effort"] != test.effort) {
			t.Fatalf("model %s: template did not receive effort %q", test.model, test.effort)
		}
	}
}

func TestConfigFailureSeparatesDeploymentFromHiccup(t *testing.T) {
	misconfigured := []string{
		`generate: post https://llm.example.test/v1/chat/completions: tls: failed to verify certificate: ` +
			`x509: certificate signed by unknown authority`,
		`post https://llm.example.test/v1/chat/completions: status 403: {"error":{"code":"forbidden"}}`,
		`post https://llm.example.test/v1/chat/completions: status 401: unauthorized`,
		`post http://vllm:8000/v1/chat/completions: dial tcp: lookup vllm: no such host`,
		`post https://gw/v1/chat/completions: status 404: the model was not found`,
	}
	for _, text := range misconfigured {
		if aiclients.ConfigFailure(errors.New(text)) == "" {
			t.Errorf("a deployment fault was reported as transient: %s", text)
		}
	}
	transient := []string{
		"generation: empty choices",
		"generation: empty answer after sanitize",
		`post https://gw/v1/chat/completions: status 503: upstream overloaded`,
		"context deadline exceeded",
	}
	for _, text := range transient {
		if reason := aiclients.ConfigFailure(errors.New(text)); reason != "" {
			t.Errorf("a transient failure was blamed on configuration (%s): %s", reason, text)
		}
	}
	if aiclients.ConfigFailure(nil) != "" {
		t.Error("a nil error was reported as a configuration failure")
	}
}
