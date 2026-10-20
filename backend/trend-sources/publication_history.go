package main

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const (
	kindEuropePMC        = "europe_pmc"
	kindCrossref         = "crossref"
	kindClinicalTrials   = "clinical_trials"
	kindBiorxiv          = "biorxiv"
	kindDataCite         = "datacite"
	kindZenodo           = "zenodo"
	kindFigshare         = "figshare"
	kindBioStudies       = "biostudies"
	publicationQueryKey  = "query"
	publicationFilterKey = "filter"
	publicationVersion   = "domain-publications-v1"
	publicationPageSize  = 100
)

type publicationPage struct {
	rows       []record
	total      *int
	returned   int
	next       string
	limitation string
}

// У каждого домена свой бюджет страниц: большой каталог медицины не вытесняет экологию и экономику.
func (s *service) publicationHistory(ctx context.Context, source webSource, data *snapshot) {
	c := coverage{Source: source.Name, From: data.From, Through: data.Through, ObservedAt: data.ObservedAt,
		StopReason: "bounded domain metadata sample; archive coverage is incomplete"}
	if source.Kind == kindBiorxiv || source.Kind == kindBioStudies {
		// Препринты идут от старых к новым; месячное окно сохраняет свежие работы при небольшом бюджете.
		if end, err := time.Parse(time.DateOnly, data.Through); err == nil {
			c.From = max(c.From, end.AddDate(0, 0, -31).Format(time.DateOnly))
		}
		c.Limitations = append(c.Limitations, "rolling 31-day repository sample; earlier archive is not covered")
	}
	cursor := ""
	seen := map[string]bool{}
	for range cmp.Or(source.Pages, 2) {
		address, err := publicationAddress(source, c.From, c.Through, cursor)
		if err != nil {
			c.StopReason = err.Error()
			break
		}
		raw, err := s.fetch(ctx, address, data)
		if err != nil {
			c.StopReason = err.Error()
			break
		}
		page, err := publicationRows(raw, source.Kind, cursor)
		if err != nil {
			c.StopReason = err.Error()
			break
		}
		proof := provenance{URL: address, SHA256: hashBytes(raw)}
		page.appendTo(data, source, &c, proof, seen)
		if page.next == "" || page.returned == 0 {
			c.Exhausted = true
			c.Cursor = ""
			c.StopReason = "provider search exhausted; records without an exact publication day are omitted"
			break
		}
		if page.next == cursor {
			c.StopReason = "provider repeated its cursor; archive coverage is incomplete"
			break
		}
		cursor, c.Cursor = page.next, page.next
	}
	data.Coverage = append(data.Coverage, c)
}

func (page publicationPage) appendTo(data *snapshot, source webSource, c *coverage, proof provenance, seen map[string]bool) {
	c.Pages = append(c.Pages, proof)
	c.Total = page.total
	if page.limitation != "" {
		c.Limitations = appendLimitation(c.Limitations, page.limitation)
	}
	if omitted := page.returned - len(page.rows); omitted > 0 {
		c.Limitations = append(c.Limitations,
			fmt.Sprintf("page omitted %d records without a usable identifier, title, exact date or supported type", omitted))
	}
	for _, row := range page.rows {
		if row.Date < c.From || row.Date > c.Through || seen[row.ID] {
			continue
		}
		seen[row.ID] = true
		row.Source, row.Industry, row.ObservedAt = source.Name, source.Industry, data.ObservedAt
		row.Provenance = []provenance{proof}
		data.Records = append(data.Records, row)
	}
}

func publicationAddress(source webSource, from, through, cursor string) (string, error) {
	if publicationDay(from) != from || publicationDay(through) != through || from > through {
		return "", errors.New("invalid publication date window")
	}
	u, err := url.Parse(source.URL)
	if err != nil {
		return "", fmt.Errorf("parse publication source: %w", err)
	}
	q := u.Query()
	switch source.Kind {
	case kindEuropePMC:
		q.Set(publicationQueryKey,
			"("+cmp.Or(q.Get(publicationQueryKey), "*")+") AND FIRST_PDATE:["+from+" TO "+through+"] sort_date:y")
		q.Set("format", "json")
		q.Set("resultType", "core")
		q.Set("pageSize", strconv.Itoa(publicationPageSize))
		q.Set("cursorMark", cmp.Or(cursor, "*"))
	case kindCrossref:
		q.Set(publicationFilterKey, joinFilter(q.Get(publicationFilterKey), "from-pub-date:"+from+",until-pub-date:"+through))
		// Сортировка по публикации несовместима с курсором Crossref; дата депозита не становится датой работы.
		q.Set("sort", "updated")
		q.Set("order", "desc")
		q.Set("rows", strconv.Itoa(publicationPageSize))
		q.Set("cursor", cmp.Or(cursor, "*"))
	case kindClinicalTrials:
		filter := "AREA[StudyFirstPostDate]RANGE[" + from + "," + through + "]"
		if previous := q.Get("filter.advanced"); previous != "" {
			filter = "(" + previous + ") AND " + filter
		}
		q.Set("filter.advanced", filter)
		q.Set("sort", "StudyFirstPostDate:desc")
		q.Set("pageSize", strconv.Itoa(publicationPageSize))
		q.Set("countTotal", "true")
		q.Set("format", "json")
		if cursor != "" {
			q.Set("pageToken", cursor)
		}
	case kindBiorxiv:
		offset, err := publicationOffset(cursor, 0)
		if err != nil {
			return "", err
		}
		u.Path = strings.TrimRight(u.Path, "/") + "/" + from + "/" + through + "/" + strconv.Itoa(offset) + "/json"
	case kindDataCite:
		page, err := publicationOffset(cursor, 1)
		if err != nil {
			return "", err
		}
		q.Set(publicationQueryKey,
			"("+cmp.Or(q.Get(publicationQueryKey), "*")+") AND publicationYear:["+from[:4]+" TO "+through[:4]+"]")
		q.Set("page[size]", strconv.Itoa(publicationPageSize))
		q.Set("page[number]", strconv.Itoa(page))
		// Поле сортировки не заменяет дату Issued: у многих записей published отсутствует.
		q.Set("sort", "-created")
	case kindZenodo, kindFigshare, kindBioStudies:
		if err := repositoryQuery(q, source.Kind, from, through, cursor); err != nil {
			return "", err
		}
	default:
		return "", errors.New("unsupported publication source")
	}
	u.RawQuery = q.Encode()
	return u.String(), nil
}

func joinFilter(previous, added string) string {
	if previous == "" {
		return added
	}
	return previous + "," + added
}

func publicationOffset(cursor string, first int) (int, error) {
	if cursor == "" {
		return first, nil
	}
	offset, err := strconv.Atoi(cursor)
	if err != nil || offset < first || offset > 1_000_000 {
		return 0, errors.New("invalid publication cursor")
	}
	return offset, nil
}
