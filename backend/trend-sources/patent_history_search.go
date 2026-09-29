package main

import (
	"context"
	"encoding/json/v2"
	"errors"
	"fmt"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"
)

const patentSearchDateBasis = "USPTO Public Search structured datePublished"

type patentHistoryWindow struct {
	From      string `json:"from"`
	Through   string `json:"through"`
	Query     string `json:"query"`
	Total     *int   `json:"provider_total"`
	Received  int    `json:"received"`
	Cursor    string `json:"next_cursor"`
	Exhausted bool   `json:"exhausted"`
}

func newPublicSearchPatent(id, day string) record {
	params := url.Values{"q": {strings.TrimPrefix(id, "US") + ".pn."}, "db": {patentSearchSource}}
	return record{ID: id, Date: day, Source: patentSource, DateBasis: patentSearchDateBasis, FamilyBasis: "unknown",
		URL: patentSearchBase + "/pubwebapp/external.html?" + params.Encode()}
}

// Готовность поздних страниц бюллетеня фиксируется до пометки ранней части месяца как ожидающей:
// полный диапазон истории не должен прятать ошибку в независимо собранных поздних выпусках.
func preparePatentHistory(data *snapshot, progress *patentMetadataProgress) map[int]bool {
	archiveComplete := map[int]bool{}
	if data.PatentIndexFrom == "" || data.PatentIndexFrom <= data.From {
		return archiveComplete
	}
	first, err := time.Parse(time.DateOnly, data.PatentIndexFrom)
	if err != nil {
		return archiveComplete
	}
	through := min(data.Through, first.AddDate(0, 0, -1).Format(time.DateOnly))
	for _, window := range monthWindows(data.From, through) {
		end, _ := time.Parse(time.DateOnly, window.End)
		last := end.AddDate(0, 0, -1).Format(time.DateOnly)
		query := "@PD>=" + strings.ReplaceAll(window.Start, "-", "") + "<=" + strings.ReplaceAll(last, "-", "")
		progress.HistoryWindows = append(progress.HistoryWindows,
			patentHistoryWindow{From: window.Start, Through: last, Query: query, Cursor: "0"})
		for i := range data.Coverage {
			c := &data.Coverage[i]
			// Только US: строка другой юрисдикции может покрывать те же дни.
			if c.Source != patentSource || window.Start < c.From || window.Start > c.Through {
				continue
			}
			archiveComplete[i] = c.Exhausted
			c.Exhausted = false
			c.StopReason = "dated USPTO Public Search history pending"
			c.Cursor = query + "@0"
		}
	}
	return archiveComplete
}

func (s *service) completePatentHistory(
	ctx context.Context, client *patentSearchClient, data *snapshot, index map[string]int,
	progress *patentMetadataProgress, archiveComplete map[int]bool,
) error {
	for i := range progress.HistoryWindows {
		window := &progress.HistoryWindows[i]
		err := s.patentHistoryRange(ctx, client, data, index, progress, i)
		for ci := range data.Coverage {
			c := &data.Coverage[ci]
			// Только US: строка другой юрисдикции может покрывать те же дни.
			if c.Source != patentSource || window.From < c.From || window.From > c.Through {
				continue
			}
			wholeMonth := window.From == c.From && window.Through == c.Through
			if wholeMonth {
				c.Total = window.Total
			}
			c.Exhausted = window.Exhausted && (wholeMonth || archiveComplete[ci])
			c.Cursor = window.Query + "@" + window.Cursor
			if c.Exhausted {
				c.StopReason = "dated USPTO Public Search history and available Gazette indices complete"
				c.Cursor = ""
			} else {
				c.StopReason = "dated USPTO Public Search history or later Gazette indices pending"
			}
		}
		if err != nil {
			return err
		}
	}
	return nil
}

func (s *service) patentHistoryRange(
	ctx context.Context, client *patentSearchClient, data *snapshot, index map[string]int,
	progress *patentMetadataProgress, windowIndex int,
) error {
	window := &progress.HistoryWindows[windowIndex]
	seen := map[string]bool{}
	for start := 0; ; {
		raw, receipt, err := s.metadataSearchPage(ctx, client, window.Query, start, nil, data.ObservedAt)
		if err != nil {
			return err
		}
		var response patentSearchResponse
		if err = json.Unmarshal(raw, &response); err != nil {
			return fmt.Errorf("decode USPTO historical page: %w", err)
		}
		if window.Total != nil && *window.Total != response.Found {
			return errors.New("USPTO historical population changed between pinned pages")
		}
		window.Total = new(response.Found)
		page := provenance{URL: receipt.URL, SHA256: receipt.SHA256}
		appendMetadataPage(data, page)
		if err = applyPatentHistoryPage(data, index, window, seen, response.Patents, page, receipt.ObservedAt); err != nil {
			return err
		}
		start += len(response.Patents)
		window.Received = start
		window.Cursor = strconv.Itoa(start)
		window.Exhausted = start == response.Found
		progress.Window = window.From + ".." + window.Through
		progress.Offset = start
		progress.Total = new(response.Found)
		if err = s.savePatentProgress(progress, data, "publication range "+progress.Window); err != nil {
			return err
		}
		if window.Exhausted {
			window.Cursor = ""
			return nil
		}
		if start > response.Found {
			return errors.New("USPTO historical page exceeds the pinned total")
		}
	}
}

func applyPatentHistoryPage(
	data *snapshot, index map[string]int, window *patentHistoryWindow, seen map[string]bool,
	records []patentSearchRecord, page provenance, observed string,
) error {
	for i := range data.Coverage {
		c := &data.Coverage[i]
		if window.From >= c.From && window.From <= c.Through && !slices.Contains(c.Pages, page) {
			c.Pages = append(c.Pages, page)
		}
	}
	for _, found := range records {
		day := structuredDay(found.Published)
		if day < window.From || day > window.Through {
			return errors.New("USPTO historical page returned a date outside the pinned range")
		}
		if err := applyPatentWeekPage(data, index, day, seen, []patentSearchRecord{found}, page, observed); err != nil {
			return err
		}
	}
	return nil
}
