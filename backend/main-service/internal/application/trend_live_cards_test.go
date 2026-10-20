package application

import (
	"context"
	"encoding/json/v2"
	"errors"
	"strings"
	"testing"

	"github.com/example/main-service/internal/platform/aiclients"
)

type liveCardTestGenerator struct {
	fakeGenerator
	draft    string
	verified string
	requests []aiclients.GenRequest
}

func TestLLMWorkersFloor(t *testing.T) {
	s := &TrendLiveService{}
	if got := s.llmWorkers(t.Context()); got != LiveDefaultWorkers {
		t.Fatalf("default workers = %d, want %d", got, LiveDefaultWorkers)
	}
	s.SetWorkers(func(context.Context, string, int) int { return 0 })
	if got := s.llmWorkers(t.Context()); got != 1 {
		t.Fatalf("zero setting gave %d workers, want 1", got)
	}
}

func (g *liveCardTestGenerator) Generate(_ context.Context, req aiclients.GenRequest) (aiclients.GenResponse, error) {
	g.requests = append(g.requests, req)
	if req.System == liveCardVerifySystem+liveQueryScopeInstruction {
		return aiclients.GenResponse{Text: g.verified, Model: "verifier-test"}, nil
	}
	return aiclients.GenResponse{Text: g.draft, Model: "writer-test"}, nil
}

func TestLiveCardGroundingRejectsUnsupportedClaims(t *testing.T) {
	works := []string{
		"Nonce challenges detect voice replay attacks. In a laboratory trial, replay acceptance fell from 20% to 2%.",
		"Online proctoring used face verification during remote university examinations.",
	}
	draft := liveCardDraft{Summary: "Одноразовые задания выявляют повторно воспроизведённую речь.",
		Advantage: "В лабораторном испытании доля принятых повторов снизилась с 20 до 2 процентов.",
		Example:   "Банк использовал проверку лица при телефонных платежах.", Market: liveMarketEarly,
		MarketBasis: "У технологии ещё не сформирован рынок."}
	draft.Evidence = []liveCardSelection{
		{Field: liveClaimSummary, Source: 1, From: 1, Through: 1},
		{Field: liveClaimAdvantage, Source: 1, From: 2, Through: 2},
		{Field: liveClaimExample, Source: 2, From: 1, Through: 1},
		{Field: liveClaimMarket, Source: 1, From: 3, Through: 3},
	}
	encoded, err := json.Marshal(draft)
	if err != nil {
		t.Fatal(err)
	}
	gen := &liveCardTestGenerator{draft: string(encoded),
		verified: liveCardTestReview(t, draft, works, liveClaimSummary, liveClaimAdvantage, liveClaimMarket)}
	card, err := NewCandidateCard(gen, func(context.Context) bool { return true })(
		liveFixtureScopeContext(t.Context(), "обнаружение мошенничества по телефону"),
		"обнаружение мошенничества по телефону", LiveCardIdentity{Label: "Проверка живости голоса",
			Mechanism: "Одноразовое произносимое задание выявляет повторное воспроизведение речи."}, works)
	if err != nil {
		t.Fatal(err)
	}
	if card.Summary == "" || card.Advantage == "" || card.Example != "" || len(card.Missing) != 1 {
		t.Fatalf("граница смысловой проверки нарушена: %+v", card)
	}
	if card.market.Known || card.market.Status != liveMarketUnknown || len(card.market.Evidence) != 0 {
		t.Fatalf("выдуманная цитата позволила утверждать ранний рынок: %+v", card.market)
	}
	if len(gen.requests) != 3 || !strings.Contains(gen.requests[1].Query, "обнаружение мошенничества по телефону") ||
		!strings.Contains(gen.requests[1].Query, works[1]) {
		t.Fatalf("проверяющая модель не получила контекст запроса и источник: %+v", gen.requests)
	}
	if !strings.Contains(gen.requests[0].Query, `"index":2,"text":" In a laboratory trial`) {
		t.Fatalf("в запросе нет явной нумерации исходных фрагментов: %s", gen.requests[0].Query)
	}
	if card.Evidence[1].Quote != " In a laboratory trial, replay acceptance fell from 20% to 2%." {
		t.Fatalf("сервер изменил выбранный исходный фрагмент: %q", card.Evidence[1].Quote)
	}
	if liveCardComplete(LiveTopic{Card: &card}) {
		t.Fatal("неполная карточка принята")
	}
	draft.Example = "В лаборатории проверяли устойчивость к повторному воспроизведению голоса."
	complete := liveGroundedCard(draft, []LiveClaimEvidence{
		{Field: liveClaimSummary, SourceID: "1", Quote: works[0], Verified: true},
		{Field: liveClaimAdvantage, SourceID: "1", Quote: works[0], Verified: true},
		{Field: liveClaimExample, SourceID: "1", Quote: works[0], Verified: true},
	}, "writer-test", "verifier-test")
	liveRemapClaimSources(complete.Evidence, []string{"source-a", "source-b"})
	complete.ExampleID = liveClaim(complete.Evidence, liveClaimExample).SourceID
	topic := LiveTopic{StudyTopic: StudyTopic{PaperIDs: []string{"source-a"}}, Card: &complete}
	if !liveCardComplete(topic) {
		t.Fatalf("полная карточка с действительной ссылкой отклонена: %+v", complete)
	}
	topic.PaperIDs = []string{"another-source"}
	if liveCardComplete(topic) {
		t.Fatal("карточка с потерянным источником принята")
	}
}

