package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json/v2"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"
)

var errBudget = errors.New("request budget reached; checkpoint retained")

// errQueryRejected — отказ по самому запросу (HTTP 400): другой термин тот же источник читает как обычно.
var errQueryRejected = errors.New("the source rejected this query")

type webSource struct {
	Name     string   `json:"name"`
	URL      string   `json:"url"`
	Kind     string   `json:"kind"`
	Industry string   `json:"industry"`
	URLs     []string `json:"urls,omitempty"`
	Pages    int      `json:"pages,omitempty"`
}
type sourceConfig struct {
	HistoryMonths int `json:"history_months"`
	RequestBudget int `json:"request_budget"`
	// Ноль — без предела, если адрес рендера задан.
	RenderBudget int `json:"render_budget,omitzero"`
	// Ноль в бюджетах документов выключает источник.
	EPDocuments int `json:"ep_documents,omitzero"`
	CNDocuments int `json:"cn_documents,omitzero"`
	// Ноль — недельный предел провайдера за вычетом запаса (epFairUseDefault).
	EPFairUseBytes int         `json:"ep_fair_use_bytes,omitzero"`
	OPSDocuments   int         `json:"ops_documents,omitzero"`
	RUDocuments    int         `json:"ru_documents,omitzero"`
	PatentTerms    []string    `json:"patent_terms"`
	Web            []webSource `json:"web"`
	// Панель: вне её ничего не запрашивается, переадресация наружу отклоняется. Пусто — хосты самих источников.
	AllowedDomains []string `json:"allowed_domains,omitempty"`
}

// collectorDomains задаёт домены патентного сборщика в коде, чтобы страница или переадресация не расширили панель.
func collectorDomains(c sourceConfig, kind string) []string {
	panel := c.domains()
	if !patentCollectorKind(kind) {
		return panel
	}
	return append(panel, "uspto.gov", "epo.org", "patents.google.com", "rospatent.gov.ru")
}

func (c sourceConfig) domains() []string {
	if len(c.AllowedDomains) > 0 {
		return c.AllowedDomains
	}
	hosts := []string{}
	for _, source := range c.Web {
		if u, err := url.Parse(source.URL); err == nil && u.Hostname() != "" {
			hosts = append(hosts, u.Hostname())
		}
	}
	return hosts
}

func defaultConfig() sourceConfig {
	c := sourceConfig{HistoryMonths: 13, RequestBudget: 600, PatentTerms: []string{
		"retrieval augmented", "speculative decoding", "mixture of experts", "model context protocol",
		"hydrogen", "perovskite", "solid state batter", "sodium ion", "carbon capture",
		"gene editing", "crispr", "bioprint", "additive manufactur",
	}}
	for _, f := range defaultFeeds() {
		if f.Name == "NASA" {
			continue
		}
		c.Web = append(c.Web, webSource{Name: f.Name, URL: f.URL, Kind: "rss", Industry: strings.TrimPrefix(f.Name, "MIT ")})
	}
	c.Web = append(c.Web, researchSources()...)
	c.Web = append(c.Web,
		webSource{Name: "Google Research", URL: "https://research.google/blog/rss/", Kind: kindFeed, Industry: "AI"},
		webSource{Name: "Hugging Face", URL: "https://huggingface.co/blog/feed.xml", Kind: kindFeed, Industry: "AI"},
		webSource{Name: "NASA features", URL: "https://www.nasa.gov/wp-json/wp/v2/feature", Kind: kindWordPress,
			Industry: "Space, energy, materials and manufacturing"},
		webSource{Name: "NASA news releases", URL: "https://www.nasa.gov/wp-json/wp/v2/press-release", Kind: kindWordPress,
			Industry: "Space, energy, materials and manufacturing"})
	return c
}
func loadConfig(name string) (sourceConfig, error) {
	c := defaultConfig()
	if name != "" {
		raw, err := os.ReadFile(name)
		if err != nil {
			return c, fmt.Errorf("read source config: %w", err)
		}
		if len(raw) > 64<<10 {
			return c, errors.New("source config exceeds64KiB")
		}
		if err = json.Unmarshal(raw, &c, json.RejectUnknownMembers(true)); err != nil {
			return c, fmt.Errorf("decode source config: %w", err)
		}
	}
	if err := validateSourceConfig(c); err != nil {
		return c, err
	}
	return c, nil
}

