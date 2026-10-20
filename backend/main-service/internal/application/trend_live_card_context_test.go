package application

import (
	"context"
	"encoding/json/v2"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"
	"unicode/utf8"

	"github.com/example/main-service/internal/platform/aiclients"
)

// Собственная сокращённая разметка сохраняет факты таблицы, не копируя текст чужой статьи.
func liveCardFulltextFixture() string {
	return `<html><script>Invented result 100 percent.</script><article><h1>ProxyPrints</h1>` +
		`<div class="ltx_abstract"><h6>Abstract</h6><p>ProxyPrints protects stored fingerprints without affecting accuracy.</p></div>` +
		`<section><h2>Method</h2><p>A deterministic keyed transform produces reusable aliases for fingerprint authentication.</p>` +
		`</section><section><h2>Introduction</h2>` +
		strings.Repeat(`<p>General background about authentication and the research problem.</p>`, 250) +
		`</section><section><h2>Evaluation</h2><h3>Experiment Setup</h3>` +
		`<p>The laboratory evaluated LivDet fingerprints using the Bozorth3 matcher.</p>` +
		`<p>The same test protocol compares the baseline against the alias-transform pipeline.</p>` +
		`<figure class="ltx_table"><figcaption>Table III. Matcher performance with and without the transform.</figcaption>` +
		"<table>\n<tr>\n<th>Metric</th>\n<th>Bozorth3</th>\n<th>ProxyPrints + Bozorth3</th>\n</tr>\n" +
		"<tr>\n<th>ROC AUC</th>\n<td>0.93</td>\n<td>0.86</td>\n</tr>\n" +
		"<tr>\n<th>Accuracy (T=40)</th>\n<td>0.83</td>\n<td>0.75</td>\n</tr>\n" +
		"<tr>\n<th>EER Rate</th>\n<td>0.14</td>\n<td>0.19</td>\n</tr>\n</table></figure>" +
		`<h3>Matcher Performance</h3><p>Matching accuracy decreases after the transform; the experiment demonstrates a trade-off.</p>` +
		`<p>The transform rejects replay of stolen aliases in this laboratory test, not every biometric attack.</p>` +
		`</section><section><h2>Limitations</h2><p>These results do not establish unchanged accuracy on commercial matchers.</p>` +
		`</section></article></html>`
}

type liveFulltextTestGenerator struct {
	fakeGenerator
	wrong    bool
	requests []aiclients.GenRequest
}

func (g *liveFulltextTestGenerator) Generate(_ context.Context, request aiclients.GenRequest) (aiclients.GenResponse, error) {
	g.requests = append(g.requests, request)
	var payload liveCardPayload
	if err := json.Unmarshal([]byte(request.Query), &payload); err != nil {
		return aiclients.GenResponse{}, err
	}
	var response any
	if request.System == liveCardSystem+liveQueryScopeInstruction {
		draft := liveCardDraft{Summary: "Детерминированное преобразование создаёт псевдонимы отпечатков пальцев.",
			Advantage: "В лабораторном испытании преобразование отклоняло повтор украденного псевдонима.",
			Example:   "На LivDet с Bozorth3 при пороге 40 точность снизилась с 0,83 до 0,75; это лабораторное испытание."}
		if g.wrong {
			draft.Advantage = "Преобразование защищает отпечатки, не влияя на точность сопоставления."
		}
		for _, field := range []string{liveClaimSummary, liveClaimAdvantage, liveClaimExample} {
			draft.Evidence = append(draft.Evidence, liveCardSelection{Field: field, Source: 1,
				From: 1, Through: len(payload.Sources[0].Excerpts)})
		}
		response = draft
	} else {
		var review liveCardReview
		for _, source := range payload.Sources {
			for _, claim := range source.Claims {
				verdict := "supported"
				if g.wrong && claim.Field == liveClaimAdvantage {
					verdict = "contradicted"
				}
				fact := "A deterministic keyed transform produces reusable aliases for fingerprint authentication."
				switch claim.Field {
				case liveClaimAdvantage:
					fact = "The transform rejects replay of stolen aliases in this laboratory test, not every biometric attack."
				case liveClaimExample:
					fact = "Matching accuracy decreases after the transform; the experiment demonstrates a trade-off."
				}
				review.Fields = append(review.Fields, liveCardFieldReview{Field: claim.Field, Verdict: verdict,
					From: new(claim.From), Through: new(claim.Through),
					SourceFact: fact,
					Parts: []LiveClaimPart{{Claim: claim.Claim, SourceFact: fact,
						Direct: true, ScopePreserved: true, ModalityPreserved: true}},
					ClaimFact: "Карточка описывает преобразование и результат испытания.",
					Reason:    "Таблица и условия лабораторного испытания определяют границы утверждения."})
				if claim.Field == liveClaimAdvantage {
					review.Fields[len(review.Fields)-1].Benefit = new(true)
				}
				review.Fields[len(review.Fields)-1].Performed = new(claim.Field == liveClaimExample)
			}
		}
		response = review
	}
	raw, err := json.Marshal(response)
	return aiclients.GenResponse{Text: string(raw), Model: "fixture"}, err
}

