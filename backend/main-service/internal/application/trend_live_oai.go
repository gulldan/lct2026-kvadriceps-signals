package application

import (
	"bytes"
	"cmp"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/json/v2"
	"encoding/xml"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"time"
)

const (
	liveOAIBase      = "https://oaipmh.arxiv.org/oai"
	liveOAIWindowKey = "trends/live/arxiv-window-v2.json.gz"
	liveOAIAttempts  = 5
	// Правило arXiv: не чаще запроса в три секунды и одно соединение суммарно со всех наших машин.
	liveOAIPause = 3 * time.Second
	// Retry-After у arXiv не заслуживает доверия (бывает 0 и 429 без заголовка): откат свой и тормозит весь
	// хост, потому что очередь у источника общая.
	liveOAIBackoff = time.Minute
	liveOAIMaxWait = 10 * time.Minute
)

// liveOAISets — все разделы верхнего уровня: зарождающееся ищется в каждой области, а не только в вычислениях.
func liveOAISets() []string {
	return []string{"cs", "physics", "math", "q-bio", "q-fin", "stat", "eess", "econ"}
}

type oaiError struct {
	Code    string `xml:"code,attr"`
	Message string `xml:",chardata"`
}

type oaiAuthor struct {
	Keyname   string `xml:"keyname"`
	Forenames string `xml:"forenames"`
}

// oaiMeta — формат metadataPrefix=arXiv: в отличие от Atom в нём есть created.
type oaiMeta struct {
	ID         string      `xml:"id"`
	Created    string      `xml:"created"`
	Updated    string      `xml:"updated"`
	Title      string      `xml:"title"`
	Abstract   string      `xml:"abstract"`
	Categories string      `xml:"categories"`
	DOI        string      `xml:"doi"`
	License    string      `xml:"license"`
	Authors    []oaiAuthor `xml:"authors>author"`
}

type oaiHeader struct {
	Status string `xml:"status,attr"`
}

type oaiRecord struct {
	Header oaiHeader `xml:"header"`
	Meta   oaiMeta   `xml:"metadata>arXiv"`
}

type oaiResponse struct {
	Error   oaiError    `xml:"error"`
	Records []oaiRecord `xml:"ListRecords>record"`
	Token   string      `xml:"ListRecords>resumptionToken"`
}

// liveWindowCache переживает перезапуск: окно в 14 месяцев — около 260 тысяч статей и порядка 200 запросов.
type liveWindowCache struct {
	Cursor   string                       `json:"cursor"`
	Sets     []string                     `json:"sets"`
	Papers   []StudyPaper                 `json:"papers"`
	Metadata map[string]LivePaperMetadata `json:"metadata"`
}

func (s *liveSources) oaiPage(ctx context.Context, values url.Values) (oaiResponse, error) {
	var out oaiResponse
	var last error
	wait := liveOAIPause
	for range liveOAIAttempts {
		// Долгие откаты осмысленны для ночного сбора, а не для человека у экрана.
		if s.patience > 0 && wait > s.patience {
			return out, fmt.Errorf("arxiv oai: source asked to wait %s, which is longer than one search may spend",
				wait.Round(time.Second))
		}
		s.step(liveStepArxiv, wait)
		if err := s.sleep(ctx, wait); err != nil {
			return out, err
		}
		body, _, status, err := s.get(ctx, s.oai+"?"+values.Encode(), nil)
		switch {
		case err != nil:
			last = fmt.Errorf("arxiv oai: %w", err)
		case status == http.StatusTooManyRequests || status == http.StatusServiceUnavailable:
			last = fmt.Errorf("arxiv oai: status %d", status)
		case status != http.StatusOK:
			return out, fmt.Errorf("arxiv oai: status %d", status)
		default:
			out = oaiResponse{}
			if err := xml.Unmarshal(body, &out); err != nil {
				return out, fmt.Errorf("arxiv oai: parse: %w", err)
			}
			if code := out.Error.Code; code != "" && code != "noRecordsMatch" {
				return out, fmt.Errorf("arxiv oai: %s: %s", code, strings.TrimSpace(out.Error.Message))
			}
			return out, nil
		}
		wait = min(max(2*wait, liveOAIBackoff), liveOAIMaxWait)
	}
	return out, last
}

