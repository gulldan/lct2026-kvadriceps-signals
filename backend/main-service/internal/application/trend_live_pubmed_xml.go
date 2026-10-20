package application

import (
	"cmp"
	"encoding/xml"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"
)

const (
	liveGenreArticle    = "article"
	liveGenreReview     = "review"
	liveGenreRetraction = "retraction"
)

// Заголовки и аннотации содержат вложенные теги: обычное текстовое поле потеряло бы названия внутри них.
type livePubMedText string

func (s *livePubMedText) UnmarshalXML(dec *xml.Decoder, start xml.StartElement) error {
	var text strings.Builder
	depth := 1
	for depth > 0 {
		token, err := dec.Token()
		if err != nil {
			return fmt.Errorf("PubMed: текст элемента %s: %w", start.Name.Local, err)
		}
		switch part := token.(type) {
		case xml.StartElement:
			depth++
		case xml.EndElement:
			depth--
		case xml.CharData:
			text.Write(part)
		}
	}
	*s = livePubMedText(strings.Join(strings.Fields(text.String()), " "))
	return nil
}

type livePubMedDate struct {
	Year  string `xml:"Year"`
	Month string `xml:"Month"`
	Day   string `xml:"Day"`
}

type livePubMedArticle struct {
	Citation struct {
		PMID    string `xml:"PMID"`
		Article struct {
			Title    livePubMedText `xml:"ArticleTitle"`
			Language string         `xml:"Language"`
			Abstract []struct {
				Label string `xml:"Label,attr"`
				Raw   string `xml:",innerxml"`
			} `xml:"Abstract>AbstractText"`
			Dates   []livePubMedDate `xml:"ArticleDate"`
			Journal struct {
				Title string         `xml:"Title"`
				Date  livePubMedDate `xml:"JournalIssue>PubDate"`
			} `xml:"Journal"`
			Authors []struct {
				Last       string `xml:"LastName"`
				Fore       string `xml:"ForeName"`
				Initials   string `xml:"Initials"`
				Collective string `xml:"CollectiveName"`
			} `xml:"AuthorList>Author"`
			Types []string `xml:"PublicationTypeList>PublicationType"`
		} `xml:"Article"`
	} `xml:"MedlineCitation"`
	IDs []struct {
		Type  string `xml:"IdType,attr"`
		Value string `xml:",chardata"`
	} `xml:"PubmedData>ArticleIdList>ArticleId"`
}

func livePubMedPapers(raw []byte, requested []string, periods []LivePeriod,
) ([]StudyPaper, map[string]LivePaperMetadata, error) {
	var result struct {
		XMLName  xml.Name            `xml:"PubmedArticleSet"`
		Articles []livePubMedArticle `xml:"PubmedArticle"`
		Error    string              `xml:"ERROR"`
	}
	if err := xml.Unmarshal(raw, &result); err != nil {
		return nil, nil, fmt.Errorf("PubMed: неверный ответ статей: %w", err)
	}
	if result.Error != "" || len(result.Articles) == 0 && len(requested) > 0 {
		return nil, nil, errors.New("PubMed не вернул запрошенные статьи")
	}
	papers := make([]StudyPaper, 0, len(result.Articles))
	metadata := map[string]LivePaperMetadata{}
	for _, article := range result.Articles {
		paper, meta, ok := article.paper(periods)
		if !ok || !slices.Contains(requested, article.Citation.PMID) {
			continue
		}
		if _, duplicate := metadata[paper.ID]; duplicate {
			continue
		}
		papers = append(papers, paper)
		metadata[paper.ID] = meta
	}
	return papers, metadata, nil
}

func (a livePubMedArticle) paper(periods []LivePeriod) (StudyPaper, LivePaperMetadata, bool) {
	article := a.Citation.Article
	date := livePubMedPublicationDate(append(slices.Clone(article.Dates), article.Journal.Date))
	if !livePubMedID(a.Citation.PMID) || len(article.Title) < liveScholarMinTitle || livePeriodIndex(periods, date) < 0 {
		return StudyPaper{}, LivePaperMetadata{}, false
	}
	var doi string
	for _, id := range a.IDs {
		if id.Type == "doi" {
			doi = strings.TrimPrefix(strings.ToLower(strings.TrimSpace(id.Value)), "https://doi.org/")
		}
	}
	address := "https://pubmed.ncbi.nlm.nih.gov/" + a.Citation.PMID + "/"
	paper := StudyPaper{ID: livePubMedSource + ":" + a.Citation.PMID, CanonicalID: doi,
		Title: string(article.Title), Date: date, URL: address}
	for _, author := range article.Authors {
		name := strings.TrimSpace(cmp.Or(author.Fore, author.Initials) + " " + author.Last)
		if author.Collective != "" {
			name = author.Collective
		}
		if name != "" {
			paper.Authors = append(paper.Authors, name)
		}
	}
	abstracts := make([]string, 0, len(article.Abstract))
	for _, section := range article.Abstract {
		var text livePubMedText
		if err := xml.Unmarshal([]byte("<text>"+section.Raw+"</text>"), &text); err != nil {
			continue
		}
		part := string(text)
		if section.Label != "" {
			part = section.Label + ": " + part
		}
		abstracts = append(abstracts, part)
	}
	paper.ContentHash = liveScholarHash(paper.ID, paper.Title, paper.Date, strings.Join(abstracts, "\n"))
	meta := LivePaperMetadata{Abstract: strings.Join(abstracts, "\n"), DOI: doi, SourceURL: address,
		Category: article.Journal.Title, ContentHash: paper.ContentHash, Genre: livePubMedGenre(article.Types)}
	if article.Language != "" {
		// Источник использует трёхбуквенные коды; известные языки приводим к формату интерфейса.
		meta.Language = article.Language
		languages := map[string]string{"eng": "en", "rus": "ru", "chi": "zh", "ger": "de", "fre": "fr"}
		if language := languages[meta.Language]; language != "" {
			meta.Language = language
		}
		meta.LanguageBasis = "язык указан в метаданных PubMed"
	}
	return paper, meta, true
}

func livePubMedID(id string) bool {
	if id == "" || len(id) > 12 {
		return false
	}
	for _, ch := range id {
		if ch < '0' || ch > '9' {
			return false
		}
	}
	return true
}

// Нужна первая публикация, а не дата индексации. Год или сезон без дня не превращаем в выдуманную дату.
func livePubMedPublicationDate(dates []livePubMedDate) string {
	first := ""
	for _, date := range dates {
		month, err := strconv.Atoi(date.Month)
		if err != nil {
			parsed, parseErr := time.Parse("Jan", date.Month)
			if parseErr != nil {
				continue
			}
			month = int(parsed.Month())
		}
		day, err := strconv.Atoi(date.Day)
		if err != nil {
			continue
		}
		value := fmt.Sprintf("%s-%02d-%02d", date.Year, month, day)
		if _, err := time.Parse(time.DateOnly, value); err == nil && (first == "" || value < first) {
			first = value
		}
	}
	return first
}

func livePubMedGenre(types []string) string {
	for _, kind := range []struct{ publication, genre string }{
		{"Retracted Publication", liveGenreRetraction}, {"Retraction of Publication", liveGenreRetraction},
		{"Published Erratum", "erratum"}, {"Editorial", "editorial"}, {"Letter", "letter"},
		{"News", GenreNews}, {"Preprint", "preprint"}, {"Review", liveGenreReview}, {"Journal Article", liveGenreArticle},
	} {
		if slices.Contains(types, kind.publication) {
			return kind.genre
		}
	}
	return GenreUnknown
}
