package main

import (
	"cmp"
	"encoding/json/v2"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"
)

const (
	genreArticle  = "article"
	genrePreprint = "preprint"
)

func publicationRows(raw []byte, kind, cursor string) (publicationPage, error) {
	var page publicationPage
	var err error
	switch kind {
	case kindEuropePMC:
		page, err = europePMCRows(raw)
	case kindCrossref:
		page, err = crossrefRows(raw)
	case kindClinicalTrials:
		page, err = clinicalTrialRows(raw)
	case kindBiorxiv:
		page, err = biorxivRows(raw, cursor)
	case kindDataCite:
		page, err = dataCiteRows(raw, cursor)
	case kindZenodo:
		page, err = zenodoRows(raw, cursor)
	case kindFigshare:
		page, err = figshareRows(raw, cursor)
	case kindBioStudies:
		page, err = bioStudiesRows(raw)
	default:
		return publicationPage{}, errors.New("unsupported publication response")
	}
	if err == nil && page.total != nil && *page.total < page.returned {
		return publicationPage{}, errors.New("provider total is smaller than its returned publication page")
	}
	return page, err
}

// Аннотация реестра остаётся аннотацией независимо от длины; она не доказывает доступность полного текста.
func publicationRecord(id, title, text, day, doi, genre, dateBasis string) (record, bool) {
	title = strings.Join(strings.Fields(textHTML(title)), " ")
	text = strings.TrimSpace(textHTML(text))
	day = publicationDay(day)
	if title == "" || day == "" || id == "" {
		return record{}, false
	}
	status := metadataOnlyText
	if text != "" {
		status = feedAbstractText
	}
	doi = publicationDOI(doi)
	canonical := id
	if doi != "" {
		canonical = "https://doi.org/" + doi
	}
	return record{ID: canonical, URL: id, CanonicalURL: canonical, DOI: doi, Genre: genre,
		Title: title, Text: text, Date: day, PublishedAt: day, DateBasis: dateBasis,
		TextStatus: status, TitleBasis: "provider bibliographic title", ParserVersion: publicationVersion}, true
}

func publicationDay(value string) string {
	if len(value) < len(time.DateOnly) {
		return ""
	}
	day := value[:len(time.DateOnly)]
	if _, err := time.Parse(time.DateOnly, day); err != nil {
		return ""
	}
	return day
}

func publicationDOI(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	for _, prefix := range []string{"https://doi.org/", "http://doi.org/", "doi:"} {
		value = strings.TrimPrefix(value, prefix)
	}
	if !strings.HasPrefix(value, "10.") || !strings.Contains(value, "/") ||
		strings.ContainsAny(value, " \r\n\t?#") {
		return ""
	}
	return value
}

// Версии набора и его запись в нескольких каталогах не дают независимых подтверждений сигнала.
func publicationDuplicate(row record, seen map[string]bool) bool {
	keys := []string{row.ID}
	for _, doi := range append([]string{row.DOI}, row.DOIAliases...) {
		if doi = publicationDOI(doi); doi != "" {
			keys = append(keys, "doi:"+doi)
		}
	}
	duplicate := false
	for _, key := range keys {
		duplicate = duplicate || seen[key]
	}
	for _, key := range keys {
		seen[key] = true
	}
	return duplicate
}

func europePMCRows(raw []byte) (publicationPage, error) {
	var response struct {
		Total *int   `json:"hitCount"`
		Error *int   `json:"errCode"`
		Next  string `json:"nextCursorMark"`
		List  *struct {
			Results []struct {
				ID       string `json:"id"`
				Source   string `json:"source"`
				DOI      string `json:"doi"`
				Title    string `json:"title"`
				Abstract string `json:"abstractText"`
				Date     string `json:"firstPublicationDate"`
			} `json:"result"`
		} `json:"resultList"`
	}
	if err := json.Unmarshal(raw, &response); err != nil || response.Error != nil || response.List == nil ||
		response.Total == nil || *response.Total < 0 {
		return publicationPage{}, errors.New("invalid Europe PMC response")
	}
	page := publicationPage{total: response.Total, returned: len(response.List.Results), next: response.Next}
	for _, work := range response.List.Results {
		address := "https://europepmc.org/article/" + url.PathEscape(work.Source) + "/" + url.PathEscape(work.ID)
		if work.ID == "" || work.Source == "" {
			continue
		}
		genre := genreArticle
		if work.Source == "PPR" {
			genre = genrePreprint
		}
		if work.Source == "MED" {
			address = "https://pubmed.ncbi.nlm.nih.gov/" + url.PathEscape(work.ID) + "/"
		}
		if row, ok := publicationRecord(address, work.Title, work.Abstract, work.Date, work.DOI, genre,
			"Europe PMC firstPublicationDate"); ok {
			page.rows = append(page.rows, row)
		}
	}
	return page, nil
}