func TestLiveCardFulltextReachesBothChecksAndPersistsLiteralEvidence(t *testing.T) {
	raw := liveCardFulltextFixture()
	var calls atomic.Int64
	gateway := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if r.URL.Path != "/v1/papers/2511.12739v1/html" {
			t.Errorf("unversioned or arbitrary source request: %s", r.URL.Path)
		}
		liveWriteContextFixture(w, raw, "2511.12739v1")
	}))
	defer gateway.Close()
	t.Setenv("ARXIV_FULLTEXT_URL", gateway.URL)
	ctx := liveWithCardContext(liveFixtureScopeContext(t.Context(), "Биометрическая аутентификация"))
	if liveWithCardContext(ctx) != ctx {
		t.Fatal("a later wave replaced the shared context budget")
	}
	const sourceID = "arxiv:2511.12739"
	byID := map[string]StudyPaper{sourceID: {ID: sourceID, Title: "ProxyPrints", Date: "2025-11-16",
		URL: "https://arxiv.org/abs/2511.12739"}}
	metadata := map[string]LivePaperMetadata{sourceID: {SourceURL: "https://arxiv.org/abs/2511.12739v1",
		Abstract: "Outdated catalogue abstract that belongs to an earlier snapshot."}}
	for _, wrong := range []bool{true, false} {
		t.Run(fmt.Sprintf("wrong=%v", wrong), func(t *testing.T) {
			gen := &liveFulltextTestGenerator{wrong: wrong}
			service := &TrendLiveService{card: NewCandidateCard(gen, func(context.Context) bool { return true })}
			candidate := LiveTopic{PaperIDs: []string{sourceID},
				Qualification: &LiveQualification{Label: "Псевдонимы отпечатков", Mechanism: "Детерминированное преобразование",
					Technology: true, Relevant: true, Coherent: true, Verified: true,
					Evidence: []LiveQualificationEvidence{{SourceID: sourceID, Quote: "Прямое описание механизма",
						SameTechnology: true, Role: "introduces", MechanismQuote: "Прямое описание механизма",
						RelevanceQuote: "Проверка личности", IdentityReason: "Это сам механизм", RelevanceReason: "Это аутентификация"}}}}
			service.writeCard(ctx, "Биометрическая аутентификация", &candidate, byID, metadata)
			if candidate.Card == nil || len(gen.requests) != 2 || len(candidate.Card.SourceContexts) != 1 {
				t.Fatalf("card context pipeline did not run: card=%+v requests=%d limits=%+v",
					candidate.Card, len(gen.requests), candidate.Limits)
			}
			saved := candidate.Card.SourceContexts[0]
			if saved.Coverage != liveCardContextExcerpted || saved.Limitation == "" || saved.ArxivID != "2511.12739v1" ||
				saved.RawSHA256 != liveCardTextHash(raw) || saved.TextSHA256 != liveCardTextHash(saved.Text) ||
				utf8.RuneCountInString(saved.Text) > liveCardContextSourceRunes || saved.BodySHA256 == "" {
				t.Fatalf("false coverage, lost provenance or unbounded context: %+v", saved)
			}
			if strings.Contains(saved.Text, "Invented result") || strings.Contains(saved.Text, "Outdated catalogue") {
				t.Fatal("active HTML or an old abstract became evidence for the fetched version")
			}
			for _, request := range gen.requests {
				var payload liveCardPayload
				if err := json.Unmarshal([]byte(request.Query), &payload); err != nil {
					t.Fatal(err)
				}
				var source strings.Builder
				if len(payload.Sources) > 0 {
					for _, excerpt := range payload.Sources[0].Excerpts {
						source.WriteString(excerpt.Text)
					}
				}
				seen := source.String()
				if seen != saved.Text {
					t.Fatal("writer, verifier and persisted proof saw different source text")
				}
				for _, required := range []string{"without affecting accuracy", "Table III", "Metric Bozorth3 ProxyPrints + Bozorth3",
					"ROC AUC 0.93 0.86", "Accuracy (T=40) 0.83 0.75", "EER Rate 0.14 0.19", "LivDet",
					"Matching accuracy decreases", "do not establish unchanged accuracy on commercial matchers"} {
					if !strings.Contains(seen, required) {
						t.Fatalf("late table, row relation or limitation was omitted: %q", required)
					}
				}
			}
			if got := liveCardComplete(candidate); got == wrong || candidate.Card.Example == "" {
				t.Fatalf("negative verdict or positive case was lost: complete=%v wrong=%v card=%+v", got, wrong, candidate.Card)
			}
			encoded, err := json.Marshal(candidate)
			if err != nil {
				t.Fatal(err)
			}
			var restored LiveTopic
			if err := json.Unmarshal(encoded, &restored); err != nil {
				t.Fatal(err)
			}
			proof := restored.Card.SourceContexts[0]
			for _, evidence := range restored.Card.Evidence {
				if evidence.SourceID != proof.SourceID || !strings.Contains(proof.Text, evidence.Quote) {
					t.Fatal("literal evidence/source binding did not survive JSON persistence")
				}
			}
		})
	}
	if calls.Load() != 1 {
		t.Fatalf("ready batches fetched the same version more than once: %d", calls.Load())
	}
}