func TestLiveDigestSourcesMarkTitleOnly(t *testing.T) {
	service := &TrendLiveService{digest: func(_ context.Context, title, _ string) (string, string, error) {
		if title == "refused" {
			return "", "", errors.New("refused")
		}
		return "Материал описывает проверку живости голоса.", "digest-test", nil
	}}
	titles := map[string]string{"p1": "Voice replay detection", "rec:news": "Voice liveness pilot", "p2": "refused"}
	digests := service.digestSources(t.Context(), []string{"p1", "rec:news", "p2"}, titles,
		map[string]string{"p1": "We detect replayed voice."})
	if digests["p1"].text != "Материал описывает проверку живости голоса." ||
		!strings.HasPrefix(digests["rec:news"].text, "Пересказ только заголовка:") {
		t.Fatalf("digests: %+v", digests)
	}
	if _, ok := digests["p2"]; ok {
		t.Fatal("отказ пересказа скрыт")
	}
}

func TestLiveCardCannotBorrowNumberFromAnotherSource(t *testing.T) {
	works := []string{"Models trained on ASVspoof 2019 were tested on the SONAR benchmark.",
		"Another detector showed a 20-fold increase in error."}
	var draft liveCardDraft
	if err := json.Unmarshal([]byte(`{"example":"На SONAR ошибка выросла в 20 раз.",
		"evidence":[{"field":"example","source":1,"from":1,"through":1}]}`), &draft); err != nil {
		t.Fatal(err)
	}
	claims := liveCardAssertions(draft, works)
	if len(claims) != 1 || len(claims[0].GroundingIssues) == 0 {
		t.Fatalf("потеряна проверка чисел или исходное утверждение: %+v", claims)
	}
	evidence, err := liveApplyCardVerification(claims, liveCardTestReview(t, draft, works, liveClaimExample))
	if err != nil || evidence[0].Verified {
		t.Fatalf("число из другого источника принято: %+v, %v", evidence, err)
	}
	draft.Example = "Модели обучили на ASVspoof 2019 и проверили на SONAR."
	groups := liveCardReviewSources(liveCardAssertions(draft, works))
	if len(groups) != 1 || groups[0].Source != 1 || len(groups[0].Excerpts) != 1 ||
		groups[0].Excerpts[0].Text != works[0] || len(groups[0].Claims[0].GroundingIssues) != 0 {
		t.Fatalf("проверяющий не получил собственный источник утверждения: %+v", groups)
	}
}

