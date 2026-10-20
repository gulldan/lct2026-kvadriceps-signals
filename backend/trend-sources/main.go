// Один бинарник работает двумя изолированными сервисами со своими снимками:
// patent-collector (патентные бюллетени) и web-collector (научные ленты).
package main

import (
	"cmp"
	"context"
	"encoding/json/v2"
	"errors"
	"fmt"
	"hash/maphash"
	"io"
	"log/slog"
	"math"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"syscall"
	"time"
	"unicode"
	"unicode/utf8"
)

const kindWeb = "web"

type record struct {
	DOI            string               `json:"doi,omitempty"`
	DOIAliases     []string             `json:"doi_aliases,omitempty"`
	Genre          string               `json:"genre,omitempty"`
	KindCode       string               `json:"kind_code,omitempty"`
	ExcludedReason string               `json:"excluded_reason,omitempty"`
	PriorityBasis  string               `json:"priority_basis,omitempty"`
	ParserVersion  string               `json:"parser_version,omitempty"`
	FilingBasis    string               `json:"filing_basis,omitempty"`
	PCTFiling      string               `json:"pct_filing,omitempty"`
	PCTApplication string               `json:"pct_application,omitempty"`
	NationalStage  string               `json:"national_stage,omitempty"`
	PriorityIDs    []string             `json:"priority_ids,omitempty"`
	PublishedAt    string               `json:"published_at,omitempty"`
	Text           string               `json:"text,omitempty"`
	TextStatus     string               `json:"text_status,omitempty"`
	TitleBasis     string               `json:"title_basis,omitempty"`
	DateBasis      string               `json:"date_basis,omitempty"`
	AvailableAt    string               `json:"available_at,omitempty"`
	ObservedAt     string               `json:"observed_at,omitempty"`
	Filing         string               `json:"filing,omitempty"`
	Priority       string               `json:"priority,omitempty"`
	Application    string               `json:"application,omitempty"`
	PriorityClaims []string             `json:"priority_claims,omitempty"`
	FamilyID       string               `json:"family_id,omitempty"`
	FamilyBasis    string               `json:"family_basis,omitempty"`
	CanonicalURL   string               `json:"canonical_url,omitempty"`
	OriginalURL    string               `json:"original_url,omitempty"`
	OriginalBasis  string               `json:"original_basis,omitempty"`
	ContentHash    string               `json:"content_hash,omitempty"`
	Provenance     []provenance         `json:"provenance,omitempty"`
	Anchors        []blockAnchor        `json:"anchors,omitempty"`
	Limitations    []string             `json:"limitations,omitempty"`
	Language       string               `json:"language,omitempty"`
	LanguageBasis  string               `json:"language_basis,omitempty"`
	FullText       *patentFullTextFacts `json:"full_text,omitempty"`
	Industry       string               `json:"industry,omitempty"`
	ID             string               `json:"id"`
	Title          string               `json:"title"`
	// OriginalTitle — заголовок на языке публикации, когда Title взят с английской карты сайта.
	OriginalTitle string `json:"original_title,omitempty"`
	Citation      string `json:"citation,omitempty"`
	Date          string `json:"date"`
	URL           string `json:"url"`
	Source        string `json:"source"`
	// Excerpt заполняется только в ответе живому поиску и в снимке не хранится.
	Excerpt string `json:"excerpt,omitempty"`
}

type patentSection struct {
	Name  string `json:"name"`
	Start int    `json:"start"`
	End   int    `json:"end"`
}

type patentClaim struct {
	Number    int  `json:"number"`
	Dependent bool `json:"dependent"`
	Start     int  `json:"start"`
	End       int  `json:"end"`
}

type patentFullTextFacts struct {
	GUID              string          `json:"guid"`
	CompositeID       string          `json:"composite_id,omitempty"`
	PageCount         int             `json:"page_count,omitempty"`
	Sections          []patentSection `json:"sections"`
	Claims            []patentClaim   `json:"claims"`
	Assignees         []string        `json:"assignees,omitempty"`
	Applicants        []string        `json:"applicants,omitempty"`
	CPC               []string        `json:"cpc,omitempty"`
	USReferences      []string        `json:"us_references,omitempty"`
	ForeignReferences []string        `json:"foreign_references,omitempty"`
	Missing           []string        `json:"missing_sections,omitempty"`
}

