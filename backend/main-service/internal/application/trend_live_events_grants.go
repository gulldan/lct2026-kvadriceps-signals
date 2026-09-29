package application

import (
	"bytes"
	"cmp"
	"compress/gzip"
	"context"
	"encoding/json/v2"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"slices"
	"strings"
	"sync"
	"time"
)

// Гранты ищутся по открытым выгрузкам: API SBIR со стенда отвечает 403, а API CORDIS не отдаёт схему финансирования.

const (
	liveGrantRefresh = 7 * 24 * time.Hour
	// Без паузы каждый поиск заново качал бы сотни мегабайт через ту же рвущуюся сеть.
	liveGrantRetry  = time.Hour
	liveGrantBudget = 30 * time.Minute
	// Грант десятилетней давности — не ранний сигнал, а память реплики не резиновая (SBIR с 2019 года — около 60 МБ).
	liveGrantYears    = 7
	liveGrantPerQuery = 12
)

type liveGrant struct {
	ID     string `json:"id"`
	Type   string `json:"type"`
	Title  string `json:"title"`
	Text   string `json:"text"`
	Actor  string `json:"actor"`
	Amount string `json:"amount,omitempty"`
	Phase  string `json:"phase"`
	Date   string `json:"date"`
	URL    string `json:"url"`
}

type liveGrantDump struct {
	LoadedAt time.Time   `json:"loaded_at"`
	Source   string      `json:"source"`
	Grants   []liveGrant `json:"grants"`
}

type liveGrantSource struct {
	name      string
	key       string
	url       string
	dateBasis string
	parse     func(file *os.File, since int) ([]liveGrant, error)
}

type liveGrantIndex struct {
	mu         sync.Mutex
	dump       *liveGrantDump
	read       bool
	refreshing bool
	failedAt   time.Time
}

// grantEvents отвечает только по выгрузке в памяти: сам поиск ничего не качает.
func (s *TrendLiveService) grantEvents(index *liveGrantIndex, source liveGrantSource,
	query LiveEventQuery) ([]LiveAdoptionEvent, error) {
	index.mu.Lock()
	dump := index.dump
	index.mu.Unlock()
	if dump == nil {
		return nil, fmt.Errorf("%s: выгрузка грантов ещё загружается в фоне", source.name)
	}
	return liveGrantMatches(dump.Grants, source, query.Term), nil
}

func (s *TrendLiveService) keepGrantDumps(ctx context.Context) {
	for _, grants := range []struct {
		index  *liveGrantIndex
		source liveGrantSource
	}{{&s.eventData.sbir, liveSBIRSource()}, {&s.eventData.cordis, liveCORDISSource()}} {
		s.keepGrantDump(ctx, grants.index, grants.source)
	}
}

func (s *TrendLiveService) keepGrantDump(ctx context.Context, index *liveGrantIndex, source liveGrantSource) {
	index.mu.Lock()
	defer index.mu.Unlock()
	if !index.read {
		index.read = true
		index.dump = s.readGrantDump(ctx, source)
	}
	now := s.now()
	stale := index.dump == nil || now.Sub(index.dump.LoadedAt) > liveGrantRefresh
	if stale && !index.refreshing && now.Sub(index.failedAt) > liveGrantRetry {
		index.refreshing = true
		go s.refreshGrants(ctx, index, source)
	}
}

func (s *TrendLiveService) readGrantDump(ctx context.Context, source liveGrantSource) *liveGrantDump {
	raw, err := s.store.GetBytes(ctx, source.key)
	if err != nil {
		return nil
	}
	reader, err := gzip.NewReader(bytes.NewReader(raw))
	if err != nil {
		return nil
	}
	defer func() { _ = reader.Close() }()
	var dump liveGrantDump
	if json.UnmarshalRead(reader, &dump) != nil {
		return nil
	}
	return &dump
}

// refreshGrants держит старую выгрузку до конца разбора новой: устаревшая лучше пустого источника.
// ponytail: каждая реплика качает сама; на стенде реплика одна, общая аренда — когда их станет больше.
func (s *TrendLiveService) refreshGrants(ctx context.Context, index *liveGrantIndex, source liveGrantSource) {
	ctx, cancel := context.WithTimeout(ctx, liveGrantBudget)
	defer cancel()
	started := time.Now()
	dump, err := liveDownloadGrants(ctx, source, s.now())
	if err == nil {
		err = s.saveGrantDump(ctx, source.key, dump)
	}
	index.mu.Lock()
	defer index.mu.Unlock()
	index.refreshing = false
	if dump != nil {
		index.dump = dump
	}
	if err != nil {
		index.failedAt = s.now()
		slog.Warn("grant dump refresh failed", "source", source.name, "error", err)
		return
	}
	slog.Info("grant dump refreshed", "source", source.name, "grants", len(dump.Grants),
		"seconds", time.Since(started).Seconds())
}

