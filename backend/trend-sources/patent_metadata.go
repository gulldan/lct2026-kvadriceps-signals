package main

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"errors"
	"fmt"
	"io"
	"maps"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode"
)

const patentMetadataVersion = "uspto-public-search-v1"
const patentSearchBase = "https://ppubs.uspto.gov"
const patentSearchBatchSize = 500
const patentSearchSource = "USPAT"
const patentSource = "USPTO"
const metadataOnlyText = "metadata_only"
const patentWithdrawnClass = "WITHDRAWN"
const patentWithdrawnReason = "USPTO withdrew this patent number from issue; " +
	"Public Search holds no granted document for it"

// Анонимной сессии Public Search не нужны аккаунт и ключ ODP; токен живёт только в памяти, не в квитанциях.
type patentSearchClient struct {
	service  *service
	http     *http.Client
	base     string
	token    string
	caseID   int64
	interval time.Duration
}

type patentSearchRecord struct {
	GUID        string   `json:"guid"`
	Number      string   `json:"publicationReferenceDocumentNumber"`
	DocumentID  string   `json:"documentId"`
	Title       string   `json:"inventionTitle"`
	Published   string   `json:"datePublished"`
	Filing      []string `json:"applicationFilingDate"`
	Priority    []string `json:"priorityClaimsDate"`
	Application string   `json:"applicationNumber"`
	FamilyID    *int64   `json:"familyIdentifierCur"`
	Kind        []string `json:"kindCode"`
	Type        string   `json:"type"`
	Class       string   `json:"mainClassificationCode"`
}

type patentSearchResponse struct {
	Found   int                  `json:"numFound"`
	Error   any                  `json:"error"`
	Patents []patentSearchRecord `json:"patents"`
}

type patentMetadataProgress struct {
	HistoryWindows  []patentHistoryWindow `json:"history_windows,omitempty"`
	RetryAfter      time.Time             `json:"retry_after,omitzero"`
	Window          string                `json:"publication_date,omitempty"`
	Offset          int                   `json:"next_start"`
	Total           *int                  `json:"provider_total"`
	Version         string                `json:"version"`
	SourceSet       string                `json:"source_set"`
	ObservedAt      string                `json:"observed_at"`
	Records         int                   `json:"records"`
	Verified        int                   `json:"verified"`
	Excluded        int                   `json:"excluded"`
	Pending         int                   `json:"pending"`
	Cursor          string                `json:"next_cursor"`
	Requests        int                   `json:"requests_used"`
	FullText        int                   `json:"full_text_documents"`
	FullTextMissing int                   `json:"full_text_unavailable"`
	Budget          int                   `json:"request_budget"`
	StopReason      string                `json:"stop_reason"`
	Exhausted       bool                  `json:"exhausted"`
}

type patentMetadataReceipt struct {
	Start      int            `json:"start"`
	Request    jsontext.Value `json:"request"`
	Version    string         `json:"version"`
	URL        string         `json:"url"`
	Query      string         `json:"query"`
	IDs        []string       `json:"requested_ids"`
	ObservedAt string         `json:"observed_at"`
	SHA256     string         `json:"response_sha256"`
	Found      int            `json:"provider_returned"`
}

func metadataVerified(r record) bool {
	return parserIncludes(r.ParserVersion, patentMetadataVersion)
}
func primaryVerified(r record) bool {
	return parserIncludes(r.ParserVersion, primaryPatentParserVersion)
}

func parserIncludes(value, version string) bool {
	for token := range strings.SplitSeq(value, ";") {
		if token == version {
			return true
		}
	}
	return false
}

var applicationDigits = regexp.MustCompile(`^(?:[0-9]{2}|D)/[0-9]{6}$`)

var patentDigits = regexp.MustCompile(`^(?:US)?(RE|PP|D)?0*([1-9][0-9]*)$`)

// Нули в адресах бюллетеня для сортов растений и повторных экспертиз — форматирование, а не часть номера.
func normalizedPatentID(value string) string {
	match := patentDigits.FindStringSubmatch(value)
	if match == nil {
		return ""
	}
	return "US" + match[1] + match[2]
}

