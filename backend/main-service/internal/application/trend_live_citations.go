package application

import (
	"bytes"
	"context"
	"crypto/sha256"
	"fmt"
	"log/slog"
	"net/url"
	"os"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/example/main-service/internal/platform/jsonx"
)

// Влияние темы — цитируемость по OpenAlex, нормированная на область и месяц публикации, как FWCI: свежие
// статьи почти не цитируются, поэтому норма помесячная.
const (
	liveOpenAlexBase     = "https://api.openalex.org"
	liveCitationsBatch   = 100
	liveCitationsPerCell = 30
	liveCitationsMinCell = 5
	liveCitationsMaxAge  = 7 * 24 * time.Hour
	liveCitationCacheKey = "trends/live/openalex-citations-v1.json"
	liveCitationSource   = "openalex"
	liveStepCitations    = "openalex"
)

var liveArxivVersion = regexp.MustCompile(`v\d+$`)

type LiveCitations struct {
	Source     string   `json:"source"`
	ObservedAt string   `json:"observed_at"`
	Papers     int      `json:"papers"`
	Missing    int      `json:"missing"`
	Total      int      `json:"total"`
	Cited      int      `json:"cited"`
	Expected   float64  `json:"expected"`
	Normalized *float64 `json:"normalized"`
}

type LiveCitationSummary struct {
	Source     string `json:"source"`
	ObservedAt string `json:"observed_at"`
	Cells      int    `json:"cells"`
	Papers     int    `json:"papers"`
	Requests   int    `json:"requests"`
}

// liveDOI — DOI издателя, иначе DataCite DOI, который arXiv выдаёт каждой работе.
func liveDOI(paper StudyPaper, meta LivePaperMetadata) string {
	if doi := strings.ToLower(strings.TrimSpace(meta.DOI)); doi != "" {
		return strings.TrimPrefix(strings.TrimPrefix(doi, "https://doi.org/"), "doi:")
	}
	if id, ok := strings.CutPrefix(paper.ID, "arxiv:"); ok {
		return "10.48550/arxiv." + strings.ToLower(liveArxivVersion.ReplaceAllString(id, ""))
	}
	return ""
}

func (s *liveSources) openAlexBatch(ctx context.Context, dois []string) (map[string]int, error) {
	page, err := s.oa.Works(ctx, url.Values{"filter": {"doi:" + strings.Join(dois, "|")},
		"select": {"doi,cited_by_count"}, "per-page": {strconv.Itoa(liveCitationsBatch)}})
	if err != nil {
		return nil, err
	}
	found := make(map[string]int, len(page.Results))
	for _, work := range page.Results {
		found[work.PlainDOI()] = work.CitedByCount
	}
	return found, nil
}

// Неизвестная OpenAlex работа записывается как пропуск, а не ноль.
func (s *liveSources) openAlexCitations(ctx context.Context, papers []StudyPaper, now time.Time) (int, error) {
	pending, byDOI := liveCitationRequests(papers, s.metadata, now)
	requests := 0
	stamp := now.UTC().Format(time.RFC3339)
	for start := 0; start < len(pending); start += liveCitationsBatch {
		// Цитирования — фон ночного прохода: с окном препринтов ячеек стало в разы больше, и без запаса они
		// съедали суточный бюджет, оставляя живой поиск без журналов и истории терминов.
		if err := liveScholarReserved(s.oa); err != nil {
			return requests, fmt.Errorf("%w; %d papers keep counts read earlier", err, len(pending)-start)
		}
		s.step(liveStepCitations, 0)
		batch := pending[start:min(start+liveCitationsBatch, len(pending))]
		found, err := s.openAlexBatch(ctx, batch)
		if err != nil {
			return requests, fmt.Errorf("%w; %d papers keep counts read earlier", err, len(pending)-start)
		}
		requests++
		for _, doi := range batch {
			for _, id := range byDOI[doi] {
				meta := s.metadata[id]
				meta.CitedAt = stamp
				if count, ok := found[doi]; ok {
					meta.CitedBy = &count
				} else {
					meta.CitedBy = nil
				}
				s.metadata[id] = meta
			}
		}
	}
	return requests, nil
}

