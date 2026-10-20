package application

import (
	"context"
	"encoding/json/v2"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	livePubMedBase   = "https://eutils.ncbi.nlm.nih.gov/entrez/eutils"
	livePubMedSource = "pubmed"
	livePubMedMax    = 200
	livePubMedBatch  = 100
	livePubMedBytes  = 16 << 20
)

// Клиент общий для поисков сервиса: ограничение НЦБИ без ключа — три запроса в секунду.
type livePubMedClient struct {
	http     *http.Client
	base     string
	interval time.Duration
	mu       sync.Mutex
	next     time.Time
}

func newLivePubMedClient() *livePubMedClient {
	return &livePubMedClient{http: &http.Client{Timeout: 15 * time.Second},
		base: livePubMedBase, interval: 350 * time.Millisecond}
}

func (c *livePubMedClient) get(ctx context.Context, action string, params url.Values) ([]byte, error) {
	c.mu.Lock()
	start := time.Now()
	if c.next.After(start) {
		start = c.next
	}
	c.next = start.Add(c.interval)
	c.mu.Unlock()
	if wait := time.Until(start); wait > 0 {
		if err := liveSleep(ctx, wait); err != nil {
			return nil, err
		}
	}
	params.Set("db", livePubMedSource)
	params.Set("tool", "rag-test-signalgraph")
	request, err := http.NewRequestWithContext(ctx, http.MethodGet,
		c.base+"/"+action+".fcgi?"+params.Encode(), http.NoBody)
	if err != nil {
		return nil, errors.New("не удалось создать запрос PubMed")
	}
	response, err := c.http.Do(request)
	if err != nil {
		return nil, errors.New("PubMed не ответил")
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("PubMed ответил HTTP %d", response.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, livePubMedBytes+1))
	if err != nil || len(body) > livePubMedBytes {
		return nil, errors.New("ответ PubMed не прочитан или превышает 16 МиБ")
	}
	return body, nil
}

func (c *livePubMedClient) search(ctx context.Context, terms []string, periods []LivePeriod) ([]string, int, error) {
	clauses := make([]string, 0, len(terms))
	for _, term := range liveScholarPhrases(terms) {
		// Разделители синтаксиса удалены: текст запроса не может расширить область поиска полями НЦБИ.
		clean := strings.Join(liveTokens(term), " ")
		if clean != "" {
			clauses = append(clauses, `"`+clean+`"[Title/Abstract]`)
		}
	}
	if len(clauses) == 0 || len(periods) == 0 {
		return nil, 0, nil
	}
	body, err := c.get(ctx, "esearch", url.Values{
		"term": {strings.Join(clauses, " OR ")}, "datetype": {"pdat"}, "retmode": {"json"},
		"mindate": {periods[0].Start}, "maxdate": {liveDay(periods[len(periods)-1].EndExclusive).
				AddDate(0, 0, -1).Format(time.DateOnly)},
		"retmax": {strconv.Itoa(livePubMedMax)}, "sort": {"pub_date"},
	})
	if err != nil {
		return nil, 0, err
	}
	var answer struct {
		Result *struct {
			Count string   `json:"count"`
			IDs   []string `json:"idlist"`
		} `json:"esearchresult"`
	}
	if json.Unmarshal(body, &answer) != nil || answer.Result == nil {
		return nil, 0, errors.New("неверный ответ поиска PubMed")
	}
	count, err := strconv.Atoi(answer.Result.Count)
	if err != nil || count < 0 || len(answer.Result.IDs) > livePubMedMax ||
		len(answer.Result.IDs) > count || count > 0 && len(answer.Result.IDs) == 0 {
		return nil, 0, errors.New("неверное число результатов PubMed")
	}
	for _, id := range answer.Result.IDs {
		if !livePubMedID(id) {
			return nil, 0, errors.New("неверный идентификатор PubMed")
		}
	}
	return answer.Result.IDs, count, nil
}

func (src *liveSources) livePubMedSearch(ctx context.Context, terms []string,
	periods []LivePeriod,
) ([]StudyPaper, map[string]LivePaperMetadata, []LiveLimit) {
	metadata := map[string]LivePaperMetadata{}
	if src.pubmed == nil {
		return nil, metadata, nil
	}
	ctx, cancel := context.WithTimeout(ctx, 45*time.Second)
	defer cancel()
	ids, found, err := src.pubmed.search(ctx, terms, periods)
	if err != nil {
		return nil, metadata, []LiveLimit{liveLimitFail(liveLimitPubMedOff, "PubMed", err)}
	}
	papers := make([]StudyPaper, 0, len(ids))
	limits := []LiveLimit{}
	for start := 0; start < len(ids); start += livePubMedBatch {
		batch := ids[start:min(start+livePubMedBatch, len(ids))]
		body, fetchErr := src.pubmed.get(ctx, "efetch", url.Values{"id": {strings.Join(batch, ",")}, "retmode": {"xml"}})
		if fetchErr == nil {
			var page []StudyPaper
			var meta map[string]LivePaperMetadata
			page, meta, fetchErr = livePubMedPapers(body, batch, periods)
			for _, paper := range page {
				papers = append(papers, paper)
				metadata[paper.ID] = meta[paper.ID]
			}
		}
		if fetchErr != nil {
			limits = append(limits, liveLimitFail(liveLimitPubMedOff, "PubMed", fetchErr))
			break
		}
	}
	// Первые двести результатов и пропущенные даты не доказывают полноту помесячного ряда.
	if len(papers) < found {
		limits = append(limits, liveLimitOf(liveLimitPubMedCapped, "PubMed: неполное покрытие окна публикаций",
			map[string]int{liveValueTaken: len(papers), liveValueFound: found}))
	}
	return papers, metadata, limits
}