// Отозванный номер приходит без ссылки на публикацию; идентичность документа остаётся в documentId и guid.
func patentRecordID(found patentSearchRecord) string {
	if id := normalizedPatentID(found.Number); id != "" {
		return id
	}
	number, ok := strings.CutPrefix(strings.Join(strings.Fields(found.DocumentID), " "), "US ")
	if !ok || found.GUID != "US-"+number {
		return ""
	}
	return normalizedPatentID(number)
}

var patentTitleMarkup = regexp.MustCompile(`<[^>]*>|\.SU[BP]\.`)

// Public Search ещё несёт старую вёрстку индексов и курсива — это оформление, а не другой патент.
func sameTitle(a, b string) bool { return patentTitleKey(a) == patentTitleKey(b) }
func patentTitleKey(value string) string {
	var key strings.Builder
	for _, r := range patentTitleMarkup.ReplaceAllString(value, "") {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			key.WriteRune(unicode.ToLower(r))
		}
	}
	return key.String()
}

func grantKind(id, kind string) bool {
	switch {
	case strings.HasPrefix(id, "USD"):
		return kind == "S" || kind == "S1"
	case strings.HasPrefix(id, "USPP"):
		return kind == "P2" || kind == "P3"
	case strings.HasPrefix(id, "USRE"):
		return kind == "E" || kind == "E1"
	default:
		return kind == "B1" || kind == "B2"
	}
}
func structuredDay(value string) string {
	day, err := time.Parse(time.RFC3339, value)
	if err != nil || day.Hour() != 0 || day.Minute() != 0 || day.Second() != 0 || day.Nanosecond() != 0 {
		return ""
	}
	return day.UTC().Format(time.DateOnly)
}
func earliestStructuredDay(values []string) string {
	earliest := ""
	for _, value := range values {
		day := structuredDay(value)
		if day == "" {
			return ""
		}
		if earliest == "" || day < earliest {
			earliest = day
		}
	}
	return earliest
}

func applyPatentMetadata(row record, found patentSearchRecord, page provenance, observed string) (record, error) {
	id := normalizedPatentID(row.ID)
	if id == "" || patentRecordID(found) != id || found.Type != patentSearchSource {
		return row, errors.New("structured patent identity/type is not exact")
	}
	// Отозванный номер — окончательный ответ: прежняя запись бюллетеня до выдачи не считается выдачей.
	if found.Class == patentWithdrawnClass {
		if len(found.Kind) != 0 || found.Published != "" {
			return row, errors.New("withdrawn patent number carries grant fields")
		}
		markPatentObserved(&row, page, observed)
		row.ExcludedReason = patentWithdrawnReason
		return row, nil
	}
	if len(found.Kind) != 1 {
		return row, errors.New("structured patent identity/type is not exact")
	}
	kind := found.Kind[0]
	if found.GUID != "US-"+found.Number+"-"+kind ||
		strings.Join(strings.Fields(found.DocumentID), " ") != "US "+found.Number+" "+kind {
		return row, errors.New("structured patent identifiers disagree")
	}
	day := structuredDay(found.Published)
	title := strings.Join(strings.Fields(found.Title), " ")
	if day == "" || title == "" || len(title) > 2000 || strings.ContainsRune(title, '\x00') {
		return row, errors.New("structured title or publication date is invalid")
	}
	row.KindCode = kind
	markPatentObserved(&row, page, observed)
	// Тот же патент позже появляется в бюллетене как сертификат: это исключённое свидетельство, а не новая выдача.
	if !grantKind(id, kind) {
		row.ExcludedReason = "USPTO document kind " + kind + " is not a new grant"
		return row, nil
	}
	if day != row.Date {
		row.ExcludedReason = "USPTO grant publication " + day + " differs from Gazette issue " + row.Date +
			"; not this issue's new grant"
		row.PublishedAt = found.Published
		return row, nil
	}
	if primaryVerified(row) && row.Title != "" && !sameTitle(row.Title, title) {
		return row, errors.New("structured title conflicts with verified primary title")
	}
	row.ExcludedReason = ""
	if !primaryVerified(row) {
		row.Title = title
		row.TitleBasis = "USPTO Public Search structured inventionTitle"
	}
	row.PublishedAt = found.Published
	if row.DateBasis != patentSearchDateBasis && !strings.Contains(row.DateBasis, "later Gazette occurrence") {
		row.DateBasis = "USPTO Public Search grant publication, confirmed against Gazette issue"
	}
	applyPatentFacts(&row, found, day)

	return row, nil
}
func markPatentObserved(row *record, page provenance, observed string) {
	if !metadataVerified(*row) {
		row.ParserVersion = strings.Trim(row.ParserVersion+";"+patentMetadataVersion, ";")
	}
	row.Provenance = append(slices.Clone(row.Provenance), page)
	row.ObservedAt = observed
	if row.AvailableAt == "" {
		row.AvailableAt = observed
	}
}
func applyPatentFacts(row *record, found patentSearchRecord, day string) {
	if row.Filing == "" && len(found.Filing) == 1 {
		if filing := structuredDay(found.Filing[0]); filing != "" && filing <= day {
			row.Filing = filing
			row.FilingBasis = "USPTO Public Search applicationFilingDate"
		}
	}
	if row.Priority == "" {
		if priority := earliestStructuredDay(found.Priority); priority != "" && priority <= day {
			row.Priority = priority
			row.PriorityBasis = "USPTO Public Search priorityClaimsDate (foreign priority)"
		}
	}
	if row.Application == "" && applicationDigits.MatchString(found.Application) {
		// Пространство номеров заявок USPTO, включая D/ для образцов, сохраняется: серию по букве не угадываем.
		row.Application = "US" + strings.NewReplacer("/", "", ",", "").Replace(found.Application)
	}
	if found.FamilyID != nil && *found.FamilyID > 0 {
		row.FamilyID = "uspto-public-search:" + strconv.FormatInt(*found.FamilyID, 10)
		row.FamilyBasis = "USPTO Public Search Family ID"
	} else if row.FamilyID == "" && row.Application != "" {
		row.FamilyID = "application:" + row.Application
		row.FamilyBasis = "same USPTO application only; wider family unknown"
	}
	if row.TextStatus == "" {
		row.TextStatus = metadataOnlyText
	}
}

