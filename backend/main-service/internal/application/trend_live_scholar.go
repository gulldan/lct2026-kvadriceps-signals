package application

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json/v2"
	"errors"
	"fmt"
	"log/slog"
	"maps"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/example/main-service/internal/platform/openalex"
)

type openAlexWork = openalex.Work

// Журналы идут шагом отдельно от arXiv: у препринта и журнальной статьи разные даты и вес.
const (
	liveStepScholar    = "scholar"
	liveScholarPerPage = 200
	// liveScholarBudget: помесячная часть даёт покрытие для стадии и роста, остаток — плотность вокруг фразы.
	liveScholarBudget   = 1400
	liveScholarCoverage = 350
	// liveScholarFloor: непустой месяц обязан быть представлен, иначе покрытие станет дырявым.
	liveScholarFloor     = 20
	liveScholarPerPeriod = 600
	liveScholarPage      = 200
	liveScholarTerms     = LiveSearchTerms
	liveScholarSource    = "openalex"
	liveScholarMinTitle  = 12
)

// Имена параметров запроса OpenAlex: линтер считает третье вхождение строки.
const (
	oaFilter  = "filter"
	oaPerPage = "per-page"
	oaPage    = "page"
	oaSelect  = "select"
)

type scholarMonthPage struct {
	works []openalex.Work
	total int
	err   error
}

func (src *liveSources) liveScholarSearch(ctx context.Context, terms []string,
	periods []LivePeriod) ([]StudyPaper, map[string]LivePaperMetadata, []LiveLimit) {
	metadata := map[string]LivePaperMetadata{}
	if src.oa == nil || !src.oa.Configured() || len(periods) == 0 {
		return nil, metadata, []LiveLimit{
			liveLimitOf(liveLimitScholarOff, "scholar: OpenAlex is not configured", nil)}
	}
	phrases := liveScholarPhrases(terms)
	if len(phrases) == 0 {
		return nil, metadata, []LiveLimit{
			liveLimitOf(liveLimitScholarNoTerms, "scholar: the query has no Latin words to search journals with", nil)}
	}
	src.step(liveStepScholar, 0)
	// Запрос помесячный: в общей выдаче неизвестно, какие месяцы попали; равные доли разрежали бы плотные месяцы.
	counts := make([]int, len(periods))
	countErrs := make([]error, len(periods))
	filters := make([]string, len(periods))
	var wg sync.WaitGroup
	for i, period := range periods {
		last := liveDay(period.EndExclusive).AddDate(0, 0, -1).Format(time.DateOnly)
		filters[i] = strings.Join([]string{
			"from_publication_date:" + period.Start,
			"to_publication_date:" + last,
			"title_and_abstract.search:" + strings.Join(phrases, "|"),
		}, ",")
		wg.Add(1)
		go func() {
			defer wg.Done()
			counts[i], countErrs[i] = src.scholarCount(ctx, filters[i], period.Complete)
		}()
	}
	wg.Wait()
	// Отказ подсчёта всех месяцев — оговорка, а не ноль: иначе поиск без журналов прошёл бы молча.
	if !slices.Contains(countErrs, nil) {
		return nil, metadata, []LiveLimit{liveScholarFailure(countErrs[0])}
	}
	quotas := liveScholarQuotas(counts)
	// Общая часть бюджета — ядро запроса: лучшие работы месяца редкой темы слабее худших месяца плотной.
	pages := make([]scholarMonthPage, len(periods)+1)
	window := strings.Join([]string{
		"from_publication_date:" + periods[0].Start,
		"to_publication_date:" + liveDay(periods[len(periods)-1].EndExclusive).AddDate(0, 0, -1).Format(time.DateOnly),
		"title_and_abstract.search:" + strings.Join(phrases, "|"),
	}, ",")
	wg.Add(1)
	go func() {
		defer wg.Done()
		pages[len(periods)] = src.scholarMonth(ctx, window, liveScholarBudget-liveScholarCoverage, 0, false)
	}()
	for i := range periods {
		if countErrs[i] != nil {
			pages[i] = scholarMonthPage{err: countErrs[i]}
			continue
		}
		if quotas[i] == 0 {
			continue
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			pages[i] = src.scholarMonth(ctx, filters[i], quotas[i], counts[i], periods[i].Complete)
		}()
	}
	wg.Wait()
	papers, total, failed, sampled, failure := liveScholarPages(pages, counts, periods, metadata)
	var limits []LiveLimit
	switch {
	case failed == len(periods):
		return nil, metadata, []LiveLimit{liveScholarFailure(failure)}
	case failed > 0:
		// Пропуск месяцев — дырявое покрытие, стадию по нему называть нельзя; правило знает именно этот код.
		limits = append(limits, liveLimitOf(liveLimitScholarCapped,
			fmt.Sprintf("scholar: %d месяцев окна из %d не опрошены", failed, len(periods)),
			map[string]int{liveValueTaken: len(papers), liveValueFound: total}))
		if _, spent := errors.AsType[*openalex.BudgetError](failure); spent {
			limits = append(limits, liveScholarFailure(failure))
		}
	case sampled:
		limits = append(limits, liveLimitOf(liveLimitScholarSampled,
			fmt.Sprintf("scholar: взято %d работ из %d найденных — доля каждого месяца окна "+
				"и самые близкие запросу работы всего окна", len(papers), total),
			map[string]int{"taken": len(papers), liveValueFound: total}))
	}
	return papers, metadata, limits
}

