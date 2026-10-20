package application

import (
	"cmp"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	liveResearchSearchParam = "search"
	liveDataCiteName        = "DataCite"
	liveOpenAIREName        = "OpenAIRE"
	liveDataCiteURL         = "https://api.datacite.org/dois"
	liveOpenAIREURL         = "https://api.openaire.eu/graph/v4/research-products"
	liveResearchPage        = 100
	liveResearchBytes       = 8 << 20
	liveResearchSample      = "research_sample"
	liveResearchOff         = "research_unavailable"
)

type liveResearchPageResult struct {
	records    []LiveSourceRecord
	total      int
	provenance LiveSourceProvenance
	observed   time.Time
}

// Общий кеш и часовой бюджет берегут публичный лимит OpenAIRE между запросами пользователей.
type liveResearchClient struct {
	http               *http.Client
	datacite, openaire string
	mu                 sync.Mutex
	cache              map[string]liveResearchPageResult
	openaireRequests   []time.Time
}

func newLiveResearchClient() *liveResearchClient {
	return &liveResearchClient{http: &http.Client{Timeout: 20 * time.Second},
		datacite: liveDataCiteURL, openaire: liveOpenAIREURL, cache: map[string]liveResearchPageResult{}}
}

func (c *liveResearchClient) page(ctx context.Context, source, address string) (liveResearchPageResult, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	now := time.Now()
	for key, page := range c.cache {
		if now.Sub(page.observed) >= 24*time.Hour {
			delete(c.cache, key)
		}
	}
	if page, ok := c.cache[address]; ok {
		return page, nil
	}
	if source == liveOpenAIREName {
		c.openaireRequests = slices.DeleteFunc(c.openaireRequests, func(t time.Time) bool { return now.Sub(t) >= time.Hour })
		if len(c.openaireRequests) >= 60 {
			return liveResearchPageResult{}, errors.New("исчерпан публичный лимит OpenAIRE: 60 запросов в час")
		}
		c.openaireRequests = append(c.openaireRequests, now)
	}
	body, err := c.get(ctx, address)
	if err != nil {
		return liveResearchPageResult{}, err
	}
	var page liveResearchPageResult
	if source == liveDataCiteName {
		page.records, page.total, err = liveDataCiteRecords(body)
	} else {
		page.records, page.total, err = liveOpenAIRERecords(body)
	}
	if err != nil {
		return liveResearchPageResult{}, err
	}
	page.observed = now
	page.provenance = LiveSourceProvenance{URL: address, SHA256: fmt.Sprintf("%x", sha256.Sum256(body))}
	// Кеш ограничен числом поисков; удаление старейшего не меняет смысл результата.
	if len(c.cache) >= 128 {
		oldest := ""
		for key, item := range c.cache {
			if oldest == "" || item.observed.Before(c.cache[oldest].observed) {
				oldest = key
			}
		}
		delete(c.cache, oldest)
	}
	c.cache[address] = page
	return page, nil
}

func (c *liveResearchClient) get(ctx context.Context, address string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, address, http.NoBody)
	if err != nil {
		return nil, errors.New("не удалось создать запрос исследовательских метаданных")
	}
	req.Header.Set("User-Agent", "rag-test-signalgraph/1.0 (+public research metadata)")
	req.Header.Set("Accept", "application/json")
	response, err := c.http.Do(req)
	if err != nil {
		return nil, errors.New("источник исследовательских метаданных не ответил")
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("источник ответил HTTP %d", response.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, liveResearchBytes+1))
	if err != nil || len(body) > liveResearchBytes {
		return nil, errors.New("ответ не прочитан или превышает 8 МиБ")
	}
	return body, nil
}

func (c *liveResearchClient) pages(ctx context.Context, source, address string) ([]liveResearchPageResult, error) {
	pages := []liveResearchPageResult{}
	param := "page"
	if source == liveDataCiteName {
		param = "page%5Bnumber%5D"
	}
	for number := 1; number <= 2; number++ {
		page, err := c.page(ctx, source, address+"&"+param+"="+strconv.Itoa(number))
		if err != nil {
			return pages, err
		}
		pages = append(pages, page)
		if page.total <= number*liveResearchPage {
			break
		}
	}
	return pages, nil
}