func newPatentSearchClient(s *service) *patentSearchClient {
	client := *s.http
	// Публичный адрес согласует h2, но подвешивает этот клиент до заголовков ответа; проверен HTTP/1.1.
	// Проверка сертификата, прокси и доверенные корни берутся из имеющегося транспорта.
	base := client.Transport
	if base == nil {
		base = http.DefaultTransport
	}
	if transport, ok := base.(*http.Transport); ok {
		transportCopy := transport.Clone()
		transportCopy.ForceAttemptHTTP2 = false
		transportCopy.Protocols = new(http.Protocols)
		transportCopy.Protocols.SetHTTP1(true)
		transportCopy.TLSNextProto = map[string]func(string, *tls.Conn) http.RoundTripper{}
		if transportCopy.TLSClientConfig == nil {
			transportCopy.TLSClientConfig = &tls.Config{}
		} else {
			transportCopy.TLSClientConfig = transportCopy.TLSClientConfig.Clone()
		}
		transportCopy.TLSClientConfig.NextProtos = []string{"http/1.1"}
		client.Transport = transportCopy
	}
	jar, _ := cookiejar.New(nil)
	client.Jar = jar
	return &patentSearchClient{service: s, http: &client, base: patentSearchBase, interval: 3 * time.Second}
}
func (c *patentSearchClient) post(ctx context.Context, path string, payload []byte) ([]byte, http.Header, error) {
	return c.do(ctx, http.MethodPost, path, payload)
}

func (c *patentSearchClient) get(ctx context.Context, path string) ([]byte, http.Header, error) {
	return c.do(ctx, http.MethodGet, path, nil)
}