func liveCitationRequests(papers []StudyPaper, metadata map[string]LivePaperMetadata, now time.Time,
) ([]string, map[string][]string) {
	pending, seen := []string{}, map[string]bool{}
	byDOI := map[string][]string{}
	for _, paper := range papers {
		meta := metadata[paper.ID]
		if at, err := time.Parse(time.RFC3339, meta.CitedAt); err == nil && now.Sub(at) < liveCitationsMaxAge {
			continue
		}
		doi := liveDOI(paper, meta)
		if doi == "" || seen[paper.ID] {
			continue
		}
		seen[paper.ID] = true
		if len(byDOI[doi]) == 0 {
			pending = append(pending, doi)
		}
		byDOI[doi] = append(byDOI[doi], paper.ID)
	}
	return pending, byDOI
}

type liveCitationCell struct{ sum, n int }

func liveCitationCellKey(paper StudyPaper, meta LivePaperMetadata) string {
	if len(paper.Date) < 7 {
		return ""
	}
	return liveBranch(meta.Category) + ":" + paper.Date[:7]
}

func liveCitationBaselinePapers(window []StudyPaper, metadata map[string]LivePaperMetadata) []StudyPaper {
	cells := map[string][]StudyPaper{}
	for _, paper := range window {
		if key := liveCitationCellKey(paper, metadata[paper.ID]); key != "" {
			cells[key] = append(cells[key], paper)
		}
	}
	out := []StudyPaper{}
	for _, key := range slices.Sorted(func(yield func(string) bool) {
		for key := range cells {
			if !yield(key) {
				return
			}
		}
	}) {
		group := cells[key]
		slices.SortFunc(group, func(a, b StudyPaper) int {
			x, y := sha256.Sum256([]byte("citations:"+a.ID)), sha256.Sum256([]byte("citations:"+b.ID))
			return bytes.Compare(x[:], y[:])
		})
		out = append(out, group[:min(liveCitationsPerCell, len(group))]...)
	}
	return out
}

func liveCitationCells(papers []StudyPaper, metadata map[string]LivePaperMetadata) map[string]liveCitationCell {
	cells := map[string]liveCitationCell{}
	for _, paper := range papers {
		meta := metadata[paper.ID]
		if meta.CitedBy == nil {
			continue
		}
		key := liveCitationCellKey(paper, meta)
		cell := cells[key]
		cell.sum, cell.n = cell.sum+*meta.CitedBy, cell.n+1
		cells[key] = cell
	}
	return cells
}

func liveTopicCitations(papers []StudyPaper, metadata map[string]LivePaperMetadata,
	cells map[string]liveCitationCell, observedAt string) *LiveCitations {
	out := &LiveCitations{Source: liveCitationSource, ObservedAt: observedAt}
	normalizable := 0
	for _, paper := range papers {
		meta := metadata[paper.ID]
		if meta.CitedBy == nil {
			out.Missing++
			continue
		}
		out.Papers++
		out.Total += *meta.CitedBy
		if *meta.CitedBy > 0 {
			out.Cited++
		}
		if cell := cells[liveCitationCellKey(paper, meta)]; cell.n >= liveCitationsMinCell {
			out.Expected += float64(cell.sum) / float64(cell.n)
			normalizable++
		}
	}
	if out.Papers+out.Missing == 0 {
		return nil
	}
	if normalizable >= liveCitationsMinCell && out.Expected >= 1 {
		ratio := float64(out.Total) / out.Expected
		out.Normalized = &ratio
	}
	return out
}

