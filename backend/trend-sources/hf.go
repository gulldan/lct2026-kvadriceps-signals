package main

import (
	"encoding/json/v2"
	"errors"
	"fmt"
	"strings"
	"time"
)

// Hugging Face: статьи дня по голосам практиков и набирающие внимание модели; JSON API без RSS — читаются как каталоги.
const (
	kindHFPapers = "hf_papers"
	kindHFModels = "hf_models"
	hfBase       = "https://huggingface.co"
	// hfParserVersion входит в идентичность записи; менять при смене разбора.
	hfParserVersion  = "hf-api-v1"
	catalogueEnglish = "каталог ведётся на английском"
)

type hfPaper struct {
	Paper struct {
		ID          string `json:"id"`
		Title       string `json:"title"`
		Summary     string `json:"summary"`
		PublishedAt string `json:"publishedAt"`
		Upvotes     int    `json:"upvotes"`
	} `json:"paper"`
}

type hfModel struct {
	ID        string   `json:"id"`
	Likes     int      `json:"likes"`
	Trending  int      `json:"trendingScore"`
	Private   bool     `json:"private"`
	CreatedAt string   `json:"createdAt"`
	Pipeline  string   `json:"pipeline_tag"`
	Library   string   `json:"library_name"`
	Tags      []string `json:"tags"`
	Downloads int      `json:"downloads"`
}

func hfDay(stamp string) string {
	if len(stamp) < len(time.DateOnly) {
		return ""
	}
	return stamp[:len(time.DateOnly)]
}

// hfPaperRows пишет голоса сообщества в текст как факт внимания, а не как оценку: рецензию они не заменяют.
func hfPaperRows(body []byte, source webSource, through string) ([]record, error) {
	var items []hfPaper
	if err := json.Unmarshal(body, &items); err != nil {
		return nil, fmt.Errorf("decode Hugging Face daily papers: %w", err)
	}
	rows := []record{}
	seen := map[string]bool{}
	for _, item := range items {
		paper := item.Paper
		day := hfDay(paper.PublishedAt)
		address := hfBase + "/papers/" + paper.ID
		if paper.ID == "" || paper.Title == "" || day == "" || day > through || seen[address] {
			continue
		}
		seen[address] = true
		rows = append(rows, record{
			ID: address, URL: address, Title: strings.Join(strings.Fields(paper.Title), " "),
			Date: day, DateBasis: "дата публикации статьи по данным Hugging Face",
			PublishedAt: paper.PublishedAt, TitleBasis: "заголовок статьи в каталоге Hugging Face",
			Source: source.Name, ParserVersion: hfParserVersion,
			Text: strings.TrimSpace(fmt.Sprintf("%s Голосов сообщества Hugging Face: %d.",
				strings.Join(strings.Fields(paper.Summary), " "), paper.Upvotes)),
			TextStatus: "abstract_only", Language: "en", LanguageBasis: catalogueEnglish,
			Limitations: []string{"community_selected; голоса Hugging Face — признак внимания практиков, не рецензия"},
		})
	}
	if len(rows) == 0 {
		return nil, errors.New("daily papers of Hugging Face returned no dated papers")
	}
	return rows, nil
}

// hfModelRows: имя модели непрозрачно, поэтому в текст идут задача, библиотека и теги — по ним запрос и совпадает.
func hfModelRows(body []byte, source webSource, through string) ([]record, error) {
	var items []hfModel
	if err := json.Unmarshal(body, &items); err != nil {
		return nil, fmt.Errorf("decode Hugging Face models: %w", err)
	}
	rows := []record{}
	seen := map[string]bool{}
	for _, item := range items {
		day := hfDay(item.CreatedAt)
		address := hfBase + "/" + item.ID
		if item.ID == "" || item.Private || day == "" || day > through || seen[address] {
			continue
		}
		seen[address] = true
		rows = append(rows, record{
			ID: address, URL: address, Title: item.ID,
			Date: day, DateBasis: "дата публикации карточки модели на Hugging Face",
			PublishedAt: item.CreatedAt, TitleBasis: "идентификатор модели в каталоге",
			Source: source.Name, ParserVersion: hfParserVersion,
			Text: strings.TrimSpace(fmt.Sprintf("Задача: %s. Библиотека: %s. Теги: %s. Отметок «нравится»: %d, "+
				"индекс тренда: %d, загрузок: %d.", item.Pipeline, item.Library, strings.Join(item.Tags, ", "),
				item.Likes, item.Trending, item.Downloads)),
			TextStatus: metadataOnlyText, Language: "en", LanguageBasis: catalogueEnglish,
			Limitations: []string{"trending_snapshot; индекс тренда Hugging Face — мгновенный срез внимания, не история"},
		})
	}
	if len(rows) == 0 {
		return nil, errors.New("trending models of Hugging Face returned no dated public models")
	}
	return rows, nil
}
