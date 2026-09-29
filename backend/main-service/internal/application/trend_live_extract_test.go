package application

import (
	"context"
	"encoding/json/v2"
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"github.com/example/main-service/internal/platform/aiclients"
)

type extractionGenerator struct {
	fakeGenerator
	generate func(context.Context, aiclients.GenRequest) (aiclients.GenResponse, error)
}

func (g extractionGenerator) Generate(ctx context.Context, req aiclients.GenRequest) (aiclients.GenResponse, error) {
	return g.generate(ctx, req)
}

func TestExtractedHistoryUsesQualifiedMembership(t *testing.T) {
	for _, established := range []bool{false, true} {
		t.Run(fmt.Sprintf("established=%t", established), func(t *testing.T) {
			service, _, _, periods := liveWaveFixture(t, 0)
			for i := range periods {
				periods[i].Population = 10000
			}
			anchor := StudyPaper{ID: "new-mechanism", Title: "Phase Resonator for fraud detection",
				Date: periods[len(periods)-2].Start, Authors: []string{"New laboratory"}}
			documents := make([]StudyPaper, 1, liveExtractCandidateDocuments)
			documents[0] = anchor
			for i := range liveExtractCandidateDocuments - 1 {
				documents = append(documents, StudyPaper{ID: fmt.Sprintf("context-%d", i),
					Title: fmt.Sprintf("Fraud detection study %d", i), Date: periods[i%7].Start,
					Authors: []string{fmt.Sprintf("Independent laboratory %d", i)}})
			}
			found := []LiveExtractedTechnology{{Name: "Phase Resonator", Label: "Фазовый резонатор",
				Documents: []string{anchor.ID}, Terms: []string{"fraud detection"}}}
			candidates, rejected := service.extractCandidates(LiveTopic{}, found, documents, nil, periods)
			if len(candidates) != 1 || len(rejected) != 0 {
				t.Fatalf("raw thematic history rejected an unqualified entity: %+v", rejected)
			}
			if raw := candidates[0]; raw.DocumentsCount != liveExtractCandidateDocuments || !liveScienceAge(raw).long {
				t.Fatalf("fixture did not reproduce the old broad neighborhood: %+v", raw)
			}
			qualify := service.qualify
			service.qualify = func(ctx context.Context, query, name string,
				inputs []LiveQualificationDocument,
			) (LiveQualification, error) {
				if inputs[0].ID != anchor.ID {
					t.Errorf("the direct source lost priority: %+v", inputs)
				}
				result, err := qualify(ctx, query, name, inputs)
				if established {
					for _, source := range inputs[1:] {
						evidence := result.Evidence[0]
						evidence.SourceID, evidence.Quote = source.ID, source.Text
						evidence.MechanismQuote, evidence.RelevanceQuote = source.Text, source.Text
						result.Evidence = append(result.Evidence, evidence)
					}
				}
				return result, err
			}
			kept, rejected := qualifyFixtureCandidates(t, service, "обнаружение мошенничества", &LiveTopic{},
				candidates, documents, periods)
			if established {
				if len(kept) != 0 || len(rejected) != 1 || rejected[0].Code != liveRejectHistory ||
					rejected[0].Papers != liveQualificationDocuments {
					t.Fatalf("grounded old history bypassed the final gate: %+v", rejected)
				}
				return
			}
			if len(kept) != 1 || len(rejected) != 0 || kept[0].DocumentsCount != 1 ||
				!slices.Equal(kept[0].PaperIDs, []string{anchor.ID}) || len(kept[0].qualificationAnchors) != 0 {
				t.Fatalf("new entity retained the history of its neighborhood: kept=%+v rejected=%+v", kept, rejected)
			}
		})
	}
}

