package application

import (
	"encoding/json/v2"
	"errors"
	"html"
	"strings"
	"time"
)

type liveDataCiteWork struct {
	DOI    string `json:"doi"`
	Titles []struct {
		Title string `json:"title"`
	} `json:"titles"`
	Creators []struct {
		Name string `json:"name"`
	} `json:"creators"`
	Dates []struct {
		Date string `json:"date"`
		Type string `json:"dateType"`
	} `json:"dates"`
	Descriptions []struct {
		Text string `json:"description"`
	} `json:"descriptions"`
	Types struct {
		General string `json:"resourceTypeGeneral"`
	} `json:"types"`
	Language string `json:"language"`
	Related  []struct {
		ID       string `json:"relatedIdentifier"`
		Type     string `json:"relatedIdentifierType"`
		Relation string `json:"relationType"`
	} `json:"relatedIdentifiers"`
}

func liveDataCiteRecords(body []byte) ([]LiveSourceRecord, int, error) {
	var response struct {
		Data []struct {
			Attributes liveDataCiteWork `json:"attributes"`
		} `json:"data"`
		Meta *struct {
			Total int `json:"total"`
		} `json:"meta"`
	}
	if json.Unmarshal(body, &response) != nil || response.Meta == nil || response.Meta.Total < len(response.Data) ||
		len(response.Data) > liveResearchPage {
		return nil, 0, errors.New("неверный ответ DataCite")
	}
	records := make([]LiveSourceRecord, 0, len(response.Data))
	for _, item := range response.Data {
		if record, ok := item.Attributes.record(); ok {
			records = append(records, record)
		}
	}

	return records, response.Meta.Total, nil
}

func (work liveDataCiteWork) record() (LiveSourceRecord, bool) {
	if len(work.Titles) == 0 || liveResearchDOI(work.DOI) == "" {
		return LiveSourceRecord{}, false
	}
	switch work.Types.General {
	case "Dataset", "Software", "JournalArticle", "Preprint":
	default:
		return LiveSourceRecord{}, false
	}
	date := ""
	for _, value := range work.Dates {
		if value.Type == "Issued" {
			date = liveResearchDay(value.Date)
			break
		}
	}
	// Год и дата регистрации DOI не задают день публикации и не участвуют в помесячном ряду.
	if date == "" {
		return LiveSourceRecord{}, false
	}
	record := liveResearchRecord("datacite:"+work.DOI, work.Titles[0].Title, date, work.DOI,
		liveDataCiteName, strings.ToLower(work.Types.General))
	if record.Genre == "journalarticle" {
		record.Genre = liveGenreArticle
	}
	for _, relation := range work.Related {
		if relation.Type == "DOI" && (relation.Relation == "IsVersionOf" || relation.Relation == "HasVersion" ||
			relation.Relation == "IsIdenticalTo") {
			record.DOIAliases = append(record.DOIAliases, relation.ID)
		}
	}
	record.Language = work.Language
	for _, author := range work.Creators {
		record.Authors = append(record.Authors, author.Name)
	}
	for _, description := range work.Descriptions {
		record.Excerpt += " " + liveResearchText(description.Text)
	}
	return record, record.Title != "" && !liveResearchAuxiliary(record.Title)
}

type liveOpenAIREWork struct {
	ID           string   `json:"id"`
	Title        string   `json:"mainTitle"`
	Date         string   `json:"publicationDate"`
	Type         string   `json:"type"`
	Descriptions []string `json:"descriptions"`
	Authors      []struct {
		Name string `json:"fullName"`
	} `json:"authors"`
	PIDs []struct {
		Scheme string `json:"scheme"`
		Value  string `json:"value"`
	} `json:"pids"`
	Language struct {
		Code string `json:"code"`
	} `json:"language"`
	Instances []struct {
		Type string `json:"type"`
	} `json:"instances"`
}

func liveOpenAIRERecords(body []byte) ([]LiveSourceRecord, int, error) {
	var response struct {
		Header *struct {
			Total int `json:"numFound"`
		} `json:"header"`
		Results []liveOpenAIREWork `json:"results"`
	}
	if json.Unmarshal(body, &response) != nil || response.Header == nil || response.Header.Total < len(response.Results) ||
		len(response.Results) > liveResearchPage {
		return nil, 0, errors.New("неверный ответ OpenAIRE")
	}
	records := make([]LiveSourceRecord, 0, len(response.Results))
	for _, work := range response.Results {
		if record, ok := work.record(); ok {
			records = append(records, record)
		}
	}

	return records, response.Header.Total, nil
}

func (work liveOpenAIREWork) record() (LiveSourceRecord, bool) {
	date := liveResearchDay(work.Date)
	if work.ID == "" || date == "" || liveResearchAuxiliary(work.Title) {
		return LiveSourceRecord{}, false
	}
	if len(work.Instances) > 0 && work.Instances[0].Type == "Review" {
		return LiveSourceRecord{}, false
	}
	dois := []string{}
	for _, pid := range work.PIDs {
		if pid.Scheme == "doi" && liveResearchDOI(pid.Value) != "" {
			dois = append(dois, pid.Value)
		}
	}
	doi := ""
	if len(dois) > 0 {
		doi = dois[0]
	}
	record := liveResearchRecord("openaire:"+work.ID, work.Title, date, doi, liveOpenAIREName, work.Type)
	record.DOIAliases = dois
	if len(work.Instances) > 0 && work.Instances[0].Type == "Article" {
		record.Genre = liveGenreArticle
	}
	if doi == "" {
		record.URL = "https://explore.openaire.eu/search/result?id=" + work.ID
	}
	record.Language = work.Language.Code
	for _, author := range work.Authors {
		record.Authors = append(record.Authors, author.Name)
	}
	record.Excerpt = liveResearchText(strings.Join(work.Descriptions, " "))
	return record, record.Title != ""
}

func liveResearchRecord(id, title, date, doi, source, genre string) LiveSourceRecord {
	return LiveSourceRecord{ID: id, Title: liveResearchText(title), Date: date, PublishedAt: date,
		URL: "https://doi.org/" + doi, DOI: doi, Source: source, Genre: genre,
		PublisherType: PublisherAggregator, Trust: TrustMedium,
		TrustBasis: "библиографическая запись; рецензирование и внедрение не подтверждены",
		DateBasis:  "publication_date", TextStatus: "metadata_only"}
}

func liveResearchDay(value string) string {
	if len(value) < 10 {
		return ""
	}
	day := value[:10]
	if _, err := time.Parse(time.DateOnly, day); err != nil {
		return ""
	}
	return day
}

// Письма рецензентов — документы одной работы, а не повторение технологии независимыми авторами.
func liveResearchAuxiliary(title string) bool {
	for _, prefix := range []string{"review for ", "peer review", "decision letter", "author response",
		"response to reviewer", "reply to reviewer"} {
		if strings.HasPrefix(strings.ToLower(strings.TrimSpace(title)), prefix) {
			return true
		}
	}
	return false
}

func liveResearchText(value string) string {
	var text strings.Builder
	inTag := false
	for _, r := range value {
		switch r {
		case '<':
			inTag = true
			text.WriteByte(' ')
		case '>':
			inTag = false
		default:
			if !inTag {
				text.WriteRune(r)
			}
		}
	}
	return strings.Join(strings.Fields(html.UnescapeString(text.String())), " ")
}
