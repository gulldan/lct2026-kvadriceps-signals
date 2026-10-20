package application

import (
	"context"
	"encoding/json/v2"
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/example/main-service/internal/platform/aiclients"
)

type qualificationGenerator struct {
	replies  []aiclients.GenResponse
	requests []aiclients.GenRequest
}

func (g *qualificationGenerator) Generate(_ context.Context, request aiclients.GenRequest) (aiclients.GenResponse, error) {
	g.requests = append(g.requests, request)
	index := len(g.requests) - 1
	if index >= len(g.replies) {
		return aiclients.GenResponse{}, fmt.Errorf("unexpected call %d", index)
	}
	return g.replies[index], nil
}

func (g *qualificationGenerator) Stream(ctx context.Context, request aiclients.GenRequest,
	_ func(string) error,
) (aiclients.GenResponse, error) {
	return g.Generate(ctx, request)
}

func qualifyFixtureCandidates(t *testing.T, service *TrendLiveService, query string, umbrella *LiveTopic,
	candidates []LiveTopic, documents []StudyPaper, periods []LivePeriod,
) ([]LiveTopic, []LiveRejection) {
	t.Helper()
	pipeline := &liveCandidatePipeline{updates: make(chan liveCandidateUpdate, len(candidates))}
	pipeline.qualify(liveFixtureScopeContext(t.Context(), query), service, query, candidates, documents, nil)
	ready := make([]liveQualificationResult, 0, len(candidates))
	for range candidates {
		ready = append(ready, *(<-pipeline.updates).qualification)
	}
	pipeline.work.Wait()
	return liveApplyQualifications(umbrella, ready, periods, nil)
}

