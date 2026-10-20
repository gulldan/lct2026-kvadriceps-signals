package application

import (
	"encoding/json/v2"
	"testing"
)

func TestLiveCuratedBranchRepairsSavedViewsWithoutRescoring(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		id, name, source, branch, want string
		curated                        bool
	}{
		{"mrna-therapeutics", "mRNA therapeutics and delivery", "curated", liveBranchComputing, liveBranchLife, true},
		{"vertical-farming", "Controlled-environment agriculture", "curated", liveBranchComputing, liveBranchLife, true},
		{"mrna-therapeutics", "Automatic topic", liveSourceDiscovered, liveBranchComputing, liveBranchComputing, false},
		{"vertical-farming", "Automatic topic", "curated", liveBranchComputing, liveBranchComputing, false},
		{"mrna-therapeutics", "Discovered topic", liveSourceDiscovered, liveBranchComputing, liveBranchComputing, true},
		{liveGraphRAG, "GraphRAG", "curated", liveBranchComputing, liveBranchComputing, true},
		{"unknown-family", "Unknown", "curated", "", "", true},
	} {
		t.Run(tc.id+"/"+tc.source+"/"+tc.name, func(t *testing.T) {
			t.Parallel()
			score := &LiveModelScore{Value: 0.42, Threshold: 0.75, Version: "saved-model", Label: liveClassifierNoise}
			topic := LiveTopic{ID: tc.id, Name: tc.name, Source: tc.source, Curated: tc.curated,
				Branch: tc.branch, Stage: studyStageUnassessed, ModelScore: score}
			snapshot := LiveSnapshot{Version: liveVersion, Method: liveMethod,
				ObservedAt: "2026-10-17T00:00:00Z", Topics: []LiveTopic{topic}}
			raw, err := json.Marshal(snapshot)
			if err != nil {
				t.Fatal(err)
			}
			svc := NewTrendLiveService(nil, nil)
			svc.loadSnapshot(raw)
			answer := liveDirectionAnswer{Contract: "saved-contract", Candidates: []LiveTopic{
				{ID: "inherited", Source: "candidate", Branch: liveBranchComputing, ModelScore: score},
				{ID: "independent", Source: liveSourceDiscovered, Branch: liveBranchMaterials, ModelScore: score},
			}}
			svc.directions[tc.id] = answer
			overview := svc.Overview()
			detail, ok := svc.Topic(tc.id)
			if !ok || len(overview.Topics) != 1 || len(detail.Candidates) != 2 {
				t.Fatalf("saved view is missing: found=%v, topics=%d, candidates=%d", ok,
					len(overview.Topics), len(detail.Candidates))
			}
			for _, got := range []LiveTopic{overview.Topics[0], detail.Topic} {
				if got.Branch != tc.want || got.Stage != topic.Stage || got.ModelScore == nil ||
					got.ModelScore.Value != score.Value || got.ModelScore.Threshold != score.Threshold ||
					got.ModelScore.Version != score.Version || got.ModelScore.Label != score.Label {
					t.Fatalf("saved assessment changed or wrong branch: %+v", got)
				}
			}
			wantInherited := liveBranchComputing
			if tc.want == liveBranchLife {
				wantInherited = liveBranchLife
			}
			if detail.Candidates[0].Branch != wantInherited || detail.Candidates[1].Branch != liveBranchMaterials ||
				detail.Candidates[0].ModelScore.Value != score.Value ||
				overview.ObservedAt != snapshot.ObservedAt || detail.ObservedAt != snapshot.ObservedAt ||
				svc.snapshot.Topics[0].Branch != tc.branch || answer.Candidates[0].Branch != liveBranchComputing {
				t.Fatal("read changed saved data, independent discovery, observation date or candidate score")
			}
		})
	}
}

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