func liveWriteContextFixture(w http.ResponseWriter, raw, id string) {
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("X-Source-Media-Type", "text/html")
	w.Header().Set("X-Arxiv-Id", id)
	w.Header().Set("X-Source-Url", "https://arxiv.org/html/"+id)
	w.Header().Set("X-Source-Observed-At", "2026-09-27T00:00:00Z")
	w.Header().Set("X-Source-Sha256", liveCardTextHash(raw))
	_, _ = io.WriteString(w, raw)
}

func TestLiveCardContextQueryBudgetCoalescesAndCancels(t *testing.T) {
	ctx := liveWithCardContext(t.Context())
	budget, ok := ctx.Value(liveCardContextKey{}).(*liveCardContextBudget)
	if !ok {
		t.Fatal("missing run budget")
	}
	var calls, active, peak atomic.Int64
	entered, release := make(chan struct{}, 2), make(chan struct{})
	budget.maxDocuments = 2
	budget.fetch = func(ctx context.Context, id string) (liveCardContextDocument, error) {
		calls.Add(1)
		current := active.Add(1)
		for previous := peak.Load(); current > previous; previous = peak.Load() {
			if peak.CompareAndSwap(previous, current) {
				break
			}
		}
		defer active.Add(-1)
		entered <- struct{}{}
		select {
		case <-release:
			return liveCardContextDocument{arxivID: id, text: "Immutable source body"}, nil
		case <-ctx.Done():
			return liveCardContextDocument{}, ctx.Err()
		}
	}
	var workers sync.WaitGroup
	for range 8 {
		workers.Go(func() {
			if _, err := budget.get(ctx, "2511.12739v1"); err != nil {
				t.Error(err)
			}
		})
	}
	workers.Go(func() { _, _ = budget.get(ctx, "2511.02645v1") })
	<-entered
	<-entered
	close(release)
	workers.Wait()
	if calls.Load() != 2 || peak.Load() > 2 {
		t.Fatalf("per-run coalescing/concurrency broken: calls=%d peak=%d", calls.Load(), peak.Load())
	}
	if _, err := budget.get(ctx, "2511.99999v1"); !errors.Is(err, errCardContextBudget) {
		t.Fatalf("unique-document budget ignored: %v", err)
	}
	for _, mode := range []string{"time", "cancel", "card_reserve"} {
		t.Run(mode, func(t *testing.T) {
			limitedCtx, cancel := context.WithCancel(t.Context())
			defer cancel()
			if mode == "card_reserve" {
				var stop context.CancelFunc
				limitedCtx, stop = context.WithTimeout(limitedCtx, time.Minute)
				defer stop()
			}
			limited := &liveCardContextBudget{entries: map[string]*liveCardContextResult{}, slots: make(chan struct{}, 2),
				remaining: 5 * time.Millisecond, maxDocuments: 32, requestTimeout: time.Second}
			var requested atomic.Int64
			limited.fetch = func(ctx context.Context, _ string) (liveCardContextDocument, error) {
				requested.Add(1)
				if mode == "cancel" {
					cancel()
				}
				<-ctx.Done()
				return liveCardContextDocument{}, ctx.Err()
			}
			_, err := limited.get(limitedCtx, "2511.12739v1")
			if err == nil {
				t.Fatal("deadline/cancellation did not stop reading")
			}
			if mode == "time" {
				_, err = limited.get(limitedCtx, "2511.02645v1")
				if !errors.Is(err, errCardContextBudget) || requested.Load() != 1 {
					t.Fatal("another wave recreated the exhausted time budget")
				}
			}
			if mode == "card_reserve" && requested.Load() != 0 {
				t.Fatal("source loading spent the remaining card-call reserve")
			}
		})
	}
}