func (c *patentSearchClient) do(ctx context.Context, method, path string, payload []byte) ([]byte, http.Header, error) {
	s := c.service
	if raw, e := os.ReadFile(filepath.Join(s.dir, "patent-metadata-retry.json")); e == nil {
		var until time.Time
		if json.Unmarshal(raw, &until) == nil && until.After(time.Now()) {
			return nil, nil, fmt.Errorf("USPTO retry postponed until %s", until.Format(time.RFC3339))
		}
	}
	if s.config.RequestBudget > 0 && s.requests >= s.config.RequestBudget {
		return nil, nil, errBudget
	}
	s.requests++
	select {
	case <-ctx.Done():
		return nil, nil, fmt.Errorf("wait for USPTO request: %w", ctx.Err())
	case <-time.After(c.interval):
	}
	req, err := c.request(ctx, method, path, payload)
	if err != nil {
		return nil, nil, err
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, nil, fmt.Errorf("USPTO request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		if resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode == http.StatusServiceUnavailable {
			until := retryTime(resp.Header.Get("Retry-After"), time.Now().UTC())
			if err = atomicSourceJSON(filepath.Join(s.dir, "patent-metadata-retry.json"), until); err != nil {
				return nil, nil, err
			}
		}
		return nil, nil, &sourceHTTPError{
			Status: resp.StatusCode, Code: "USPTO Public Search; Retry-After=" + resp.Header.Get("Retry-After"),
		}
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, (16<<20)+1))
	if err == nil && len(body) > 16<<20 {
		err = errors.New("USPTO Public Search response exceeds16MiB")
	}
	return body, resp.Header, err
}
func (c *patentSearchClient) request(ctx context.Context, method, path string, payload []byte) (*http.Request, error) {
	var reader io.Reader = http.NoBody
	if payload != nil {
		reader = bytes.NewReader(payload)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.base+path, reader)
	if err != nil {
		return nil, fmt.Errorf("USPTO request: %w", err)
	}
	if payload != nil {
		req.Header.Set("Content-Type", "application/json; charset=UTF-8")
		if path == "/api/users/me/session" {
			req.Header.Set("Content-Type", "text/plain;charset=UTF-8")
		}
	}
	req.Header.Set("User-Agent", "rag-test-signalgraph/1.0 (+public research metadata)")
	req.Header.Set("Origin", c.base)
	req.Header.Set("Referer", c.base+"/pubwebapp/")
	if c.token != "" {
		req.Header.Set("X-Access-Token", c.token)
	}
	return req, nil
}

func (c *patentSearchClient) session(ctx context.Context) error {
	body, headers, err := c.post(ctx, "/api/users/me/session", []byte("-1"))
	if err != nil {
		return err
	}
	var session struct {
		Case struct {
			ID int64 `json:"caseId"`
		} `json:"userCase"`
	}
	if err = json.Unmarshal(body, &session); err != nil {
		return fmt.Errorf("decode USPTO session: %w", err)
	}
	token := headers.Get("X-Access-Token")
	if token == "" || session.Case.ID <= 0 {
		return errors.New("USPTO anonymous session did not return its required fields")
	}
	c.token, c.caseID = token, session.Case.ID
	return nil
}
func patentSearchQuery(ids []string) (string, error) {
	terms := make([]string, 0, len(ids))
	for _, id := range ids {
		normalized := normalizedPatentID(id)
		if normalized == "" {
			return "", errors.New("invalid requested patent ID")
		}
		terms = append(terms, strings.TrimPrefix(normalized, "US")+".pn.")
	}
	return strings.Join(terms, " OR "), nil
}
func (c *patentSearchClient) page(
	ctx context.Context, query string, start int, ids []string, observed string,
) ([]byte, patentMetadataReceipt, error) {
	receipt := patentMetadataReceipt{
		Version: patentMetadataVersion, URL: c.base + "/api/searches/searchWithBeFamily",
		Query: query, Start: start, IDs: ids, ObservedAt: observed,
	}
	var err error
	if c.token == "" {
		if err = c.session(ctx); err != nil {
			return nil, receipt, fmt.Errorf("USPTO metadata page: %w", err)
		}
	}
	payload, err := c.payload(query, start)
	if err != nil {
		return nil, receipt, err
	}
	receipt.Request = payload
	body, _, err := c.post(ctx, "/api/searches/searchWithBeFamily", payload)
	if failure, ok := errors.AsType[*sourceHTTPError](err); ok && failure.Status == http.StatusUnauthorized {
		// Как публичное приложение: после истечения сессия обновляется один раз.
		c.token = ""
		if err = c.session(ctx); err == nil {
			if payload, err = c.payload(query, start); err == nil {
				receipt.Request = payload
				body, _, err = c.post(ctx, "/api/searches/searchWithBeFamily", payload)
			}
		}
	}
	if err == nil {
		receipt.SHA256 = hashBytes(body)
	}
	return body, receipt, err
}