// maxWebSources ограничивает один проход сбора, а не размер панели источников.
const maxWebSources = 192

// maxPanelDomains равен потолку числа лент: почти каждая лента приносит свой домен.
const maxPanelDomains = 192

func validateSourceConfig(c sourceConfig) error {
	if !validBudgets(c) || len(c.Web) == 0 || len(c.Web) > maxWebSources || len(c.PatentTerms) > 64 {
		return errors.New("invalid source history, request, render or EP budget, or source count")
	}
	names := map[string]bool{}
	panel := c.domains()
	if len(panel) == 0 || len(panel) > maxPanelDomains {
		return errors.New("the collected panel must name 1..192 domains")
	}
	for _, f := range c.Web {
		u, e := url.Parse(f.URL)
		if e != nil || u.Scheme != schemeHTTPS || u.Host == "" || u.User != nil || f.Name == "" || names[f.Name] ||
			!slices.Contains([]string{kindFeed, "rss", kindWordPress, kindURL, kindURLs, kindSitemap,
				kindCNCF, kindTechPort, kindHFPapers, kindHFModels, kindBingNews}, f.Kind) {
			return errors.New("source requires unique name, HTTPS URL and feed/rss/wordpress/url/urls/sitemap kind")
		}
		if f.Pages < 0 || f.Pages > 500 || (f.Kind == kindURLs) != (len(f.URLs) > 0) || len(f.URLs) > 500 {
			return errors.New("a URL list belongs to kind=urls and holds at most 500 pages")
		}
		names[f.Name] = true
	}
	return nil
}

func validBudgets(c sourceConfig) bool {
	return c.HistoryMonths >= 1 && c.HistoryMonths <= 36 && c.RequestBudget >= 1 && c.RequestBudget <= 20000 &&
		c.RenderBudget >= 0 && c.RenderBudget <= 2000 && c.EPDocuments >= 0 && c.EPDocuments <= 5000 &&
		c.EPFairUseBytes >= 0 && c.EPFairUseBytes <= epFairUseDefault && c.OPSDocuments >= 0 && c.OPSDocuments <= 20000 &&
		c.RUDocuments >= 0 && c.RUDocuments <= 5000 && c.CNDocuments >= 0 && c.CNDocuments <= 5000
}

func (c sourceConfig) fingerprint(kind string) string {
	// Рабочие бюджеты страниц не меняют состав источников и в отпечаток не входят.
	c.RequestBudget, c.RenderBudget = 0, 0
	if patentCollectorKind(kind) {
		c.Web = nil
	} else {
		c.PatentTerms = nil
		c.CNDocuments = 0
	}
	b, _ := json.Marshal(c)
	if kind == kindCNPatents {
		b = append([]byte(cnParserVersion+"\x00"), b...)
	} else if kind != kindPatents {
		b = append([]byte(webArticleParserVersion+"\x00"+webFeedParserVersion+"\x00"), b...)
	}
	return hashBytes(b)
}
func hashBytes(b []byte) string { h := sha256.Sum256(b); return hex.EncodeToString(h[:]) }

type coverage struct {
	MetadataPending     int          `json:"metadata_pending"`
	MetadataVerified    int          `json:"metadata_verified"`
	Excluded            int          `json:"excluded"`
	FilingDates         int          `json:"filing_dates"`
	PriorityDates       int          `json:"priority_dates"`
	ProviderFamilies    int          `json:"provider_families"`
	ApplicationFamilies int          `json:"application_families"`
	Source              string       `json:"source"`
	From                string       `json:"from"`
	Through             string       `json:"through"`
	Received            int          `json:"received"`
	Titles              int          `json:"titles"`
	Content             int          `json:"content"`
	MissingTitles       int          `json:"missing_titles"`
	Families            int          `json:"families"`
	Total               *int         `json:"provider_total"`
	Cursor              string       `json:"next_cursor"`
	StopReason          string       `json:"stop_reason"`
	Exhausted           bool         `json:"exhausted"`
	ObservedAt          string       `json:"observed_at"`
	Pages               []provenance `json:"pages"`
	Limitations         []string     `json:"limitations,omitempty"`
}