// liveScholarPhrases выбрасывает запятую и черту: в фильтре OpenAlex они разделяют фильтры и значения.
func liveScholarPhrases(terms []string) []string {
	phrases := make([]string, 0, liveScholarTerms)
	for _, term := range terms {
		cleaned := strings.Map(func(r rune) rune {
			if r == ',' || r == '|' || r == '"' {
				return ' '
			}
			return r
		}, term)
		cleaned = strings.Join(strings.Fields(cleaned), " ")
		if cleaned == "" || !strings.ContainsFunc(cleaned, func(r rune) bool {
			return r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z'
		}) || slices.Contains(phrases, cleaned) {
			continue
		}
		phrases = append(phrases, cleaned)
		if len(phrases) == liveScholarTerms {
			break
		}
	}
	return phrases
}

// liveScholarPaper отбрасывает работу без даты или вне окна: иначе она молча искажала бы рост.
func liveScholarPaper(work openAlexWork, periods []LivePeriod) (StudyPaper, LivePaperMetadata, bool) {
	const secureScheme, reviewType = "https", "review"
	title := strings.Join(strings.Fields(work.Title), " ")
	if len(title) < liveScholarMinTitle || work.PublicationDate == "" {
		return StudyPaper{}, LivePaperMetadata{}, false
	}
	if livePeriodIndex(periods, work.PublicationDate) < 0 {
		return StudyPaper{}, LivePaperMetadata{}, false
	}
	id := liveScholarID(work)
	if id == "" {
		return StudyPaper{}, LivePaperMetadata{}, false
	}
	address := work.ID
	if doi := work.PlainDOI(); doi != "" {
		address = "https://doi.org/" + doi
	}
	// Страница первоисточника важнее агрегатора, схема ссылки проверяется до интерфейса; DOI остаётся идентификатором.
	if work.PrimaryLocation != nil {
		landing, err := url.Parse(strings.TrimSpace(work.PrimaryLocation.LandingPageURL))
		if err == nil && landing.Hostname() != "" && landing.User == nil &&
			(landing.Scheme == secureScheme || landing.Scheme == "http") {
			address = landing.String()
		}
	}

	authors := make([]string, 0, len(work.Authorships))
	authorships := make([]LiveAuthorship, 0, len(work.Authorships))
	for _, authorship := range work.Authorships {
		if authorship.Author.DisplayName != "" {
			authors = append(authors, authorship.Author.DisplayName)
		}
		// Организация — по идентификатору OpenAlex, а не по имени: на нём держится счёт независимых групп.
		for _, institution := range authorship.Institutions {
			authorships = append(authorships, LiveAuthorship{
				AuthorID: authorship.Author.ID, AuthorName: authorship.Author.DisplayName,
				OrganizationID: institution.ID, OrganizationName: institution.DisplayName,
				SourceURL: work.ID,
			})
		}
	}
	paper := StudyPaper{
		ID: id, Title: title, Date: work.PublicationDate, URL: address,
		CanonicalID: work.PlainDOI(), Authors: authors,
		ContentHash: liveScholarHash(work.ID, title, work.PublicationDate),
	}
	meta := LivePaperMetadata{
		Abstract: work.Abstract(), DOI: work.PlainDOI(), SourceURL: address,
		Category: work.Venue(), Authorships: authorships, CitedBy: new(work.CitedByCount),
		ContentHash: paper.ContentHash,
	}
	if language := strings.ToLower(strings.TrimSpace(work.Language)); language != "" {
		meta.Language, meta.LanguageBasis = language, "язык указан в метаданных OpenAlex"
	}
	// Незнакомый тип остаётся неизвестным: тип материала не говорит ни о рецензировании, ни о доверии к издателю.
	switch work.Type {
	case "article", "book", "book-chapter", "book-review", "conference-abstract", "conference-paper",
		"data-paper", "dataset", "dissertation", "editorial", "erratum", "letter", "libguides", "other",
		"paratext", "peer-review", "preprint", "reference-entry", "report", "retraction", reviewType,
		"software", "software-paper", "standard", "supplementary-materials":
		meta.Genre = work.Type
	}
	return paper, meta, true
}