func (c *patentSearchClient) payload(query string, start int) ([]byte, error) {
	// Поля searchQuery — как у публичного приложения; termGraph из ответа counts — внутреннее состояние, а не запрос.
	searchQuery := map[string]any{
		"querySource": "brs", "caseId": c.caseID, "q": query, "queryName": query,
		"qt": "brs", "op": "OR", "highlights": "1", "plurals": false,
		"britishEquivalents": false,
		"databaseFilters":    []map[string]any{{"databaseName": patentSearchSource, "countryCodes": []string{}}},
		"searchType":         1, "ignorePersist": true, "userEnteredQuery": query,
		"viewName": "tile", "spellCheck": false,
	}
	payload, err := json.Marshal(map[string]any{
		"start": start, "pageCount": patentSearchBatchSize, "sort": "date_publ desc",
		"docFamilyFiltering": "noFiltering", "searchType": 1, "queryId": 0,
		"tagDocSearch": false, "query": searchQuery,
	})
	if err != nil {
		return nil, fmt.Errorf("encode USPTO public search: %w", err)
	}
	return payload, nil
}

func atomicSourceJSON(name string, value any) error {
	if err := os.MkdirAll(filepath.Dir(name), 0750); err != nil {
		return fmt.Errorf("create source checkpoint directory: %w", err)
	}
	f, err := os.CreateTemp(filepath.Dir(name), ".source-json-")
	if err != nil {
		return fmt.Errorf("create source checkpoint: %w", err)
	}
	defer func() { _ = os.Remove(f.Name()) }()
	writeErr := json.MarshalWrite(f, value)
	if err = errors.Join(writeErr, f.Close()); err != nil {
		return err
	}
	if err = os.Rename(f.Name(), name); err != nil {
		return fmt.Errorf("publish source checkpoint: %w", err)
	}
	return nil
}
func (s *service) metadataPage(
	ctx context.Context, client *patentSearchClient, ids []string, observed string,
) ([]byte, patentMetadataReceipt, error) {
	query, err := patentSearchQuery(ids)
	if err != nil {
		return nil, patentMetadataReceipt{}, err
	}
	return s.metadataSearchPage(ctx, client, query, 0, ids, observed)
}
func (s *service) metadataSearchPage(
	ctx context.Context, client *patentSearchClient, query string, start int,
	ids []string, observed string,
) ([]byte, patentMetadataReceipt, error) {
	var err error
	key := hashBytes([]byte(patentMetadataVersion + "\x00" + query + "\x00" + strconv.Itoa(start)))
	name := filepath.Join(s.dir, "patent-metadata", key+".json")
	if saved, e := os.ReadFile(name); e == nil {
		var receipt patentMetadataReceipt
		if json.Unmarshal(saved, &receipt) == nil && receipt.Version == patentMetadataVersion &&
			receipt.Query == query && receipt.Start == start && slices.Equal(receipt.IDs, ids) {
			if body, e := os.ReadFile(filepath.Join(s.dir, "raw", receipt.SHA256)); e == nil &&
				hashBytes(body) == receipt.SHA256 {
				return body, receipt, nil
			}
		}
	}
	body, receipt, err := client.page(ctx, query, start, ids, observed)
	if err != nil {
		return nil, receipt, fmt.Errorf("USPTO metadata page: %w", err)
	}
	var response patentSearchResponse
	if err = json.Unmarshal(body, &response); err != nil {
		return nil, receipt, fmt.Errorf("USPTO metadata page: %w", err)
	}
	if response.Error != nil || response.Found < start+len(response.Patents) ||
		len(response.Patents) > patentSearchBatchSize || (response.Found > start && len(response.Patents) == 0) ||
		(len(ids) > 0 && response.Found != len(response.Patents)) {
		return nil, receipt, errors.New("USPTO search was incomplete or returned an error; batch not checkpointed")
	}
	receipt.Found = response.Found
	if err = os.MkdirAll(filepath.Join(s.dir, "raw"), 0750); err != nil {
		return nil, receipt, fmt.Errorf("USPTO metadata page: %w", err)
	}
	if err = os.WriteFile(filepath.Join(s.dir, "raw", receipt.SHA256), body, 0600); err != nil {
		return nil, receipt, fmt.Errorf("USPTO metadata page: %w", err)
	}
	if err = atomicSourceJSON(name, receipt); err != nil {
		return nil, receipt, fmt.Errorf("USPTO metadata page: %w", err)
	}
	return body, receipt, nil
}

