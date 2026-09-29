package application

import (
	"context"
	"encoding/json/v2"
	"testing"

	"github.com/example/main-service/internal/platform/aiclients"
)

func TestOptionalCanonicalNameDoesNotBypassOrBlockMechanismReview(t *testing.T) {
	const query = "обнаружение синтетической речи"
	documents := []LiveQualificationDocument{
		{ID: "direct", Text: "The Phase Coherence Detector finds synthetic speech by comparing adjacent frequency bins."},
		{ID: "survey", Text: "A survey calls this family Survey Alias Detector and compares several methods."},
	}
	for _, test := range []struct {
		name, canonical, beforeReview, afterReview string
		survey, refusedMechanism, refusedRelevance bool
	}{
		{name: "literal name is retained", canonical: "Phase Coherence Detector",
			beforeReview: "Phase Coherence Detector", afterReview: "Phase Coherence Detector"},
		{name: "invented optional name is discarded", canonical: "Enhanced Phase Coherence Detector"},
		{name: "ambiguous optional name is discarded", canonical: "PCD"},
		{name: "removed source cannot supply the name", canonical: "Survey Alias Detector",
			survey: true, beforeReview: "Survey Alias Detector"},
		{name: "missing name does not override mechanism refusal", canonical: "Invented Detector", refusedMechanism: true},
		{name: "missing name does not override relevance refusal", canonical: "Invented Detector", refusedRelevance: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			evidence := []liveQualificationSelection{{Source: 1, From: 1, Through: 1, Criteria: []string{"b1"}}}
			identity := qualificationIdentityFixture("direct", "introduces", true)
			identity.Mechanism.Supported = new(!test.refusedMechanism)
			identity.Relevance.Supported = new(!test.refusedRelevance)
			identities := []liveQualificationIdentity{identity}
			if test.survey {
				evidence = append(evidence, liveQualificationSelection{Source: 2, From: 1, Through: 1, Criteria: []string{"b1"}})
				identities = append(identities, qualificationIdentityFixture("survey", "review", true))
			}
			proposal, err := json.Marshal(map[string]any{
				"canonical_name": test.canonical, "label": "Фазовое обнаружение синтетической речи",
				"mechanism":  "Сравнение фаз соседних частотных интервалов для обнаружения синтетической речи",
				"technology": true, "coherent": true, "reason": "Прямой источник описывает механизм обнаружения",
				"evidence": evidence,
			})
			if err != nil {
				t.Fatal(err)
			}
			review, err := json.Marshal(map[string]any{
				"technology": true, "reason": "Механизм и применимость проверены по выбранным источникам", "sources": identities,
			})
			if err != nil {
				t.Fatal(err)
			}
			generator := &qualificationGenerator{replies: []aiclients.GenResponse{
				{Text: string(proposal), Model: "reader"}, {Text: string(review), Model: "reviewer"},
			}}
			qualify := NewCandidateQualifier(generator, func(context.Context) bool { return true })
			got, err := qualify(liveFixtureScopeContext(t.Context(), query), query, "phase coherence", documents)
			if err != nil || len(generator.requests) != 2 {
				t.Fatalf("необязательное имя отменило независимую проверку: запросов %d, ошибка %v", len(generator.requests), err)
			}
			var input struct {
				Proposal LiveQualification `json:"proposal"`
			}
			if err := json.Unmarshal([]byte(generator.requests[1].Query), &input); err != nil {
				t.Fatal(err)
			}
			if input.Proposal.CanonicalName != test.beforeReview || got.CanonicalName != test.afterReview {
				t.Fatalf("неподтверждённое имя сохранено или буквальное потеряно: вход %q, итог %q",
					input.Proposal.CanonicalName, got.CanonicalName)
			}
			accepted := !test.refusedMechanism && !test.refusedRelevance
			if got.Verified != accepted || accepted && (len(got.Evidence) != 1 || got.Evidence[0].SourceID != "direct" ||
				got.Evidence[0].Quote != documents[0].Text || !liveGroundedQualificationEvidence(got.Evidence[0])) {
				t.Fatalf("проверка механизма, области или прямых свидетельств потеряна: %+v", got)
			}
		})
	}
}

func TestLiveClassifierCanonicalName(t *testing.T) {
	const sourceID = "arxiv:2512.12689"
	const canonical = "Fidelity-based Quantum Autoencoder"
	const originalID = "extract:fidelity-driven-quantum-autoencoder-fid-qae"
	documents := []StudyPaper{{ID: sourceID, Title: "Quantum Autoencoders for Fraud Detection"},
		{ID: "other", Title: canonical}}
	metadata := map[string]LivePaperMetadata{sourceID: {
		Abstract: "We propose the Fidelity-based Quantum Autoencoder (FiD-QAE) for credit card fraud detection.",
	}}
	for _, test := range []struct {
		name          string
		canonical     string
		source        string
		verified      bool
		invalid       bool
		wantCanonical bool
	}{
		{name: "source name replaces concatenated name and acronym", canonical: canonical, source: sourceID, verified: true,
			wantCanonical: true},
		{name: "invented expansion", canonical: "Fidelity-enhanced Quantum Autoencoder", source: sourceID,
			verified: true, invalid: true},
		{name: "unselected source", canonical: canonical, source: "absent", verified: true, invalid: true},
		{name: "unreviewed name", canonical: canonical, source: sourceID},
		{name: "partial word", canonical: "Fidelity-based Quantum Autoencod", source: sourceID,
			verified: true, invalid: true},
		{name: "ambiguous single word", canonical: "Fidelity", source: sourceID, verified: true, invalid: true},
		{name: "no explicit name", source: sourceID, verified: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			qualification := LiveQualification{CanonicalName: test.canonical, Verified: test.verified,
				Evidence: []LiveQualificationEvidence{{SourceID: test.source}}}
			inputs := make([]LiveQualificationDocument, 0, len(documents))
			for _, document := range documents {
				inputs = append(inputs, LiveQualificationDocument{ID: document.ID,
					Text: liveWorkLine(document, metadata[document.ID])})
			}
			if err := liveValidateQualificationName(qualification, inputs); (err != nil) != test.invalid {
				t.Fatalf("неверная проверка исходного имени: %v", err)
			}
			candidate := LiveTopic{StudyTopic: StudyTopic{ID: originalID, PaperIDs: []string{sourceID}},
				Name: "Fidelity-Driven Quantum Autoencoder (FiD-QAE)", Qualification: &qualification}
			input := liveClassifierInput("банковский антифрод", "test", []LiveTopic{candidate}, documents, metadata)
			want := ""
			if test.wantCanonical {
				want = "fidelity based quantum autoencoder"
			}
			if got := input.Candidates[0]; got.phrase != want || got.ID != originalID {
				t.Fatalf("фраза потеряна, взята из чужого источника или изменила идентичность: %+v", got)
			}
		})
	}
}