type provenance struct {
	URL    string `json:"url"`
	SHA256 string `json:"sha256"`
	Method string `json:"method,omitempty"`
}

type snapshot struct {
	PatentIndexFrom string                  `json:"gazette_from,omitempty"`
	PatentMetadata  *patentMetadataProgress `json:"patent_metadata,omitempty"`
	Version         int                     `json:"version"`
	SourceSet       string                  `json:"source_set"`
	Coverage        []coverage              `json:"coverage"`
	Source          string                  `json:"source"`
	ObservedAt      string                  `json:"observed_at"`
	From            string                  `json:"from"`
	Through         string                  `json:"through"`
	Records         []record                `json:"records"`
	Pages           []provenance            `json:"pages"`
	Limits          []string                `json:"limits"`
	index           *stemIndex
}

type service struct {
	metadataProgress patentMetadataProgress
	kind, dir        string
	config           sourceConfig
	requests         int
	renders          int
	renderURL        string
	renderToken      string
	renderQueue      []string
	http             *http.Client
	render           *http.Client
	mu               sync.RWMutex
	data             *snapshot
	lastError        string
	nextRunAt        time.Time
	collecting       bool
	robots           map[string]robotsRules
	// bingMu: список имён пишут параллельные ответы живому поиску, а читает проход.
	bingMu               sync.Mutex
	ops                  opsSession
	opsURL, rospatentURL string
}

func (s *service) collectEvery(ctx context.Context, every time.Duration) {
	failures := 0
	for {
		if ctx.Err() != nil {
			return
		}
		s.mu.Lock()
		s.collecting = true
		s.nextRunAt = time.Time{}
		s.mu.Unlock()
		jobCtx, cancel := context.WithTimeout(ctx, 90*time.Minute)
		err := s.collect(jobCtx, time.Now().UTC())
		cancel()
		if err != nil {
			failures++
			s.mu.Lock()
			s.lastError = err.Error()
			s.mu.Unlock()
			slog.Error("collection failed", "source", s.kind, "error", err)
		} else {
			failures = 0
		}
		delay := collectionDelay(every, failures)
		s.mu.Lock()
		s.collecting = false
		s.nextRunAt = time.Now().UTC().Add(delay)
		s.mu.Unlock()
		if !s.waitRetrying(ctx, delay) {
			return
		}
	}
}

// waitRetrying ждёт следующего прохода, повторяя медленные источники; false — сервис останавливается.
func (s *service) waitRetrying(ctx context.Context, delay time.Duration) bool {
	next := time.Now().Add(delay)
	for {
		select {
		case <-ctx.Done():
			return false
		case <-time.After(min(time.Until(next), slowRetryEvery)):
		}
		if !time.Now().Before(next) {
			return true
		}
		if err := s.retrySlow(ctx, time.Now().UTC()); err != nil {
			slog.Warn("slow source retry failed", "source", s.kind, "error", err)
		}
	}
}

// lockCollection не даёт основному проходу и повтору медленных источников писать снимок одновременно.
func (s *service) lockCollection() (func(), error) {
	lock, err := os.OpenFile(filepath.Join(s.dir, "collection.lock"), os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return nil, fmt.Errorf("open collection lock: %w", err)
	}
	if err = syscall.Flock(int(lock.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		_ = lock.Close()
		return nil, errors.New("another collection holds the source snapshot lock")
	}
	return func() {
		_ = syscall.Flock(int(lock.Fd()), syscall.LOCK_UN)
		_ = lock.Close()
	}, nil
}

func (s *service) publish(data *snapshot) error {
	slices.SortFunc(data.Records, func(a, b record) int { return strings.Compare(b.Date+a.ID, a.Date+b.ID) })
	if err := atomicSourceJSON(filepath.Join(s.dir, "snapshot.json"), *data); err != nil {
		return err
	}
	data.index = newStemIndex(data.Records)
	s.mu.Lock()
	s.data = data
	s.mu.Unlock()
	return nil
}

func collectionDelay(every time.Duration, failures int) time.Duration {
	if failures <= 0 {
		return every
	}
	return min(every, time.Minute*time.Duration(1<<min(failures-1, 4)))
}

// Последняя ошибка сбора не отменяет ещё пригодный снимок.
func (s *service) ready(w http.ResponseWriter, _ *http.Request) {
	s.mu.RLock()
	ready := s.data != nil
	if ready {
		observed, err := time.Parse(time.RFC3339, s.data.ObservedAt)
		ready = err == nil && time.Since(observed) <= 72*time.Hour && !observed.After(time.Now().Add(time.Minute))
	}
	s.mu.RUnlock()
	if !ready {
		http.Error(w, "usable collection snapshot unavailable", http.StatusServiceUnavailable)
		return
	}
	w.WriteHeader(http.StatusOK)
}

func healthcheck() int {
	client := http.Client{Timeout: 5 * time.Second}
	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet,
		"http://127.0.0.1:8090/readyz", http.NoBody)
	if err != nil {
		return 1
	}
	r, err := client.Do(req)
	if err != nil {
		return 1
	}
	_ = r.Body.Close()
	if r.StatusCode != http.StatusOK {
		return 1
	}
	return 0
}

