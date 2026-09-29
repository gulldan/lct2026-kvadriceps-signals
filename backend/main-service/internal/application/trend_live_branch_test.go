package application

import "testing"

func TestLiveBranchFollowsPrimaryCategory(t *testing.T) {
	t.Parallel()
	for category, want := range map[string]string{
		"cs.LG": liveBranchComputing, "stat.ML": liveBranchComputing, "stat.ME": liveBranchMath,
		"quant-ph": liveBranchQuantum, "cond-mat.mtrl-sci": liveBranchMaterials, "physics.chem-ph": liveBranchMaterials,
		"astro-ph.CO": liveBranchSpace, "physics.space-ph": liveBranchSpace, "q-bio.NC": liveBranchLife,
		"physics.med-ph": liveBranchLife, "physics.ao-ph": liveBranchEarth, "math.AP": liveBranchMath,
		"hep-th": liveBranchPhysics, "physics.optics": liveBranchPhysics, "eess.SP": liveBranchEngineering,
		"q-fin.PM": liveBranchEconomics, "": "",
	} {
		if got := liveBranch(category); got != want {
			t.Fatalf("%q → %q, want %q", category, got, want)
		}
	}
	metadata := map[string]LivePaperMetadata{"a": {Category: "cs.AI"}, "b": {Category: "quant-ph"}, "c": {Category: "quant-ph"}}
	papers := []StudyPaper{{ID: "a", Date: "2026-01-02"}, {ID: "b", Date: "2026-01-03"}, {ID: "c", Date: "2026-02-01"},
		{ID: "unknown", Date: "2026-02-02"}}
	if got := liveTopicBranch(papers, metadata); got != liveBranchQuantum {
		t.Fatalf("majority branch = %q", got)
	}
	sample := liveBranchSample(papers, metadata, 2)
	if len(sample) != 2 {
		t.Fatalf("sample = %d papers", len(sample))
	}
}

func TestLiveBranchSampleSkipsNonTechnologyBranches(t *testing.T) {
	t.Parallel()
	metadata := map[string]LivePaperMetadata{}
	papers := make([]StudyPaper, 0, 30)
	for _, row := range []struct {
		prefix, category string
	}{{"c", "cs.LG"}, {"m", "math.AP"}, {"u", ""}} {
		for i := range 10 {
			id := row.prefix + string(rune('0'+i))
			metadata[id] = LivePaperMetadata{Category: row.category}
			papers = append(papers, StudyPaper{ID: id, Date: "2026-0" + string(rune('1'+i%9)) + "-01"})
		}
	}
	sample := liveBranchSample(papers, metadata, 9)
	if len(sample) != 9 {
		t.Fatalf("выборка %d работ, ожидали 9", len(sample))
	}
	for _, paper := range sample {
		if branch := liveBranch(metadata[paper.ID].Category); branch != liveBranchComputing {
			t.Fatalf("в выборку попала область %q, а она не про технологии", branch)
		}
	}
}