func (s *service) enrichPatentMetadata(ctx context.Context, data *snapshot) error {
	client := newPatentSearchClient(s)
	index, dates, err := patentMetadataIndex(data)
	if err != nil {
		return err
	}
	progress := patentMetadataProgress{
		Version: patentMetadataVersion, SourceSet: data.SourceSet, ObservedAt: data.ObservedAt,
		Records: len(index), Budget: s.config.RequestBudget,
	}
	archiveComplete := preparePatentHistory(data, &progress)
	stop := ""
	for _, day := range dates {
		if data.PatentIndexFrom != "" && day < data.PatentIndexFrom {
			continue
		}
		if err := s.metadataWeek(ctx, client, data, index, day, &progress); err != nil {
			stop = err.Error()
			break
		}
	}
	if stop == "" {
		if err := s.completePatentHistory(ctx, client, data, index, &progress, archiveComplete); err != nil {
			stop = err.Error()
		}
	}
	// Хвост — старые сертификаты, ошибки индекса, номера вне недельного поиска — только после всего архива.
	if stop == "" {
		if err := s.metadataTail(ctx, client, data, &progress); err != nil {
			stop = err.Error()
		}
	}
	// Полный текст последним: он тратит по запросу на кандидата и опирается на проверенные выше метаданные.
	if stop == "" {
		if err := s.patentFullTexts(ctx, client, data, &progress); err != nil {
			stop = err.Error()
		}
	}
	if stop == "" && data.PatentIndexFrom == "" {
		stop = "Gazette issue boundary is not verified; historical date range remains unknown"
	}
	updatePatentProgress(&progress, data, stop, s.requests)
	if raw, e := os.ReadFile(filepath.Join(s.dir, "patent-metadata-retry.json")); e == nil {
		_ = json.Unmarshal(raw, &progress.RetryAfter)
	}
	data.PatentMetadata = &progress
	if err := atomicSourceJSON(filepath.Join(s.dir, "patent-metadata-progress.json"), progress); err != nil {
		return err
	}
	s.mu.Lock()
	s.metadataProgress = progress
	s.mu.Unlock()
	metadataCoverage(data, progress.StopReason)
	data.Limits = append(data.Limits,
		"Public Search supplies verified grants through dated 500-record pages with no family-result filtering, "+
			"including verified IDs absent from the Gazette and earlier dates within the configured history; "+
			"unresolved IDs use bounded 40-ID queries. New-grant counts exclude document-kind or publication-date conflicts "+
			"and numbers USPTO withdrew from issue. "+
			"Provider Family IDs are attributed to USPTO Public Search; application identities alone do not establish wider families. "+
			"Missing priority dates remain unknown.")
	return nil
}
func (s *service) savePatentProgress(progress *patentMetadataProgress, data *snapshot, stop string) error {
	updatePatentProgress(progress, data, stop, s.requests)
	if err := atomicSourceJSON(filepath.Join(s.dir, "patent-metadata-progress.json"), progress); err != nil {
		return err
	}
	s.mu.Lock()
	s.metadataProgress = *progress
	s.metadataProgress.HistoryWindows = slices.Clone(progress.HistoryWindows)
	s.mu.Unlock()
	return nil
}
func (s *service) metadataWeek(
	ctx context.Context, client *patentSearchClient, data *snapshot,
	index map[string]int, day string, progress *patentMetadataProgress,
) error {
	query := strings.ReplaceAll(day, "-", "") + ".PD."
	total := -1
	seen := map[string]bool{}
	for start := 0; ; {
		raw, receipt, err := s.metadataSearchPage(ctx, client, query, start, nil, data.ObservedAt)
		if err != nil {
			return err
		}
		var response patentSearchResponse
		if err = json.Unmarshal(raw, &response); err != nil {
			return fmt.Errorf("decode USPTO stored page: %w", err)
		}
		if total >= 0 && response.Found != total {
			return errors.New("USPTO weekly population changed between saved pages; no completeness claimed")
		}
		total = response.Found
		page := provenance{URL: receipt.URL, SHA256: receipt.SHA256}
		appendMetadataPage(data, page)
		if err = applyPatentWeekPage(data, index, day, seen, response.Patents, page, receipt.ObservedAt); err != nil {
			return err
		}
		progress.Window = day
		progress.Offset = start + len(response.Patents)
		progress.Total = new(total)
		if err = s.savePatentProgress(progress, data, "publication date "+day); err != nil {
			return err
		}
		start += len(response.Patents)
		if start == total {
			return nil
		}
		if start > total {
			return errors.New("USPTO weekly page exceeds pinned total")
		}
	}
}
func applyPatentWeekPage(
	data *snapshot, index map[string]int, day string, seen map[string]bool,
	records []patentSearchRecord, page provenance, observed string,
) error {
	for _, found := range records {
		if structuredDay(found.Published) != day {
			return errors.New("USPTO weekly page returned a different publication date")
		}
		id := patentRecordID(found)
		if seen[id] {
			return errors.New("duplicate patent between weekly pages; no completeness claimed")
		}
		seen[id] = true
		i, ok := index[id]
		if !ok {
			if id == "" {
				return errors.New("invalid structured patent identifier")
			}
			i = len(data.Records)
			index[id] = i
			data.Records = append(data.Records, newPublicSearchPatent(id, day))
		}
		row := data.Records[i]
		if metadataVerified(row) {
			continue
		}
		if row.Date != day {
			// Дату выдачи задаёт сам датированный ответ USPTO; позднее появление в сертификате или индексе её не сдвигает.
			row.Date = day
			row.DateBasis = "USPTO Public Search dated grant result; later Gazette occurrence retained in provenance"
			params := url.Values{"q": {strings.TrimPrefix(id, "US") + ".pn."}, "db": {patentSearchSource}}
			row.URL = patentSearchBase + "/pubwebapp/external.html?" + params.Encode()
		}
		enriched, applyErr := applyPatentMetadata(row, found, page, observed)
		if applyErr != nil {
			recordMetadataFailure(&data.Records[i], page, applyErr)
			continue
		}
		data.Records[i] = enriched
	}

	return nil
}
func (s *service) metadataTail(
	ctx context.Context, client *patentSearchClient, data *snapshot, progress *patentMetadataProgress,
) error {
	refs := []int{}
	for i, row := range data.Records {
		if !metadataVerified(row) {
			refs = append(refs, i)
		}
	}
	slices.SortFunc(refs, func(a, b int) int { return strings.Compare(data.Records[a].ID, data.Records[b].ID) })
	const tailSize = 40 // Публичный адрес отвергает длинные тела запросов с OR.
	for start := 0; start < len(refs); start += tailSize {
		batch := refs[start:min(start+tailSize, len(refs))]
		ids := make([]string, 0, len(batch))
		for _, i := range batch {
			ids = append(ids, data.Records[i].ID)
		}
		raw, receipt, err := s.metadataPage(ctx, client, ids, data.ObservedAt)
		if err != nil {
			return err
		}
		var response patentSearchResponse
		if err = json.Unmarshal(raw, &response); err != nil {
			return fmt.Errorf("decode USPTO stored page: %w", err)
		}
		if err = applyPatentTail(data, batch, response.Patents, receipt); err != nil {
			return err
		}
		if err = s.savePatentProgress(progress, data, "remaining exact IDs"); err != nil {
			return err
		}
	}
	return nil
}
func applyPatentTail(data *snapshot, batch []int, records []patentSearchRecord, receipt patentMetadataReceipt) error {
	returned := map[string][]patentSearchRecord{}
	for _, found := range records {
		id := patentRecordID(found)
		returned[id] = append(returned[id], found)
	}
	page := provenance{URL: receipt.URL, SHA256: receipt.SHA256}
	appendMetadataPage(data, page)
	for _, i := range batch {
		row := data.Records[i]
		found := returned[normalizedPatentID(row.ID)]
		if len(found) != 1 {
			continue
		}
		enriched, err := applyPatentMetadata(row, found[0], page, receipt.ObservedAt)
		if err != nil {
			recordMetadataFailure(&data.Records[i], page, err)
			continue
		}
		data.Records[i] = enriched
	}

	return nil
}
func recordMetadataFailure(row *record, page provenance, err error) {
	if !slices.Contains(row.Provenance, page) {
		row.Provenance = append(slices.Clone(row.Provenance), page)
	}
	if !primaryVerified(*row) {
		row.ExcludedReason = "structured metadata not verified: " + err.Error()
	}
}