func (src *liveSources) researchSearch(ctx context.Context, terms []string, periods []LivePeriod) *LiveChannel {
	if src.research == nil || len(periods) == 0 {
		return nil
	}
	phrases := liveResearchPhrases(terms)
	if len(phrases) == 0 {
		return nil
	}
	channel := &LiveChannel{SourceSet: "research-v1", Source: "DataCite; OpenAIRE (CC BY)",
		ObservedAt: time.Now().UTC().Format(time.RFC3339), Partial: true,
		Counts: make([]*int, len(periods)), Records: []LiveSourceRecord{}}
	ctx, cancel := context.WithTimeout(ctx, 45*time.Second)
	defer cancel()
	query := strings.Join(phrases, " OR ")
	from, through := periods[0].Start, liveDay(periods[len(periods)-1].EndExclusive).AddDate(0, 0, -1).Format(time.DateOnly)
	through = min(through, time.Now().UTC().Format(time.DateOnly))
	dc := url.Values{liveCoherenceQuery: {"(titles.title:(" + query + ") OR descriptions.description:(" + query +
		") OR subjects.subject:(" + query + ")) AND publicationYear:[" + from[:4] + " TO " + through[:4] + "]"},
		"page[size]": {"100"}, "resource-type-id": {"dataset,software,journal-article,preprint"}}
	oa := url.Values{liveResearchSearchParam: {query}, "page_size": {"100"}, oaFilter: {
		"type:publication|dataset|software,from_publication_date:" + from + ",to_publication_date:" + through}}
	for _, provider := range []struct{ name, address string }{
		{liveDataCiteName, src.research.datacite + "?" + dc.Encode()}, {liveOpenAIREName, src.research.openaire + "?" + oa.Encode()},
	} {
		pages, err := src.research.pages(ctx, provider.name, provider.address)
		if err != nil {
			channel.Limits = append(channel.Limits, provider.name+": "+err.Error())
		}
		coverage := LiveSourceCoverage{Source: provider.name, From: from, Through: through, StopReason: "bounded_metadata_sample"}
		for _, page := range pages {
			coverage.Received += len(page.records)
			coverage.ProviderTotal, coverage.ObservedAt = new(page.total), page.observed.UTC().Format(time.RFC3339)
			coverage.Pages = append(coverage.Pages, page.provenance)
			for _, record := range page.records {
				if livePeriodIndex(periods, record.Date) < 0 || record.Date > through {
					continue
				}
				record.ObservedAt = coverage.ObservedAt
				record.Provenance = []LiveSourceProvenance{page.provenance}
				channel.Records = append(channel.Records, record)
			}
		}
		if len(pages) > 0 {
			channel.Coverage = append(channel.Coverage, coverage)
		}
	}
	liveResearchDedup(channel, nil, nil)
	liveResearchCounts(channel, periods)
	return channel
}

// Один результат в нескольких каталогах не становится независимыми подтверждениями.
func liveResearchDedup(channel *LiveChannel, papers []StudyPaper, metadata map[string]LivePaperMetadata) {
	if channel == nil {
		return
	}
	seen := map[string]bool{}
	for _, paper := range papers {
		seen[liveScholarTitleKey(paper.Title)] = true
		if doi := liveResearchDOI(cmp.Or(paper.CanonicalID, liveDOI(paper, metadata[paper.ID]))); doi != "" {
			seen[doi] = true
		}
	}
	channel.Records = slices.DeleteFunc(channel.Records, func(record LiveSourceRecord) bool {
		title := liveScholarTitleKey(record.Title)
		keys := []string{title}
		for _, value := range append(slices.Clone(record.DOIAliases), record.DOI) {
			if doi := liveResearchDOI(value); doi != "" {
				keys = append(keys, doi)
			}
		}
		duplicate := seen[title]
		for _, key := range keys {
			duplicate = duplicate || seen[key]
		}
		for _, key := range keys {
			seen[key] = true
		}
		return duplicate
	})
	channel.Total = len(channel.Records)
}

func liveResearchPhrases(terms []string) []string {
	phrases := []string{}
	for _, term := range liveScholarPhrases(terms) {
		if clean := strings.Join(liveTokens(term), " "); clean != "" {
			phrases = append(phrases, `"`+clean+`"`)
		}
	}
	return phrases
}