// cached ключует кэш неизменной неделей или месяцем: адрес вида /week36 переиспользуется из года в год.
func (s *service) cached(ctx context.Context, address, identity string, data *snapshot) ([]byte, error) {
	name := s.cachePath(identity, address)
	if b, err := os.ReadFile(name); err == nil {
		// Страница могла лечь в дневной кэш без приведения к UTF-8.
		b = sourceUTF8(b, address, "", data)
		data.Pages = append(data.Pages, provenance{URL: address, SHA256: hashBytes(b)})
		return b, nil
	}
	b, err := s.fetch(ctx, address, data)
	if err != nil {
		return nil, err
	}
	return b, cachePut(name, b)
}

func (s *service) cachePath(identity, address string) string {
	return filepath.Join(s.dir, "cache", hashBytes([]byte(identity+"\x00"+address)))
}

func cachePut(name string, b []byte) error {
	if err := os.MkdirAll(filepath.Dir(name), 0750); err != nil {
		return fmt.Errorf("page cache directory: %w", err)
	}
	tmp, err := os.CreateTemp(filepath.Dir(name), ".source-page-")
	if err != nil {
		return fmt.Errorf("create temporary page file: %w", err)
	}
	defer func() { _ = os.Remove(tmp.Name()) }()
	_, writeErr := tmp.Write(b)
	closeErr := tmp.Close()
	if err = errors.Join(writeErr, closeErr); err != nil {
		return fmt.Errorf("write temporary page file: %w", err)
	}
	if err = os.Rename(tmp.Name(), name); err != nil {
		return fmt.Errorf("publish page cache: %w", err)
	}
	return nil
}
func monthWindows(from, through string) []period {
	start, _ := time.Parse(time.DateOnly, from)
	end, _ := time.Parse(time.DateOnly, through)
	out := []period{}
	for t := start; t.Before(end.AddDate(0, 0, 1)); t = t.AddDate(0, 1, 0) {
		out = append(out, period{t.Format(time.DateOnly),
			min(t.AddDate(0, 1, 0).Format(time.DateOnly), end.AddDate(0, 0, 1).Format(time.DateOnly))})
	}
	return out
}
func summarizeCoverage(data *snapshot) {
	for i := range data.Coverage {
		c := &data.Coverage[i]
		ids := map[string]bool{}
		for _, r := range data.Records {
			if r.Source != c.Source || r.Date < c.From || r.Date > c.Through {
				continue
			}
			addCoverageRecord(c, r, ids)
		}
		c.Families = len(ids)
	}
}
func addCoverageRecord(c *coverage, r record, ids map[string]bool) {
	c.Received++
	if metadataVerified(r) {
		c.MetadataVerified++
	} else if r.Source == patentSource {
		c.MetadataPending++
	}
	if r.ExcludedReason != "" {
		if metadataVerified(r) {
			c.Excluded++
		} else if r.Title == "" {
			c.MissingTitles++
		}
		return
	}
	if r.Filing != "" {
		c.FilingDates++
	}
	if r.Priority != "" {
		c.PriorityDates++
	}
	if r.FamilyBasis == "USPTO Public Search Family ID" || r.FamilyBasis == opsFamilyBasis {
		c.ProviderFamilies++
	} else if strings.HasPrefix(r.FamilyID, "application:") {
		c.ApplicationFamilies++
	}
	if r.Title != "" {
		c.Titles++
	} else {
		c.MissingTitles++
	}
	if r.Text != "" {
		c.Content++
	}
	if r.FamilyID != "" {
		ids[r.FamilyID] = true
	}
}

