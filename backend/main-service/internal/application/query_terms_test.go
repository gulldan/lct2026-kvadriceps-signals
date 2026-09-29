package application

import (
	"context"
	"testing"

	"github.com/example/main-service/internal/platform/aiclients"
)

type fakeGenerator struct{ text string }

func (f fakeGenerator) Generate(context.Context, aiclients.GenRequest) (aiclients.GenResponse, error) {
	return aiclients.GenResponse{Text: f.text}, nil
}

func (f fakeGenerator) Stream(
	ctx context.Context, req aiclients.GenRequest, _ func(string) error,
) (aiclients.GenResponse, error) {
	return f.Generate(ctx, req)
}

func TestNewQueryTranslator(t *testing.T) {
	t.Parallel()
	on := func(context.Context) bool { return true }
	terms, err := NewQueryTranslator(fakeGenerator{`Sure: ["Large Language Models", "machine learning"]`}, on, 8)(
		t.Context(), "технологии в ИИ")
	if err != nil || len(terms) != 2 || terms[0] != "large language models" {
		t.Fatalf("terms = %q, err = %v", terms, err)
	}
	if _, err = NewQueryTranslator(fakeGenerator{"no json here"}, on, 8)(t.Context(), "x"); err == nil {
		t.Fatal("unusable answer accepted")
	}
	off := func(context.Context) bool { return false }
	if _, err = NewQueryTranslator(fakeGenerator{"[]"}, off, 8)(t.Context(), "x"); err == nil {
		t.Fatal("unconfigured gateway accepted")
	}
}

func TestNewTopicNamer(t *testing.T) {
	t.Parallel()
	on := func(context.Context) bool { return true }
	label, err := NewTopicNamer(fakeGenerator{"«Квантовая коррекция ошибок».\n"}, on)(t.Context(), []string{"a", "b"})
	if err != nil || label != "Квантовая коррекция ошибок" {
		t.Fatalf("label = %q, err = %v", label, err)
	}
	if _, err = NewTopicNamer(fakeGenerator{"Quantum error correction"}, on)(t.Context(), []string{"a"}); err == nil {
		t.Fatal("latin name accepted")
	}
}
