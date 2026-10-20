package application

import (
	"cmp"
	"context"
	"encoding/json/v2"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"slices"
	"strings"
	"time"
)

const (
	liveEuropePMCBase   = "https://www.ebi.ac.uk/europepmc/webservices/rest"
	livePaperArticle    = "article"
	livePaperConference = "conference-paper"
	livePaperDOIField   = "doi"
	livePaperJSON       = "json"
	livePaperQuery      = "query"
	livePaperDisabled   = "false"
)

var livePaperDOIPattern = regexp.MustCompile(`^10\.[0-9]{4,9}/[^\s"<>?#\\]+$`)
var livePaperPMCIDPattern = regexp.MustCompile(`^PMC[1-9][0-9]{0,15}$`)

// Приватный документ и набор данных не становятся публичной статьёй из-за похожего идентификатора.
func liveCardDOI(paper StudyPaper, meta LivePaperMetadata) string {
	if paper.DocumentID != "" || paper.SnapshotID != "" || !slices.Contains(
		[]string{livePaperArticle, "journal-article", "research-article", liveMLExtractPreprint,
			livePaperConference, "proceedings-article"}, meta.Genre) {
		return ""
	}
	doi := liveNormalizePaperDOI(meta.DOI)
	if canonical := liveNormalizePaperDOI(paper.CanonicalID); canonical != "" && canonical != doi {
		return ""
	}
	return doi
}

func liveNormalizePaperDOI(raw string) string {
	doi := strings.ToLower(strings.TrimSpace(raw))
	for _, prefix := range []string{"https://doi.org/", "http://doi.org/", "doi:"} {
		doi = strings.TrimPrefix(doi, prefix)
	}
	if !livePaperDOIPattern.MatchString(doi) {
		return ""
	}
	return doi
}

type livePaperTextClient struct {
	http      *http.Client
	pmc       string
	unpaywall string
	email     string
}

func liveCardSourceFetcher(gateway string) func(context.Context, string) (liveCardContextDocument, error) {
	arxiv := liveCardHTMLFetcher(gateway)
	client := livePublisherClient()
	client.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		if len(via) >= 3 || !livePaperPublicURL(req.URL) {
			return errors.New("paper redirect refused")
		}
		return nil
	}
	papers := &livePaperTextClient{http: client, pmc: liveEuropePMCBase, unpaywall: "https://api.unpaywall.org/v2",
		email: strings.TrimSpace(cmp.Or(os.Getenv("UNPAYWALL_EMAIL"), os.Getenv("PUBSEARCH_MAILTO")))}
	return func(ctx context.Context, key string) (liveCardContextDocument, error) {
		if doi, ok := strings.CutPrefix(key, "doi:"); ok {
			if os.Getenv("RADAR_PAPER_CONTEXTS") == livePaperDisabled {
				return liveCardContextDocument{}, errors.New("journal full texts disabled")
			}
			if !livePaperDOIPattern.MatchString(doi) {
				return liveCardContextDocument{}, errors.New("invalid paper DOI")
			}
			return papers.fetch(ctx, doi)
		}
		return arxiv(ctx, key)
	}
}

func livePaperPublicURL(u *url.URL) bool {
	return u.Scheme == liveCardSecureScheme && u.Hostname() != "" && u.User == nil && (u.Port() == "" || u.Port() == "443")
}

func (c *livePaperTextClient) read(ctx context.Context, address string) ([]byte, string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, address, http.NoBody)
	if err != nil {
		return nil, "", errors.New("invalid paper request")
	}
	req.Header.Set("Accept", "application/json, application/xml, text/html")
	req.Header.Set("User-Agent", "rag-test-research/1.0")
	response, err := c.http.Do(req)
	if err != nil {
		return nil, "", errors.New("paper provider unavailable")
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode != http.StatusOK {
		return nil, "", fmt.Errorf("paper provider HTTP %d", response.StatusCode)
	}
	raw, err := io.ReadAll(io.LimitReader(response.Body, liveCardContextMaxHTML+1))
	if err != nil || len(raw) > liveCardContextMaxHTML {
		return nil, "", errors.New("paper response exceeds bounds or is incomplete")
	}
	return raw, response.Request.URL.String(), nil
}

func (c *livePaperTextClient) fetch(ctx context.Context, doi string) (liveCardContextDocument, error) {
	// Медленный каталог не должен съесть всё время, отведённое на открытую копию.
	pmcContext, cancel := context.WithTimeout(ctx, 4*time.Second)
	document, pmcErr := c.europePMC(pmcContext, doi)
	cancel()
	if pmcErr == nil {
		return document, nil
	}
	if c.email == "" || ctx.Err() != nil {
		return liveCardContextDocument{}, pmcErr
	}
	document, err := c.openCopy(ctx, doi)
	if err != nil {
		return liveCardContextDocument{}, errors.Join(pmcErr, err)
	}
	return document, nil
}

func (c *livePaperTextClient) europePMC(ctx context.Context, doi string) (liveCardContextDocument, error) {
	query := url.Values{livePaperQuery: {`DOI:"` + doi + `"`}, "format": {livePaperJSON}, "resultType": {"core"}, "pageSize": {"2"}}
	lookup := c.pmc + "/search?" + query.Encode()
	raw, _, err := c.read(ctx, lookup)
	if err != nil {
		return liveCardContextDocument{}, err
	}
	var result struct {
		Count *int `json:"hitCount"`
		List  struct {
			Works []struct {
				DOI   string `json:"doi"`
				PMCID string `json:"pmcid"`
				Open  string `json:"isOpenAccess"`
			} `json:"result"`
		} `json:"resultList"`
	}
	if json.Unmarshal(raw, &result) != nil || result.Count == nil || *result.Count < 0 {
		return liveCardContextDocument{}, errors.New("invalid Europe PMC response")
	}
	for _, work := range result.List.Works {
		if !strings.EqualFold(work.DOI, doi) || work.Open != "Y" || !livePaperPMCIDPattern.MatchString(work.PMCID) {
			continue
		}
		body, address, err := c.read(ctx, c.pmc+"/"+work.PMCID+"/fullTextXML")
		if err != nil {
			return liveCardContextDocument{}, err
		}
		document, err := liveParsePaperJATS(ctx, body, doi)
		if err != nil {
			return liveCardContextDocument{}, err
		}
		document.address, document.rawHash, document.observed = address, liveCardTextHash(string(body)), time.Now().UTC()
		document.provider, document.lookupURL, document.lookupHash = "europepmc", lookup, liveCardTextHash(string(raw))
		document.parser = "jats-card-blocks-v1"
		return document, nil
	}
	return liveCardContextDocument{}, errors.New("europepmc has no matching open full text")
}