// liveArxivSubmitted берёт месяц подачи из идентификатора: <created> часто указывает на последнюю версию.
// Старый стиль (cs/0701001) не разбирается — такие статьи в окно не попадают.
func liveArxivSubmitted(id, created string) string {
	if len(id) < 5 || id[4] != '.' {
		return ""
	}
	for i := range 4 {
		if id[i] < '0' || id[i] > '9' {
			return ""
		}
	}
	month := id[2:4]
	if month < "01" || month > "12" {
		return ""
	}
	prefix := "20" + id[:2] + "-" + month
	if len(created) == len(time.DateOnly) && strings.HasPrefix(created, prefix) {
		return created
	}
	return prefix + "-01"
}

// arxivHarvest перечисляет наборы по дате изменения, поэтому отбирает статьи по дате подачи: иначе старая
// статья с ревизией встала бы в текущий месяц.
func (s *liveSources) arxivHarvest(ctx context.Context, periods []LivePeriod, from, until time.Time,
) ([]StudyPaper, error) {
	papers := []StudyPaper{}
	seen := map[string]bool{}
	for _, set := range liveOAISets() {
		values := url.Values{"verb": {"ListRecords"}, "metadataPrefix": {"arXiv"}, "set": {set},
			"from": {from.Format(time.DateOnly)}, "until": {until.Format(time.DateOnly)}}
		for page := 1; ; page++ {
			out, err := s.oaiPage(ctx, values)
			if err != nil {
				return nil, fmt.Errorf("set %s: %w", set, err)
			}
			for _, record := range out.Records {
				id := strings.TrimSpace(record.Meta.ID)
				date := liveArxivSubmitted(id, record.Meta.Created)
				if record.Header.Status == "deleted" || id == "" || seen[id] ||
					livePeriodIndex(periods, date) < 0 {
					continue
				}
				seen[id] = true
				papers = append(papers, s.recordOAIRecord(id, date, record.Meta))
			}
			slog.Info("arxiv oai page", "set", set, "page", page, "records", len(out.Records), "kept", len(papers))
			if out.Token == "" {
				break
			}
			values = url.Values{"verb": {"ListRecords"}, "resumptionToken": {out.Token}}
		}
	}
	return papers, nil
}

func (s *liveSources) recordOAIRecord(id, date string, meta oaiMeta) StudyPaper {
	if s.metadata == nil {
		s.metadata = map[string]LivePaperMetadata{}
	}
	title := strings.Join(strings.Fields(meta.Title), " ")
	abstract := strings.Join(strings.Fields(meta.Abstract), " ")
	available := cmp.Or(strings.TrimSpace(meta.Updated), meta.Created)
	category, _, _ := strings.Cut(strings.TrimSpace(meta.Categories), " ")
	authors := make([]string, 0, len(meta.Authors))
	for _, author := range meta.Authors {
		name := strings.TrimSpace(strings.TrimSpace(author.Forenames) + " " + strings.TrimSpace(author.Keyname))
		if name != "" {
			authors = append(authors, name)
		}
	}
	s.metadata["arxiv:"+id] = LivePaperMetadata{
		Abstract: abstract, DOI: strings.TrimSpace(meta.DOI), Category: category,
		AvailableAt: available, ObservedAt: time.Now().UTC().Format(time.RFC3339),
		SourceURL:   "https://arxiv.org/abs/" + id,
		ContentHash: fmt.Sprintf("%x", sha256.Sum256([]byte(title+"\n"+abstract+"\n"+available))),
	}
	return StudyPaper{ID: "arxiv:" + id, Title: title, Date: date, CanonicalID: "arxiv:" + id,
		ContentHash: "", URL: "https://arxiv.org/abs/" + id, Authors: authors, DocumentID: "", SnapshotID: ""}
}