func TestLiveCardReportAssessmentIsOptionalAndAttributed(t *testing.T) {
	works := []string{
		"Nonce challenges detect replayed speech, and a laboratory trial showed fewer accepted replay attacks.",
		"Meridian Research analytical report: Meridian Research rates nonce-based voice liveness suitable for controlled pilots, " +
			"while warning about unresolved accessibility barriers.",
	}
	for _, tc := range []struct {
		name      string
		source    int
		supported bool
		want      bool
	}{
		{name: "experiment_is_not_an_analytical_report", source: 1, supported: false, want: false},
		{name: "attributed_assessment", source: 2, supported: true, want: true},
		{name: "invented_report_source", source: 3, supported: true, want: false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			draft := liveCardDraft{Summary: "Одноразовое задание выявляет запись голоса.",
				Advantage: "В испытании уменьшилось число принятых повторов.", Example: "Проведено лабораторное испытание.",
				ReportAssessment: "В аналитическом отчёте Meridian Research технология признана подходящей для ограниченных пилотов; " +
					"отмечены нерешённые ограничения доступности."}
			for _, field := range []string{liveClaimSummary, liveClaimAdvantage, liveClaimExample} {
				draft.Evidence = append(draft.Evidence, liveCardSelection{Field: field, Source: 1, From: 1, Through: 1})
			}
			draft.Evidence = append(draft.Evidence,
				liveCardSelection{Field: LiveClaimReportAssessment, Source: tc.source, From: 1, Through: 1})
			raw, err := json.Marshal(draft)
			if err != nil {
				t.Fatal(err)
			}
			fields := []string{liveClaimSummary, liveClaimAdvantage, liveClaimExample}
			if tc.supported {
				fields = append(fields, LiveClaimReportAssessment)
			}
			gen := &liveCardTestGenerator{draft: string(raw), verified: liveCardTestReview(t, draft, works, fields...)}
			card, cardErr := NewCandidateCard(gen, func(context.Context) bool { return true })(
				liveFixtureScopeContext(t.Context(), "Защита от повторного воспроизведения речи"),
				"Защита от повторного воспроизведения речи", LiveCardIdentity{Label: "Одноразовая проверка голоса",
					Mechanism: "Проверка произнесённого одноразового задания выявляет запись голоса."}, works)
			if cardErr != nil || (card.ReportAssessment != "") != tc.want || len(card.Missing) != 0 {
				t.Fatalf("неверный допуск оценки отчёта: %+v, %v", card, cardErr)
			}
			liveRemapClaimSources(card.Evidence, []string{"trial", "report"})
			card.ExampleID = liveClaim(card.Evidence, liveClaimExample).SourceID
			if !liveCardComplete(LiveTopic{StudyTopic: StudyTopic{PaperIDs: []string{"trial", "report"}}, Card: &card}) {
				t.Fatal("необязательная оценка отчёта изменила готовность карточки")
			}
			if tc.want {
				claim := liveClaim(card.Evidence, LiveClaimReportAssessment)
				if card.ReportAssessment != draft.ReportAssessment || claim == nil || claim.SourceID != "report" ||
					claim.Quote != works[1] || !strings.Contains(gen.requests[1].Query, draft.ReportAssessment) {
					t.Fatalf("утрачены атрибуция или собственный источник оценки: %+v", card)
				}
			}
		})
	}
}

func TestLiveCardCarriesQualifiedIdentityThroughWriterAndVerifier(t *testing.T) {
	const query = "Выявление повторно воспроизведённой речи"
	want := LiveCardIdentity{Label: "Активная проверка живости голоса",
		Mechanism: "Система задаёт случайную последовательность слов и сверяет содержание ответа с одноразовым заданием."}
	documents := []StudyPaper{{ID: "source-a", Title: "Nonce challenge speech verification", Date: "2026-06-01"}}
	metadata := map[string]LivePaperMetadata{"source-a": {
		Abstract: "Random spoken challenges reject prerecorded speech. A laboratory trial evaluated nonce challenge responses.",
	}}
	draft := liveCardDraft{Summary: "Случайное речевое задание позволяет проверить живость голоса.",
		Advantage: "Отклоняется заранее записанная речь.", Example: "В лаборатории проверили ответы на одноразовое речевое задание."}
	for _, field := range []string{liveClaimSummary, liveClaimAdvantage, liveClaimExample} {
		draft.Evidence = append(draft.Evidence, liveCardSelection{Field: field, Source: 1, From: 1, Through: 2})
	}
	raw, err := json.Marshal(draft)
	if err != nil {
		t.Fatal(err)
	}
	works := []string{liveWorkLine(documents[0], metadata["source-a"])}
	gen := &liveCardTestGenerator{draft: string(raw),
		verified: liveCardTestReview(t, draft, works, liveClaimSummary, liveClaimAdvantage, liveClaimExample)}
	service := NewTrendLiveService(nil, nil)
	service.SetCardWriter(NewCandidateCard(gen, func(context.Context) bool { return true }))
	candidates := []LiveTopic{
		{PaperIDs: []string{"source-a"}, Name: "Broad audio detection", Label: "Название для отображения",
			Qualification: &LiveQualification{Verified: true, Technology: true, Relevant: true, Coherent: true,
				Label: want.Label, Mechanism: want.Mechanism,
				Evidence: []LiveQualificationEvidence{{SourceID: "source-a", Quote: metadata["source-a"].Abstract,
					MechanismQuote: metadata["source-a"].Abstract, RelevanceQuote: metadata["source-a"].Abstract,
					RelevanceReason: "Требуемая задача подтверждена источником.",
					IdentityReason:  "Проверяется именно одноразовое речевое задание.",
					SameTechnology:  true, Role: "introduces"}}}},
		{PaperIDs: []string{"source-a"}, Name: "Unqualified name"},
		{PaperIDs: []string{"source-a"}, Qualification: &LiveQualification{Verified: true, Label: want.Label}},
	}
	for _, job := range service.cardJobs(liveFixtureScopeContext(t.Context(), query), query, candidates, documents, metadata) {
		job()
	}
	if len(gen.requests) != 2 || candidates[0].Card == nil || !liveCardComplete(candidates[0]) {
		t.Fatalf("проверенная сущность не дошла до готовой карточки: calls=%d, card=%+v", len(gen.requests), candidates[0].Card)
	}
	for i, request := range gen.requests {
		if !strings.Contains(request.System, "Все три обязательных поля могут опираться на один документ") {
			t.Fatalf("обращение %d спутало число полей с числом источников", i)
		}
		var payload liveCardPayload
		if decodeErr := json.Unmarshal([]byte(request.Query), &payload); decodeErr != nil {
			t.Fatal(decodeErr)
		}
		if payload.Query != query || payload.Identity != want ||
			strings.Contains(request.Query, candidates[0].Name) || strings.Contains(request.Query, candidates[0].Label) {
			t.Fatalf("обращение %d потеряло или подменило проверенный механизм: %+v", i, payload)
		}
		if i == 0 && (len(payload.Sources) != 1 || len(payload.Sources[0].Excerpts) != 2) {
			t.Fatalf("генератор потерял исходные фрагменты: %+v", payload)
		}
		if i == 1 {
			if len(payload.Sources) != 1 || len(payload.Sources[0].Claims) != 3 {
				t.Fatalf("проверяющий потерял отдельные утверждения: %+v", payload)
			}
			var source strings.Builder
			for _, excerpt := range payload.Sources[0].Excerpts {
				source.WriteString(excerpt.Text)
			}
			if source.String() != liveWorkLine(documents[0], metadata["source-a"]) {
				t.Fatalf("контекст идентичности подменил первоисточник: %+v", payload)
			}
		}
	}
	for _, candidate := range candidates[1:] {
		if candidate.Card != nil || len(candidate.Limits) != 1 || candidate.Limits[0].Code != liveLimitCardOff {
			t.Fatalf("карточка без проверенного механизма всё же написана: %+v", candidate)
		}
	}
}

