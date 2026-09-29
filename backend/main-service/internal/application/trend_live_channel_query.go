package application

import (
	"context"
	"encoding/json/v2"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"strings"
	"sync"
)

const liveChannelQueryBatches = 2

// Сборщик может работать со старым пределом восьми фраз: полный план делится без перезапуска источника.
func (src *liveSources) queryChannelTerms(ctx context.Context, base string, terms []string,
	periods []LivePeriod, excerpts bool,
) (*LiveChannel, error) {
	if len(terms) == 0 || len(terms) > liveChannelQueryBatches*liveChannelTerms {
		return nil, fmt.Errorf("%w: expected 1–%d terms, got %d", errLiveChannelQuery,
			liveChannelQueryBatches*liveChannelTerms, len(terms))
	}
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("source query context: %w", err)
	}
	batches := slices.Collect(slices.Chunk(terms, liveChannelTerms))
	channels := make([]*LiveChannel, len(batches))
	failures := make([]error, len(batches))
	var wg sync.WaitGroup
	for i, batch := range batches {
		wg.Go(func() {
			body, err := json.Marshal(struct {
				Terms    []string     `json:"terms"`
				Periods  []LivePeriod `json:"periods"`
				Excerpts bool         `json:"excerpts,omitzero"`
			}{batch, periods, excerpts})
			if err == nil {
				channels[i], err = src.queryChannel(ctx, base, body, len(periods))
			}
			if err != nil {
				failures[i] = fmt.Errorf("source query batch %d/%d: %w", i+1, len(batches), err)
			}
		})
	}
	wg.Wait()
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("source query context: %w", err)
	}
	available := slices.DeleteFunc(channels, func(channel *LiveChannel) bool { return channel == nil })
	if len(available) == 0 {
		return nil, errors.Join(failures...)
	}
	if len(batches) == 1 {
		return available[0], nil
	}
	merged := liveMergeQueryChannels(available, periods)
	for _, err := range failures {
		if err != nil {
			merged.Partial = true
			merged.Limits = append(merged.Limits, "Часть поискового плана источника не выполнена; "+
				"счётчики — нижняя граница по ответившим пакетам: "+err.Error())
		}
	}
	slog.Info("radar source query batches", "source", merged.Source, "terms", len(terms), "batches", len(batches),
		"succeeded", len(available), "records", len(merged.Records), "total", merged.Total, "partial", merged.Partial)
	return merged, nil
}

// Сумма ответов неверна: одна запись может совпасть с фразами обоих пакетов.
// Усечённые выборки дают лишь нижнюю границу объединения; полные можно пересчитать по записям.
func liveMergeQueryChannels(channels []*LiveChannel, periods []LivePeriod) *LiveChannel {
	merged := *channels[0]
	merged.Records, merged.Coverage, merged.Limits = nil, nil, nil
	merged.Counts, merged.Total = make([]*int, len(periods)), 0
	seen := map[string]bool{}
	coverage := map[string]int{}
	bounded, mixed := false, false
	for _, channel := range channels {
		merged.Partial = merged.Partial || channel.Partial
		mixed = mixed || channel.ObservedAt != merged.ObservedAt || channel.SourceSet != merged.SourceSet
		bounded = bounded || channel.Total > len(channel.Records)
		merged.Total = max(merged.Total, channel.Total)
		liveQueryMaxCounts(merged.Counts, channel.Counts)
		for _, record := range channel.Records {
			if key := liveRecordKey(record); !seen[key] {
				seen[key] = true
				merged.Records = append(merged.Records, record)
			}
		}
		for _, limit := range channel.Limits {
			if !slices.Contains(merged.Limits, limit) {
				merged.Limits = append(merged.Limits, limit)
			}
		}
		merged.Coverage = liveQueryCoverage(merged.Coverage, channel.Coverage, coverage)
	}
	if bounded || mixed {
		merged.Partial = true
		merged.Limits = append(merged.Limits,
			"Пакеты источника усечены или относятся к разным снимкам; счётчики — нижняя граница без повторного счёта записей.")
	}
	liveQueryUnionCounts(&merged, periods, bounded || mixed)
	return &merged
}

func liveQueryMaxCounts(into, from []*int) {
	for i, count := range from {
		if count == nil {
			continue
		}
		if into[i] == nil {
			into[i] = new(0)
		}
		*into[i] = max(*into[i], *count)
	}
}

// Покрытие относится ко всему снимку, а не к поисковой фразе: повторный запрос не создаёт новых наблюдений.
func liveQueryCoverage(into, from []LiveSourceCoverage, seen map[string]int) []LiveSourceCoverage {
	for _, row := range from {
		key := strings.Join([]string{row.Source, row.From, row.Through, row.ObservedAt}, "\x00")
		if at, exists := seen[key]; exists {
			for _, page := range row.Pages {
				if !slices.Contains(into[at].Pages, page) {
					into[at].Pages = append(into[at].Pages, page)
				}
			}
		} else {
			seen[key] = len(into)
			row.Pages = slices.Clone(row.Pages)
			into = append(into, row)
		}
	}
	return into
}

func liveQueryUnionCounts(channel *LiveChannel, periods []LivePeriod, bounded bool) {
	counts := make([]int, len(periods))
	for _, record := range channel.Records {
		if i := livePeriodIndex(periods, record.Date); i >= 0 {
			counts[i]++
		}
	}
	total := 0
	for i, count := range counts {
		if channel.Counts[i] == nil && count == 0 {
			continue
		}
		if bounded && channel.Counts[i] != nil {
			count = max(count, *channel.Counts[i])
		}
		channel.Counts[i] = new(count)
		total += count
	}
	if bounded {
		channel.Total = max(channel.Total, total, len(channel.Records))
	} else {
		channel.Total = len(channel.Records)
	}
}