// liveScholarID отличим от идентификаторов arXiv: иначе работы двух каналов сливались бы по случайному совпадению.
func liveScholarID(work openAlexWork) string {
	if doi := work.PlainDOI(); doi != "" {
		return liveScholarSource + ":" + doi
	}
	if short := strings.TrimPrefix(work.ID, "https://openalex.org/"); short != work.ID && short != "" {
		return liveScholarSource + ":" + strings.ToLower(short)
	}
	return ""
}

func liveScholarHash(parts ...string) string {
	sum := sha256.Sum256([]byte(strings.Join(parts, "\n")))
	return hex.EncodeToString(sum[:])
}

func liveScholarMerge(arxiv []StudyPaper, arxivMeta map[string]LivePaperMetadata,
	scholar []StudyPaper, scholarMeta map[string]LivePaperMetadata) ([]StudyPaper, map[string]LivePaperMetadata) {
	known := make(map[string]bool, len(arxiv)*2)
	for _, paper := range arxiv {
		known[liveScholarTitleKey(paper.Title)] = true
		if doi := strings.ToLower(arxivMeta[paper.ID].DOI); doi != "" {
			known[doi] = true
		}
		if doi := strings.ToLower(paper.CanonicalID); doi != "" {
			known[doi] = true
		}
	}
	merged := slices.Clone(arxiv)
	metadata := maps.Clone(arxivMeta)
	if metadata == nil {
		metadata = map[string]LivePaperMetadata{}
	}
	for _, paper := range scholar {
		if known[liveScholarTitleKey(paper.Title)] || known[strings.ToLower(paper.CanonicalID)] {
			continue
		}
		known[liveScholarTitleKey(paper.Title)] = true
		if doi := strings.ToLower(paper.CanonicalID); doi != "" {
			known[doi] = true
		}
		merged = append(merged, paper)
		metadata[paper.ID] = scholarMeta[paper.ID]
	}
	return merged, metadata
}

func liveScholarTitleKey(title string) string {
	return strings.ToLower(strings.Join(strings.Fields(title), " "))
}