func linkDuplicates(rows []record) {
	canonical := map[string]int{}
	for i := range rows {
		r := &rows[i]
		if r.Text == "" || r.Date == "" {
			continue
		}
		if r.ContentHash == "" {
			r.ContentHash = hashBytes([]byte(r.Text))
		}
		key := r.ContentHash
		if j, ok := canonical[key]; ok {
			first := &rows[j]
			if r.Date < first.Date {
				first.OriginalURL = r.URL
				first.OriginalBasis = "identical content, earlier publication"
				canonical[key] = i
			} else if r.URL != first.URL {
				r.OriginalURL = first.URL
				r.OriginalBasis = "identical content, earlier publication"
			}
		} else {
			canonical[key] = i
		}
	}
}

// exportRecords привязывает страницы к снимку: после обновления — 409, а не пропуск строк за сдвинувшимся смещением.
func (s *service) exportRecords(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	limit, err := strconv.Atoi(q.Get("limit"))
	if err != nil || limit < 1 || limit > 100 {
		http.Error(w, "limit1..100 required", http.StatusBadRequest)
		return
	}
	from, fe := time.Parse(time.DateOnly, q.Get("from"))
	through, te := time.Parse(time.DateOnly, q.Get("through"))
	if fe != nil || te != nil || through.Before(from) || len(q.Get("query")) > 512 {
		http.Error(w, "valid calendar required", http.StatusBadRequest)
		return
	}
	s.mu.RLock()
	data := s.data
	s.mu.RUnlock()
	if data == nil {
		http.Error(w, "source not ready", http.StatusServiceUnavailable)
		return
	}
	candidates := []record{}
	for _, v := range data.Records {
		if v.Date >= q.Get("from") && v.Date <= q.Get("through") && v.Title != "" && v.ExcludedReason == "" &&
			exportMatches(v, q.Get("query")) {
			candidates = append(candidates, v)
		}
	}
	slices.SortFunc(candidates, func(a, b record) int { return strings.Compare(a.ID, b.ID) })
	version, err := recordsVersion(data.SourceSet, candidates)
	if err != nil {
		http.Error(w, "record snapshot could not be hashed", http.StatusInternalServerError)
		return
	}
	start := 0
	if cursor := q.Get("cursor"); cursor != "" {
		parts := strings.Split(cursor, ":")
		if len(parts) != 2 || parts[0] != version {
			http.Error(w, "snapshot changed; restart deduplicated scope", http.StatusConflict)
			return
		}
		start, err = strconv.Atoi(parts[1])
		if err != nil || start < 0 || start > len(candidates) {
			http.Error(w, "invalid cursor", http.StatusBadRequest)
			return
		}
	}
	end := min(start+limit, len(candidates))
	next := ""
	if end < len(candidates) {
		next = fmt.Sprintf("%s:%d", version, end)
	}
	respond(w, http.StatusOK, map[string]any{
		"records": candidates[start:end], "next_cursor": next, "exhausted": end == len(candidates),
		"total": len(candidates), "observed_at": data.ObservedAt, "source_set": data.SourceSet,
		"coverage": data.Coverage, "limits": data.Limits,
	})
}

// Темы уже отобраны панелью сборщика: английских ключевых слов в китайском тексте не требуем.
func exportMatches(row record, query string) bool {
	if query == "country:CN" {
		return row.Source == cnSource && cnID.MatchString(row.ID)
	}
	return query == "*" || matches(row.Title+" "+row.Text, strings.Split(query, "|"))
}

// Исправление фактов сбрасывает курсоры, а время получения и провенанс в хеш не входят и обход не сбрасывают.
func recordsVersion(sourceSet string, rows []record) (string, error) {
	h := sha256.New()
	if err := json.MarshalWrite(h, sourceSet); err != nil {
		return "", fmt.Errorf("hash source set: %w", err)
	}
	var row record
	for i := range rows {
		row = rows[i]
		row.ObservedAt = ""
		row.AvailableAt = ""
		row.ParserVersion = ""
		row.Provenance = nil
		if err := json.MarshalWrite(h, &row, json.Deterministic(true)); err != nil {
			return "", fmt.Errorf("hash record: %w", err)
		}
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func formatTime(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.UTC().Format(time.RFC3339)
}