func TestLiveCardContextUnavailableKeepsAbstractAndExactBindings(t *testing.T) {
	for _, mode := range []string{"unavailable", "wrong_version", "hash", "mime", "source_mime", "overflow"} {
		t.Run(mode, func(t *testing.T) {
			raw := `<article><h1>Research</h1><p>The laboratory supports a bounded mechanism.</p></article>`
			gateway := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				if mode == "unavailable" {
					w.WriteHeader(http.StatusNotFound)
					return
				}
				id := "2511.12739v1"
				if mode == "wrong_version" {
					id = "2511.12739v2"
				}
				if mode == "overflow" {
					raw = strings.Repeat("x", liveCardContextMaxHTML+1)
				}
				buffer := httptest.NewRecorder()
				liveWriteContextFixture(buffer, raw, id)
				if mode == "hash" {
					buffer.Header().Set("X-Source-Sha256", "wrong")
				}
				if mode == "mime" {
					buffer.Header().Set("Content-Type", "application/pdf")
				}
				if mode == "source_mime" {
					buffer.Header().Set("X-Source-Media-Type", "application/pdf")
				}
				for key, values := range buffer.Header() {
					w.Header()[key] = values
				}
				_, _ = w.Write(buffer.Body.Bytes())
			}))
			defer gateway.Close()
			t.Setenv("ARXIV_FULLTEXT_URL", gateway.URL)
			works := []string{"Original source abstract remains usable.", "Private corpus title"}
			ids := []string{"arxiv:2511.12739", "rec:private"}
			papers := map[string]StudyPaper{ids[0]: {ID: ids[0], URL: "https://arxiv.org/abs/2511.12739v1"},
				ids[1]: {ID: ids[1], URL: "/sources/private"}}
			before := slices.Clone(works)
			contexts := liveCardContexts(liveWithCardContext(t.Context()), works, ids, papers, nil)
			if len(contexts) != 1 || contexts[0].Coverage != liveCardContextAbstract || contexts[0].Limitation == "" ||
				contexts[0].RawSHA256 != "" || !slices.Equal(works, before) || contexts[0].TextSHA256 != liveCardTextHash(works[0]) {
				t.Fatalf("unavailable fulltext invented coverage, changed the abstract or leaked private context: %+v", contexts)
			}
		})
	}
}

