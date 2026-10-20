package main

import (
	"encoding/json/v2"
	"errors"
	"strconv"
	"strings"
)

// В ответах сервера число результатов бывает строкой, а счётчик страницы — числом.
type publicationCount int

func (count *publicationCount) UnmarshalJSON(raw []byte) error {
	value, err := strconv.Atoi(strings.Trim(string(raw), `"`))
	if err != nil || value < 0 {
		return errors.New("invalid publication count")
	}
	*count = publicationCount(value)
	return nil
}

func biorxivRows(raw []byte, cursor string) (publicationPage, error) {
	var response struct {
		Messages []struct {
			Status string           `json:"status"`
			Total  publicationCount `json:"total"`
		} `json:"messages"`
		Collection []struct {
			DOI       string `json:"doi"`
			Title     string `json:"title"`
			Abstract  string `json:"abstract"`
			Date      string `json:"date"`
			Version   string `json:"version"`
			Published string `json:"published"`
		} `json:"collection"`
	}
	if err := json.Unmarshal(raw, &response); err != nil || len(response.Messages) == 0 ||
		response.Messages[0].Status != "ok" {
		return publicationPage{}, errors.New("invalid bioRxiv/medRxiv response")
	}
	offset, err := publicationOffset(cursor, 0)
	if err != nil {
		return publicationPage{}, err
	}
	page := publicationPage{total: new(int(response.Messages[0].Total)), returned: len(response.Collection)}
	if next := offset + len(response.Collection); next < *page.total {
		page.next = strconv.Itoa(next)
	}
	for _, work := range response.Collection {
		doi := publicationDOI(work.DOI)
		if doi == "" || work.Version != "1" {
			continue
		}
		row, ok := publicationRecord("https://doi.org/"+doi, work.Title, work.Abstract, work.Date, doi, genrePreprint,
			"bioRxiv/medRxiv date of version 1")
		if ok {
			if published := publicationDOI(work.Published); published != "" {
				row.DOIAliases = []string{published}
			}
			row.Limitations = []string{"preprint; peer review is not established by repository inclusion"}
			page.rows = append(page.rows, row)
		}
	}
	return page, nil
}

type dataCiteWork struct {
	DOI    string `json:"doi"`
	Titles []struct {
		Title string `json:"title"`
	} `json:"titles"`
	Descriptions []struct {
		Text string `json:"description"`
	} `json:"descriptions"`
	Dates []struct {
		Date string `json:"date"`
		Type string `json:"dateType"`
	} `json:"dates"`
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

func dataCiteRows(raw []byte, cursor string) (publicationPage, error) {
	var response struct {
		Meta *struct {
			Total int `json:"total"`
		} `json:"meta"`
		Links struct {
			Next string `json:"next"`
		} `json:"links"`
		Data []struct {
			Attributes dataCiteWork `json:"attributes"`
		} `json:"data"`
	}
	if err := json.Unmarshal(raw, &response); err != nil || response.Meta == nil || response.Meta.Total < 0 {
		return publicationPage{}, errors.New("invalid DataCite response")
	}
	number, err := publicationOffset(cursor, 1)
	if err != nil {
		return publicationPage{}, err
	}
	page := publicationPage{total: new(response.Meta.Total), returned: len(response.Data)}
	if response.Links.Next != "" {
		page.next = strconv.Itoa(number + 1)
	}
	for _, item := range response.Data {
		if row, ok := item.Attributes.record(); ok {
			page.rows = append(page.rows, row)
		}
	}
	return page, nil
}

func (work dataCiteWork) record() (record, bool) {
	doi, day := publicationDOI(work.DOI), ""
	genre := strings.ToLower(work.Types.General)
	if genre == "journalarticle" {
		genre = genreArticle
	}
	if doi == "" || len(work.Titles) == 0 || !supportedRepositoryGenre(genre) {
		return record{}, false
	}
	for _, date := range work.Dates {
		if date.Type == "Issued" {
			day = publicationDay(date.Date)
			break
		}
	}
	texts := make([]string, 0, len(work.Descriptions))
	for _, description := range work.Descriptions {
		texts = append(texts, description.Text)
	}
	row, ok := publicationRecord("https://doi.org/"+doi, work.Titles[0].Title, strings.Join(texts, "\n"),
		day, doi, genre, "DataCite Issued; registration and publicationYear do not define publication day")
	if !ok {
		return record{}, false
	}
	row.Language, row.LanguageBasis = feedLanguage(work.Language)
	for _, relation := range work.Related {
		if relation.Type != "DOI" {
			continue
		}
		switch relation.Relation {
		case "IsVersionOf", "HasVersion", "IsIdenticalTo":
			if alias := publicationDOI(relation.ID); alias != "" {
				row.DOIAliases = append(row.DOIAliases, alias)
			}
		}
	}
	return row, true
}