func gateway() int {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	err := arxivGateway(ctx)
	stop()
	if err != nil && !errors.Is(err, http.ErrServerClosed) {
		slog.Error("arxiv gateway", "error", err)
		return 1
	}
	return 0
}

// restore: нечитаемый или более старый снимок — холодный старт, а не частичная история.
func (s *service) restore(dir string) {
	raw, err := os.ReadFile(filepath.Join(dir, "snapshot.json"))
	if err != nil {
		return
	}
	var data snapshot
	if json.Unmarshal(raw, &data) != nil || data.ObservedAt == "" || (data.Version != 2 && data.Version != 3) {
		return
	}
	data.index = newStemIndex(data.Records)
	s.data = &data
	if data.PatentMetadata != nil {
		s.metadataProgress = *data.PatentMetadata
	}
}

func main() {
	if len(os.Args) == 2 && os.Args[1] == "healthcheck" {
		os.Exit(healthcheck())
	}
	kind := os.Getenv("SOURCE_KIND")
	if kind == "arxiv" {
		os.Exit(gateway())
	}
	if !patentCollectorKind(kind) && kind != kindWeb {
		slog.Error("SOURCE_KIND must be patents, cn-patents or web")
		os.Exit(1)
	}
	dir := cmp.Or(os.Getenv("DATA_DIR"), "/data")
	cfg, err := loadConfig(os.Getenv("SOURCE_CONFIG"))
	if err != nil {
		slog.Error("source config", "error", err)
		os.Exit(1)
	}
	proxy, viaProxy, err := sourceProxy()
	if err != nil {
		slog.Error("source proxy", "error", err)
		os.Exit(1)
	}
	if viaProxy {
		slog.Info("source requests go through a proxy", "scheme", proxy.Scheme, "host", proxy.Host)
	}
	// Сборщик ходит только на публичные адреса и не покидает панель, что бы ни просили страница или переадресация.
	s := &service{kind: kind, dir: dir, config: cfg,
		http: newWebClient(40*time.Second, collectorDomains(cfg, kind))}
	if address := os.Getenv("RENDER_URL"); address != "" {
		s.renderURL, s.render = address, &http.Client{Timeout: renderTimeout}
		s.renderToken = os.Getenv("RENDER_TOKEN")
	}
	if err = os.MkdirAll(dir, 0750); err != nil {
		slog.Error("data directory", "error", err)
		os.Exit(1)
	}
	s.restore(dir)
	if len(os.Args) == 2 && os.Args[1] == "--once" {
		ctx, cancel := context.WithTimeout(context.Background(), 90*time.Minute)
		err = s.collect(ctx, time.Now().UTC())
		cancel()
		if err != nil {
			slog.Error("collection failed", "error", err)
			os.Exit(1)
		}
		return
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	go s.collectEvery(ctx, 6*time.Hour)
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })
	mux.HandleFunc("GET /readyz", s.ready)
	mux.HandleFunc("GET /v1/status", s.status)
	mux.HandleFunc("POST /v1/query", s.query)
	mux.HandleFunc("GET /v1/records", s.exportRecords)
	srv := &http.Server{Addr: ":8090", Handler: mux, ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 10 * time.Second,
		WriteTimeout: 15 * time.Second, IdleTimeout: time.Minute}
	go func() {
		<-ctx.Done()
		shutdown, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutdown)
	}()
	err = srv.ListenAndServe()
	stop()
	if err != nil && !errors.Is(err, http.ErrServerClosed) {
		slog.Error("server", "error", err)
		os.Exit(1)
	}
}

