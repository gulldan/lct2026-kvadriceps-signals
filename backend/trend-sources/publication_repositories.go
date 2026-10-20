package main

import (
	"cmp"
	"encoding/json/v2"
	"errors"
	"fmt"
	"net/url"
	"strconv"
	"strings"
)

const zenodoPageSize = 25

func repositoryQuery(q url.Values, kind, from, through, cursor string) error {
	if kind == kindBioStudies {
		query := "release_date:[" + from + " TO " + through + "]"
		if term := q.Get(publicationQueryKey); term != "" && term != "*" {
			query = "(" + term + ") AND " + query
		}
		q.Set(publicationQueryKey, query)
		q.Set("pagination", "cursor")
		q.Set("pageSize", strconv.Itoa(publicationPageSize))
		if cursor != "" {
			q.Set("cursor", cursor)
		}
		return nil
	}
	page, err := publicationOffset(cursor, 1)
	if err != nil {
		return err
	}
	q.Set("page", strconv.Itoa(page))
	if kind == kindZenodo {
		q.Set("q", "("+cmp.Or(q.Get("q"), "*")+") AND publication_date:["+from+" TO "+through+"]")
		q.Set("sort", "mostrecent")
		q.Set("size", strconv.Itoa(zenodoPageSize))
	} else {
		// Этот маршрут перечисляет записи; произвольный поисковый текст он молча игнорирует.
		q.Set("published_since", from)
		q.Set("order", "published_date")
		q.Set("order_direction", "desc")
		q.Set("page_size", strconv.Itoa(publicationPageSize))
	}
	return nil
}

func zenodoRows(raw []byte, cursor string) (publicationPage, error) {
	var response struct {
		Hits *struct {
			Total int `json:"total"`
			Hits  []struct {
				ID         int    `json:"id"`
				DOI        string `json:"doi"`
				ConceptDOI string `json:"conceptdoi"`
				Metadata   struct {
					Title        string `json:"title"`
					Description  string `json:"description"`
					Date         string `json:"publication_date"`
					ResourceType struct {
						Type    string `json:"type"`
						Subtype string `json:"subtype"`
					} `json:"resource_type"`
				} `json:"metadata"`
			} `json:"hits"`
		} `json:"hits"`
		Links struct {
			Next string `json:"next"`
		} `json:"links"`
	}
	if err := json.Unmarshal(raw, &response); err != nil || response.Hits == nil || response.Hits.Total < 0 {
		return publicationPage{}, errors.New("invalid Zenodo response")
	}
	number, err := publicationOffset(cursor, 1)
	if err != nil {
		return publicationPage{}, err
	}
	page := publicationPage{total: new(response.Hits.Total), returned: len(response.Hits.Hits)}
	if response.Links.Next != "" {
		page.next = strconv.Itoa(number + 1)
	}
	for _, work := range response.Hits.Hits {
		genre := work.Metadata.ResourceType.Type
		if genre == "publication" {
			genre = work.Metadata.ResourceType.Subtype
		}
		if work.ID <= 0 || !supportedRepositoryGenre(genre) {
			continue
		}
		row, ok := publicationRecord("https://zenodo.org/records/"+strconv.Itoa(work.ID), work.Metadata.Title,
			work.Metadata.Description, work.Metadata.Date, work.DOI, genre, "Zenodo metadata.publication_date")
		if ok {
			if concept := publicationDOI(work.ConceptDOI); concept != "" {
				row.DOIAliases = []string{concept}
			}
			page.rows = append(page.rows, row)
		}
	}
	return page, nil
}

func supportedRepositoryGenre(genre string) bool {
	switch genre {
	case "dataset", "software", genreArticle, genrePreprint:
		return true
	default:
		return false
	}
}

func figshareRows(raw []byte, cursor string) (publicationPage, error) {
	var response []struct {
		ID        int    `json:"id"`
		DOI       string `json:"doi"`
		Title     string `json:"title"`
		Published string `json:"published_date"`
		Type      string `json:"defined_type_name"`
		Timeline  struct {
			Online      string `json:"firstOnline"`
			Publication string `json:"publisherPublication"`
		} `json:"timeline"`
	}
	if err := json.Unmarshal(raw, &response); err != nil || response == nil {
		return publicationPage{}, errors.New("invalid Figshare response")
	}
	number, err := publicationOffset(cursor, 1)
	if err != nil {
		return publicationPage{}, err
	}
	page := publicationPage{returned: len(response), limitation: "Figshare public list does not expose a provider total"}
	if len(response) == publicationPageSize {
		page.next = strconv.Itoa(number + 1)
	}
	for _, work := range response {
		if work.ID <= 0 || !supportedRepositoryGenre(work.Type) {
			continue
		}
		day := cmp.Or(publicationDay(work.Timeline.Online), publicationDay(work.Timeline.Publication),
			publicationDay(work.Published))
		row, ok := publicationRecord("https://figshare.com/articles/"+strconv.Itoa(work.ID), work.Title, "", day,
			work.DOI, work.Type, "Figshare firstOnline, publisherPublication or public deposit; not modified_date")
		if ok {
			page.rows = append(page.rows, row)
		}
	}
	return page, nil
}

func bioStudiesRows(raw []byte) (publicationPage, error) {
	var response struct {
		Total *int   `json:"totalHits"`
		Exact bool   `json:"isTotalHitsExact"`
		Next  string `json:"nextCursor"`
		Hits  []struct {
			ID      string `json:"accession"`
			Title   string `json:"title"`
			Date    string `json:"release_date"`
			Public  bool   `json:"isPublic"`
			Content string `json:"content"`
		} `json:"hits"`
	}
	if err := json.Unmarshal(raw, &response); err != nil || response.Total == nil || *response.Total < 0 {
		return publicationPage{}, errors.New("invalid BioStudies response")
	}
	page := publicationPage{returned: len(response.Hits), next: response.Next, total: response.Total}
	if !response.Exact {
		page.limitation = fmt.Sprintf("BioStudies reports an approximate total of %d; exact provider total is unknown", *response.Total)
		page.total = nil
	}
	for _, work := range response.Hits {
		// Архивная копия статьи из PMC не становится новым независимым набором данных.
		if !work.Public || work.ID == "" || strings.HasPrefix(work.ID, "S-EPMC") {
			continue
		}
		row, ok := publicationRecord("https://www.ebi.ac.uk/biostudies/studies/"+url.PathEscape(work.ID),
			work.Title, work.Content, work.Date, "", "dataset", "BioStudies release_date of public study package")
		if ok {
			page.rows = append(page.rows, row)
		}
	}
	return page, nil
}