// withScholar вызывается до сборки темы: счётчики периодов считаются по переданным работам.
func (src *liveSources) withScholar(ctx context.Context, papers []StudyPaper,
	metadata map[string]LivePaperMetadata, terms []string,
	periods []LivePeriod) ([]StudyPaper, []LiveLimit) {
	var pubmed []StudyPaper
	var pubmedMeta map[string]LivePaperMetadata
	var pubmedLimits []LiveLimit
	var wg sync.WaitGroup
	wg.Go(func() { pubmed, pubmedMeta, pubmedLimits = src.livePubMedSearch(ctx, terms, periods) })
	scholar, scholarMeta, limits := src.liveScholarSearch(ctx, terms, periods)
	scholar, revisionLimits := liveRefreshPublisherRevisions(ctx, scholar, scholarMeta, livePublisherClient())
	limits = append(limits, revisionLimits...)
	merged, mergedMeta := liveScholarMerge(papers, metadata, scholar, scholarMeta)
	limits = append(limits, liveLimitOf(liveLimitScholarJournals,
		fmt.Sprintf("scholar: добавлено %d работ из журналов вне arXiv", len(merged)-len(papers)),
		map[string]int{"added": len(merged) - len(papers)}))
	wg.Wait()
	limits = append(limits, pubmedLimits...)
	before := len(merged)
	merged, mergedMeta = liveScholarMerge(merged, mergedMeta, pubmed, pubmedMeta)
	if len(pubmed) > 0 {
		limits = append(limits, liveLimitOf(liveLimitPubMedAdded, "PubMed: добавлены научные публикации",
			map[string]int{"added": len(merged) - before}))
	}
	if src.metadata == nil {
		src.metadata = map[string]LivePaperMetadata{}
	}
	maps.Copy(src.metadata, mergedMeta)
	return merged, limits
}

func liveScholarQuotas(counts []int) []int {
	total := 0
	for _, count := range counts {
		total += count
	}
	quotas := make([]int, len(counts))
	if total == 0 {
		return quotas
	}
	for i, count := range counts {
		if count == 0 {
			continue
		}
		share := liveScholarCoverage * count / total
		quotas[i] = min(max(share, liveScholarFloor), min(count, liveScholarPerPeriod))
	}
	return quotas
}

func (src *liveSources) scholarCount(ctx context.Context, filter string, closed bool) (int, error) {
	result, err := src.scholarWorks(ctx, url.Values{
		oaFilter: {filter}, oaPerPage: {"1"}, oaPage: {"1"}, oaSelect: {"id"},
	}, closed)
	if err != nil {
		return 0, err
	}
	return result.Meta.Count, nil
}

// scholarMonth добирает страницами до своей доли: страница OpenAlex не длиннее двухсот работ.
func (src *liveSources) scholarMonth(ctx context.Context, filter string, quota, count int,
	closed bool,
) scholarMonthPage {
	page := scholarMonthPage{total: count}
	for number := 1; len(page.works) < quota; number++ {
		size := min(quota-len(page.works), liveScholarPage)
		result, err := src.scholarWorks(ctx, url.Values{
			oaFilter:  {filter},
			oaPerPage: {strconv.Itoa(size)},
			oaPage:    {strconv.Itoa(number)},
			oaSelect: {"id,doi,title,type,language,publication_date,cited_by_count,primary_location,authorships," +
				"abstract_inverted_index"},
		}, closed)
		if err != nil {
			if len(page.works) == 0 {
				return scholarMonthPage{err: err}
			}
			return page
		}
		if result.Meta.Count > 0 {
			page.total = result.Meta.Count
		}
		page.works = append(page.works, result.Results...)
		if len(result.Results) < size {
			return page
		}
	}
	return page
}

// liveScholarPages: отказ общей страницы — потеря плотности, а не пропуск месяца, и в покрытие не входит.
func liveScholarPages(pages []scholarMonthPage, counts []int, periods []LivePeriod,
	metadata map[string]LivePaperMetadata,
) (papers []StudyPaper, total, failed int, sampled bool, failure error) {
	seen := map[string]bool{}
	for i, page := range pages {
		month := i < len(periods)
		if page.err != nil {
			if month {
				failed++
				failure = page.err
			}
			continue
		}
		if month {
			total += max(page.total, counts[i])
			sampled = sampled || page.total > len(page.works)
		}
		for _, work := range page.works {
			paper, meta, ok := liveScholarPaper(work, periods)
			if !ok || seen[paper.ID] {
				continue
			}
			seen[paper.ID] = true
			papers = append(papers, paper)
			metadata[paper.ID] = meta
		}
	}
	return papers, total, failed, sampled, failure
}

