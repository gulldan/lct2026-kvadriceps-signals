package application

import (
	"context"
	"encoding/json/v2"
	"fmt"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"testing"
)

// Сервер воспроизводит действующий контракт сборщика: восемь фраз, пересекающиеся совпадения и максимум 200 записей.
func TestLiveSourceQueryPreservesExpandedPlanAgainstLegacyCollector(t *testing.T) {
	for _, mode := range []string{"patent", "web", "capped", "one_failure", "all_failures", "canceled", "over_budget"} {
		t.Run(mode, func(t *testing.T) {
			periods := []LivePeriod{{Start: "2026-01-01", EndExclusive: "2026-02-01"},
				{Start: "2026-02-01", EndExclusive: "2026-03-01"}, {Start: "2026-03-01", EndExclusive: "2026-04-01"}}
			terms := make([]string, 0, 16)
			for i := range 12 {
				terms = append(terms, fmt.Sprintf("mechanism %02d", i))
			}
			rows := liveChannelQueryFixture(mode == "capped")
			var mu sync.Mutex
			var asked [][]string
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var request struct {
					Terms   []string     `json:"terms"`
					Periods []LivePeriod `json:"periods"`
				}
				if r.URL.Path != "/v1/query" || json.UnmarshalRead(r.Body, &request) != nil ||
					len(request.Terms) == 0 || len(request.Terms) > 8 || !slices.Equal(request.Periods, periods) {
					http.Error(w, "invalid terms or calendar", http.StatusBadRequest)
					return
				}
				mu.Lock()
				asked = append(asked, slices.Clone(request.Terms))
				mu.Unlock()
				if mode == "all_failures" || mode == "one_failure" && slices.Contains(request.Terms, terms[11]) {
					http.Error(w, "source unavailable", http.StatusServiceUnavailable)
					return
				}
				matched := []LiveSourceRecord{}
				counts := make([]*int, len(periods))
				for _, row := range rows {
					if !slices.ContainsFunc(request.Terms, func(term string) bool { return strings.Contains(row.Title, term) }) {
						continue
					}
					matched = append(matched, row)
					i := livePeriodIndex(periods, row.Date)
					if counts[i] == nil {
						counts[i] = new(0)
					}
					*counts[i]++
				}
				w.Header().Set("Content-Type", "application/json")
				_ = json.MarshalWrite(w, LiveChannel{Source: "legacy source", SourceSet: "test", ObservedAt: "2026-04-01T00:00:00Z",
					Counts: counts, Total: len(matched), Records: matched[:min(200, len(matched))],
					Coverage: []LiveSourceCoverage{{Source: "legacy source", Received: len(rows), ObservedAt: "2026-04-01T00:00:00Z",
						Pages: []LiveSourceProvenance{{URL: "https://example.org/collection", SHA256: "hash"}}}}})
			}))
			defer server.Close()
			for _, name := range []string{"PATENT_COLLECTOR_URL", "CN_PATENT_COLLECTOR_URL", "WEB_COLLECTOR_URL"} {
				t.Setenv(name, "")
			}
			target := "CN_PATENT_COLLECTOR_URL"
			if mode == "web" {
				target = "WEB_COLLECTOR_URL"
			}
			t.Setenv(target, server.URL)
			topic := LiveTopic{ID: "search:compatibility", Query: LiveQuery{Terms: slices.Clone(terms)},
				Name: "анализ платежей и защита клиентов и контроль операций и проверка переводов"}
			if mode == "over_budget" {
				topic.Query.Terms = append(topic.Query.Terms, "extra a", "extra b", "extra c", "extra d", "extra e")
			}
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			if mode == "canceled" {
				cancel()
			}
			src := NewTrendLiveService(nil, nil).newSources()
			liveChannels(ctx, src, &topic, periods)
			channel := topic.PatentsCN
			if mode == "web" {
				channel = topic.Web
				terms = append(terms, liveRussianTerms(topic.Name)...)
			}
			if mode == "canceled" || mode == "over_budget" {
				if len(asked) != 0 || channel != nil || len(topic.Limits) != 1 {
					t.Fatalf("canceled or oversized plan escaped: calls=%d channel=%+v limits=%+v", len(asked), channel, topic.Limits)
				}
				// Отвергнутый план одной темы не выключает канал для следующих тем того же прохода.
				next := LiveTopic{ID: "search:next", Query: LiveQuery{Terms: terms[:2]}}
				liveChannels(ctx, src, &next, periods)
				if mode == "over_budget" && next.PatentsCN == nil {
					t.Fatalf("one oversized plan switched the channel off: %+v", next.Limits)
				}
				return
			}
			flat := slices.Concat(asked...)
			slices.Sort(flat)
			slices.Sort(terms)
			if len(asked) != 2 || !slices.Equal(flat, terms) {
				t.Fatalf("query terms were dropped or repeated: batches=%v expected=%v", asked, terms)
			}
			if mode == "all_failures" {
				if channel != nil || len(topic.Limits) != 1 {
					t.Fatalf("complete source failure was swallowed: %+v", topic)
				}
				return
			}
			wantTotal, wantLast, wantFirst, partial := 3, 2, 1, false
			switch mode {
			case "capped":
				wantTotal, wantLast, wantFirst, partial = 350, 350, 0, true
			case "one_failure":
				wantTotal, wantLast, partial = 2, 1, true
			}
			if channel == nil || channel.Total != wantTotal || len(channel.Records) != wantTotal ||
				channel.Counts[1] == nil || *channel.Counts[1] != wantLast || channel.Counts[2] != nil || channel.Partial != partial {
				t.Fatalf("union lost last phrase, inflated overlaps or invented coverage: %+v", channel)
			}
			if wantFirst > 0 && (channel.Counts[0] == nil || *channel.Counts[0] != wantFirst) ||
				wantFirst == 0 && channel.Counts[0] != nil || partial != (len(channel.Limits) > 0) {
				t.Fatalf("unknown periods or partial response were not preserved: %+v", channel)
			}
			if len(channel.Coverage) != 1 || channel.Coverage[0].Received != len(rows) || len(channel.Coverage[0].Pages) != 1 {
				t.Fatalf("source snapshot coverage was multiplied by query batches: %+v", channel.Coverage)
			}
		})
	}
}

func liveChannelQueryFixture(capped bool) []LiveSourceRecord {
	if !capped {
		return []LiveSourceRecord{
			{ID: "first", Title: "mechanism 00", Date: "2026-01-10", URL: "https://example.org/first"},
			{ID: "common", Title: "mechanism 00 and mechanism 11", Date: "2026-02-10", URL: "https://example.org/common"},
			{ID: "last", Title: "mechanism 11", Date: "2026-02-11", URL: "https://example.org/last"},
		}
	}
	rows := make([]LiveSourceRecord, 451)
	for i := range rows {
		title := "mechanism 00 and mechanism 11"
		if i < 150 {
			title = "mechanism 00"
		} else if i >= 301 {
			title = "mechanism 11"
		}
		rows[i] = LiveSourceRecord{ID: fmt.Sprintf("record-%d", i), Title: title,
			Date: "2026-02-10", URL: fmt.Sprintf("https://example.org/work/%d", i)}
	}
	return rows
}
