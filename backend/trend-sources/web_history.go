package main

import (
	"bytes"
	"cmp"
	"context"
	"encoding/xml"
	"errors"
	"fmt"
	"log/slog"
	"maps"
	"net"
	"net/url"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"golang.org/x/net/html"
)

// feedArticle: недоступная статья сужает охват, но запись ленты остаётся; второй ответ — запрос не дождался ответа.
func (s *service) feedArticle(ctx context.Context, row record, source webSource, data *snapshot,
	c *coverage) (record, bool) {
	base, _ := url.Parse(source.URL)
	target, err := url.Parse(row.URL)
	if base == nil || err != nil || base.Host != target.Host || allowedAddress(target, s.config.domains()) != nil {
		return row, false
	}
	if rules := s.robotsFor(ctx, target, data); !rules.allows(target) {
		c.Limitations = append(c.Limitations, rules.limitation(row.URL))
		return row, false
	}
	article, err := s.cached(ctx, row.URL, "article:"+data.Through, data)
	if err != nil {
		c.Cursor = row.ID
		c.StopReason = err.Error()
		row.Limitations = appendLimitation(row.Limitations, err.Error())
		c.Limitations = append(c.Limitations, row.URL+": "+err.Error())
		timeout, ok := errors.AsType[net.Error](err)
		return row, ok && timeout.Timeout()
	}
	enriched, parseErr := articleDocument(article, row, data.ObservedAt)
	if parseErr != nil {
		// Текста нет, но он может появиться после скриптов — страница ждёт в ограниченной очереди рендера.
		if errors.Is(parseErr, errArticleBodyUnavailable) {
			s.renderQueue = append(s.renderQueue, row.ID)
		}
		row.Limitations = appendLimitation(row.Limitations, parseErr.Error())
		c.Limitations = append(c.Limitations, row.URL+": "+parseErr.Error())
		return row, false
	}
	enriched.Provenance = append(enriched.Provenance, provenance{URL: row.URL, SHA256: hashBytes(article)})
	return enriched, false
}

func (s *service) feedHistory(ctx context.Context, source webSource, data *snapshot, old map[string]record) {
	c := coverage{Source: source.Name, From: data.From, Through: data.Through, ObservedAt: data.ObservedAt,
		StopReason: "RSS history is a sample; archive coverage unknown"}
	raw, err := s.fetch(ctx, source.URL, data)
	// Статьи издателя оборванной ленты не запрашиваются: сеть обрывает их так же, и двадцать ожиданий
	// по сорок секунд растянули бы проход на четверть часа.
	budget, cutOff := cmp.Or(source.Pages, 20), ""
	if errors.Is(err, errFeedCutOff) {
		budget, cutOff, err = 0, err.Error(), nil
	}
	if err == nil {
		c.Pages = append(c.Pages, provenance{URL: source.URL, SHA256: hashBytes(raw)})
	}
	var rows []record
	if err == nil {
		rows, err = feedRows(raw, feed{URL: source.URL, Name: source.Name}, data.Through)
	}
	if err != nil {
		c.StopReason = err.Error()
		data.Coverage = append(data.Coverage, c)
		return
	}
	s.feedRecords(ctx, source, data, &c, rows, old, provenance{URL: source.URL, SHA256: hashBytes(raw)}, budget)
	if cutOff != "" {
		c.StopReason = cutOff + "; items received in full are kept, articles of this publisher are not requested"
		data.Limits = appendLimitation(data.Limits, "A feed the network cuts off keeps only the items received in "+
			"full before the cut; their text is the feed summary, not the article.")
	}
	s.renderPending(ctx, data, &c)
	data.Coverage = append(data.Coverage, c)
}

