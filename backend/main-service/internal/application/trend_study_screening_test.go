package application

import (
	"slices"
	"testing"
)

func TestStudyScreeningPinsEveryDecisionAndRecomputesMembership(t *testing.T) {
	corpus, err := studyCatalog()
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]int{"graph": 33, "adaptive": 14, "multimodal": 24, "agentic": 11, "memory": 5}
	for _, topic := range studyTopics(corpus.Papers, true, nil) {
		if topic.Screening != "abstracts" || !topic.Reviewed || len(topic.PaperIDs) != want[topic.ID] {
			t.Fatalf("unreviewed or incorrect membership: %+v", topic)
		}
		var count int
		for _, period := range topic.Series {
			count += period.Count
		}
		if count != len(topic.PaperIDs) {
			t.Fatal("screening was not applied before measurements")
		}
		t.Logf("%s: included=%d prior=%d/%d recent=%d/%d groups=%d stage=%s momentum=%s",
			topic.ID, len(topic.PaperIDs), topic.Growth.Prior.Count, topic.Growth.Prior.Population,
			topic.Growth.Recent.Count, topic.Growth.Recent.Population, topic.Groups, topic.Stage, topic.Momentum)
	}
	decisions := studyDecisions()
	for _, family := range studyFamilies() {
		for _, paper := range corpus.Papers {
			if !family.pattern.MatchString(paper.Title) {
				continue
			}
			decision := decisions[family.id][paper.ID]
			if decision.ContentHash != paper.ContentHash || decision.Reason == "" {
				t.Fatal("missing primary review", family.id, paper.ID)
			}
		}
	}
	// «vision» внутри «supervision» давало два ложных совпадения мультимодальности.
	multimodal := studyTopics(corpus.Papers, true, nil)[2]
	if slices.Contains(multimodal.PaperIDs, "2025.emnlp-industry.99") ||
		slices.Contains(multimodal.PaperIDs, "2025.findings-emnlp.817") {
		t.Fatal("lexical homograph survived primary abstract screening")
	}
	changed := slices.Clone(corpus.Papers)
	for i := range changed {
		if changed[i].ID == "2025.findings-emnlp.817" {
			changed[i].ContentHash = "new-unreviewed-source"
		}
	}
	if studyTopics(changed, true, nil)[2].Screening != "none" {
		t.Fatal("changed primary source inherited an old reviewed status")
	}
}