func appendMetadataPage(data *snapshot, page provenance) {
	if !slices.Contains(data.Pages, page) {
		data.Pages = append(data.Pages, page)
	}
}

func patentMetadataIndex(data *snapshot) (map[string]int, []string, error) {
	foldPatentIDs(data)
	index := map[string]int{}
	days := map[string]bool{}
	for i := range data.Records {
		row := &data.Records[i]
		// Против USPTO проверяется только US: номер EP — не номер патента США.
		if row.Source != patentSource {
			continue
		}
		id := normalizedPatentID(row.ID)
		if id == "" {
			return nil, nil, errors.New("invalid archived patent ID")
		}
		index[id] = i
		days[row.Date] = true
		if !metadataVerified(*row) && !primaryVerified(*row) {
			row.ExcludedReason = "new-grant metadata not yet verified"
		}
	}
	dates := slices.Sorted(maps.Keys(days))
	slices.Reverse(dates)

	return index, dates, nil
}

// Бюллетень пишет номера растений и переизданий с нулями (USPP037486), поиск USPTO — без них (USPP37486).
// Если страница бюллетеня прочиталась позже поиска, один патент приходил под двумя ID, и индекс отменял
// весь патентный проход вместе с EP, OPS и Роспатентом. Двойник сворачивается в запись с проверенными метаданными.
func foldPatentIDs(data *snapshot) {
	kept := map[string]int{}
	rows := data.Records[:0]
	for _, row := range data.Records {
		id := normalizedPatentID(row.ID)
		at, twin := kept[id]
		switch {
		case row.Source != patentSource || id == "":
			rows = append(rows, row)
		case !twin:
			kept[id] = len(rows)
			rows = append(rows, row)
		case metadataVerified(row) && !metadataVerified(rows[at]):
			rows[at] = row
		}
	}
	data.Records = rows
}

