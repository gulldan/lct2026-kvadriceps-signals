package analysisdoc

import (
	"slices"
	"testing"
)

func TestNormalizeTerms(t *testing.T) {
	t.Parallel()
	got := NormalizeTerms([]string{" Large  Language Models ", "Artificial Intelligence Technologies",
		"artificial intelligence", "нейросети", "x",
		"drop%like", "graph rag", "edge ai", "robotics", "vision", "speech", "agents", "planning", "ninth"})
	want := []string{"large language models", "artificial intelligence", "graph rag", "edge ai", "robotics",
		"vision", "speech", "agents"}
	if !slices.Equal(got, want) {
		t.Fatalf("NormalizeTerms = %q, want %q", got, want)
	}
	if out := NormalizeTerms(nil); len(out) != 0 {
		t.Fatalf("nil terms = %q", out)
	}
}