func (s *TrendLiveService) loadWindowCache(ctx context.Context) liveWindowCache {
	empty := liveWindowCache{Cursor: "", Sets: nil, Papers: nil, Metadata: nil}
	raw, err := s.store.GetBytes(ctx, liveOAIWindowKey)
	if err != nil {
		return empty
	}
	reader, err := gzip.NewReader(bytes.NewReader(raw))
	if err != nil {
		return empty
	}
	defer func() { _ = reader.Close() }()
	plain, err := io.ReadAll(reader)
	if err != nil {
		return empty
	}
	var cache liveWindowCache
	if json.Unmarshal(plain, &cache) != nil || !slices.Equal(cache.Sets, liveOAISets()) {
		return empty
	}
	return cache
}

func (s *TrendLiveService) saveWindowCache(ctx context.Context, cache liveWindowCache) error {
	var buf bytes.Buffer
	writer := gzip.NewWriter(&buf)
	if err := json.MarshalWrite(writer, cache); err != nil {
		return fmt.Errorf("live: encode window: %w", err)
	}
	if err := writer.Close(); err != nil {
		return fmt.Errorf("live: compress window: %w", err)
	}
	if err := s.store.Put(ctx, liveOAIWindowKey, bytes.NewReader(buf.Bytes()), int64(buf.Len()),
		"application/gzip"); err != nil {
		return fmt.Errorf("live: persist window: %w", err)
	}
	return nil
}

// liveMergeWindow берёт аннотации перенесённых статей из кеша: без них смысловой разбор их не увидит.
func liveMergeWindow(src *liveSources, cache liveWindowCache, fresh []StudyPaper,
	periods []LivePeriod) []StudyPaper {
	papers := make([]StudyPaper, 0, len(cache.Papers)+len(fresh))
	seen := make(map[string]bool, len(fresh))
	for _, paper := range fresh {
		seen[paper.ID] = true
		papers = append(papers, paper)
	}
	if src.metadata == nil {
		src.metadata = map[string]LivePaperMetadata{}
	}
	for _, paper := range cache.Papers {
		if seen[paper.ID] || livePeriodIndex(periods, paper.Date) < 0 {
			continue
		}
		seen[paper.ID] = true
		papers = append(papers, paper)
		if meta, ok := cache.Metadata[paper.ID]; ok {
			if _, fresher := src.metadata[paper.ID]; !fresher {
				src.metadata[paper.ID] = meta
			}
		}
	}
	return papers
}

func (s *TrendLiveService) liveWindow(ctx context.Context, src *liveSources, periods []LivePeriod,
) ([]StudyPaper, []int, error) {
	start := liveDay(periods[0].Start)
	until := liveDay(periods[len(periods)-1].EndExclusive).AddDate(0, 0, -1)
	cache := s.loadWindowCache(ctx)
	from := start
	if cursor := liveDay(cache.Cursor); !cursor.Before(start) && !cursor.After(until) {
		from = cursor
	}
	fresh, err := src.arxivHarvest(ctx, periods, from, until)
	if err != nil {
		return nil, nil, err
	}
	papers := liveMergeWindow(src, cache, fresh, periods)
	population := make([]int, len(periods))
	for _, paper := range papers {
		if index := livePeriodIndex(periods, paper.Date); index >= 0 {
			population[index]++
		}
	}
	kept := map[string]LivePaperMetadata{}
	for _, paper := range papers {
		if meta, ok := src.metadata[paper.ID]; ok {
			kept[paper.ID] = meta
		}
	}
	if err := s.saveWindowCache(ctx, liveWindowCache{Cursor: until.Format(time.DateOnly), Sets: liveOAISets(),
		Papers: papers, Metadata: kept}); err != nil {
		slog.Warn("radar window cache not persisted", "error", err)
	}
	// Поиску уходит kept, а не src.metadata: цикл дальше пишет в свою карту, поиск читает окно из
	// HTTP-горутин, и общая карта роняла бы процесс «concurrent map read and map write».
	s.setWindow(papers, kept)
	slog.Info("arxiv window harvested", "from", from.Format(time.DateOnly), "until", until.Format(time.DateOnly),
		"fresh", len(fresh), "total", len(papers))
	return papers, population, nil
}