func (s *service) status(w http.ResponseWriter, _ *http.Request) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.data == nil {
		respond(w, 503, map[string]any{"status": "collecting", "error": s.lastError})
		return
	}
	titles, undated := 0, 0
	for _, row := range s.data.Records {
		if row.Title != "" && row.ExcludedReason == "" {
			if row.Date == "" {
				undated++
			} else {
				titles++
			}
		}
	}
	respond(w, http.StatusOK, map[string]any{
		"status": "ready", "source": s.data.Source, "observed_at": s.data.ObservedAt,
		"records": len(s.data.Records), "dated_titles": titles, "from": s.data.From, "through": s.data.Through,
		"undated_records": undated,
		"pages":           s.data.Pages[:min(16, len(s.data.Pages))], "pages_total": len(s.data.Pages),
		"limits": s.data.Limits, "error": s.lastError, "source_set": s.data.SourceSet, "coverage": s.data.Coverage,
		"next_run_at": formatTime(s.nextRunAt), "collecting": s.collecting, "patent_metadata": s.metadataProgress,
	})
}

func respond(w http.ResponseWriter, status int, value any) {
	raw, err := json.Marshal(value)
	if err != nil {
		http.Error(w, "encode failed", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_, _ = w.Write(raw)
}

type period struct {
	Start string `json:"start"`
	End   string `json:"end_exclusive"`
}
type request struct {
	Terms   []string `json:"terms"`
	Periods []period `json:"periods"`
	// Titles: только заголовок — совпадение в тексте обычно мимолётное; фрагменты лишь с флагом, иначе раздуется снимок.
	Titles bool `json:"titles,omitzero"`
	// Excerpts: фрагменты при обычном отборе по тексту — из них живой поиск извлекает рыночных кандидатов.
	Excerpts bool `json:"excerpts,omitzero"`
}

// queryExcerptRunes хватает на пилот или раунд с условиями, а ответ на сотню записей остаётся в сотнях килобайт.
const queryExcerptRunes = 700

func queryExcerpt(row record, terms []string) string {
	sentences := textSentences(row.Text)
	start := max(0, slices.IndexFunc(sentences, func(sentence string) bool { return matches(sentence, terms) }))
	excerpt := []rune(strings.Join(sentences[start:], " "))
	return strings.TrimSpace(string(excerpt[:min(len(excerpt), queryExcerptRunes)]))
}

func queryText(row record, q request) string {
	if q.Titles {
		return row.Title
	}
	return row.Title + " " + row.Text
}

func queryPreview(row record, q request) record {
	preview := row
	if q.Titles || q.Excerpts {
		preview.Excerpt = queryExcerpt(row, q.Terms)
	}
	preview.Text = ""
	// Якоря и разметка патента живому поиску не нужны, а разметка тяжелее самой записи.
	preview.Anchors, preview.FullText = nil, nil
	return preview
}

func textSentences(text string) []string {
	sentences, start := []string{}, 0
	var previous rune
	for i, current := range text {
		if unicode.IsSpace(current) && strings.ContainsRune(".!?", previous) {
			sentences = append(sentences, strings.TrimSpace(text[start:i]))
			start = i
		}
		previous = current
	}
	if tail := strings.TrimSpace(text[start:]); tail != "" {
		sentences = append(sentences, tail)
	}
	return sentences
}

// queryRecords: пятнадцати технологиям нужно по три своих документа; без текста двести записей — сотни килобайт.
const queryRecords = 200

func (s *service) query(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 32<<10))
	var q request
	if err != nil || json.Unmarshal(body, &q) != nil || !validQuery(q) {
		http.Error(w, "invalid terms or calendar", http.StatusBadRequest)
		return
	}
	s.mu.RLock()
	data, lastError := s.data, s.lastError
	s.mu.RUnlock()
	if data == nil {
		http.Error(w, "collection not ready", http.StatusServiceUnavailable)
		return
	}
	// Исходный текст нужен только ML-источнику радара: остальным он лишь раздувает ответ.
	fullText := r.URL.Query().Get("full_text") == "1"
	counts := make([]*int, len(q.Periods))
	records := []record{}
	for _, at := range data.index.candidates(q.Terms, len(data.Records)) {
		row := data.Records[at]
		if row.Title == "" || row.ExcludedReason != "" || !matches(queryText(row, q), q.Terms) {
			continue
		}
		i := slices.IndexFunc(q.Periods, func(p period) bool { return row.Date >= p.Start && row.Date < p.End })
		if i < 0 {
			continue
		}
		if counts[i] == nil {
			counts[i] = new(0)
		}
		*counts[i]++
		preview := queryPreview(row, q)
		if fullText {
			preview.Text = row.Text
		}
		records = append(records, preview)
	}
	records = s.withBingNews(r.Context(), q, records, counts)
	// ponytail: индексы заголовков RSS и бюллетеней — ограниченная выборка: месяц без найденных записей
	// остаётся неизвестным, пока не доказан полный охват.
	limits := slices.Clone(data.Limits)
	if lastError != "" {
		limits = append(limits, "Latest refresh failed; showing the last successful collection")
	}
	respond(w, 200, struct {
		Source     string     `json:"source"`
		ObservedAt string     `json:"observed_at"`
		Counts     []*int     `json:"counts"`
		Total      int        `json:"total"`
		Partial    bool       `json:"partial"`
		Limits     []string   `json:"limits"`
		Records    []record   `json:"records"`
		SourceSet  string     `json:"source_set"`
		Coverage   []coverage `json:"coverage"`
	}{data.Source, data.ObservedAt, counts, len(records), true, limits,
		records[:min(queryRecords, len(records))], data.SourceSet, data.Coverage})
}