func (s *service) feedRowsForPass(source string, rows []record, old map[string]record) []record {
	// Страницы без текста — первыми, чтобы большая лента не заслоняла свои старые записи и следующие источники.
	slices.SortStableFunc(rows, func(a, b record) int {
		aText, bText := old[a.ID].TextStatus == articleText, old[b.ID].TextStatus == articleText
		switch {
		case aText == bText:
			return 0
		case aText:
			return 1
		default:
			return -1
		}
	})
	s.mu.RLock()
	cursor := ""
	if s.data != nil {
		for _, previous := range s.data.Coverage {
			if previous.Source == source {
				cursor = previous.Cursor
			}
		}
	}
	s.mu.RUnlock()
	// Вечно недоступная страница не должна занимать первое место бюджета: продолжаем с отложенной прошлым проходом.
	if at := slices.IndexFunc(rows, func(r record) bool { return r.ID == cursor }); at > 0 {
		return append(slices.Clone(rows[at:]), rows[:at]...)
	}
	return rows
}

// feedArticleTimeouts: у части изданий каждая статья висит до предела клиента в сорок секунд, и двадцать таких
// ожиданий съедали четверть часа прохода; после двух подряд издатель не отвечает и дальше.
const feedArticleTimeouts = 2

func consecutive(run int, hit bool) int {
	if hit {
		return run + 1
	}
	return 0
}

func (s *service) feedRecords(ctx context.Context, source webSource, data *snapshot, c *coverage,
	rows []record, old map[string]record, page provenance, budget int) {
	rows = s.feedRowsForPass(source.Name, rows, old)
	attempts, timeouts := 0, 0
	deferred := false
	stop := "feed article budget reached; remaining entries retained as metadata"
	for _, row := range rows {
		if row.Date < data.From {
			continue
		}
		row.Industry = source.Industry
		row.Provenance = []provenance{page}
		previous, known := old[row.ID]
		reusable := known && sameFeedArticle(previous, row)
		if reusable &&
			(strings.HasPrefix(previous.ObservedAt, data.Through) || attempts >= budget) {
			data.Records = append(data.Records, previous)
			continue
		}
		if row.TextStatus == articleText {
			data.Records = append(data.Records, row)
			continue
		}
		if attempts >= budget {
			if !deferred {
				c.Cursor = row.ID
				deferred = true
			}
			c.StopReason = stop
			data.Records = append(data.Records, row)
			continue
		}
		attempts++
		enriched, timedOut := s.feedArticle(ctx, row, source, data, c)
		if timeouts = consecutive(timeouts, timedOut); timeouts == feedArticleTimeouts {
			budget = attempts
			stop = "article pages of this publisher did not answer twice in a row; remaining entries retained as metadata"
		}
		if enriched.TextStatus != articleText && reusable {
			previous.Limitations = appendLimitation(previous.Limitations, "refresh failed; retaining previously extracted text")
			enriched = previous
		}
		data.Records = append(data.Records, enriched)
	}
}

func sameFeedArticle(previous, current record) bool {
	return previous.Source == current.Source && previous.TextStatus == articleText &&
		previous.Date == current.Date && previous.Title == current.Title
}

func (s *service) webHistory(ctx context.Context, data *snapshot) error {
	old := s.previousRecords()
	s.mu.RLock()
	if s.data != nil && s.data.SourceSet != data.SourceSet {
		maps.DeleteFunc(old, func(_ string, row record) bool { return !s.retained(row) })
	}
	var covered []coverage
	if s.data != nil {
		covered = s.data.Coverage
	}
	s.mu.RUnlock()
	read := map[string]bool{}
	for _, source := range s.config.Web {
		if ctx.Err() != nil {
			break
		}
		s.sourceHistory(ctx, source, data, old)
		if ctx.Err() == nil {
			read[source.Name] = true
		}
	}
	if err := ctx.Err(); err != nil {
		if len(read) == 0 {
			return fmt.Errorf("collection cancelled: %w", err)
		}
		slog.Warn("collection deadline reached; publishing the sources read so far", "read", len(read),
			"sources", len(s.config.Web), "error", err)
		data.Coverage = unreadCoverage(data, covered, s.config.Web, read, err)
	}
	dropWebBoilerplate(data)
	if len(data.Records) == 0 && len(old) == 0 {
		return errors.New("no dated publisher source available")
	}
	data.Limits = append(data.Limits,
		"Configured publishers and catalogues only; no census of the web. RSS history remains unknown. "+
			"WordPress archives report per-month page exhaustion and retained checkpoints.",
		"Publication dates come from publisher fields; availability means the first successful observation, not an "+
			"assumed historical release. Content unavailable or conflicting dates remains metadata only. Exact "+
			"duplicates and explicit primary-source links do not count as independent evidence.")
	return nil
}

