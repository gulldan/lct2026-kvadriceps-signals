package main

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"

	yaml "go.yaml.in/yaml/v3"
)

// Каталоги публикуют пару «технология плюс стадия зрелости», на которой построена эталонная разметка,
// поэтому каталог — прямой источник кандидатов, а не ещё одно подтверждение.

const (
	kindCNCF     = "cncf"
	kindTechPort = "techport"

	cncfParserVersion     = "cncf-landscape-yaml-v1"
	techportParserVersion = "nasa-techport-api-v1"

	// techportProjects: список отдаёт только номера, и карточка каждого проекта — отдельный запрос.
	techportProjects = 120
)

type cncfLandscape struct {
	Landscape []struct {
		Name          string `yaml:"name"`
		Subcategories []struct {
			Name  string `yaml:"name"`
			Items []struct {
				Name        string `yaml:"name"`
				Description string `yaml:"description"`
				HomepageURL string `yaml:"homepage_url"`
				RepoURL     string `yaml:"repo_url"`
				Project     string `yaml:"project"`
				Extra       struct {
					Accepted string `yaml:"accepted"`
				} `yaml:"extra"`
			} `yaml:"items"`
		} `yaml:"subcategories"`
	} `yaml:"landscape"`
}

// cncfRows берёт только принятые проекты: запись без уровня зрелости — просто продукт.
func cncfRows(body []byte, source webSource, through string) ([]record, error) {
	var landscape cncfLandscape
	if err := yaml.Unmarshal(body, &landscape); err != nil {
		return nil, fmt.Errorf("decode CNCF landscape: %w", err)
	}
	rows := []record{}
	seen := map[string]bool{}
	for _, category := range landscape.Landscape {
		for _, subcategory := range category.Subcategories {
			for _, item := range subcategory.Items {
				day := strings.TrimSpace(item.Extra.Accepted)
				address := firstAddress(item.HomepageURL, item.RepoURL)
				if item.Project == "" || len(day) != len(time.DateOnly) || day > through ||
					address == "" || seen[address] {
					continue
				}
				seen[address] = true
				section := category.Name + " / " + subcategory.Name
				rows = append(rows, cncfRecord(item.Name, item.Description, item.Project, address, day,
					section, source.Name))
			}
		}
	}
	if len(rows) == 0 {
		return nil, fmt.Errorf("CNCF landscape has no accepted projects on or before %s", through)
	}
	return rows, nil
}

// cncfRecord пишет уровень зрелости и в текст: именно он делает запись стадией.
func cncfRecord(name, description, project, address, day, section, source string) record {
	title := name + " — " + project + " CNCF"
	if description != "" {
		title = name + " — " + description
	}
	return record{
		ID: address, URL: address, Title: strings.Join(strings.Fields(title), " "),
		Date: day, DateBasis: "дата принятия проекта в CNCF",
		TitleBasis: "название и описание проекта в каталоге", Source: source,
		Text: strings.TrimSpace(description + " Уровень зрелости CNCF: " + project +
			". Раздел каталога: " + section + "."),
		TextStatus: metadataOnlyText, ParserVersion: cncfParserVersion,
		Language: "en", LanguageBasis: catalogueEnglish,
	}
}

type techportList struct {
	Projects []struct {
		ProjectID   int    `json:"projectId"`
		LastUpdated string `json:"lastUpdated"`
	} `json:"projects"`
}

type techportProject struct {
	Project struct {
		ProjectID        int    `json:"projectId"`
		Title            string `json:"title"`
		Description      string `json:"description"`
		Benefits         string `json:"benefits"`
		StartDateString  string `json:"startDateString"`
		StartYear        int    `json:"startYear"`
		StartMonth       int    `json:"startMonth"`
		LeadOrganization struct {
			OrganizationName string `json:"organizationName"`
		} `json:"leadOrganization"`
	} `json:"project"`
}

// techportRows берёт голову списка в порядке сервиса, чтобы проход был ограничен и повторяем.
func techportRows(body []byte) ([]int, error) {
	var list techportList
	if err := json.Unmarshal(body, &list); err != nil {
		return nil, fmt.Errorf("decode TechPort index: %w", err)
	}
	ids := make([]int, 0, min(len(list.Projects), techportProjects))
	for _, project := range list.Projects {
		if len(ids) == techportProjects {
			break
		}
		if project.ProjectID > 0 {
			ids = append(ids, project.ProjectID)
		}
	}
	if len(ids) == 0 {
		return nil, errors.New("TechPort index has no projects")
	}
	return ids, nil
}