func validQuery(q request) bool {
	if len(q.Terms) == 0 || len(q.Terms) > 8 || len(q.Periods) == 0 || len(q.Periods) > 36 {
		return false
	}
	for _, t := range q.Terms {
		if len(t) > 200 || words(t) == "" {
			return false
		}
	}
	for i, p := range q.Periods {
		a, e := time.Parse(time.DateOnly, p.Start)
		b, f := time.Parse(time.DateOnly, p.End)
		if e != nil || f != nil || !b.After(a) || (i > 0 && q.Periods[i-1].End != p.Start) {
			return false
		}
	}
	return true
}

func words(value string) string {
	return strings.Join(tokens(value), " ")
}

func tokens(value string) []string {
	// Ленты пишут «ё» как придётся; свод здесь, потому что через этот разбор проходят и текст, и сам термин.
	folded := strings.Map(func(r rune) rune {
		if r == 'ё' {
			return 'е'
		}
		return r
	}, strings.ToLower(value))
	return strings.FieldsFunc(folded,
		func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsNumber(r) })
}

// stemMinBytes — три кириллические буквы: более короткая основа общая у половины языка.
const stemMinBytes = 6

// stem снимает одно окончание: лента пишет «твердотельных аккумуляторов», запрос — «твердотельные аккумуляторы».
// Отбрасывание окончания, а не общее начало, не сводит «сеть» и «сетчатка»; чередования в корне не ловятся.
func stem(token string) string {
	first, _ := utf8.DecodeRuneInString(token)
	if !unicode.Is(unicode.Cyrillic, first) {
		return token
	}
	// Окончания в три, две и одну букву: кириллица в UTF-8 — два байта на букву.
	for size := 6; size >= 2; size -= 2 {
		if len(token)-size >= stemMinBytes && russianEnding(token[len(token)-size:]) {
			return token[:len(token)-size]
		}
	}
	return token
}

// russianEnding — только именная и прилагательная парадигма: заголовок называет предмет существительным.
func russianEnding(suffix string) bool {
	switch suffix {
	case "ами", "ями", "ыми", "ими", "ого", "его", "ому", "ему":
		return true
	case "ов", "ев", "ей", "ой", "ый", "ий", "ая", "яя", "ое", "ее", "ые", "ие", "ых", "их", "ым", "им",
		"ую", "юю", "ам", "ям", "ах", "ях", "ом", "ем", "ью":
		return true
	case "а", "я", "ы", "и", "о", "е", "у", "ю", "ь", "й":
		return true
	}
	return false
}

// matches ищет термин непрерывной последовательностью слов по основам; «*» в конце — поиск по началу последнего слова.
func matches(text string, terms []string) bool {
	title := tokens(text)
	stems := make([]string, len(title))
	for i, word := range title {
		stems[i] = stem(word)
	}
	for _, term := range terms {
		want := tokens(term)
		if len(want) == 0 || len(want) > len(title) {
			continue
		}
		wantStems := make([]string, len(want))
		for i, word := range want {
			wantStems[i] = stem(word)
		}
		prefix := strings.HasSuffix(strings.TrimSpace(term), "*")
		for start := 0; start+len(want) <= len(title); start++ {
			if matchRun(title[start:], stems[start:], want, wantStems, prefix) {
				return true
			}
		}
	}
	return false
}

