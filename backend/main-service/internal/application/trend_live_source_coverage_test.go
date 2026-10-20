package application

import (
	"context"
	"fmt"
	"math"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"

	"github.com/example/main-service/internal/platform/aiclients"
)

func TestSourceCoverageAcrossDomains(t *testing.T) {
	model, err := LoadWeakSignalModel()
	if err != nil {
		t.Fatal(err)
	}
	for _, domain := range []struct{ name, phrase, source string }{
		{"biomed", "lipid nanoparticles", "pubmed"},
		{"chemistry", "photoredox catalysis", "openalex"},
		{"energy", "solid oxide electrolysis", "openalex"},
		{"OpenAIRE chemistry", "contact electro catalysis", "rec:openaire"},
		{"DataCite agro", "microbial inoculants", "rec:datacite"},
		{"IT", "hybrid kv cache", "openalex"},
	} {
		t.Run(domain.name, func(t *testing.T) {
			papers := make([]StudyPaper, 0, 8)
			metadata := map[string]LivePaperMetadata{}
			ids := make([]string, 0, 8)
			for i, suffix := range []string{"first experiment", "second experiment", "duplicate DOI", "outside window", "unrelated"} {
				paper := StudyPaper{ID: fmt.Sprintf("%s:%d", domain.source, i), CanonicalID: fmt.Sprintf("10.1234/%d", i),
					Title: domain.phrase + " " + suffix, Date: "2026-08-14", URL: "https://journal.example/paper"}
				switch i {
				case 2:
					paper.CanonicalID = papers[0].CanonicalID
				case 3:
					paper.Date = "2020-01-01"
				case 4:
					paper.Title = "Unrelated scientific article"
				}
				papers, ids = append(papers, paper), append(ids, paper.ID)
				metadata[paper.ID] = LivePaperMetadata{Genre: "article"}
			}
			for _, noise := range []struct{ id, address, genre string }{
				{"record:web", "https://news.example", "article"},
				{"openalex:preprint", "https://arxiv.org/abs/2608.1", "article"},
				{"openalex:editorial", "https://journal.example/editorial", "editorial"},
				{"rec:datacite:data", "https://doi.org/10.1234/data", "dataset"},
				{"rec:openaire:code", "https://doi.org/10.1234/code", "software"},
			} {
				papers = append(papers, StudyPaper{ID: noise.id, Title: domain.phrase + " " + noise.id,
					Date: "2026-08-14", URL: noise.address})
				ids = append(ids, noise.id)
				metadata[noise.id] = LivePaperMetadata{Genre: noise.genre}
			}
			topic := LiveTopic{StudyTopic: StudyTopic{ID: "ngram:test", PaperIDs: ids}}
			topic.Qualification = liveCoverageQualification(slices.Delete(slices.Clone(ids), 4, 5))
			if strings.HasPrefix(domain.source, liveRecordPrefix) {
				topic.PaperIDs, topic.EvidenceIDs = nil, ids
			}
			input := liveClassifierInput(domain.name, model.Version, []LiveTopic{topic}, papers, metadata)
			j := liveJournalCandidateEvidence(input.Candidates[0], scholarPeriods())
			if j == nil || j.Total != 2 || len(j.Titles) != 2 {
				t.Fatalf("счёт работ не ограничен прямыми свидетельствами, окном и DOI: %+v", j)
			}
			e := &WeakSignalEvidence{Window: &LivePhraseWindow{}, Journals: j, History: LiveTermHistory{2025: 8},
				HN: &LiveHNCounts{}, Rating: &LiveMaturityRating{Stage: 1, Confidence: 0.8}, Embedding: model.Embedding.Mean}
			evidence := map[string]*WeakSignalEvidence{domain.phrase: e}
			inputs, embedding, _ := weakSignalRow([]string{domain.phrase}, evidence, "2026-09")
			before, contributions, _ := model.Score(inputs, embedding)
			after := model.prediction("test", []string{domain.phrase}, evidence, "2026-09")
			windowContribution := 0.0
			for _, contribution := range contributions[:14] {
				windowContribution += contribution
			}
			hnContribution := 0.0
			for i, name := range model.Inputs {
				if strings.Contains(name, "_hn_") {
					hnContribution += contributions[i]
				}
			}
			if hnContribution < 0 {
				t.Fatalf("пустой Hacker News штрафует научный сигнал без обсуждений: %.6f", hnContribution)
			}
			wantLogit := math.Log(before/(1-before)) - windowContribution
			if math.Abs(after.Value-1/(1+math.Exp(-wantLogit))) > 1e-12 || after.Value <= before || liveDataGap(e) != "" {
				t.Fatalf("исключение окна изменило другие признаки: %.6f → %.6f, %+v", before, after.Value, after)
			}
			for _, feature := range after.Features {
				if strings.HasPrefix(feature.Name, "Окно arXiv") &&
					(feature.Contribution != 0 || !strings.Contains(feature.Value, "вклад равен нулю")) {
					t.Fatalf("исключение источника скрыто: %+v", feature)
				}
			}
			t.Logf("fixed evidence, arXiv empty: %.6f → %.6f; arXiv log-odds %.6f → 0; empty HN %.6f",
				before, after.Value, windowContribution, hnContribution)
			// Научная публикация не отменяет зрелость: другие признаки продолжают определять решение.
			e.Rating = &LiveMaturityRating{Stage: 4, Confidence: 1}
			e.History = LiveTermHistory{2000: 500, 2010: 500, 2023: 500, 2024: 500, 2025: 500}
			if p := model.prediction("mature", []string{domain.phrase}, evidence, "2026-09"); liveModelDecision(&p.LiveModelScore) {
				t.Fatalf("зрелая контрольная технология стала слабым сигналом: %+v", p)
			}
			// Найденное окно, включая ИТ, сохраняет побитово прежнюю формулу и подсказку зрелости.
			e.Window = &LivePhraseWindow{Total: 6, LiveClassifierFeatures: LiveClassifierFeatures{
				Documents: 6, ActivePeriods: 3, AuthorGroups: 4, FirstObserved: "2026-03"}}
			inputs, embedding, _ = weakSignalRow([]string{domain.phrase}, evidence, "2026-09")
			baseline, _, _ := model.Score(inputs, embedding)
			if p := model.prediction("measured", []string{domain.phrase}, evidence, "2026-09"); p.Value != baseline ||
				e.ratingEvidence().Source != "" {
				t.Fatalf("изменён расчёт измеренного окна: %.16f != %.16f", p.Value, baseline)
			}
			// Сбой не считается неприменимостью, а одна журнальная работа остаётся недостаточным основанием.
			e.Window, e.History = nil, nil
			if e.journalCoverage() || liveDataGap(e) != liveGapNotCollected {
				t.Fatal("сбой источника повысил оценку")
			}
			e.Window, e.History = &LivePhraseWindow{}, LiveTermHistory{}
			e.Journals.Total = 1
			if liveDataGap(e) != liveGapTooEarly {
				t.Fatal("одна журнальная работа не вызвала воздержание")
			}
			e.Journals = nil
			if liveDataGap(e) != liveGapNotFound {
				t.Fatal("отсутствие любых свидетельств не вызвало воздержание")
			}
		})
	}
}