// metadataCoverage трогает только US: «метаданные ожидают» описывает проверку USPTO, которой у других нет.
func metadataCoverage(data *snapshot, reason string) {
	for i := range data.Coverage {
		if data.Coverage[i].Source != patentSource {
			continue
		}
		metadataWindow(&data.Coverage[i], data.Records, reason)
	}
}
func metadataWindow(c *coverage, rows []record, reason string) {
	pages := make(map[provenance]bool, len(c.Pages))
	for _, page := range c.Pages {
		pages[page] = true
	}
	for _, r := range rows {
		if r.Source != c.Source || r.Date < c.From || r.Date > c.Through {
			continue
		}
		for _, page := range r.Provenance {
			if !pages[page] {
				c.Pages = append(c.Pages, page)
				pages[page] = true
			}
		}
		if metadataVerified(r) {
			continue
		}
		c.Exhausted = false
		c.StopReason = "structured grant metadata pending: " + reason
		if c.Cursor == "" {
			c.Cursor = r.ID
		}
	}
}

func updatePatentProgress(progress *patentMetadataProgress, data *snapshot, stop string, requests int) {
	progress.Verified, progress.Excluded, progress.Pending = 0, 0, 0
	progress.Cursor = ""
	for _, row := range data.Records {
		if metadataVerified(row) {
			if row.ExcludedReason != "" {
				progress.Excluded++
			} else {
				progress.Verified++
			}
		} else {
			progress.Pending++
			if progress.Cursor == "" || row.ID < progress.Cursor {
				progress.Cursor = row.ID
			}
		}
	}
	progress.Requests = requests
	progress.Records = len(data.Records)
	progress.Exhausted = progress.Pending == 0 && stop == ""
	progress.StopReason = stop
	if progress.Exhausted {
		progress.StopReason = "dated patent scope verified or explicitly excluded"
	} else if progress.StopReason == "" {
		progress.StopReason = "metadata pending"
	}
}
