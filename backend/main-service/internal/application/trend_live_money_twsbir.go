package application

import (
	"bytes"
	"context"
	"encoding/csv"
	"errors"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// SBIR Тайваня — гранты министерства экономики малым компаниям на НИОКР. Портал открытых данных выкладывает
// по CSV на год программы (год по календарю Миньго), а в файле года лежат и одобрения прошлого года.

const (
	liveMoneyTWSBIRMeta    = "https://data.gov.tw/api/v2/rest/dataset/26131"
	liveMoneyTWSBIRPage    = "https://data.gov.tw/dataset/26131"
	liveMoneyTWSBIRSource  = "SBIR Тайваня: гранты малым компаниям"
	liveMoneyTWSBIRLicense = "открытая лицензия правительства Тайваня 1.0, совместима с CC BY 4.0"
	liveMoneyTWSBIRRead    = 8 << 20
	// Год Миньго отстаёт от григорианского на 1911 лет.
	liveMoneyTWSBIREra = 1911
)

type liveMoneyTWSBIRDataset struct {
	Result struct {
		Distribution []struct {
			Description string `json:"resourceDescription"`
			URL         string `json:"resourceDownloadUrl"`
		} `json:"distribution"`
	} `json:"result"`
}

func liveMoneyTWSBIR(ctx context.Context, client *http.Client, rubrics []liveRubric,
	from, through string) ([]LiveMoneyEvent, int, error) {
	var dataset liveMoneyTWSBIRDataset
	if err := liveMoneyJSON(ctx, client, http.MethodGet, liveMoneyTWSBIRMeta, nil, &dataset); err != nil {
		return nil, 0, fmt.Errorf("tw sbir: dataset: %w", err)
	}
	first, _ := strconv.Atoi(from[:min(len(from), 4)])
	last, _ := strconv.Atoi(through[:min(len(through), 4)])
	year := regexp.MustCompile(`(\d{3})年度`)
	events, records, seen := []LiveMoneyEvent{}, 0, map[string]bool{}
	for _, file := range dataset.Result.Distribution {
		m := year.FindStringSubmatch(file.Description)
		if m == nil {
			continue
		}
		// Файл прошлого года тоже читается: одобрения в нём заходят в начало окна.
		if roc, _ := strconv.Atoi(m[1]); roc+liveMoneyTWSBIREra < first-1 || roc+liveMoneyTWSBIREra > last {
			continue
		}
		raw, err := liveMarketFetch(ctx, client, http.MethodGet, file.URL, nil, nil, liveMoneyTWSBIRRead)
		if err != nil {
			return events, records, fmt.Errorf("tw sbir: %s: %w", m[1], err)
		}
		found, read, err := liveMoneyTWSBIREvents(rubrics, raw, from, through)
		if err != nil {
			return events, records, fmt.Errorf("tw sbir: %s: %w", m[1], err)
		}
		records += read
		for _, event := range found {
			if !seen[event.ID] {
				seen[event.ID] = true
				events = append(events, event)
			}
		}
	}
	return events, records, nil
}

func liveMoneyTWSBIREvents(rubrics []liveRubric, raw []byte, from, through string) ([]LiveMoneyEvent, int, error) {
	reader := csv.NewReader(bytes.NewReader(bytes.TrimPrefix(raw, []byte("\ufeff"))))
	reader.FieldsPerRecord = -1
	header, err := reader.Read()
	if err != nil {
		return nil, 0, fmt.Errorf("header: %w", err)
	}
	column := map[string]int{}
	for i, name := range header {
		column[strings.TrimSpace(name)] = i
	}
	events, records := []LiveMoneyEvent{}, 0
	for {
		row, err := reader.Read()
		if errors.Is(err, io.EOF) {
			return events, records, nil
		}
		if err != nil {
			return events, records, fmt.Errorf("row: %w", err)
		}
		field := func(name string) string {
			if i, ok := column[name]; ok && i < len(row) {
				return strings.TrimSpace(row[i])
			}
			return ""
		}
		day, err := time.Parse("20060102", field("核准日期"))
		if date := day.Format(time.DateOnly); err != nil || date < from || date > through || field("獎補助事項") == "" {
			continue
		}
		records++
		event := liveMoneyGrant(rubrics, liveMoneyTWSBIRSource, field("獎補助事項"), "")
		event.Company, event.Date = field("獎補助對象"), day.Format(time.DateOnly)
		event.ID = liveMoneyID("tw-sbir", event.Company, event.Title, event.Date)
		event.Region, event.Country, event.Investors = liveMoneyRegionAS, "TW", "SBIR, министерство экономики Тайваня"
		event.URL, event.License = liveMoneyTWSBIRPage, liveMoneyTWSBIRLicense
		if amount, convErr := strconv.ParseFloat(field("補助金額(元)"), 64); convErr == nil && amount > 0 {
			event.Amount = liveMoneyIn("NT$", amount)
		}
		events = append(events, event)
	}
}