// stemIndex — пары «хеш основы в старших 32 битах, номер записи в младших» по возрастанию: восемь байт на пару
// вместо словаря строк, а совпадение хешей лишь добавляет кандидата, которого всё равно проверяет matches.
type stemIndex struct {
	seed  maphash.Seed
	pairs []uint64
}

func newStemIndex(rows []record) *stemIndex {
	index := &stemIndex{seed: maphash.MakeSeed()}
	var keys []uint64
	for at, row := range rows {
		if row.Title == "" || row.ExcludedReason != "" {
			continue
		}
		keys = keys[:0]
		for _, word := range tokens(row.Title + " " + row.Text) {
			keys = append(keys, index.key(word)|uint64(at))
		}
		slices.Sort(keys)
		index.pairs = append(index.pairs, slices.Compact(keys)...)
	}
	slices.Sort(index.pairs)
	return index
}

func (x *stemIndex) key(word string) uint64 {
	return maphash.String(x.seed, stem(word)) &^ math.MaxUint32
}

// candidates — номера записей по возрастанию, где есть все основы хотя бы одной фразы; последнее слово с «*»
// ищется по началу, а не по основе, поэтому фраза без других слов и снимок без индекса проверяют все записи.
func (x *stemIndex) candidates(terms []string, total int) []int {
	all := func() []int {
		every := make([]int, total)
		for i := range every {
			every[i] = i
		}
		return every
	}
	if x == nil {
		return all()
	}
	found := []int{}
	for _, term := range terms {
		want := tokens(term)
		if len(want) == 0 {
			continue
		}
		if strings.HasSuffix(strings.TrimSpace(term), "*") {
			if want = want[:len(want)-1]; len(want) == 0 {
				return all()
			}
		}
		keys := make([]uint64, len(want))
		for i, word := range want {
			keys[i] = x.key(word)
		}
		rarest := slices.MinFunc(keys, func(a, b uint64) int {
			return cmp.Compare(len(x.postings(a)), len(x.postings(b)))
		})
		for _, pair := range x.postings(rarest) {
			at := pair & math.MaxUint32
			if !slices.ContainsFunc(keys, func(key uint64) bool {
				_, ok := slices.BinarySearch(x.pairs, key|at)
				return !ok
			}) {
				found = append(found, int(at))
			}
		}
	}
	slices.Sort(found)
	return slices.Compact(found)
}

func (x *stemIndex) postings(key uint64) []uint64 {
	start, _ := slices.BinarySearch(x.pairs, key)
	end, _ := slices.BinarySearch(x.pairs[start:], key|math.MaxUint32)
	return x.pairs[start : start+end]
}

func matchRun(title, stems, want, wantStems []string, prefix bool) bool {
	for i := range want {
		if prefix && i == len(want)-1 {
			return strings.HasPrefix(title[i], want[i])
		}
		if stems[i] != wantStems[i] {
			return false
		}
	}
	return true
}

// retained: правка набора лент не должна стирать накопленную историю.
func (s *service) retained(row record) bool {
	if s.kind != kindWeb {
		return true
	}
	return slices.ContainsFunc(s.config.Web, func(source webSource) bool { return source.Name == row.Source })
}

func mergeRetained(data *snapshot, old *snapshot, now time.Time, keep func(record) bool) {
	previousByID := map[string]record{}
	if old != nil {
		for _, r := range old.Records {
			previousByID[r.ID] = r
		}
	}
	rows := data.Records
	data.Records = []record{}
	if old != nil {
		same := old.SourceSet == data.SourceSet || old.SourceSet == ""
		for _, row := range old.Records {
			if same || keep(row) {
				rows = append(rows, row)
			}
		}
	}
	seen := map[string]bool{}
	for _, row := range rows {
		if !inSnapshotWindow(row, data, now) || publicationDuplicate(row, seen) {
			continue
		}
		if previous, ok := previousByID[row.ID]; ok && previous.AvailableAt != "" {
			row.AvailableAt = previous.AvailableAt
		}
		row.ObservedAt = cmp.Or(row.ObservedAt, data.ObservedAt)
		row.AvailableAt = cmp.Or(row.AvailableAt, row.ObservedAt)
		data.Records = append(data.Records, row)
	}
	if old != nil {
		data.Pages = mergePages(data.Pages, old.Pages)
	}
}

