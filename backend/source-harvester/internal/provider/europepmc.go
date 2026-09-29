package provider

import (
	"context"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"slices"
	"strings"
	"time"
)

// europePMC обогащает только точный DOI Crossref: провенанс поиска не меняет и похожую по названию статью не подставляет.
type europePMC struct {
	base    string
	fetcher *fetcher
}

func newEuropePMC() *europePMC {
	f := newFetcher(time.Second)
	f.accept = "*/*" // Поиск отдаёт JSON, а fullTextXML на Accept только с JSON отвечает 406.
	return &europePMC{base: "https://www.ebi.ac.uk/europepmc/webservices/rest", fetcher: f}
}

type europeWork struct {
	DOI      string `json:"doi"`
	PMCID    string `json:"pmcid"`
	Abstract string `json:"abstractText"`
	Open     string `json:"isOpenAccess"`
}

type paperEnrichment struct {
	Crossref jsontext.Value `json:"crossref"`
	Lookup   jsontext.Value `json:"europepmc_lookup"`
	URL      string         `json:"full_text_url,omitempty"`
	SHA256   string         `json:"full_text_sha256,omitempty"`
	XML      string         `json:"full_text_xml,omitempty"`
	Observed time.Time      `json:"observed_at"`
}

var pmcidPattern = regexp.MustCompile(`^PMC[1-9][0-9]{0,15}$`)

func (e *europePMC) enrich(ctx context.Context, record *Record) error {
	query := `DOI:"` + strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace(record.Source.NativeID) + `"`
	q := url.Values{"query": {query}, "format": {"json"}, "resultType": {"core"}, "pageSize": {"2"}}
	raw, observed, err := e.fetcher.get(ctx, e.base+"/search?"+q.Encode())
	if err != nil {
		return err // Пока провайдер недоступен, курсор не двигается.
	}
	var response struct {
		Total *int `json:"hitCount"`
		List  struct {
			Records []europeWork `json:"result"`
		} `json:"resultList"`
	}
	if err = json.Unmarshal(raw, &response); err != nil || response.Total == nil || *response.Total < 0 {
		return errors.New("invalid Europe PMC DOI lookup response")
	}
	evidence := paperEnrichment{Crossref: record.Raw, Lookup: raw, Observed: observed}
	record.Source.ObservedAt = observed
	match := slices.IndexFunc(response.List.Records, func(w europeWork) bool {
		return strings.EqualFold(strings.TrimSpace(w.DOI), record.Source.NativeID)
	})
	if match < 0 {
		record.Limitations = append(record.Limitations, "europepmc_doi_not_found")
	} else if err = e.enrichMatched(ctx, record, response.List.Records[match], &evidence); err != nil {
		return err
	}
	record.Raw, err = json.Marshal(evidence)
	if err != nil {
		return fmt.Errorf("encode paper enrichment: %w", err)
	}
	record.Source.ContentHash = digest([]byte(record.Text))
	record.Source.Revision = paperRevision(record.Text, record.Source.Publication)
	return record.Source.Validate()
}

func (e *europePMC) enrichMatched(ctx context.Context, record *Record, work europeWork, evidence *paperEnrichment) error {
	if record.TextStatus == "title_only" {
		abstract := stripJATS(work.Abstract)
		record.Text, record.TextStatus, record.Source.Sections = paperText(record.Title, abstract)
		if abstract != "" {
			record.Limitations = slices.DeleteFunc(record.Limitations, func(s string) bool { return s == "abstract_unavailable" })
		}
	}
	if work.Open != "Y" || !pmcidPattern.MatchString(work.PMCID) {
		record.Limitations = append(record.Limitations, "europepmc_open_full_text_unavailable")
		return nil
	}
	evidence.URL = e.base + "/" + work.PMCID + "/fullTextXML"
	raw, observed, err := e.fetcher.get(ctx, evidence.URL)
	if err != nil {
		if status, ok := errors.AsType[*HTTPError](err); ok && status.Status == http.StatusNotFound {
			record.Limitations = append(record.Limitations, "europepmc_full_text_not_found")
			return nil
		}
		if errors.Is(err, ErrResponseTooLarge) {
			record.Limitations = append(record.Limitations, "europepmc_full_text_exceeds_8_mib")
			return nil
		}
		return err
	}
	evidence.XML, evidence.SHA256, evidence.Observed = string(raw), digest(raw), observed
	record.Source.ObservedAt = observed
	paper, err := parseJATSPaper(raw, record.Source.NativeID, record.Title)
	if err != nil {
		record.Limitations = append(record.Limitations, "europepmc_full_text_rejected: "+err.Error())
		return nil //nolint:nilerr // Metadata fallback is explicit; rejected XML and reason stay in the manifest.
	}
	record.Text, record.TextStatus = paper.Text, "full_text"
	record.Source.Sections, record.Source.Anchors = paper.Sections, paper.Anchors
	if len(paper.Language) == 2 {
		record.Source.Language, record.Source.LanguageBasis = paper.Language, "Europe PMC JATS article/@xml:lang"
	}
	record.ContentURL = evidence.URL
	record.Limitations = slices.DeleteFunc(record.Limitations, func(s string) bool {
		return s == "full_text_not_fetched" || s == "abstract_unavailable"
	})
	record.Limitations = append(record.Limitations,
		"jats_text_only; embedded text and table cells retained; images, formula layout and supplements not rendered")
	return nil
}
