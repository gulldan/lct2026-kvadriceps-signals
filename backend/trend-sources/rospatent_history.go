package main

import (
	"context"
	"encoding/json/v2"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"
)

// rospatentHistory — выборка RU по терминам с 1994 года, не весь реестр; два последних месяца перечитываются.
func (s *service) rospatentHistory(ctx context.Context, data *snapshot) {
	if s.config.RUDocuments <= 0 {
		return
	}
	c := coverage{Source: rospatentSource, From: data.From, Through: data.Through, ObservedAt: data.ObservedAt}
	if rospatentKey() == "" {
		c.StopReason = "Rospatent API key (ROSPATENT_API_KEY) is not configured"
		data.Coverage = append(data.Coverage, c)
		data.Limits = append(data.Limits, "The Russian patent scope is off: no Rospatent API key is configured.")
		return
	}
	through, _ := time.Parse(time.DateOnly, data.Through)
	frozen := time.Date(through.Year(), through.Month(), 1, 0, 0, 0, 0, time.UTC).AddDate(0, -1, 0).Format(time.DateOnly)
	sweep := opsSweep{coverage: &c, seen: map[string]bool{}, frozen: frozen}
outer:
	for _, term := range s.config.PatentTerms {
		for _, month := range monthWindows(data.From, data.Through) {
			if stop := s.rospatentMonth(ctx, data, &sweep, term, month); stop != "" {
				c.StopReason = stop
				break outer
			}
		}
	}
	sweep.finish("every configured term and month was read from Rospatent")
	data.Coverage = append(data.Coverage, c)
	data.Limits = append(data.Limits,
		"Russian patent records come from the Rospatent search platform over the configured terms in the RU dataset "+
			"since 1994, so they are a term sample; English titles and abstracts are used when the platform carries "+
			"them, otherwise the record is Russian and says so. Full text is not read. The platform's specification "+
			"names no request quota; the collector keeps one request a second, skips a term the platform rejects "+
			"and stops at the first key or quota refusal.")
}

func (s *service) rospatentMonth(ctx context.Context, data *snapshot, sweep *opsSweep, term string, month period) string {
	if sweep.rejected[term] {
		return ""
	}
	for offset := 0; ; offset += rospatentPage {
		if sweep.read >= s.config.RUDocuments {
			sweep.coverage.Cursor = fmt.Sprintf("%s|%s|%d", term, month.Start, offset)
			return "Rospatent record budget reached; the sweep continues from this term and month"
		}
		address := rospatentQueryURL(term, month, offset)
		raw, cached, err := s.rospatentPage(ctx, data, term, month, offset, month.End <= sweep.frozen)
		if err != nil {
			return sweep.stop(term, month.Start, offset, err)
		}
		var page rospatentSearch
		if err = json.Unmarshal(raw, &page); err != nil {
			sweep.coverage.Limitations = append(sweep.coverage.Limitations,
				"Rospatent page not readable: "+address+": "+err.Error())
			return ""
		}
		rospatentAppend(data, sweep, page, address, hashBytes(raw), cached)
		if offset+rospatentPage >= min(page.Total, page.Available) || len(page.Hits) == 0 {
			return ""
		}
	}
}

func rospatentQueryURL(term string, month period, offset int) string {
	return fmt.Sprintf("%s/search?q=%s&from=%s&offset=%d", rospatentBase, term, month.Start, offset)
}

func (s *service) rospatentPage(ctx context.Context, data *snapshot, term string, month period, offset int,
	frozen bool) ([]byte, bool, error) {
	address := rospatentQueryURL(term, month, offset)
	name := s.cachePath("rospatent:v1", address)
	if frozen {
		if b, err := os.ReadFile(name); err == nil {
			data.Pages = append(data.Pages, provenance{URL: address, SHA256: hashBytes(b)})
			return b, true, nil
		}
	}
	raw, err := s.rospatentSearch(ctx, term, month, offset)
	if err != nil {
		return nil, false, err
	}
	if err = s.storeRaw(raw, address, http.MethodPost, data); err != nil {
		return nil, false, err
	}
	if frozen {
		if err = cachePut(name, raw); err != nil {
			return nil, false, err
		}
	}
	return raw, false, nil
}

func rospatentAppend(data *snapshot, sweep *opsSweep, page rospatentSearch, address, digest string, cached bool) {
	for _, hit := range page.Hits {
		row, ok := rospatentRecord(hit, data.ObservedAt)
		if !ok || sweep.seen[row.ID] {
			continue
		}
		sweep.seen[row.ID] = true
		if !cached {
			sweep.read++
		}
		row.Provenance = []provenance{{URL: address, SHA256: digest, Method: http.MethodPost}}
		data.Records = append(data.Records, row)
	}
}

// rospatentQueryTerm не пускает операторы платформы в простой термин.
func rospatentQueryTerm(term string) string {
	return strings.Join(strings.Fields(strings.NewReplacer(`"`, " ", "(", " ", ")", " ", "*", " ").Replace(term)), " ")
}
