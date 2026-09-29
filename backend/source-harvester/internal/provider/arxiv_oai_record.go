package provider

import (
	"encoding/json/v2"
	"encoding/xml"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strings"
	"time"
	"unicode"
)

type arxivOAIRecord struct {
	Header struct {
		Status     string `xml:"status,attr"`
		Identifier string `xml:"identifier"`
		Datestamp  string `xml:"datestamp"`
	} `xml:"header"`
	Meta struct {
		XMLName    xml.Name `xml:"http://arxiv.org/OAI/arXivRaw/ arXivRaw"`
		ID         string   `xml:"id"`
		Title      string   `xml:"title"`
		Abstract   string   `xml:"abstract"`
		Authors    string   `xml:"authors"`
		Categories string   `xml:"categories"`
		DOI        string   `xml:"doi"`
		Journal    string   `xml:"journal-ref"`
		Versions   []struct {
			Version string `xml:"version,attr"`
			Date    string `xml:"date"`
		} `xml:"version"`
	} `xml:"metadata>arXivRaw"`
	XML string `xml:",innerxml"`
}

func (item arxivOAIRecord) record(observed time.Time, scope Scope) (Record, error) {
	if item.Header.Status == "deleted" {
		return Record{}, errors.New("arxiv OAI record was deleted")
	}
	if item.Header.Status != "" || item.Meta.ID == "" || item.Header.Identifier != "oai:arXiv.org:"+item.Meta.ID {
		return Record{}, errors.New("arxiv OAI record identifier or status is invalid")
	}
	stamp, err := time.Parse(time.DateOnly, item.Header.Datestamp)
	if err != nil || stamp.After(observed) || item.Header.Datestamp > scope.Through || item.Header.Datestamp < scope.From {
		return Record{}, errors.New("arxiv OAI metadata datestamp is outside the requested window")
	}
	entry := atomEntry{Title: item.Meta.Title, Summary: item.Meta.Abstract, DOI: item.Meta.DOI, Journal: item.Meta.Journal}
	for i, version := range item.Meta.Versions {
		at, dateErr := http.ParseTime(version.Date)
		if dateErr != nil || !arxivOAIVersion(version.Version, i) || at.After(observed) || at.Before(entry.Updated) {
			return Record{}, errors.New("arxiv OAI version history is missing, unordered or invalid")
		}
		if i == 0 {
			entry.Published = at
		}
		entry.Updated = at
		entry.ID = "https://arxiv.org/abs/" + item.Meta.ID + version.Version
	}
	if entry.Published.IsZero() {
		return Record{}, errors.New("arxiv OAI record lacks the v1 publication date")
	}
	for category := range strings.FieldsSeq(item.Meta.Categories) {
		entry.Categories = append(entry.Categories, struct {
			Term string `json:"term" xml:"term,attr"`
		}{category})
	}
	if !arxivOAIMatches(scope.Query, item.Meta.Title+" "+item.Meta.Abstract, strings.Fields(item.Meta.Categories)) {
		return Record{}, errors.New("arxiv OAI record does not match the local topic filter")
	}
	record, err := arxivRecord(entry, observed, scope)
	if err != nil {
		return record, err
	}
	record.Source.Publication.Basis = "arXivRaw version v1 date (not OAI datestamp)"
	record.Source.RevisionPublication.Basis = "arXivRaw explicit version date"
	record.Source.AvailableAt = new(maxTime(stamp, entry.Updated))
	record.Source.AvailableBasis = "later of arXivRaw version date and OAI metadata datestamp"
	record.Limitations = append(record.Limitations, "oai_latest_metadata_only; versioned PDF retained separately")
	record.Raw, err = json.Marshal(struct {
		Transport string `json:"transport"`
		XML       string `json:"record_xml"`
	}{"arxiv_oai_pmh_arXivRaw", item.XML})
	if err != nil {
		return record, fmt.Errorf("encode arxiv OAI metadata evidence: %w", err)
	}
	return record, record.Source.Validate()
}

func arxivOAIMatches(query, text string, categories []string) bool {
	if strings.HasPrefix(query, "cat:") {
		for term := range strings.SplitSeq(query, " OR ") {
			if slices.Contains(categories, strings.TrimPrefix(term, "cat:")) {
				return true
			}
		}
		return false
	}
	text = " " + arxivOAIWords(text+" "+strings.Join(categories, " ")) + " "
	for term := range strings.FieldsSeq(arxivOAIWords(query)) {
		if !strings.Contains(text, " "+term+" ") {
			return false
		}
	}
	return true
}

func arxivOAIWords(text string) string {
	return strings.ToLower(strings.Join(strings.FieldsFunc(text, func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsNumber(r)
	}), " "))
}

func arxivOAIQuerySet(scope Scope) (string, error) {
	set := ""
	if strings.HasPrefix(scope.Query, "cat:") {
		var err error
		set, err = arxivOAIQueryCategories(scope)
		if err != nil {
			return "", err
		}
	} else if arxivOAIWords(scope.Query) == "" {
		return "", errors.New("arxiv OAI topic query requires a word")
	}
	if scope.Category != "" {
		set = arxivOAISet(scope.Category)
	}
	return set, nil
}

func arxivOAIQueryCategories(scope Scope) (string, error) {
	set, archive := "", ""
	for term := range strings.SplitSeq(scope.Query, " OR ") {
		category, ok := strings.CutPrefix(term, "cat:")
		if !ok || category == "" {
			return "", errors.New("arxiv OAI supports category queries joined by OR only")
		}
		check := scope
		check.Category = category
		if err := check.ValidateFor(providerArXiv); err != nil {
			return "", err
		}
		root, _, _ := strings.Cut(arxivOAISet(category), ":")
		if archive == "" {
			archive, set = root, root
		} else if archive != root {
			set = ""
		}
	}
	return set, nil
}