// unreadCoverage помечает непрочитанные к сроку источники так, что их дочитывает retrySlow: раньше проход,
// не успевший за полтора часа, выбрасывался целиком, и снимок сутками не обновлялся. Записи прошлых проходов
// остаются по mergeRetained, счётчики охвата пересчитывает summarizeCoverage.
func unreadCoverage(data *snapshot, previous []coverage, sources []webSource, read map[string]bool,
	cause error) []coverage {
	fresh := slices.DeleteFunc(data.Coverage, func(c coverage) bool { return !read[c.Source] })
	for _, source := range sources {
		if read[source.Name] {
			continue
		}
		c := coverage{Source: source.Name, From: data.From, Through: data.Through,
			StopReason: "the pass ended before this source was read: " + cause.Error()}
		if at := slices.IndexFunc(previous, func(p coverage) bool { return p.Source == source.Name }); at >= 0 {
			c.ObservedAt, c.Cursor = previous[at].ObservedAt, previous[at].Cursor
		}
		fresh = append(fresh, c)
	}
	return fresh
}

func (s *service) sourceHistory(ctx context.Context, source webSource, data *snapshot, old map[string]record) {
	start := len(data.Records)
	switch source.Kind {
	case kindWordPress:
		s.wordpressHistory(ctx, source, data)
	case kindURL, kindURLs, kindSitemap:
		s.pageHistory(ctx, source, data, old)
	case kindCNCF, kindTechPort, kindHFPapers, kindHFModels:
		s.catalogueHistory(ctx, source, data, old)
	case kindBingNews:
		s.bingHistory(ctx, source, data)
	case kindEuropePMC, kindCrossref, kindClinicalTrials, kindBiorxiv, kindDataCite, kindZenodo, kindFigshare, kindBioStudies:
		s.publicationHistory(ctx, source, data)
	default:
		s.feedHistory(ctx, source, data, old)
	}
	if source.Genre != "" {
		for i := start; i < len(data.Records); i++ {
			data.Records[i].Genre = source.Genre
			data.Records[i].Limitations = appendLimitation(data.Records[i].Limitations,
				"investor content; not proof of investment commitment or scientific validation")
		}
	}
}

// slowRetryEvery: сеть стенда пропускает часть изданий через раз, и оборванная лента через полчаса приходит целиком.
const slowRetryEvery = 15 * time.Minute

// retrySlow не меняет время наблюдения снимка: это дочитывание того же прохода.
func (s *service) retrySlow(ctx context.Context, now time.Time) error {
	s.mu.RLock()
	old := s.data
	s.mu.RUnlock()
	if s.kind != kindWeb || old == nil {
		return nil
	}
	slow := slowSources(s.config.Web, old.Coverage)
	if len(slow) == 0 {
		return nil
	}
	unlock, err := s.lockCollection()
	if err != nil {
		return err
	}
	defer unlock()
	s.requests, s.renders, s.robots, s.renderQueue = 0, 0, nil, nil
	data := snapshot{Version: old.Version, Source: old.Source, SourceSet: old.SourceSet, ObservedAt: old.ObservedAt,
		From: old.From, Through: old.Through, Records: []record{}, Pages: []provenance{}, Limits: slices.Clone(old.Limits)}
	previous := s.previousRecords()
	retried := map[string]bool{}
	for _, source := range slow {
		retried[source.Name] = true
		s.sourceHistory(ctx, source, &data, previous)
	}
	if err = ctx.Err(); err != nil {
		return fmt.Errorf("slow retry cancelled: %w", err)
	}
	dropWebBoilerplate(&data)
	for i := range data.Records {
		data.Records[i].ObservedAt = cmp.Or(data.Records[i].ObservedAt, now.Format(time.RFC3339))
	}
	mergeRetained(&data, old, now, s.retained)
	linkDuplicates(data.Records)
	summarizeCoverage(&data)
	data.Coverage = replaceCoverage(old.Coverage, data.Coverage, retried)
	slog.Info("slow sources retried", "sources", len(slow), "records", len(data.Records))
	return s.publish(&data)
}