func TestCandidateQualificationRebuildsOnlyGroundedEvidence(t *testing.T) {
	periods := livePeriods(time.Date(2026, 9, 17, 12, 0, 0, 0, time.UTC))
	for i := range periods {
		periods[i].Population = 10000
	}
	documents := make([]StudyPaper, 0, 5)
	evidence := make([]LiveQualificationEvidence, 0, 3)
	for i := range 3 {
		id, title := fmt.Sprintf("detector-%d", i), fmt.Sprintf("Detecting synthetic speech with phase coherence, experiment %d", i)
		documents = append(documents, StudyPaper{ID: id, Title: title, Date: periods[len(periods)-4+i].Start,
			Authors: []string{fmt.Sprintf("Independent Lab %d", i)}})
		evidence = append(evidence, LiveQualificationEvidence{SourceID: id, Quote: title})
	}
	documents = append(documents,
		StudyPaper{ID: "clinical", Title: "Speech data for neurological diagnosis", Date: periods[0].Start},
		StudyPaper{ID: "rec:news", Title: "Speech data collection in hospitals", Date: periods[1].Start})
	proposal := LiveQualification{Label: "Фазовое обнаружение синтезированной речи", Technology: true, Relevant: true,
		Mechanism: "Проверка согласованности фазовых переходов речевого сигнала для обнаружения синтеза",
		Coherent:  true, Verified: true, Reason: "Три работы описывают один механизм обнаружения", Evidence: evidence}
	encoded, err := json.Marshal(map[string]any{"label": proposal.Label, "technology": true, "relevant": true,
		"coherent": true, "verified": true, "mechanism": proposal.Mechanism, "reason": proposal.Reason,
		"evidence": []liveQualificationSelection{
			{Source: 3, From: 1, Through: 1, Criteria: []string{"b1"}}, {Source: 4, From: 1, Through: 1,
				Criteria: []string{"b1"}}, {Source: 5, From: 1, Through: 1, Criteria: []string{"b1"}},
		}})
	if err != nil {
		t.Fatal(err)
	}
	reviewJSON, err := json.Marshal(map[string]any{"technology": true, "relevant": true,
		"reason": "Каждый фрагмент подтверждает механизм обнаружения", "sources": []liveQualificationIdentity{
			qualificationIdentityFixture("detector-0", "introduces", true),
			qualificationIdentityFixture("detector-1", "implements", true),
			qualificationIdentityFixture("detector-2", "evaluates", true)}})
	if err != nil {
		t.Fatal(err)
	}
	review := string(reviewJSON)
	on := func(context.Context) bool { return true }
	generator := &qualificationGenerator{replies: []aiclients.GenResponse{
		{Text: string(encoded), Model: "reader-v1"}, {Text: review, Model: "reviewer-v1"},
	}}
	service := &TrendLiveService{qualify: NewCandidateQualifier(generator, on)}
	original := LiveTopic{StudyTopic: StudyTopic{ID: "ngram:speech-data",
		PaperIDs: []string{"clinical", "detector-0", "detector-1", "detector-2"}, EvidenceIDs: []string{"rec:news"}},
		Name: "Speech Data", Label: "Предварительное неверное название", PapersCount: 4, DocumentsCount: 5, RecordsCount: 1,
		QueryRelevance: new(0.72), Confidence: &LiveConfidence{Value: 1},
		Web: &LiveChannel{Total: 1, Records: []LiveSourceRecord{{ID: "news", Title: documents[4].Title}}}}
	umbrella := LiveTopic{}
	kept, rejected := qualifyFixtureCandidates(t, service, "обнаружение синтезированной речи", &umbrella,
		[]LiveTopic{original}, documents, periods)
	if len(kept) != 1 || len(rejected) != 0 {
		t.Fatalf("grounded candidate rejected: %+v", rejected)
	}
	got := kept[0]
	if got.Label != proposal.Label || got.DocumentsCount != 3 || got.PapersCount != 3 || got.RecordsCount != 0 ||
		got.Web != nil || got.FirstYear == periods[0].Start || got.QueryRelevance != nil || slices.Contains(got.PaperIDs, "clinical") {
		t.Fatalf("stale entity, evidence or measurements survived: %+v", got)
	}
	if len(generator.requests) != 2 || !strings.Contains(generator.requests[1].Query, "обнаружение синтезированной речи") ||
		got.Qualification.Model != "reader-v1" || got.Qualification.VerifierModel != "reviewer-v1" || !got.Qualification.Verified ||
		got.Qualification.Mechanism != proposal.Mechanism || got.Qualification.Evidence[0].Role != "introduces" {
		t.Fatalf("independent query-aware review missing: %+v", got.Qualification)
	}
	var reviewInput struct {
		Documents []liveQualificationReviewSource `json:"documents"`
	}
	if err := json.Unmarshal([]byte(generator.requests[1].Query), &reviewInput); err != nil {
		t.Fatal(err)
	}
	if len(reviewInput.Documents) != 3 || slices.ContainsFunc(reviewInput.Documents,
		func(document liveQualificationReviewSource) bool {
			return document.ID == "clinical" || document.ID == "rec:news"
		}) {
		t.Fatalf("reviewer received unselected documents: %+v", reviewInput.Documents)
	}
	var proposalInput struct {
		Documents []liveQualificationSource `json:"documents"`
	}
	if err := json.Unmarshal([]byte(generator.requests[0].Query), &proposalInput); err != nil {
		t.Fatal(err)
	}
	if first := proposalInput.Documents[0].Excerpts[0]; first.Index != 1 || !strings.Contains(first.Text, documents[4].Title) {
		t.Fatalf("proposal excerpts do not have explicit original coordinates: %+v", first)
	}
	sparse := strings.Replace(string(encoded), `,{"source":5,"from":1,"through":1,"criteria":["b1"]}`, "", 1)
	single := strings.Replace(sparse, `,{"source":4,"from":1,"through":1,"criteria":["b1"]}`, "", 1)
	for _, tc := range []struct {
		name, first, second, code string
	}{
		{"incoherent", `{"technology":true,"relevant":true,"coherent":false,"reason":"Общее слово объединяет разные предметы"}`,
			"", liveRejectIncoherent},
		{"behaviour_is_not_technology", `{"technology":false,"reason":"Показатель отношения пользователей не является технологией"}`,
			"", liveRejectNotTechnology},
		{"missing_source", strings.Replace(string(encoded), `"source":3`, `"source":99`, 1), "", liveRejectUnqualified},
		{"invented_excerpt", strings.Replace(string(encoded), `"through":1`, `"through":99`, 1),
			"", liveRejectUnqualified},
		{"single_source_without_review", single, "", liveRejectUnqualified},
		{"sparse_with_duplicate_is_still_invalid", strings.Replace(sparse, `"source":4`, `"source":3`, 1),
			"", liveRejectUnqualified},
		{"review_refuses_wrong_purpose", string(encoded),
			`{"technology":true,"relevant":false,"reason":"Это создание угрозы, а запрос требует обнаружения"}`,
			liveRejectUnqualified},
		{"review_cannot_add_source", string(encoded), strings.Replace(review, "detector-0", "clinical", 1), liveRejectUnqualified},
	} {
		t.Run(tc.name, func(t *testing.T) {
			g := &qualificationGenerator{replies: []aiclients.GenResponse{
				{Text: tc.first, Model: "reader-v1"}, {Text: tc.second, Model: "reviewer-v1"},
			}}
			s := &TrendLiveService{qualify: NewCandidateQualifier(g, on)}
			accepted, refused := qualifyFixtureCandidates(t, s, "обнаружение синтезированной речи", &LiveTopic{},
				[]LiveTopic{original}, documents, periods)
			if len(accepted) != 0 || len(refused) != 1 || refused[0].Code != tc.code || refused[0].OriginalName != original.Name {
				t.Fatalf("invalid candidate accepted or incorrectly explained: %+v", refused)
			}
		})
	}
	kept, rejected = qualifyFixtureCandidates(t, &TrendLiveService{}, "запрос", &LiveTopic{},
		[]LiveTopic{original}, documents, periods)
	if len(kept) != 0 || len(rejected) != 1 || rejected[0].Code != liveRejectUnqualified {
		t.Fatal("unavailable qualifier silently passed a candidate")
	}
	t.Run("server_copies_original_excerpts", func(t *testing.T) {
		text := "Метод.\n\tAn Long Short Term Memory (LSTM)-based module.  DSP remains Digital Signal Processing!"
		parts := liveSourceExcerpts(text)
		if len(parts) != 3 || strings.Join(parts, "") != text {
			t.Fatalf("source text changed during indexing: %q", parts)
		}
		docs := []LiveQualificationDocument{{ID: "source-1", Text: text}}
		claim, resolveErr := liveResolveQualificationExcerpt(liveQualificationSelection{Source: 1, From: 2, Through: 3},
			docs)
		if resolveErr != nil || claim.SourceID != "source-1" || claim.Quote != parts[1]+parts[2] {
			t.Fatalf("source range was rewritten: %+v, %v", claim, resolveErr)
		}
	})
}