func TestExtractedAnchorPrecedesMarketContext(t *testing.T) {
	service, _, _, periods := liveWaveFixture(t, 0)
	anchor := StudyPaper{ID: "new-mechanism", Title: "Phase Resonator for fraud detection",
		Date: periods[len(periods)-2].Start, Authors: []string{"Source laboratory"}}
	documents := make([]StudyPaper, 1, liveQualificationDocuments+2)
	documents[0] = anchor
	byID := map[string]StudyPaper{anchor.ID: anchor}
	for i := range liveQualificationDocuments + 1 {
		paper := StudyPaper{ID: fmt.Sprintf("rec:market-%d", i), Title: "Fraud detection in production",
			Date: anchor.Date, Authors: []string{fmt.Sprintf("Market source %d", i)}}
		documents = append(documents, paper)
		byID[paper.ID] = paper
	}
	found := []LiveExtractedTechnology{{Name: "Phase Resonator", Documents: []string{anchor.ID},
		Terms: []string{"fraud detection"}}}
	candidates, rejected := service.extractCandidates(LiveTopic{}, found, documents, nil, periods)
	if len(candidates) != 1 || len(rejected) != 0 || len(candidates[0].EvidenceIDs) <= liveQualificationDocuments {
		t.Fatalf("fixture did not collect enough market context: %+v %+v", candidates, rejected)
	}
	inputs := liveQualificationInputs(candidates[0], byID, nil)
	if len(inputs) != liveQualificationDocuments || inputs[0].ID != anchor.ID {
		t.Fatalf("market matches displaced the direct source or escaped the budget: %+v", inputs)
	}
	seen := map[string]bool{}
	for i, input := range inputs {
		if seen[input.ID] || i > 0 && !strings.HasPrefix(input.ID, liveRecordPrefix) {
			t.Fatalf("source deduplication or market context disappeared: %+v", inputs)
		}
		seen[input.ID] = true
	}
}

// Один класс под тремя именами из трёх пакетов — один кандидат с работами трёх групп, и плодовитая группа
// не вытесняет из проверки сущности остальные; общий термин-подстрока другой способ к классу не приклеивает.
func TestExtractedClassMergesAcrossBatches(t *testing.T) {
	service, _, _, periods := liveWaveFixture(t, 0)
	date := periods[len(periods)-2].Start
	paper := func(id, title, lab string) StudyPaper {
		return StudyPaper{ID: id, Title: title, Date: date, Authors: []string{lab}}
	}
	documents := make([]StudyPaper, liveQualificationDocuments, liveQualificationDocuments+3)
	prolific := make([]string, liveQualificationDocuments)
	for i := range documents {
		documents[i] = paper(fmt.Sprintf("prolific-%d", i), fmt.Sprintf("Provenance graph variant %d", i),
			"Prolific laboratory")
		prolific[i] = documents[i].ID
	}
	documents = append(documents,
		paper("other-lab", "PROVEX: explainable provenance-based IDS", "Second laboratory"),
		paper("third-lab", "Kairos: whole-system provenance graphs for intrusion detection", "Third laboratory"),
		paper("federated", "FedIDS: federated intrusion detection for IoT", "Fourth laboratory"))
	found := []LiveExtractedTechnology{
		{Name: "Provenance Graph Intrusion Detection", Label: "Обнаружение вторжений по графам происхождения",
			Terms: []string{"provenance-based intrusion detection"}, Documents: prolific},
		{Name: "provenance-based intrusion detection", Documents: []string{"other-lab"}},
		{Name: "intrusion detection over provenance graphs", Documents: []string{"third-lab"}},
		{Name: "federated intrusion detection", Terms: []string{"intrusion detection"}, Documents: []string{"federated"}},
	}
	candidates, rejected := service.extractCandidates(LiveTopic{}, found, documents, nil, periods)
	at := slices.IndexFunc(candidates, func(c LiveTopic) bool { return c.Label == found[0].Label })
	if len(candidates) != 2 || len(rejected) != 0 || at < 0 {
		t.Fatalf("class variants were not merged into one candidate: candidates=%d rejected=%d",
			len(candidates), len(rejected))
	}
	class := candidates[at]
	if class.PapersCount != liveQualificationDocuments+2 || class.Groups != 3 || liveIndependentSources(class) != 3 {
		t.Fatalf("merged class lost works or author groups: papers=%d groups=%d", class.PapersCount, class.Groups)
	}
	byID := map[string]StudyPaper{}
	for _, document := range documents {
		byID[document.ID] = document
	}
	inputs := liveQualificationInputs(class, byID, nil)
	if len(inputs) != liveQualificationDocuments || inputs[0].ID != prolific[0] || inputs[1].ID != "other-lab" ||
		inputs[2].ID != "third-lab" {
		t.Fatalf("one prolific group displaced independent confirmations: %+v", inputs)
	}
}

