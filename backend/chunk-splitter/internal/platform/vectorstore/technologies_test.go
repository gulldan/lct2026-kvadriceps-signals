package vectorstore_test

import (
	"slices"
	"strings"
	"testing"

	"github.com/example/chunk-splitter/internal/platform/vectorstore"
)

func TestTechnologyNamespaceCarriesItsModel(t *testing.T) {
	seen := map[string]bool{}
	for _, model := range []string{"a-b", "a/b", "a_b", "A-B"} {
		name := vectorstore.TechnologyNamespace(model, "r1")
		if seen[name] {
			t.Fatal("distinct model names share a vector namespace")
		}
		seen[name] = true
	}
	first := vectorstore.TechnologyNamespace("F2LLM-v2", "weights@r1")
	second := vectorstore.TechnologyNamespace("F2LLM-v2", "weights@r2")
	third := vectorstore.TechnologyNamespace("other-model", "weights@r1")
	if first == second || first == third {
		t.Fatalf("a changed model or revision reused a namespace: %s %s %s", first, second, third)
	}
	if !strings.HasPrefix(first, "technologies-") {
		t.Fatalf("the technology namespace is not separated from documents: %s", first)
	}
	for _, name := range []string{first, second, third} {
		if strings.ContainsAny(name, " @/.") {
			t.Fatalf("namespace %q is not a safe collection name", name)
		}
	}
}

func TestTechnologyAccessFilterIsAnOr(t *testing.T) {
	reader := "6f1c1f3a-0d5a-4a6a-9a4a-2a7c9c2f1b01"
	filter := vectorstore.TechnologyFilter{Reader: reader, Languages: []string{"ru"}}.SearchFilter()
	if len(filter.EitherOf["access_scope"]) != 1 || filter.EitherOf["access_scope"][0] != "public" {
		t.Fatalf("a private corpus admitted more than public technologies: %+v", filter.EitherOf)
	}
	if !slices.Contains(filter.EitherOf["owner_id"], reader) {
		t.Fatal("a reader cannot find their own technology")
	}
	if !slices.Contains(filter.AnyOf["language"], "ru") {
		t.Fatal("a narrowing filter was dropped")
	}
	// Сужающие фильтры должны оставаться сужающими: язык в EitherOf расширил бы запрос до всех языков.
	if _, widened := filter.EitherOf["language"]; widened {
		t.Fatal("a narrowing filter was turned into a widening one")
	}
	shared := vectorstore.TechnologyFilter{Reader: reader, SharedCorpus: true}.SearchFilter()
	if !slices.Contains(shared.EitherOf["access_scope"], "shared") {
		t.Fatal("a shared corpus did not admit shared technologies")
	}
	anonymous := vectorstore.TechnologyFilter{}.SearchFilter()
	if _, ok := anonymous.EitherOf["owner_id"]; ok {
		t.Fatal("a query with no reader claimed an owner")
	}
}