func TestCandidateQualificationRequiresDirectSharedMechanism(t *testing.T) {
	documents := []LiveQualificationDocument{
		{ID: "architecture", Text: "A hybrid speech system combines resilient watermarking and voice anonymisation."},
		{ID: "legal", Text: "An evidentiary framework maps provenance, statistical watermarking and attestation to legal proof."},
		{ID: "survey", Text: "A survey compares capabilities of watermarking, provenance frameworks and detection technologies."},
	}
	proposal := `{"label":"Гибридные водяные знаки для речи",
		"mechanism":"Совмещение устойчивого водяного знака с анонимизацией голоса",
		"technology":true,"relevant":true,"coherent":true,"reason":"Все работы относятся к водяным знакам",
		"evidence":[{"source":1,"from":1,"through":1,"criteria":["b1"]},
		{"source":2,"from":1,"through":1,"criteria":["b1"]},
		{"source":3,"from":1,"through":1,"criteria":["b1"]}]}`
	for _, roles := range []struct {
		name, second, third string
		same                bool
	}{
		{name: "overview_is_not_direct_evidence", second: "context", third: "review", same: true},
		{name: "independent_mechanisms_are_not_one", second: "introduces", third: "evaluates", same: false},
	} {
		t.Run(roles.name, func(t *testing.T) {
			review, err := json.Marshal(map[string]any{"technology": true, "relevant": true,
				"reason": "Весь набор относится к одной области", "sources": []liveQualificationIdentity{
					qualificationIdentityFixture("architecture", "introduces", true),
					qualificationIdentityFixture("legal", roles.second, roles.same),
					qualificationIdentityFixture("survey", roles.third, roles.same),
				}})
			if err != nil {
				t.Fatal(err)
			}
			generator := &qualificationGenerator{replies: []aiclients.GenResponse{
				{Text: proposal, Model: "reader"}, {Text: string(review), Model: "reviewer"},
			}}
			qualify := NewCandidateQualifier(generator, func(context.Context) bool { return true })
			qualification, qualifyErr := qualify(liveFixtureScopeContext(t.Context(), "Проверка подлинности речи"),
				"Проверка подлинности речи", "watermarking", documents)
			code, _ := liveQualificationRejection(qualification)
			// Прямой источник остаётся, но обзор и чужой способ не увеличивают число подтверждений.
			if qualifyErr != nil || code != "" || !qualification.Verified || len(qualification.Evidence) != 1 ||
				qualification.Evidence[0].SourceID != "architecture" ||
				!strings.Contains(qualification.Reason, qualification.Mechanism) {
				t.Fatalf("the direct single source was lost or inflated: %+v, %v", qualification, qualifyErr)
			}
		})
	}
}