func TestLiveCardContextIdentityAndWholeBlocks(t *testing.T) {
	for _, scenario := range []struct {
		id, paperURL, metadataURL, documentID, want string
	}{
		{"arxiv:2511.12739", "https://arxiv.org/abs/2511.12739v1", "https://arxiv.org/abs/2511.12739", "", "2511.12739v1"},
		{"arxiv:2511.12739v1", "https://arxiv.org/abs/2511.12739", "https://arxiv.org/abs/2511.12739v2", "", ""},
		{"arxiv:2511.12739", "https://arxiv.org/abs/2511.12739v1", "https://arxiv.org/abs/2511.02645v1", "", ""},
		{"private", "https://arxiv.org/abs/2511.12739v1", "", "private-document", ""},
		{"untrusted", "https://arxiv.org.evil.example/abs/2511.12739v1", "", "", ""},
	} {
		id, _ := liveCardArxivID(StudyPaper{ID: scenario.id, URL: scenario.paperURL, DocumentID: scenario.documentID},
			LivePaperMetadata{SourceURL: scenario.metadataURL})
		if id != scenario.want {
			t.Fatalf("identity/private-source boundary changed: %+v got %q", scenario, id)
		}
	}
	if _, err := liveParseCardHTML(t.Context(), []byte("<article>"+
		strings.Repeat("<p>many nodes</p>", liveCardContextMaxNodes/2)+"</article>")); err == nil {
		t.Fatal("HTML node budget was ignored")
	}
	document, err := liveParseCardHTML(t.Context(), []byte(liveCardFulltextFixture()))
	if err != nil {
		t.Fatal(err)
	}
	text, spans := liveSelectCardContext(document, 150)
	if strings.Contains(text, "ROC AUC") || strings.Contains(text, "Accuracy (T=40)") {
		t.Fatal("a table too large for its context was split into detached rows")
	}
	body := []rune(document.text)
	for _, span := range spans {
		if span.StartRune < 0 || span.EndRune > len(body) || span.EndRune <= span.StartRune ||
			!strings.Contains(text, string(body[span.StartRune:span.EndRune])) {
			t.Fatal("selected block locator does not bind to its normalized source")
		}
	}
	if utf8.RuneCountInString(text) > 150 {
		t.Fatal("whole-block selection exceeded its cap")
	}
	short, err := liveParseCardHTML(t.Context(), []byte("<article><p>A complete small source body.</p></article>"))
	if err != nil {
		t.Fatal(err)
	}
	selected, ranges := liveSelectCardContext(short, 150)
	if selected != short.text || len(ranges) != 1 || ranges[0].EndRune != utf8.RuneCountInString(short.text) {
		t.Fatal("a complete body was unnecessarily excerpted")
	}

	// URL OAI не содержит версии: новый HTML не наследует прежний текст каталога.
	latest, err := liveParseCardHTML(t.Context(), []byte(`<article><h1>Version two title</h1>`+
		`<div class="ltx_abstract"><p>The new abstract reports an accuracy trade-off.</p></div>`+
		`<section><h2>Results</h2><p>Measured accuracy declines in the new version.</p></section></article>`))
	if err != nil {
		t.Fatal(err)
	}
	latest.arxivID, latest.address = "2511.12739v2", "https://arxiv.org/html/2511.12739v2"
	budget := &liveCardContextBudget{entries: map[string]*liveCardContextResult{}, slots: make(chan struct{}, 2),
		remaining: time.Second, requestTimeout: time.Second, maxDocuments: 1,
		fetch: func(context.Context, string) (liveCardContextDocument, error) { return latest, nil }}
	proof := LiveCardSourceContext{ArxivID: "2511.12739", Coverage: liveCardContextAbstract,
		Text: "Old title: the outdated abstract claims unchanged accuracy."}
	liveExpandCardContext(t.Context(), budget, &proof, liveCardContextSourceRunes)
	if proof.ArxivID != latest.arxivID || proof.Coverage != liveCardContextFull ||
		proof.Text != latest.text || strings.Contains(proof.Text, "outdated") {
		t.Fatalf("old unversioned snapshot was attributed to the newly fetched version: %+v", proof)
	}
}

