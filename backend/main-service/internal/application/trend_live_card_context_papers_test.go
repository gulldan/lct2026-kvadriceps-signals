package application

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
)

type livePaperTransport func(*http.Request) (*http.Response, error)

func (f livePaperTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	return f(request)
}

func TestLivePaperContextProvenanceAndFallback(t *testing.T) {
	const doi = "10.1234/paper"
	jats := `<article><front><article-meta><article-id pub-id-type="doi">` + doi + `</article-id>` +
		`<title-group><article-title>Laboratory trial</article-title></title-group>` +
		`<abstract><p>A promising laboratory result.</p></abstract></article-meta></front>` +
		`<body><sec id="results"><title id="heading">Results</title><p>Accuracy fell from 83 to 75 percent.</p>` +
		`<table-wrap><table><tr><th>Baseline</th><th>Method</th></tr><tr><td>83</td><td>75</td></tr></table></table-wrap>` +
		`</sec><sec><title>Limitations</title><p>The trial does not establish clinical benefit.</p></sec></body></article>`
	html := `<html><head><meta name="citation_doi" content="` + doi + `"></head><body>` +
		liveCardFulltextFixture() + `</body></html>`
	for _, test := range []struct {
		name, xml, html, provider string
	}{
		{name: "Europe PMC", xml: jats, provider: "europepmc"},
		{name: "Unpaywall fallback", xml: strings.ReplaceAll(jats, doi, "10.1234/other"), html: html, provider: "unpaywall"},
		{name: "wrong DOI", xml: strings.ReplaceAll(jats, doi, "10.1234/other"), html: strings.ReplaceAll(html, doi, "10.1234/other")},
		{name: "abstract only", xml: `<article/>`, html: `<meta name="citation_doi" content="` + doi + `">` +
			`<article><p>Only an abstract.</p></article>`},
		{name: "PDF is not HTML", xml: `<article/>`, html: `%PDF-1.7`},
		{name: "entities rejected", xml: `<!DOCTYPE article [<!ENTITY external SYSTEM "file:///private">]>` + jats},
	} {
		t.Run(test.name, func(t *testing.T) {
			client := &livePaperTextClient{pmc: liveEuropePMCBase, unpaywall: "https://api.unpaywall.org/v2", email: "private@example.org"}
			client.http = &http.Client{Transport: livePaperTransport(func(request *http.Request) (*http.Response, error) {
				body := ""
				switch request.URL.Hostname() {
				case "www.ebi.ac.uk":
					if strings.HasSuffix(request.URL.Path, "/search") {
						if request.URL.Query().Get("query") != `DOI:"`+doi+`"` {
							t.Error("lookup did not bind the exact DOI")
						}
						body = `{"hitCount":1,"resultList":{"result":[{"doi":"` + doi + `","pmcid":"PMC123","isOpenAccess":"Y"}]}}`
					} else {
						body = test.xml
					}
				case "api.unpaywall.org":
					if request.URL.Query().Get("email") != client.email {
						t.Error("Unpaywall contact missing")
					}
					body = `{"doi":"` + doi + `","is_oa":true,"best_oa_location":{"url_for_landing_page":"https://papers.example/article"}}`
				case "papers.example":
					body = test.html
				default:
					t.Error("unexpected destination")
				}
				return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(body)),
					Header: make(http.Header), Request: request}, nil
			})}
			ctx := liveWithCardContext(t.Context())
			budget, ok := ctx.Value(liveCardContextKey{}).(*liveCardContextBudget)
			if !ok {
				t.Fatal("missing context budget")
			}
			budget.fetch = func(ctx context.Context, key string) (liveCardContextDocument, error) {
				if key != "doi:"+doi {
					t.Fatalf("wrong lookup key: %s", key)
				}
				return client.fetch(ctx, doi)
			}
			works := []string{"Original catalogue abstract"}
			contexts := liveCardContexts(ctx, works, []string{"paper"}, map[string]StudyPaper{"paper": {ID: "paper"}},
				map[string]LivePaperMetadata{"paper": {DOI: doi, Genre: "article"}})
			if len(contexts) != 1 || contexts[0].DOI != doi || contexts[0].TextSHA256 != liveCardTextHash(works[0]) {
				t.Fatalf("lost DOI, context or content hash: %+v", contexts)
			}
			saved := contexts[0]
			if test.provider == "" {
				if saved.Coverage != liveCardContextAbstract || works[0] != "Original catalogue abstract" || saved.Limitation == "" {
					t.Fatalf("unreadable or unrelated article became full text: %+v", saved)
				}
				return
			}
			if saved.Provider != test.provider || saved.Coverage == liveCardContextAbstract || len(saved.Ranges) == 0 ||
				saved.RawSHA256 == "" || saved.LookupSHA256 == "" || saved.ObservedAt.IsZero() ||
				strings.Contains(fmt.Sprint(saved), client.email) || strings.Contains(works[0], "Original catalogue") {
				t.Fatalf("source provenance lost or private contact exposed: %+v", saved)
			}
		})
	}
	if got := liveCardDOI(StudyPaper{DocumentID: "private"}, LivePaperMetadata{DOI: doi, Genre: "article"}); got != "" {
		t.Fatal("private document entered public DOI lookup")
	}
	if got := liveCardDOI(StudyPaper{CanonicalID: "10.1234/other"}, LivePaperMetadata{DOI: doi, Genre: "article"}); got != "" {
		t.Fatal("conflicting catalogue identity entered public DOI lookup")
	}
}