type crossrefDate struct {
	Parts [][]int `json:"date-parts"`
}

func (date crossrefDate) day() string {
	if len(date.Parts) == 0 || len(date.Parts[0]) < 3 {
		return ""
	}
	p := date.Parts[0]
	return publicationDay(fmt.Sprintf("%04d-%02d-%02d", p[0], p[1], p[2]))
}

func crossrefRows(raw []byte) (publicationPage, error) {
	var response struct {
		Status  string `json:"status"`
		Message struct {
			Total *int   `json:"total-results"`
			Next  string `json:"next-cursor"`
			Items []struct {
				DOI       string       `json:"DOI"`
				Title     []string     `json:"title"`
				Abstract  string       `json:"abstract"`
				Online    crossrefDate `json:"published-online"`
				Print     crossrefDate `json:"published-print"`
				Published crossrefDate `json:"published"`
				Language  string       `json:"language"`
				Type      string       `json:"type"`
			} `json:"items"`
		} `json:"message"`
	}
	if err := json.Unmarshal(raw, &response); err != nil || response.Status != "ok" ||
		response.Message.Total == nil || *response.Message.Total < 0 {
		return publicationPage{}, errors.New("invalid Crossref response")
	}
	message := response.Message
	page := publicationPage{total: message.Total, returned: len(message.Items), next: message.Next}
	if len(message.Items) < publicationPageSize {
		page.next = ""
	}
	for _, work := range message.Items {
		doi := publicationDOI(work.DOI)
		if len(work.Title) == 0 || doi == "" {
			continue
		}
		day := cmp.Or(work.Online.day(), work.Print.day(), work.Published.day())
		genre := genreArticle
		if work.Type == "posted-content" {
			genre = genrePreprint
		}
		row, ok := publicationRecord("https://doi.org/"+doi, work.Title[0], work.Abstract, day, doi, genre,
			"Crossref published-online, published-print or published; exact day only")
		if ok {
			row.Language, row.LanguageBasis = feedLanguage(work.Language)
			page.rows = append(page.rows, row)
		}
	}
	return page, nil
}

func clinicalTrialRows(raw []byte) (publicationPage, error) {
	var response struct {
		Total   *int   `json:"totalCount"`
		Next    string `json:"nextPageToken"`
		Studies []struct {
			Protocol struct {
				Identification struct {
					ID    string `json:"nctId"`
					Title string `json:"briefTitle"`
				} `json:"identificationModule"`
				Status struct {
					FirstPost struct {
						Date string `json:"date"`
						Type string `json:"type"`
					} `json:"studyFirstPostDateStruct"`
				} `json:"statusModule"`
				Description struct {
					Summary string `json:"briefSummary"`
				} `json:"descriptionModule"`
			} `json:"protocolSection"`
		} `json:"studies"`
	}
	if err := json.Unmarshal(raw, &response); err != nil || response.Total == nil || *response.Total < 0 {
		return publicationPage{}, errors.New("invalid ClinicalTrials.gov response")
	}
	page := publicationPage{total: response.Total, returned: len(response.Studies), next: response.Next}
	for _, item := range response.Studies {
		trial := item.Protocol
		if !strings.HasPrefix(trial.Identification.ID, "NCT") || trial.Status.FirstPost.Type != "ACTUAL" {
			continue
		}
		row, ok := publicationRecord("https://clinicaltrials.gov/study/"+url.PathEscape(trial.Identification.ID),
			trial.Identification.Title, trial.Description.Summary, trial.Status.FirstPost.Date, "", "clinical_trial",
			"ClinicalTrials.gov studyFirstPostDateStruct; actual registry publication, not trial start")
		if ok {
			row.Limitations = []string{"trial registration is not a published outcome or proof of clinical efficacy"}
			page.rows = append(page.rows, row)
		}
	}
	return page, nil
}