func TestLiveCardContextSharesReadingAcrossCardsAndProviders(t *testing.T) {
	t.Setenv("ARXIV_FULLTEXT_URL", "http://gateway.test")
	papers := []StudyPaper{{ID: "a", URL: "https://arxiv.org/abs/2511.12739"}, {ID: "b"}, {ID: "c"}, {ID: "d"}}
	metadata := map[string]LivePaperMetadata{"b": {DOI: "10.1234/b", Genre: "article"},
		"c": {DOI: "10.1234/c", Genre: "article"}, "d": {DOI: "10.1234/d", Genre: "article"}}
	synctest.Test(t, func(t *testing.T) {
		ctx := liveWithCardContext(t.Context())
		liveAllocateCardContext(ctx, papers, metadata)
		budget, ok := ctx.Value(liveCardContextKey{}).(*liveCardContextBudget)
		if !ok {
			t.Fatal("missing budget")
		}
		budget.requestTimeout = time.Minute
		budget.fetch = func(ctx context.Context, id string) (liveCardContextDocument, error) {
			if strings.HasPrefix(id, "doi:") {
				<-ctx.Done()
				return liveCardContextDocument{}, ctx.Err()
			}
			return liveCardContextDocument{text: "A complete paper with its experimental limitations."}, nil
		}
		_, _ = budget.get(ctx, "doi:10.1234/b")
		if _, err := budget.get(ctx, "doi:10.1234/c"); !errors.Is(err, errCardContextBudget) {
			t.Fatalf("journal quota ignored: %v", err)
		}
		if _, err := budget.get(ctx, "2511.12739"); err != nil {
			t.Fatalf("journals exhausted the preprint reserve: %v", err)
		}
	})
	ctx := liveWithCardContext(t.Context())
	liveAllocateCardContext(ctx, papers[1:], metadata)
	budget, ok := ctx.Value(liveCardContextKey{}).(*liveCardContextBudget)
	if !ok || budget.partitioned {
		t.Fatal("single-provider corpus lost part of its budget")
	}
	var calls int
	budget.fetch = func(context.Context, string) (liveCardContextDocument, error) {
		calls++
		return liveCardContextDocument{text: "A complete paper with its experimental limitations."}, nil
	}
	byID := map[string]StudyPaper{"b": papers[1], "c": papers[2], "d": papers[3]}
	works := []string{"Abstract B", "Abstract C", "Abstract D"}
	contexts := liveCardContexts(ctx, works, []string{"b", "c", "d"}, byID, metadata)
	if calls != 1 || contexts[0].Coverage != liveCardContextFull || contexts[1].Coverage != liveCardContextAbstract ||
		contexts[1].Limitation == "" || works[2] != "Abstract D" {
		t.Fatal("one card consumed other cards' reading allowance")
	}
}