// Рыночный проход: технология из двух изданий несёт их записи с паспортом и после проверки сущности держится
// на двух независимых издателях, а технология одного пресс-релиза отсекается как шум до очереди проверки.
func TestMarketRecordsBecomeCandidates(t *testing.T) {
	service, _, _, periods := liveWaveFixture(t, 0)
	records := []LiveSourceRecord{
		{ID: "fg", Title: "Bank pilots tokenized deposits for corporate treasury", Date: "2026-09-10",
			URL: "https://fintech.global/2026/09/10/bank-pilots-tokenized-deposits/", Source: "FinTech Global",
			Excerpt: "The bank settles corporate payments with tokenized deposits on a permissioned ledger."},
		{ID: "py", Title: "Bank consortium settles interbank payments on a shared ledger", Date: "2026-09-12",
			URL: "https://www.pymnts.com/news/2026/tokenized-deposits-consortium/", Source: "PYMNTS",
			Excerpt: "Tokenized deposits let member banks settle interbank payments around the clock."},
		{ID: "pr", Title: "Acme launches QuantumRail instant payment switch", Date: "2026-09-15",
			URL: "https://www.prnewswire.com/news-releases/acme-quantumrail-302000000.html", Source: "PR Newswire"},
	}
	stampPassports(records)
	umbrella := LiveTopic{Name: "цифровые платежи", Query: LiveQuery{Terms: []string{"digital payments"}},
		Web: &LiveChannel{Records: records, Counts: []*int{}}}
	service.extract = func(_ context.Context, request CandidateExtractionRequest) ([]LiveExtractedTechnology, error) {
		if !request.Market {
			return nil, nil
		}
		cited := func(phrase string) []string {
			ids := []string{}
			for _, document := range request.Documents {
				if strings.Contains(strings.ToLower(document.Text), phrase) {
					ids = append(ids, document.ID)
				}
			}
			return ids
		}
		if len(cited("around the clock")) == 0 {
			t.Error("market extraction read titles without the feed excerpts")
		}
		return []LiveExtractedTechnology{
			{Name: "tokenized deposits", Label: "Токенизированные депозиты", Documents: cited("tokenized deposits")},
			{Name: "instant payment switch", Label: "Коммутатор мгновенных платежей", Documents: cited("quantumrail")},
		}, nil
	}
	query := "цифровые платежи"
	candidates, rejected, documents := service.shortlist(t.Context(), query, &umbrella, nil,
		map[string]LivePaperMetadata{}, periods, nil, nil)
	if len(candidates) != 1 || candidates[0].Web == nil || len(candidates[0].Web.Records) != 2 ||
		candidates[0].Web.Records[0].PublisherType != PublisherMedia || liveIndependentSources(candidates[0]) < 2 {
		t.Fatalf("market records did not become one candidate with two publishers: %+v", candidates)
	}
	if !slices.ContainsFunc(rejected, func(row LiveRejection) bool { return row.Code == liveRejectHype }) {
		t.Fatalf("a press release alone was not refused as noise: %+v", rejected)
	}
	service.qualify = func(_ context.Context, query, _ string, inputs []LiveQualificationDocument) (LiveQualification, error) {
		result := LiveQualification{Scope: liveFixtureQueryScope(query), Label: "Токенизированные депозиты",
			Mechanism:  "Банковский депозит выпускается токеном в разрешённом реестре и переводится между банками",
			Technology: true, Relevant: true, Coherent: true, Verified: true, Model: "fixture", VerifierModel: "fixture",
			Method: liveQualificationMethod, Reason: "Оба издания описывают расчёты токенизированными депозитами"}
		for _, input := range inputs {
			result.Evidence = append(result.Evidence, LiveQualificationEvidence{ScopeCriteria: []string{"b1"},
				SourceID: input.ID, Quote: input.Text, SameTechnology: true, Role: "deployment",
				IdentityReason: "Прямое описание применения", MechanismQuote: input.Text, RelevanceQuote: input.Text,
				RelevanceReason: "Прямо описаны расчёты платежей"})
		}
		return result, nil
	}
	kept, refused := qualifyFixtureCandidates(t, service, query, &umbrella, candidates, documents, periods)
	if len(kept) != 1 || len(refused) != 0 || liveIndependentSources(kept[0]) < liveMinIndependent {
		t.Fatalf("qualified market candidate lost its independent publishers: kept=%+v refused=%+v", kept, refused)
	}
}