func TestCandidateQualificationSingleSourceStillRequiresDirectReview(t *testing.T) {
	documents := []LiveQualificationDocument{{ID: "direct-source",
		Text: "A nonce challenge detects replayed speech by matching spoken words against a random one-time request."}}
	const proposal = `{"label":"Одноразовая речевая проверка",
		"mechanism":"Сверка произнесённых слов со случайным одноразовым запросом выявляет повтор речи",
		"technology":true,"relevant":true,"coherent":true,"reason":"Механизм прямо описан в одном источнике",
		"evidence":[{"source":1,"from":1,"through":1,"criteria":["b1"]}]}`
	for _, role := range []string{"introduces", "implements", "evaluates", "deployment", "review", "context", "different"} {
		t.Run(role, func(t *testing.T) {
			review, err := json.Marshal(map[string]any{"technology": true, "relevant": true,
				"reason": "Проверена связь источника с механизмом", "sources": []liveQualificationIdentity{
					qualificationIdentityFixture(documents[0].ID, role, role != "different")}})
			if err != nil {
				t.Fatal(err)
			}
			generator := &qualificationGenerator{replies: []aiclients.GenResponse{
				{Text: proposal, Model: "reader"}, {Text: string(review), Model: "reviewer"}}}
			qualify := NewCandidateQualifier(generator, func(context.Context) bool { return true })
			result, err := qualify(liveFixtureScopeContext(t.Context(), "выявление повторно воспроизведённой речи"),
				"выявление повторно воспроизведённой речи", "nonce challenge", documents)
			accepted := slices.Contains([]string{"introduces", "implements", "evaluates", "deployment"}, role)
			if err != nil || len(generator.requests) != 2 || result.Verified != accepted || result.Coherent != accepted {
				t.Fatalf("single source skipped direct identity review: %+v, %v", result, err)
			}
			for _, request := range generator.requests {
				if !strings.Contains(request.System, "Достаточно одного прямого источника конкретного механизма") ||
					strings.Contains(request.System, "три подтверждения") {
					t.Fatal("source cardinality contract differs between the reader and independent reviewer")
				}
			}
			if !strings.Contains(generator.requests[0].System, "coherent=true, если хотя бы один выбранный источник прямо") {
				t.Fatal("coherence still uses an unspecified minimum source count")
			}
			code, _ := liveQualificationRejection(result)
			if accepted {
				if code != "" || len(result.Evidence) != 1 || result.Evidence[0].Quote != documents[0].Text {
					t.Fatalf("literal single-source evidence was lost or inflated: %+v", result)
				}
			} else if code != liveRejectIncoherent || len(result.Evidence) != 0 {
				t.Fatalf("single-source exception admitted indirect evidence: %+v", result)
			}
		})
	}
}