func inSnapshotWindow(row record, data *snapshot, now time.Time) bool {
	if published, err := time.Parse(time.RFC3339, row.PublishedAt); err == nil && published.After(now) {
		return false
	}
	// Запись без даты устаревает по дате наблюдения, но та не становится датой публикации и не идёт в счёт трендов.
	day := cmp.Or(row.Date, sourceDay(row.ObservedAt))
	return day == "" || day >= data.From && day <= data.Through
}

func mergePages(current, retained []provenance) []provenance {
	seen := map[provenance]bool{}
	for _, page := range current {
		seen[page] = true
	}
	for _, page := range retained {
		if !seen[page] {
			current = append(current, page)
			seen[page] = true
		}
	}
	return current
}

// patentScopes пишет сбой в строку своей юрисдикции: недоступный US не должен делать EP пустым.
func (s *service) patentScopes(ctx context.Context, data *snapshot) error {
	usErr := s.patentHistory(ctx, data)
	if usErr != nil {
		data.Coverage = append(data.Coverage, coverage{Source: patentSource, From: data.From,
			Through: data.Through, ObservedAt: data.ObservedAt, StopReason: usErr.Error()})
		data.Limits = append(data.Limits,
			"The US patent scope was not collected in this pass: "+usErr.Error()+
				". Retained US observations stand; they are not re-verified by this pass.")
	}
	s.epHistory(ctx, data)
	s.opsHistory(ctx, data)
	s.rospatentHistory(ctx, data)
	if len(data.Records) == 0 && len(s.previousRecords()) == 0 && usErr != nil {
		return usErr
	}
	return nil
}

func (s *service) collect(ctx context.Context, now time.Time) error {
	unlock, err := s.lockCollection()
	if err != nil {
		return err
	}
	defer unlock()
	s.requests, s.renders = 0, 0
	s.robots = nil
	s.renderQueue = nil
	if s.config.HistoryMonths == 0 {
		s.config = defaultConfig()
	}
	month := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC)
	data := snapshot{Version: 3, SourceSet: s.config.fingerprint(s.kind), ObservedAt: now.Format(time.RFC3339),
		From: month.AddDate(0, -s.config.HistoryMonths, 0).Format(time.DateOnly), Through: now.Format(time.DateOnly),
		Records: []record{}, Pages: []provenance{}, Limits: []string{}}
	switch s.kind {
	case kindCNPatents:
		data.Source = cnSource
		s.cnHistory(ctx, &data)
	case kindPatents:
		data.Source = s.patentSourceLabel()
		err = s.patentScopes(ctx, &data)
	default:
		names := make([]string, 0, len(s.config.Web))
		for _, source := range s.config.Web {
			names = append(names, source.Name)
		}
		data.Source = strings.Join(names, " · ")
		err = s.webHistory(ctx, &data)
	}
	if err != nil {
		return err
	}
	// Веб-проход, не успевший к сроку, публикует прочитанное: остальное помечено в охвате для дочитывания.
	if err = ctx.Err(); err != nil && s.kind != kindWeb {
		return fmt.Errorf("collection cancelled: %w", err)
	}
	s.mu.RLock()
	old := s.data
	s.mu.RUnlock()
	mergeRetained(&data, old, now, s.retained)
	if s.kind == kindPatents {
		if err = s.enrichPatentMetadata(ctx, &data); err != nil {
			return err
		}
	}
	linkDuplicates(data.Records)
	summarizeCoverage(&data)
	if err = s.publish(&data); err != nil {
		return err
	}
	s.mu.Lock()
	s.lastError = ""
	s.mu.Unlock()
	slog.Info("collection complete", "source", s.kind, "records", len(data.Records), "pages", len(data.Pages), "through",
		data.Through)
	return nil
}

func (s *service) patentSourceLabel() string {
	label := "USPTO Public Search · Official Gazette + EPO European Publication Server"
	if key, secret := opsCredentials(); s.config.OPSDocuments > 0 && key != "" && secret != "" {
		label += " + EPO OPS (DOCDB)"
	}
	if s.config.RUDocuments > 0 && rospatentKey() != "" {
		label += " + Rospatent"
	}
	return label
}