// techportRecord датирует проект началом финансирования — наблюдаемым событием для технологии.
func techportRecord(body []byte, source webSource, through string) (record, error) {
	var card techportProject
	if err := json.Unmarshal(body, &card); err != nil {
		return record{}, fmt.Errorf("decode TechPort project: %w", err)
	}
	project := card.Project
	day := techportDay(project.StartYear, project.StartMonth)
	if project.Title == "" || day == "" || day > through {
		return record{}, fmt.Errorf("TechPort project %d has no usable title or start date", project.ProjectID)
	}
	address := fmt.Sprintf("https://techport.nasa.gov/projects/%d", project.ProjectID)
	text := strings.TrimSpace(project.Description + " " + project.Benefits)
	if lead := project.LeadOrganization.OrganizationName; lead != "" {
		text = strings.TrimSpace(text + " Ведущая организация: " + lead + ".")
	}
	return record{
		ID: address, URL: address, Title: strings.Join(strings.Fields(project.Title), " "),
		Date: day, DateBasis: "начало финансирования проекта по данным TechPort",
		TitleBasis: "название проекта в реестре NASA", Source: source.Name,
		Text: text, TextStatus: metadataOnlyText, ParserVersion: techportParserVersion,
		Language: "en", LanguageBasis: "реестр ведётся на английском",
	}, nil
}

func techportDay(year, month int) string {
	if year < 1958 || month < 1 || month > 12 {
		return ""
	}
	return fmt.Sprintf("%04d-%02d-01", year, month)
}

func firstAddress(candidates ...string) string {
	for _, candidate := range candidates {
		candidate = strings.TrimSpace(candidate)
		if candidate == "" {
			continue
		}
		if parsed, err := url.Parse(candidate); err == nil && parsed.Scheme == "https" && parsed.Host != "" {
			return candidate
		}
	}
	return ""
}

func (s *service) catalogueHistory(ctx context.Context, source webSource, data *snapshot, old map[string]record) {
	c := coverage{Source: source.Name, From: data.From, Through: data.Through, ObservedAt: data.ObservedAt,
		StopReason: "каталог отдаёт текущий состав, историю исключений он не публикует"}
	raw, err := s.fetch(ctx, source.URL, data)
	if err != nil {
		c.StopReason = err.Error()
		data.Coverage = append(data.Coverage, c)
		return
	}
	page := provenance{URL: source.URL, SHA256: hashBytes(raw)}
	c.Pages = append(c.Pages, page)
	var rows []record
	switch source.Kind {
	case kindCNCF:
		rows, err = cncfRows(raw, source, data.Through)
	case kindTechPort:
		rows, err = s.techportCards(ctx, source, data, &c, raw)
	case kindHFPapers:
		rows, err = hfPaperRows(raw, source, data.Through)
	case kindHFModels:
		rows, err = hfModelRows(raw, source, data.Through)
	}
	if err != nil {
		c.StopReason = err.Error()
		data.Coverage = append(data.Coverage, c)
		return
	}
	s.feedRecords(ctx, source, data, &c, rows, old, page, cmp.Or(source.Pages, 20))
	data.Coverage = append(data.Coverage, c)
}

// techportCards пропускает сбойную карточку: один проект не должен стоить всего источника.
func (s *service) techportCards(ctx context.Context, source webSource, data *snapshot, c *coverage,
	index []byte,
) ([]record, error) {
	ids, err := techportRows(index)
	if err != nil {
		return nil, err
	}
	rows := make([]record, 0, len(ids))
	for _, id := range ids {
		if ctx.Err() != nil {
			break
		}
		address := fmt.Sprintf("%s/%d", strings.TrimRight(catalogueBase(source.URL), "/"), id)
		body, fetchErr := s.fetch(ctx, address, data)
		if fetchErr != nil {
			c.MissingTitles++
			continue
		}
		row, parseErr := techportRecord(body, source, data.Through)
		if parseErr != nil {
			c.MissingTitles++
			continue
		}
		c.Pages = append(c.Pages, provenance{URL: address, SHA256: hashBytes(body)})
		rows = append(rows, row)
	}
	if len(rows) == 0 {
		return nil, errors.New("TechPort returned no usable project cards")
	}
	return rows, nil
}

func catalogueBase(address string) string {
	if parsed, err := url.Parse(address); err == nil {
		parsed.RawQuery = ""
		return parsed.String()
	}
	return address
}