// liveDownloadGrants пишет во временный файл, а не в память: выгрузка SBIR весит 370 МБ, нужна шестая часть.
func liveDownloadGrants(ctx context.Context, source liveGrantSource, now time.Time) (*liveGrantDump, error) {
	file, err := os.CreateTemp("", "radar-grants-*")
	if err != nil {
		return nil, fmt.Errorf("%s: temp file: %w", source.name, err)
	}
	defer func() { _ = file.Close(); _ = os.Remove(file.Name()) }()
	request := func(ctx context.Context) (*http.Request, error) {
		req, buildErr := http.NewRequestWithContext(ctx, http.MethodGet, source.url, http.NoBody)
		if buildErr != nil {
			return nil, fmt.Errorf("%s: build request: %w", source.name, buildErr)
		}
		req.Header.Set("User-Agent", liveEventAgent)
		return req, nil
	}
	restart := func() error {
		if truncErr := file.Truncate(0); truncErr != nil {
			return fmt.Errorf("%s: truncate: %w", source.name, truncErr)
		}
		if _, seekErr := file.Seek(0, io.SeekStart); seekErr != nil {
			return fmt.Errorf("%s: rewind: %w", source.name, seekErr)
		}
		return nil
	}
	// Без общего срока на соединение: сотни мегабайт идут минутами, а зависание ловит сторож куска.
	if err = liveMarketRead(ctx, nil, request, file, restart, 1<<40); err != nil {
		return nil, fmt.Errorf("%s: download: %w", source.name, err)
	}
	if _, err = file.Seek(0, io.SeekStart); err != nil {
		return nil, fmt.Errorf("%s: rewind: %w", source.name, err)
	}
	grants, err := source.parse(file, now.Year()-liveGrantYears)
	if err != nil {
		return nil, fmt.Errorf("%s: parse: %w", source.name, err)
	}
	if len(grants) == 0 {
		return nil, fmt.Errorf("%s: выгрузка разобрана, но грантов в ней нет", source.name)
	}
	return &liveGrantDump{LoadedAt: now, Source: source.url, Grants: grants}, nil
}

func (s *TrendLiveService) saveGrantDump(ctx context.Context, key string, dump *liveGrantDump) error {
	var buf bytes.Buffer
	writer := gzip.NewWriter(&buf)
	if err := json.MarshalWrite(writer, dump); err != nil {
		return fmt.Errorf("grants: encode: %w", err)
	}
	if err := writer.Close(); err != nil {
		return fmt.Errorf("grants: compress: %w", err)
	}
	if err := s.store.Put(ctx, key, bytes.NewReader(buf.Bytes()), int64(buf.Len()), "application/gzip"); err != nil {
		return fmt.Errorf("grants: persist: %w", err)
	}
	return nil
}

// liveGrantMatches отсекает подстрокой: резать десятки мегабайт на предложения под каждый термин дорого.
func liveGrantMatches(grants []liveGrant, source liveGrantSource, term string) []LiveAdoptionEvent {
	probe := liveGrantProbe(term)
	if probe == "" {
		return []LiveAdoptionEvent{}
	}
	head := []rune(probe)
	variants := []string{probe, strings.ToUpper(string(head[:1])) + string(head[1:]), strings.ToUpper(probe)}
	events := []LiveAdoptionEvent{}
	for _, grant := range grants {
		if !slices.ContainsFunc(variants, func(v string) bool {
			return strings.Contains(grant.Text, v) || strings.Contains(grant.Title, v)
		}) {
			continue
		}
		quote := cmp.Or(liveTermSentence(grant.Text, term), liveTermSentence(grant.Title, term))
		if quote == "" {
			continue
		}
		events = append(events, LiveAdoptionEvent{
			Type: grant.Type, Source: source.name,
			Record: liveMarketRecord(grant.ID, grant.Title, grant.Date, grant.URL, source.name, source.dateBasis),
			Actor:  grant.Actor, Amount: grant.Amount, Phase: grant.Phase, Quote: quote,
		})
	}
	slices.SortStableFunc(events, func(a, b LiveAdoptionEvent) int { return strings.Compare(b.Record.Date, a.Record.Date) })
	return events[:min(len(events), liveGrantPerQuery)]
}

// liveGrantProbe — начало слова, а не слово: «batteries» не содержит «battery», а «Robots» содержит «Robot».
func liveGrantProbe(term string) string {
	var longest []rune
	for _, word := range liveWords(term) {
		if runes := []rune(word); len(runes) > len(longest) {
			longest = runes
		}
	}
	return string(longest[:min(len(longest), 5)])
}