func TestCandidateQualificationRepairsFormOnce(t *testing.T) {
	documents := []LiveQualificationDocument{{ID: "direct-source",
		Text: "A nonce challenge detects replayed speech by matching spoken words against a random one-time request."}}
	const proposal = `{"label":"Одноразовая речевая проверка",
		"mechanism":"Сверка произнесённых слов со случайным одноразовым запросом выявляет повтор речи",
		"technology":true,"coherent":true,"reason":"%s","evidence":[{"source":1,"from":1,"through":1,"criteria":["b1"]}]}`
	valid := qualificationIdentityFixture(documents[0].ID, "introduces", true)
	english := valid
	english.Relevance.ClaimFact = "Nonce challenge for replay detection"
	review := func(identity liveQualificationIdentity) string {
		encoded, err := json.Marshal(map[string]any{"technology": true, "reason": "Проверена связь источника с механизмом",
			"sources": []liveQualificationIdentity{identity}})
		if err != nil {
			t.Fatal(err)
		}
		return string(encoded)
	}
	generator := &qualificationGenerator{replies: []aiclients.GenResponse{
		{Text: fmt.Sprintf(proposal, "Nonce challenge directly detects replayed speech"), Model: "reader"},
		{Text: fmt.Sprintf(proposal, "Механизм прямо описан в одном источнике"), Model: "reader"},
		{Text: review(english), Model: "reviewer"}, {Text: review(valid), Model: "reviewer"}}}
	qualify := NewCandidateQualifier(generator, func(context.Context) bool { return true })
	query := "выявление повторно воспроизведённой речи"
	result, err := qualify(liveFixtureScopeContext(t.Context(), query), query, "nonce challenge", documents)
	if err != nil || !result.Verified || len(generator.requests) != 4 {
		t.Fatalf("form error was not repaired by one retry: %+v, %v, calls=%d", result, err, len(generator.requests))
	}
	for i, cause := range map[int]string{1: "missing Russian reason", 3: "claim fact missing"} {
		if !strings.Contains(generator.requests[i].Query, "отклонён проверкой формы") ||
			!strings.Contains(generator.requests[i].Query, cause) {
			t.Fatalf("retry %d does not show the rejected form: %s", i, generator.requests[i].Query)
		}
	}
}