func TestExtractionExpansionFindsUnseenMechanismsWithinBudget(t *testing.T) {
	for _, mode := range []string{liveMLSourceOff, liveMLSourceOn} {
		t.Run("unexamined sources and years/"+mode, func(t *testing.T) { testExtractionUnexamined(t, mode) })
	}
	t.Run("rich source continuation excludes prior mechanisms", testExtractionContinuation)
	for _, budget := range []time.Duration{5 * time.Second, 5 * time.Minute} {
		t.Run(budget.String(), func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				var calls atomic.Int64
				s := &TrendLiveService{extract: func(ctx context.Context, _ CandidateExtractionRequest) ([]LiveExtractedTechnology, error) {
					calls.Add(1)
					<-ctx.Done()
					return nil, ctx.Err()
				}}
				ctx, cancel := context.WithTimeout(t.Context(), budget)
				defer cancel()
				// Все места общего предела заняты волнами: добор не должен простоять свой срок в очереди.
				ctx = context.WithValue(ctx, liveWorkerSlotsKey{}, &liveWaveWorkers{calls: make(liveWorkerSlots)})
				started := time.Now()
				found := s.expandExtraction(ctx, "mechanisms", []StudyPaper{{ID: "review", Title: "Review"}},
					nil, []LiveTopic{{Name: "already considered"}})
				if len(found) != 0 || calls.Load() != 1 || time.Since(started) != min(budget, liveExtractExpansionWait) {
					t.Fatalf("unbounded expansion: calls=%d elapsed=%s found=%+v", calls.Load(), time.Since(started), found)
				}
				cancel()
				_ = s.expandExtraction(ctx, "mechanisms", []StudyPaper{{ID: "review", Title: "Review"}},
					nil, []LiveTopic{{Name: "already considered"}})
				if calls.Load() != 1 {
					t.Fatal("canceled parent started another extraction")
				}
			})
		})
	}
}

func testExtractionUnexamined(t *testing.T, mode string) {
	t.Helper()
	documents := make([]StudyPaper, 0, 321)
	byID := map[string]StudyPaper{}
	metadata := map[string]LivePaperMetadata{}
	for i := range 320 {
		source := "arxiv"
		if i%2 == 1 {
			source = "patent"
		}
		paper := StudyPaper{ID: fmt.Sprintf("%s:%d", source, i), Title: fmt.Sprintf("Mechanism study %d", i),
			Date: fmt.Sprintf("%d-01-01", 2024+i%3), URL: fmt.Sprintf("https://example.org/work/%d", i)}
		documents = append(documents, paper)
		byID[paper.ID] = paper
		metadata[paper.ID] = LivePaperMetadata{Language: "en"}
	}
	copyOfFirst := documents[0]
	copyOfFirst.ID = "corpus:duplicate"
	documents = append(documents, copyOfFirst)
	var mu sync.Mutex
	var requests []CandidateExtractionRequest
	s := NewTrendLiveService(nil, nil)
	s.extract = func(_ context.Context, req CandidateExtractionRequest) ([]LiveExtractedTechnology, error) {
		mu.Lock()
		defer mu.Unlock()
		requests = append(requests, req)
		return nil, nil
	}
	var mlCalls atomic.Int64
	s.SetMLCandidateSource(mode, func(context.Context, LiveMLExtractRequest) (LiveMLExtractResponse, error) {
		mlCalls.Add(1)
		return LiveMLExtractResponse{}, errors.New("synthetic unavailable extractor")
	}, "test")
	finish, run := s.mlStartExtraction(t.Context(), "mechanisms", &LiveTopic{}, documents, metadata)
	finish()
	if mode == liveMLSourceOn && (mlCalls.Load() != 1 || run.Used != liveMLSourceOff || run.FallbackReason == "") {
		t.Fatalf("configured extractor did not enter fallback: %+v", run)
	}
	if mode == liveMLSourceOff && (mlCalls.Load() != 0 || run != nil) {
		t.Fatal("disabled extractor was used")
	}
	seen := map[string]bool{}
	for _, request := range requests {
		groups := map[string]bool{}
		for _, document := range request.Documents {
			paper, exists := byID[document.ID]
			if !exists || seen[document.ID] {
				t.Fatalf("duplicate or unknown source consumes extraction budget: %s", document.ID)
			}
			seen[document.ID] = true
			groups[liveSourceOf(paper.ID)+paper.Date[:4]] = true
		}
		if len(groups) != 6 {
			t.Fatalf("one source/year dominates extraction: %v", groups)
		}
	}
	if len(seen) != liveExtractDocuments {
		t.Fatalf("initial coverage = %d", len(seen))
	}
	requests = nil
	_ = s.expandExtraction(t.Context(), "mechanisms", documents, metadata, nil)
	if len(requests) != liveExtractExpansionBatches {
		t.Fatalf("additional requests = %d", len(requests))
	}
	for _, request := range requests {
		if len(request.Documents) != liveExtractBatch || request.Limit != liveExtractExpansionCandidates {
			t.Fatalf("additional batch exceeds its budget: %+v", request)
		}
		for _, document := range request.Documents {
			if seen[document.ID] {
				t.Fatalf("read an old source while unexamined sources remain: %s", document.ID)
			}
			seen[document.ID] = true
		}
	}
}