// slowSources — недочитанные из-за сети; прямой отказ издателя (403, 404) повтор не изменит.
func slowSources(sources []webSource, covered []coverage) []webSource {
	slow := map[string]bool{}
	for _, c := range covered {
		for _, marker := range []string{errFeedCutOff.Error(), "Timeout", "timeout", "deadline exceeded",
			"connection reset", "HTTP 429", "redirect chain is too long"} {
			if strings.Contains(c.StopReason, marker) {
				slow[c.Source] = true
			}
		}
	}
	return slices.DeleteFunc(slices.Clone(sources), func(source webSource) bool { return !slow[source.Name] })
}

func replaceCoverage(old, fresh []coverage, retried map[string]bool) []coverage {
	bySource := map[string][]coverage{}
	for _, c := range fresh {
		bySource[c.Source] = append(bySource[c.Source], c)
	}
	merged := make([]coverage, 0, len(old))
	for _, c := range old {
		if !retried[c.Source] {
			merged = append(merged, c)
			continue
		}
		merged = append(merged, bySource[c.Source]...)
		delete(bySource, c.Source)
	}
	return merged
}

func (s *service) pageList(ctx context.Context, source webSource, data *snapshot, c *coverage) []string {
	limit := cmp.Or(source.Pages, 50)
	switch source.Kind {
	case kindURL:
		return []string{source.URL}
	case kindURLs:
		return source.URLs[:min(limit, len(source.URLs))]
	}
	raw, err := s.cached(ctx, source.URL, "sitemap:"+data.Through, data)
	if err != nil {
		c.StopReason = err.Error()
		return nil
	}
	c.Pages = append(c.Pages, provenance{URL: source.URL, SHA256: hashBytes(raw)})
	var sitemap struct {
		URLs []struct {
			Loc string `xml:"loc"`
		} `xml:"url"`
	}
	if err = xml.Unmarshal(raw, &sitemap); err != nil {
		c.StopReason = "sitemap is not readable: " + err.Error()
		return nil
	}
	addresses := []string{}
	for _, entry := range sitemap.URLs {
		if location := strings.TrimSpace(entry.Loc); location != "" && len(addresses) < limit {
			addresses = append(addresses, location)
		}
	}
	if len(addresses) == limit {
		c.Cursor = addresses[len(addresses)-1]
		c.StopReason = "sitemap page ceiling reached"
	}
	return addresses
}

// pageHistory пишет каждую отклонённую страницу в охват, а не пропускает молча.
func (s *service) pageHistory(ctx context.Context, source webSource, data *snapshot, old map[string]record) {
	c := coverage{Source: source.Name, From: data.From, Through: data.Through, ObservedAt: data.ObservedAt,
		StopReason: "configured pages only; no crawl beyond the listed addresses"}
	panel := s.config.domains()
	for _, address := range s.pageList(ctx, source, data, &c) {
		target, err := url.Parse(address)
		if err != nil || allowedAddress(target, panel) != nil {
			c.Limitations = append(c.Limitations, "address outside the configured domains: "+address)
			continue
		}
		if rules := s.robotsFor(ctx, target, data); !rules.allows(target) {
			c.Limitations = append(c.Limitations, rules.limitation(address))
			continue
		}
		if previous, ok := old[address]; ok && previous.TextStatus == articleText &&
			strings.HasPrefix(previous.ObservedAt, data.Through) {
			data.Records = append(data.Records, previous)
			continue
		}
		raw, fetchErr := s.cached(ctx, address, "page:"+data.Through, data)
		if fetchErr != nil {
			c.Cursor = address
			c.StopReason = fetchErr.Error()
			c.Limitations = append(c.Limitations, "page unavailable or above the size limit: "+address)
			continue
		}
		row := record{ID: address, URL: address, Source: source.Name, Industry: source.Industry, Title: pageTitle(raw),
			TitleBasis: "publisher record", TextStatus: metadataOnlyText,
			Provenance: []provenance{{URL: address, SHA256: hashBytes(raw)}}}
		enriched, parseErr := articleDocument(raw, row, data.ObservedAt)
		if parseErr != nil {
			if errors.Is(parseErr, errArticleBodyUnavailable) {
				s.renderQueue = append(s.renderQueue, address)
			}
			row.Limitations = appendLimitation(row.Limitations, parseErr.Error())
			c.Limitations = append(c.Limitations, address+": "+parseErr.Error())
			data.Records = append(data.Records, row)
			continue
		}
		enriched.Title = cmp.Or(enriched.Title, pageTitle(raw), address)
		data.Records = append(data.Records, enriched)
	}
	s.renderPending(ctx, data, &c)
	data.Coverage = append(data.Coverage, c)
}