// liveScholarCache: текстовый поиск стоит у OpenAlex десять кредитов, а закрытые месяцы почти не меняются.
type liveScholarCache struct {
	mu      sync.Mutex
	entries map[string]liveScholarCached
	works   int
	store   liveStore
}

type liveScholarCached struct {
	Page    openalex.Page `json:"page"`
	Expires time.Time     `json:"expires"`
}

const (
	liveScholarCacheTTL = 7 * 24 * time.Hour
	// ponytail: предел в памяти реплики (~10 КБ на работу); кеш в Valkey — если реплик станет больше.
	liveScholarCacheWorks = 8000
)

func newLiveScholarCache(store liveStore) *liveScholarCache {
	return &liveScholarCache{entries: map[string]liveScholarCached{}, store: store}
}

// scholarWorks не трогает резерв суточного бюджета OpenAlex: он отложен под историю терминов.
func (src *liveSources) scholarWorks(ctx context.Context, query url.Values, closed bool) (openalex.Page, error) {
	cached := closed && src.scholarCache != nil
	key := query.Encode()
	if cached {
		if page, ok := src.scholarCache.get(ctx, key); ok {
			return page, nil
		}
	}
	if err := liveScholarReserved(src.oa); err != nil {
		return openalex.Page{}, err
	}
	page, err := src.oa.Works(ctx, query)
	if err == nil && cached {
		src.scholarCache.put(ctx, key, page)
	}
	return page, err
}

// get идёт в хранилище после памяти: память пуста после выкладки, а ответ закрытого месяца ещё годен.
func (c *liveScholarCache) get(ctx context.Context, key string) (openalex.Page, bool) {
	c.mu.Lock()
	entry, ok := c.entries[key]
	c.mu.Unlock()
	if ok && time.Now().Before(entry.Expires) {
		return entry.Page, true
	}
	if c.store == nil {
		return openalex.Page{}, false
	}
	raw, err := c.store.GetBytes(ctx, liveScholarObject(key))
	if err != nil || json.Unmarshal(raw, &entry) != nil || !time.Now().Before(entry.Expires) {
		return openalex.Page{}, false
	}
	c.remember(key, entry)
	return entry.Page, true
}

// put пишет в хранилище в фоне: его отказ не должен ни держать поиск, ни ронять его.
// ponytail: объекты не удаляются, только перезаписываются (~20 МБ на новый запрос); чистка по сроку — когда станет тесно.
func (c *liveScholarCache) put(ctx context.Context, key string, page openalex.Page) {
	entry := liveScholarCached{Page: page, Expires: time.Now().Add(liveScholarCacheTTL)}
	c.remember(key, entry)
	if c.store == nil {
		return
	}
	go func() {
		raw, err := json.Marshal(entry)
		if err == nil {
			err = c.store.Put(context.WithoutCancel(ctx), liveScholarObject(key), bytes.NewReader(raw),
				int64(len(raw)), "application/json")
		}
		if err != nil {
			slog.Warn("radar scholar cache not saved", "error", err)
		}
	}()
}

func liveScholarObject(key string) string {
	return "trends/live/openalex/scholar/" + liveScholarHash(key) + ".json"
}

func (c *liveScholarCache) remember(key string, entry liveScholarCached) {
	c.mu.Lock()
	defer c.mu.Unlock()
	now := time.Now()
	drop := func(key string) {
		c.works -= len(c.entries[key].Page.Results)
		delete(c.entries, key)
	}
	drop(key)
	for other, cached := range c.entries {
		if now.After(cached.Expires) {
			drop(other)
		}
	}
	// Сверх предела уходят случайные записи (обход карты случаен): неделя кеша не стоит очереди вытеснения.
	for other := range c.entries {
		if c.works+len(entry.Page.Results) <= liveScholarCacheWorks {
			break
		}
		drop(other)
	}
	c.entries[key] = entry
	c.works += len(entry.Page.Results)
}