func testExtractionContinuation(t *testing.T) {
	names := []string{"sparse attention", "linear attention", "paged attention", "flash attention", "grouped query attention",
		"multi query attention", "sliding window attention", "block sparse attention"}
	const additional = "latent attention"
	documents := []StudyPaper{{ID: "review", Title: "Comparison of attention mechanisms"}}
	metadata := map[string]LivePaperMetadata{"review": {
		Abstract: strings.Join(names, "; ") + strings.Repeat(". Discussion of mechanisms", 25) + ". " + additional,
	}}
	var calls int
	gen := extractionGenerator{generate: func(_ context.Context, req aiclients.GenRequest) (aiclients.GenResponse, error) {
		calls++
		var input struct {
			Query     string   `json:"query"`
			Excluded  []string `json:"excluded"`
			Limit     int      `json:"limit"`
			Documents []struct {
				Text string `json:"text"`
			} `json:"documents"`
		}
		if err := json.Unmarshal([]byte(req.Query), &input); err != nil {
			return aiclients.GenResponse{}, err
		}
		returned := slices.Clone(names)
		if calls == 1 {
			if strings.Contains(input.Documents[0].Text, additional) || input.Limit != 8 || req.MaxTokens != 1200 {
				t.Error("initial extraction changed its bounded contract")
			}
		} else {
			if !strings.Contains(input.Documents[0].Text, additional) || !slices.Equal(input.Excluded, names) ||
				input.Query != "attention mechanisms" || input.Limit != 12 || req.MaxTokens != 2400 {
				t.Errorf("continuation lost context, exclusions or budget: %+v", input)
			}
			returned = []string{names[0], additional}
		}
		technologies := []map[string]any{}
		for _, name := range returned {
			technologies = append(technologies, map[string]any{"name_en": name, "documents": []int{1}})
		}
		technologies = append(technologies, map[string]any{"name_en": "invented source", "documents": []int{99}})
		raw, err := json.Marshal(map[string]any{"technologies": technologies})
		return aiclients.GenResponse{Text: string(raw)}, err
	}}
	s := &TrendLiveService{extract: NewCandidateExtractor(gen, func(context.Context) bool { return true })}
	initial := s.startExtraction(liveFixtureScopeContext(t.Context(), "attention mechanisms"),
		"attention mechanisms", documents, metadata)()
	known := make([]LiveTopic, len(initial))
	for i, technology := range initial {
		known[i] = LiveTopic{Name: technology.Name, PaperIDs: technology.Documents}
	}
	found := s.expandExtraction(liveFixtureScopeContext(t.Context(), "attention mechanisms"),
		"attention mechanisms", documents, metadata, known)
	if calls != 2 || len(initial) != 8 || len(found) != 1 || found[0].Name != additional ||
		!slices.Equal(found[0].Documents, []string{"review"}) {
		t.Fatalf("continuation repeated known entities or invented provenance: calls=%d initial=%d found=%+v",
			calls, len(initial), found)
	}
}