func pageTitle(raw []byte) string {
	doc, err := html.Parse(bytes.NewReader(raw))
	if err != nil {
		return ""
	}
	title := ""
	visit(doc, func(n *html.Node) {
		if title == "" && n.Type == html.ElementNode && n.Data == elementTitle {
			title = strings.Join(strings.Fields(content(n)), " ")
		}
	})
	return title
}

// dropWebBoilerplate убирает блоки, повторяющиеся на большинстве страниц домена: меню, подвалы, баннеры.
// ponytail: один проход по текущему сбору; профиль между сборами требует истории домена, которой нет в снимке.
func dropWebBoilerplate(data *snapshot) {
	pages := map[string]int{}
	counts := map[string]map[string]int{}
	for _, row := range data.Records {
		host := recordHost(row)
		if host == "" || row.TextStatus != articleText {
			continue
		}
		pages[host]++
		if counts[host] == nil {
			counts[host] = map[string]int{}
		}
		for block := range uniqueBlocks(row.Text) {
			counts[host][block]++
		}
	}
	for i, row := range data.Records {
		host := recordHost(row)
		if host == "" || row.TextStatus != articleText || pages[host] < 3 {
			continue
		}
		if cleaned, ok := withoutRepeatedBlocks(row, counts[host], pages[host]); ok {
			data.Records[i] = cleaned
		}
	}
}

// withoutRepeatedBlocks полагается на один якорь на блок в порядке блоков: убран блок — убран и якорь.
func withoutRepeatedBlocks(row record, counts map[string]int, pages int) (record, bool) {
	kept := []string{}
	anchors := []blockAnchor{}
	byStart := map[int]blockAnchor{}
	for _, anchor := range row.Anchors {
		byStart[anchor.Start] = anchor
	}
	dropped, at, originalAt := 0, 0, 0
	for _, block := range textBlocks(row.Text) {
		anchor, anchored := byStart[originalAt]
		length := utf8.RuneCountInString(block)
		anchored = anchored && anchor.End == originalAt+length && anchor.Path != ""
		originalAt += length + 1
		if counts[block]*2 > pages {
			dropped++
			continue
		}
		if at > 0 {
			at++
		}
		kept = append(kept, block)
		if anchored {
			anchors = append(anchors, blockAnchor{anchor.Path, at, at + length})
		}
		at += utf8.RuneCountInString(block)
	}
	if dropped == 0 || len(kept) == 0 {
		return row, false
	}
	row.Text = strings.Join(kept, "\n")
	row.Anchors = anchors
	row.ContentHash = hashBytes([]byte(row.Text))
	row.Limitations = appendLimitation(row.Limitations,
		"boilerplate_removed; blocks repeated across this domain's pages are not stored as evidence")
	return row, true
}

func recordHost(row record) string {
	u, err := url.Parse(row.URL)
	if err != nil {
		return ""
	}
	return strings.ToLower(u.Hostname())
}

func textBlocks(text string) []string {
	blocks := []string{}
	for block := range strings.SplitSeq(text, "\n") {
		if block = strings.TrimSpace(block); len(block) > 0 {
			blocks = append(blocks, block)
		}
	}
	return blocks
}

func uniqueBlocks(text string) map[string]bool {
	unique := map[string]bool{}
	for _, block := range textBlocks(text) {
		unique[block] = true
	}
	return unique
}