func TestEmbeddedClassifierUsesJournalEvidence(t *testing.T) {
	model, err := LoadWeakSignalModel()
	if err != nil {
		t.Fatal(err)
	}
	service := NewTrendLiveService(nil, nil)
	service.snapshot = &LiveSnapshot{Periods: scholarPeriods()}
	service.history = fakeOpenAlex(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"nbHits":0}`))
	}))
	t.Cleanup(server.Close)
	service.hnCache = newLiveHNCache(server.URL)
	service.semantic = &semanticTestEncoder{batch: LiveEmbeddingBatch{Model: model.Embedding.Model,
		Vectors: [][]float64{model.Embedding.Mean}}}
	service.setWindow([]StudyPaper{{ID: "arxiv:unrelated", Title: "Unrelated research", Date: "2026-09-01"}}, nil)
	rated := aiclients.GenResponse{Text: `{"stage":1,"confidence":0.8}`}
	gen := &qualificationGenerator{replies: []aiclients.GenResponse{rated, rated}}
	service.rater = NewLiveMaturityRater(gen, func(context.Context) bool { return true })
	classify, version, err := service.EmbeddedClassifier()
	if err != nil {
		t.Fatal(err)
	}
	candidate := LiveClassifierCandidate{ID: "ngram:lipid-nanoparticles", journals: []liveJournalPaper{
		{paper: StudyPaper{ID: "pubmed:1", Title: "Lipid-based nanoparticles delivery study", Date: "2026-08-14"}},
		{paper: StudyPaper{ID: "pubmed:2", Title: "Lipid nanoparticles targeting study", Date: "2026-09-02"}},
	}}
	input := LiveClassifierRequest{FeatureSchema: LiveClassifierSchema, ModelVersion: version,
		Candidates: []LiveClassifierCandidate{candidate, {ID: "extract:lipid-nanoparticles", phrase: "lipid nanoparticles"}}}
	output, err := classify(t.Context(), input)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = liveValidatePredictions(input, output); err != nil {
		t.Fatal(err)
	}
	if version != model.Version+liveSourceCoverageVersion || len(gen.requests) != 1 {
		t.Fatalf("неверная версия или число оценок: %s, %+v", version, gen.requests)
	}
	request := gen.requests[0]
	if request.System != liveJournalRatingSystem || !strings.Contains(request.Query, "2 matching papers") ||
		!strings.Contains(request.Query, "2026-08-01") || !strings.Contains(request.Query, "targeting study") {
		t.Fatalf("модель языка получила ноль работ вместо журналов: %+v", request)
	}
	if !slices.ContainsFunc(output.Predictions[0].Features, func(f LiveModelFeature) bool {
		return strings.HasPrefix(f.Name, "Окно arXiv") && f.Contribution == 0
	}) {
		t.Fatalf("живой классификатор сохранил штраф: %+v", output)
	}
	if output.Predictions[1].DataGap != liveGapNotFound || slices.ContainsFunc(output.Predictions[1].Features,
		func(f LiveModelFeature) bool { return strings.Contains(f.Value, "исключено") }) {
		t.Fatalf("сосед с тем же именем унаследовал чужие научные свидетельства: %+v", output.Predictions[1])
	}
	// Отдельный ключ кэша не позволяет переиспользовать ответ на прежнюю подсказку об arXiv.
	_, err = service.rater.Rate(t.Context(), "lipid nanoparticles", LiveRatingEvidence{Total: 2})
	if err != nil || len(gen.requests) != 2 || gen.requests[1].System != liveRatingSystem {
		t.Fatalf("смешаны кэши подсказок: %v %+v", err, gen.requests)
	}
	// Отказ окна не должен отключать зрелость, когда журнальный контекст уже собран.
	service.setWindow(nil, nil)
	journal := liveJournalCandidateEvidence(input.Candidates[0], scholarPeriods())
	evidence := service.weakSignalEvidence(t.Context(), t.Context(), []string{"lipid nanoparticles"},
		scholarPeriods(), model.Embedding.Model, map[string]*LiveRatingEvidence{"lipid nanoparticles": journal})["lipid nanoparticles"]
	if evidence.Window != nil || evidence.Rating == nil || liveDataGap(evidence) != "" || evidence.journalCoverage() {
		t.Fatalf("сбой окна превратился в потерю журнального контекста: %+v", evidence)
	}
	if description := evidence.describe(0, 2026); description != "не измерено" {
		t.Fatalf("отказ окна представлен как успешный нулевой ответ: %s", description)
	}
}

func TestScientificCoverageIncludesDissertationsAndOtherPreprints(t *testing.T) {
	model, err := LoadWeakSignalModel()
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		genre, url string
		covered    bool
	}{
		{"dissertation", "https://repository.example/thesis", true},
		{"preprint", "https://doi.org/10.1234/preprint", true},
		{"preprint", "https://arxiv.org/abs/2610.12345", false},
		{"dataset", "https://doi.org/10.1234/data", false},
		{"news", "https://news.example/pilot", false},
	} {
		t.Run(test.genre+test.url, func(t *testing.T) {
			paper := StudyPaper{ID: "openalex:source", Title: "Solid electrolyte membranes", Date: "2026-09-01", URL: test.url}
			topic := LiveTopic{ID: "ngram:solid-electrolyte-membranes", PaperIDs: []string{paper.ID}}
			topic.Qualification = liveCoverageQualification(topic.PaperIDs)
			input := liveClassifierInput("battery", model.Version, []LiveTopic{topic}, []StudyPaper{paper},
				map[string]LivePaperMetadata{paper.ID: {Genre: test.genre}})
			const phrase = "solid electrolyte membranes"
			journal := liveJournalCandidateEvidence(input.Candidates[0], scholarPeriods())
			e := &WeakSignalEvidence{Window: &LivePhraseWindow{}, Journals: journal, History: LiveTermHistory{2025: 3},
				HN: &LiveHNCounts{}, Rating: &LiveMaturityRating{Stage: 1, Confidence: .8}, Embedding: model.Embedding.Mean}
			got := model.prediction("test", []string{phrase}, map[string]*WeakSignalEvidence{phrase: e}, "2026-10")
			for _, feature := range got.Features {
				if strings.HasPrefix(feature.Name, "Окно arXiv") && (feature.Contribution == 0) != test.covered {
					t.Fatalf("неверная применимость научного источника: %+v", feature)
				}
			}
		})
	}
	// Одна историческая работа не превращается в достаточное основание из-за пустого окна.
	e := &WeakSignalEvidence{Window: &LivePhraseWindow{}, History: LiveTermHistory{2025: 1}, HN: &LiveHNCounts{}}
	if liveDataGap(e) != liveGapTooEarly {
		t.Fatal("одна историческая работа не вызвала воздержание")
	}
}

func liveCoverageQualification(ids []string) *LiveQualification {
	qualification := liveIdentityFixture("Научный механизм", "Прямо описанный способ применения",
		"The source directly describes the studied mechanism and its application.", nil).Qualification
	proof := qualification.Evidence[0]
	qualification.Evidence = nil
	for _, id := range ids {
		proof.SourceID = id
		qualification.Evidence = append(qualification.Evidence, proof)
	}
	return qualification
}

func TestScientificCoverageRequiresOwnIndependentlyVerifiedMechanism(t *testing.T) {
	papers := []StudyPaper{
		{ID: "openalex:direct", Title: "Lipid nanoparticles delivery study", Date: "2026-08-14"},
		{ID: "openalex:context", Title: "Lipid nanoparticles overview", Date: "2026-08-14"},
	}
	metadata := map[string]LivePaperMetadata{"openalex:direct": {Genre: "article"}, "openalex:context": {Genre: "review"}}
	for _, scenario := range []string{"direct", "unqualified", "unreviewed", "other mechanism", "context role", "ungrounded"} {
		t.Run(scenario, func(t *testing.T) {
			topic := LiveTopic{ID: "extract:lipid-nanoparticles", PaperIDs: []string{papers[0].ID, papers[1].ID},
				Qualification: liveCoverageQualification([]string{papers[0].ID})}
			switch scenario {
			case "unqualified":
				topic.Qualification = nil
			case "unreviewed":
				topic.Qualification.Verified = false
			case "other mechanism":
				topic.Qualification.Evidence[0].SameTechnology = false
			case "context role":
				topic.Qualification.Evidence[0].Role = "context"
			case "ungrounded":
				topic.Qualification.Evidence[0].MechanismQuote = ""
			}
			input := liveClassifierInput("medicine", "test", []LiveTopic{topic}, papers, metadata)
			journal := liveJournalCandidateEvidence(input.Candidates[0], scholarPeriods())
			coverage := (&WeakSignalEvidence{Window: &LivePhraseWindow{}, Journals: journal}).journalCoverage()
			if coverage != (scenario == "direct") || coverage &&
				(journal.Total != 1 || !strings.Contains(journal.Titles[0], papers[0].Title)) {
				t.Fatalf("совпадение фразы без собственного прямого свидетельства открыло нейтрализацию: %+v", journal)
			}
		})
	}
}