// collectCitations: отказ OpenAlex — оговорка снимка, остальное наблюдение от цитирований не зависит.
func (s *TrendLiveService) collectCitations(ctx context.Context, src *liveSources, snapshot *LiveSnapshot,
	window []StudyPaper) {
	if os.Getenv("OPENALEX_CITATIONS") == "false" {
		return
	}
	now := time.Now()
	baseline := liveCitationBaselinePapers(window, src.metadata)
	papers := slices.Clone(baseline)
	for _, topics := range [][]LiveTopic{snapshot.Topics, snapshot.Discovered} {
		for _, topic := range topics {
			for _, id := range topic.PaperIDs {
				papers = append(papers, StudyPaper{ID: id, Date: liveDateOf(snapshot.Papers, id)})
			}
		}
	}
	liveSeedCitations(src.metadata, s.loadCitationCache(ctx), papers)
	requests, err := src.openAlexCitations(ctx, papers, now)
	if err != nil {
		slog.Warn("radar citations unavailable", "error", err, "requests", requests)
		snapshot.Limits = append(snapshot.Limits, liveLimitFail(liveLimitCitationsOff, "citations", err))
	}
	s.saveCitationCache(ctx, src.metadata, papers)
	cells := liveCitationCells(baseline, src.metadata)
	observedAt := now.UTC().Format(time.RFC3339)
	known := 0
	for _, cell := range cells {
		known += cell.n
	}
	snapshot.Citations = &LiveCitationSummary{Source: liveCitationSource, ObservedAt: observedAt, Cells: len(cells),
		Papers: known, Requests: requests}
	byID := make(map[string]StudyPaper, len(snapshot.Papers))
	for _, paper := range snapshot.Papers {
		byID[paper.ID] = paper
	}
	for _, topics := range [][]LiveTopic{snapshot.Topics, snapshot.Discovered} {
		for i := range topics {
			members := make([]StudyPaper, 0, len(topics[i].PaperIDs))
			for _, id := range topics[i].PaperIDs {
				paper, ok := byID[id]
				if !ok {
					paper = StudyPaper{ID: id}
				}
				members = append(members, paper)
			}
			topics[i].Citations = liveTopicCitations(members, src.metadata, cells, observedAt)
		}
	}
}

func liveDateOf(papers []StudyPaper, id string) string {
	for _, paper := range papers {
		if paper.ID == id {
			return paper.Date
		}
	}
	return ""
}

type liveCitationRecord struct {
	CitedBy *int   `json:"cited_by,omitempty"`
	CitedAt string `json:"cited_at"`
}

// Счётчики живут в отдельном объекте: при отказе OpenAlex норма считается по последним прочитанным значениям.
func (s *TrendLiveService) loadCitationCache(ctx context.Context) map[string]liveCitationRecord {
	cached := map[string]liveCitationRecord{}
	if raw, err := s.store.GetBytes(ctx, liveCitationCacheKey); err == nil {
		_ = jsonx.Unmarshal(raw, &cached)
	}
	return cached
}

func liveSeedCitations(metadata map[string]LivePaperMetadata, cached map[string]liveCitationRecord, papers []StudyPaper) {
	for _, paper := range papers {
		if rec, ok := cached[paper.ID]; ok && metadata[paper.ID].CitedAt == "" {
			meta := metadata[paper.ID]
			meta.CitedBy, meta.CitedAt = rec.CitedBy, rec.CitedAt
			metadata[paper.ID] = meta
		}
	}
}

func (s *TrendLiveService) saveCitationCache(ctx context.Context, metadata map[string]LivePaperMetadata,
	papers []StudyPaper) {
	records := map[string]liveCitationRecord{}
	for _, paper := range papers {
		if meta := metadata[paper.ID]; meta.CitedAt != "" {
			records[paper.ID] = liveCitationRecord{CitedBy: meta.CitedBy, CitedAt: meta.CitedAt}
		}
	}
	raw, err := jsonx.Marshal(records)
	if err == nil {
		err = s.store.Put(ctx, liveCitationCacheKey, bytes.NewReader(raw), int64(len(raw)), "application/json")
	}
	if err != nil {
		slog.Warn("radar citation cache not saved", "error", err)
	}
}