func TestCandidateQualificationCountsDirectSubset(t *testing.T) {
	documents := []LiveQualificationDocument{
		{ID: "design", Text: "Phase coherence detects synthetic speech by comparing adjacent frequency bins."},
		{ID: "implementation", Text: "We implement synthetic speech detection using phase coherence across frequency bins."},
		{ID: "evaluation", Text: "The evaluation compares adjacent frequency bins to detect synthetic speech via phase coherence."},
		{ID: "survey", Text: "A survey lists phase coherence among several synthetic speech detection approaches."},
	}
	proposal := `{"label":"Фазовое обнаружение синтезированной речи",
		"mechanism":"Проверка фазовой согласованности соседних частотных интервалов для обнаружения синтеза",
		"technology":true,"relevant":true,"coherent":true,"reason":"Предложены прямые и обзорные свидетельства",
		"evidence":[{"source":1,"from":1,"through":1,"criteria":["b1"]},{"source":2,"from":1,"through":1,"criteria":["b1"]},
		{"source":3,"from":1,"through":1,"criteria":["b1"]},{"source":4,"from":1,"through":1,"criteria":["b1"]}]}`
	for _, direct := range []int{3, 2, 1, 0} {
		t.Run(fmt.Sprintf("%d_direct", direct), func(t *testing.T) {
			assessments := make([]liveQualificationIdentity, 0, len(documents))
			for i, document := range documents {
				assessment := qualificationIdentityFixture(document.ID, "review", true)
				if i < direct {
					assessment.Role = "evaluates"
				}
				assessments = append(assessments, assessment)
			}
			review, err := json.Marshal(map[string]any{"technology": true, "relevant": true,
				"reason": "Набор содержит прямые свидетельства и обзор", "sources": assessments})
			if err != nil {
				t.Fatal(err)
			}
			generator := &qualificationGenerator{replies: []aiclients.GenResponse{
				{Text: proposal, Model: "reader"}, {Text: string(review), Model: "reviewer"},
			}}
			qualify := NewCandidateQualifier(generator, func(context.Context) bool { return true })
			qualification, qualifyErr := qualify(liveFixtureScopeContext(t.Context(), "обнаружение синтетической речи"),
				"обнаружение синтетической речи", "phase coherence", documents)
			accepted := direct >= liveCandidateMinDocuments
			if qualifyErr != nil || qualification.Coherent != accepted || qualification.Verified != accepted ||
				len(qualification.Evidence) != direct || qualification.Method != liveQualificationMethod {
				t.Fatalf("direct subset was not counted: %+v, %v", qualification, qualifyErr)
			}
			if !accepted {
				code, _ := liveQualificationRejection(qualification)
				if code != liveRejectIncoherent {
					t.Fatalf("sparse direct subset has wrong rejection: %s", code)
				}
			}
		})
	}
}

func qualificationIdentityFixture(id, role string, same bool) liveQualificationIdentity {
	return liveQualificationIdentity{SourceID: id, SameTechnology: same, Role: role,
		Candidate: liveQualificationCriterion{Supported: new(true), From: 1, Through: 1,
			SourceFact: "Источник описывает исходный технический способ",
			ClaimFact:  "Кандидат называет этот технический способ",
			Reason:     "Определяющие признаки исходного кандидата сохранены"},
		Mechanism: liveQualificationCriterion{Supported: new(true), From: 1, Through: 1,
			SourceFact: "Источник описывает конкретный технический способ",
			ClaimFact:  "Предложение описывает этот технический способ",
			Reason:     "Прямо описан проверяемый механизм"},
		Relevance: liveQualificationCriterion{Criteria: []string{"b1"}, Supported: new(true), From: 1, Through: 1,
			SourceFact: "Исследование решает запрошенную задачу в указанной области",
			ClaimFact:  "Запрос требует решить эту задачу в той же области",
			Reason:     "Прямо описана требуемая задача"}}
}

func TestCandidateFinalSelectionRefillsAfterDeduplication(t *testing.T) {
	candidates := make([]LiveTopic, 0, liveTopCandidates+2)
	for i := range liveTopCandidates + 2 {
		candidates = append(candidates, LiveTopic{StudyTopic: StudyTopic{ID: fmt.Sprintf("candidate-%02d", i),
			PaperIDs: []string{fmt.Sprintf("source-%02d", i)}}, Name: fmt.Sprintf("candidate-%02d", i),
			Label: fmt.Sprintf("Технология %02d", i), EvidenceLevel: liveEvidenceStrict,
			Confidence: &LiveConfidence{Value: 0.5},
			ModelScore: &LiveModelScore{Label: liveClassifierWeak, Value: float64(i) / 100}})
	}
	candidates[0].Label = candidates[1].Label
	kept, rejected := liveSelectCandidates(candidates, nil)
	if len(kept) != liveTopCandidates || kept[0].Name != "candidate-16" {
		t.Fatalf("final selection lost replacement or model order: %d, %s", len(kept), kept[0].Name)
	}
	if len(rejected) != 2 || rejected[0].Code != liveRejectDuplicate || rejected[1].Code != liveRejectRank {
		t.Fatalf("wrong final selection reasons: %+v", rejected)
	}
	kept, rejected = liveSelectCandidates(kept[:14], nil)
	if len(kept) != 14 || len(rejected) != 0 {
		t.Fatal("a short verified answer claims that all fifteen places are occupied")
	}
	if liveSameTechnology("Аутентификация", "Аутентификация квантовыми ключами") {
		t.Fatal("a broad name erased a different mechanism without shared evidence")
	}
}

