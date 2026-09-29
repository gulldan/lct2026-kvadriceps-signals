package application

import (
	"math/rand/v2"
	"testing"
)

func TestLiveSemanticGroupRetainsNewMethodsAfterFormerSeedCeiling(t *testing.T) {
	const seeds = 520
	vectors := make([][]float64, seeds+1)
	for i := range seeds {
		vectors[i] = make([]float64, seeds)
		vectors[i][i] = 1
	}
	vectors[seeds] = vectors[seeds-1]
	groups, noise, err := liveSemanticGroup(t.Context(), vectors)
	if err != nil || noise != 0 || len(groups) != seeds {
		t.Fatalf("later methods dropped: groups=%d noise=%d error=%v", len(groups), noise, err)
	}
	if len(groups[seeds-1].rows) != 2 || groups[seeds-1].rows[1] != seeds {
		t.Fatal("later work did not join its new method")
	}
	if _, _, err := liveSemanticGroup(t.Context(), make([][]float64, liveSemanticDiscoveryLimit+1)); err == nil {
		t.Fatal("unbounded grouping accepted")
	}
}

func TestLiveSemanticBudgetRevisionKeepsVerifiedPreviousIdentity(t *testing.T) {
	previous := []LiveDiscovery{{ID: "semantic:old-v2", Method: "dated-semantic-leader-v2", Name: "Primary operation",
		PaperIDs: []string{"a", "b"},
		History:  []LiveLineageEvent{{Kind: "created", At: "2026-08-01", Name: "Primary operation"}}}}
	current := []LiveDiscovery{{ID: "semantic:new-v3-seed", Method: liveSemanticDiscoveryMethod, Name: "Primary operation",
		PaperIDs: []string{"a", "b", "later"}}}
	liveDiscoveryLineage(current, previous, "2026-09-11", "snapshot-observed-date-v1")
	if current[0].ID != previous[0].ID || len(current[0].History) != 1 || current[0].Method != "dated-semantic-leader-v3" {
		t.Fatalf("budget correction reset an established method identity: %+v", current[0])
	}
}

func BenchmarkLiveSemanticGroupFullInput(b *testing.B) {
	// Независимые случайные векторы дают максимум различных зёрен — худший случай перебора.
	random := rand.New(rand.NewPCG(20260911, 4096))
	vectors := make([][]float64, liveSemanticDiscoveryLimit)
	for i := range vectors {
		vector := make([]float64, 1024)
		for j := range vector {
			vector[j] = random.NormFloat64()
		}
		vectors[i], _ = liveUnitVector(vector)
	}
	b.ResetTimer()
	for b.Loop() {
		groups, noise, err := liveSemanticGroup(b.Context(), vectors)
		if err != nil || noise != 0 || len(groups) != len(vectors) {
			b.Fatal("bounded input was not preserved")
		}
	}
}