func TestLiveEnglishLeakCatchesMixedDigest(t *testing.T) {
	for text, leak := range map[string]bool{
		"Based на названии и аннотации, этот материал":    true,
		"This материал представляет собой исследование":   true,
		"Предлагается LLM-based система обнаружения атак": false,
		"Модель Qwen3 и метод LoRA дают прирост точности": false,
	} {
		if liveEnglishLeak.MatchString(text) != leak {
			t.Errorf("liveEnglishLeak(%q) = %v, want %v", text, !leak, leak)
		}
	}
}

func TestLiveStudyLimitationsRequireVerifiedLiteralEvidence(t *testing.T) {
	works := []string{"Continuous drug delivery beyond 10 minutes causes interference between chambers " +
		"in this brain organoid device."}
	for _, supported := range []bool{true, false} {
		draft := liveCardDraft{StudyLimitations: "В устройстве с органоидами мозга подача препарата дольше 10 минут " +
			"вызывает влияние между камерами.",
			Evidence: []liveCardSelection{{Field: liveClaimStudyLimitations, Source: 1, From: 1, Through: 1}}}
		raw, err := json.Marshal(draft)
		if err != nil {
			t.Fatal(err)
		}
		var fields []string
		if supported {
			fields = []string{liveClaimStudyLimitations}
		}
		var review liveCardReview
		if decodeErr := json.Unmarshal([]byte(liveCardTestReview(t, draft, works, fields...)), &review); decodeErr != nil {
			t.Fatal(decodeErr)
		}
		if supported {
			review.Fields[0].Parts = []LiveClaimPart{{Claim: draft.StudyLimitations, SourceFact: works[0],
				Direct: true, ScopePreserved: true, ModalityPreserved: true}}
		}
		gen := &liveCardTestGenerator{draft: string(raw), verified: liveEncodeCardReview(t, review)}
		card, err := NewCandidateCard(gen, func(context.Context) bool { return true })(
			liveFixtureScopeContext(t.Context(), "органоиды для тестирования лекарств"), "органоиды для тестирования лекарств",
			LiveCardIdentity{Label: "Органоиды мозга",
				Mechanism: "Микрофлюидное устройство тестирования лекарств на органоидах мозга."}, works)
		if err != nil || (card.StudyLimitations != "") != supported {
			t.Fatalf("unsupported limitation: %+v %v", card, err)
		}
		if supported {
			proof := liveClaim(card.Evidence, liveClaimStudyLimitations)
			if proof == nil || proof.Quote != works[0] || !strings.Contains(gen.requests[1].Query, draft.StudyLimitations) {
				t.Fatal("limitation lost its source or bypassed verification")
			}
		}
	}
}