func TestLiveTechnologyLabelKeepsProperNames(t *testing.T) {
	for text, want := range map[string]string{
		"Графовый поиск с дополнением": "Графовый поиск с дополнением",
		"«GraphRAG»":    "GraphRAG",
		"KG-RAG":        "KG-RAG",
		"Qwen3":         "Qwen3",
		"Hop Reasoning": "",
		"Memory":        "",
		"hop":           "",
	} {
		if got := liveTechnologyLabel(text); got != want {
			t.Errorf("liveTechnologyLabel(%q) = %q, want %q", text, got, want)
		}
	}
}

func TestLiveNamedLabelKeepsMethodName(t *testing.T) {
	quote := []LiveClaimEvidence{{Field: "summary", Quote: "we present RA-VLA, a retrieval-augmented VLA framework"}}
	for summary, want := range map[string]string{
		"RA-VLA — это фреймворк извлечения контекста для VLA-модели.":   "RA-VLA: адаптация VLA на лету",
		"RA-VLA (Retrieval-Augmented VLA) адаптирует модель без весов.": "RA-VLA: адаптация VLA на лету",
		"VLA-модели адаптируются к задаче без обновления весов.":        "Адаптация VLA на лету",
		"Фреймворк RA-VLA извлекает контекст из памяти.":                "Адаптация VLA на лету",
		"ICI-VLA — обучение по демонстрациям в контексте.":              "Адаптация VLA на лету",
	} {
		topic := LiveTopic{Name: "retrieval augmented adaptation", Label: "Адаптация VLA на лету",
			Card: &LiveCard{Summary: summary, Evidence: quote}}
		if got := liveNamedLabel(topic); got != want {
			t.Errorf("%q: %q, want %q", summary, got, want)
		}
	}
	field := LiveTopic{Name: "Edge Attribution Patching", Label: "Механистическая интерпретируемость",
		Card: &LiveCard{Summary: "Edge attribution patching (EAP) — метод механистической интерпретируемости."}}
	if got := liveNamedLabel(field); got != "EAP: механистическая интерпретируемость" {
		t.Errorf("имя из нескольких слов: %q", got)
	}
	named := LiveTopic{Label: "RA-VLA: адаптация", Card: &LiveCard{Summary: "RA-VLA — фреймворк.", Evidence: quote}}
	if got := liveNamedLabel(named); got != named.Label {
		t.Errorf("имя повторено: %q", got)
	}
	// Русская приставка — тоже имя, иначе вышло бы «OmniCalib: омниКалиб: …».
	transliterated := LiveTopic{Name: "omnicalib", Label: "омниКалиб: самонастройка гуманоидов",
		Card: &LiveCard{Summary: "OmniCalib — метод самонастройки.", Evidence: quote}}
	if got := liveNamedLabel(transliterated); got != transliterated.Label {
		t.Errorf("вторая приставка: %q", got)
	}
}

func TestLiveDetailDropsLabelWithChineseFragment(t *testing.T) {
	candidates := []LiveTopic{
		{Name: "GigaBrain-0", Label: "GigaBrain-0: модели «视觉-语言-действие»"},
		{Name: "retrieval augmented adaptation", Label: "Адаптация VLA на лету"},
	}
	detail := liveDetail(&LiveSnapshot{}, LiveTopic{}, nil, nil, candidates, nil, nil)
	if got := detail.Candidates[0].Label; got != "GigaBrain-0" {
		t.Errorf("смесь письменностей: %q", got)
	}
	if got := detail.Candidates[1].Label; got != "Адаптация VLA на лету" {
		t.Errorf("русское название заменено: %q", got)
	}
}
