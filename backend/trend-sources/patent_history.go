package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strings"
	"time"

	"golang.org/x/net/html"
)

const primaryPatentParserVersion = "gazette-primary-v2"

var primaryPatentNumber = regexp.MustCompile(`^(US(?:RE|D|PP)?[0-9]+)([A-Z][0-9]|S)$`)

var filingLine = regexp.MustCompile(`Filed on ([A-Za-z]+\.?\s+[0-9]{1,2},\s+[0-9]{4}), as Appl\. No\. ([0-9/,]+)`)
var patentDate = regexp.MustCompile(`[A-Za-z]+\.?\s+[0-9]{1,2},\s+[0-9]{4}`)
var applicationNumber = regexp.MustCompile(`Appl\. No\. ([0-9/,]+)`)
var priorityClaim = regexp.MustCompile(
	`application No\. ([0-9A-Z/.,-]+) \(([A-Z]{2})\), filed on ([A-Za-z.]+ [0-9]{1,2}, [0-9]{4})`,
)
var pctFilingLine = regexp.MustCompile(`PCT Filed ([A-Za-z.]+ [0-9]{1,2}, [0-9]{4}), PCT No\. ([A-Z0-9/]+)`)
var nationalStageLine = regexp.MustCompile(`371\(c\)\(1\), \(2\) Date ([A-Za-z.]+ [0-9]{1,2}, [0-9]{4})`)

type patentFields struct {
	title, claim, filing, application, priority string
	priorities, priorityIDs                     []string
	numberFound                                 bool
}

func (f *patentFields) cell(text string, row *record) {
	if match := applicationNumber.FindStringSubmatch(text); match != nil {
		f.application = "US" + strings.NewReplacer("/", "", ",", "").Replace(match[1])
	}
	if match := pctFilingLine.FindStringSubmatch(text); match != nil {
		row.PCTFiling = parsePatentDay(match[1])
		row.PCTApplication = match[2]
	}
	if match := nationalStageLine.FindStringSubmatch(text); match != nil {
		row.NationalStage = parsePatentDay(match[1])
	}
	if match := filingLine.FindStringSubmatch(text); match != nil {
		f.filing = parsePatentDay(match[1])
		f.application = "US" + strings.NewReplacer("/", "", ",", "").Replace(match[2])
	}
	if !strings.Contains(text, "Claims priority of") && !strings.Contains(text, "Claims priority from") {
		return
	}
	for _, value := range patentDate.FindAllString(text, -1) {
		if d := parsePatentDay(value); d != "" && (f.priority == "" || d < f.priority) {
			f.priority = d
		}
	}
	// Приоритет первичного документа сохраняется целиком: одной общей даты мало для слияния в семейство.
	f.priorities = append(f.priorities, text)
	for _, claim := range priorityClaim.FindAllStringSubmatch(text, -1) {
		f.priorityIDs = append(f.priorityIDs, claim[2]+strings.NewReplacer("/", "", "-", "", ",", "").Replace(claim[1]))
	}
}

func (f *patentFields) node(n *html.Node, row *record) {
	if n.Type != html.ElementNode {
		return
	}
	text := strings.Join(strings.Fields(content(n)), " ")
	switch {
	case n.Data == "td" && strings.Contains(strings.ToLower(attr(n, "style")), "text-transform: uppercase"):
		f.title = text
	case n.Data == "b" && strings.HasPrefix(text, "US "):
		normalized := strings.NewReplacer(" ", "", ",", "").Replace(text)
		if match := primaryPatentNumber.FindStringSubmatch(normalized); match != nil && match[1] == row.ID &&
			(match[2] != "S" || strings.HasPrefix(row.ID, "USD")) {
			f.numberFound = true
		}
	case n.Data == elementDiv && attr(n, "class") == "claim_text_root":
		f.claim = text
	case n.Data == "td":
		f.cell(text, row)
	}
}

func (f *patentFields) apply(row record, observed string) record {
	row.Title = f.title
	row.TitleBasis = "USPTO primary document title"
	row.Text = f.claim
	row.TextStatus = metadataOnlyText
	if f.claim != "" {
		row.TextStatus = "representative_claim"
		row.ContentHash = hashBytes([]byte(f.claim))
	}
	row.ParserVersion = primaryPatentParserVersion
	row.FilingBasis = "USPTO US application filing"
	if f.filing == "" && row.PCTFiling != "" {
		f.filing = row.PCTFiling
		row.FilingBasis = "USPTO PCT filing for a national-stage US application"
	}
	row.Filing, row.Priority, row.Application = f.filing, f.priority, f.application
	if f.priority != "" {
		row.PriorityBasis = "USPTO primary document priority claim"
	}
	row.PriorityClaims = f.priorities
	row.PriorityIDs = f.priorityIDs
	row.ObservedAt, row.AvailableAt = observed, observed
	row.FamilyID, row.FamilyBasis = "", "unknown"
	switch {
	case len(f.priorityIDs) > 0:
		slices.Sort(f.priorityIDs)
		row.PriorityIDs = slices.Compact(f.priorityIDs)
		row.FamilyID = "us-priority-set:" + hashBytes([]byte(strings.Join(row.PriorityIDs, "\n")))
		row.FamilyBasis = "identical explicitly cited priority claims; not a worldwide family"
	case f.application != "":
		row.FamilyID = "application:" + f.application
		row.FamilyBasis = "same US application only; wider family unknown"
	}
	return row
}

func patentDocument(body []byte, row record, observed string) (record, error) {
	doc, err := html.Parse(bytes.NewReader(body))
	if err != nil {
		return row, fmt.Errorf("parse patent document: %w", err)
	}
	var fields patentFields
	visit(doc, func(n *html.Node) { fields.node(n, &row) })
	if !fields.numberFound || fields.title == "" || len(fields.title) > 2000 {
		return row, errors.New("primary patent ID/title could not be verified")
	}
	return fields.apply(row, observed), nil
}

func parsePatentDay(value string) string {
	value = strings.Join(strings.Fields(strings.ReplaceAll(value, ".", "")), " ")
	for _, format := range []string{"Jan 2, 2006", "January 2, 2006"} {
		if d, e := time.Parse(format, value); e == nil {
			return d.Format(time.DateOnly)
		}
	}
	return ""
}
func candidatePatent(row record, terms []string) bool {
	if len(terms) == 0 {
		return true
	}
	text := words(row.Title + " " + row.Citation)
	// Отбор бывает шире нужного: в счёт идут только первичные название и формула, не ссылка на изобретателя.
	for _, term := range terms {
		if strings.Contains(text, words(term)) {
			return true
		}
	}
	return false
}
func (s *service) previousRecords() map[string]record {
	old := map[string]record{}
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.data == nil {
		return old
	}
	for _, r := range s.data.Records {
		old[r.ID] = r
	}
	return old
}

// patentWindows объявляет месяцы охвата до чтения выпусков: непредложенный архивом месяц — явный пробел.
func patentWindows(data *snapshot, available []issue) {
	for _, window := range monthWindows(data.From, data.Through) {
		day, _ := time.Parse(time.DateOnly, window.End)
		c := coverage{
			Source: patentSource, From: window.Start, Through: day.AddDate(0, 0, -1).Format(time.DateOnly),
			ObservedAt: data.ObservedAt, StopReason: "outside_available_52_week_Gazette_ID_scope", Exhausted: false,
		}
		if window.Start >= available[len(available)-1].Date[:7]+"-01" {
			c.StopReason = textNotCollected
		}
		data.Coverage = append(data.Coverage, c)
	}
}

// coverageFor сверяет и источник: причина остановки USPTO в строке другой юрисдикции описала бы чужой обход.
func coverageFor(data *snapshot, date string) int {
	return slices.IndexFunc(data.Coverage, func(c coverage) bool {
		return c.Source == patentSource && date >= c.From && date <= c.Through
	})
}

// enrichPatent: сбой сужает охват, но не саму строку.
func (s *service) enrichPatent(ctx context.Context, row record, issueDate string, data *snapshot, c *coverage) record {
	if !candidatePatent(row, s.config.PatentTerms) {
		return row
	}
	raw, err := s.cached(ctx, row.URL, issueDate, data)
	if err != nil {
		c.Exhausted = false
		c.StopReason = err.Error()
		c.Cursor = row.ID
		return row
	}
	enriched, parseErr := patentDocument(raw, row, data.ObservedAt)
	if parseErr != nil {
		c.Exhausted = false
		c.StopReason = "unverified primary document"
		return row
	}
	enriched.Provenance = []provenance{{URL: row.URL, SHA256: hashBytes(raw)}}
	return enriched
}

type patentPass struct {
	listed, currentIssue issue
	cover                provenance
	c                    *coverage
	old                  map[string]record
}

// patentLetter возвращает false, когда кончился бюджет или контекст: вызывающий останавливается, а не повторяет Z.
func (s *service) patentLetter(ctx context.Context, data *snapshot, pass patentPass, letter rune) bool {
	c, currentIssue, listed, old := pass.c, pass.currentIssue, pass.listed, pass.old
	address := fmt.Sprintf("%s/OG/patentee/alpha%c.html", currentIssue.URL, letter)
	body, err := s.cached(ctx, address, listed.Date, data)
	if err != nil {
		c.Exhausted = false
		c.Cursor = currentIssue.Date + ":" + string(letter)
		c.StopReason = err.Error()
		return !errors.Is(err, errBudget) && ctx.Err() == nil
	}
	c.Pages = append(c.Pages, provenance{URL: address, SHA256: hashBytes(body)})
	rows, err := patentRows(body, currentIssue)
	if err != nil {
		c.Exhausted = false
		c.StopReason = err.Error()
		return true
	}
	for _, row := range rows {
		if prev, ok := old[row.ID]; ok && (primaryVerified(prev) || metadataVerified(prev)) {
			data.Records = append(data.Records, prev)
			continue
		}
		row = s.enrichPatent(ctx, row, currentIssue.Date, data, c)
		if row.TitleBasis == "unambiguous patentee index" {
			row.Provenance = []provenance{{URL: address, SHA256: hashBytes(body)}}
		}
		row.Provenance = append(row.Provenance, pass.cover)
		if listed.Date != currentIssue.Date {
			row.DateBasis = "USPTO issue cover " + currentIssue.Date + "; directory label " + listed.Date
		}
		data.Records = append(data.Records, row)
	}
	return true
}

func (s *service) patentIssue(ctx context.Context, data *snapshot, listed issue, old map[string]record) {
	if coverageFor(data, listed.Date) < 0 {
		return
	}
	currentIssue, cover, coverErr := s.resolvePatentIssue(ctx, listed, data)
	if coverErr != nil {
		c := &data.Coverage[coverageFor(data, listed.Date)]
		c.Exhausted = false
		c.StopReason = coverErr.Error()
		c.Cursor = listed.Date + ":cover"
		return
	}
	if data.PatentIndexFrom == "" || currentIssue.Date < data.PatentIndexFrom {
		data.PatentIndexFrom = currentIssue.Date
	}
	ci := coverageFor(data, currentIssue.Date)
	if ci < 0 {
		return
	}
	c := &data.Coverage[ci]
	c.Pages = append(c.Pages, cover)
	if c.StopReason == textNotCollected || c.StopReason == "outside_available_52_week_Gazette_ID_scope" {
		c.StopReason = "indices_complete; selected primary documents verified"
		c.Exhausted = true
	}
	pass := patentPass{listed: listed, currentIssue: currentIssue, cover: cover, c: c, old: old}
	for letter := 'A'; letter <= 'Z'; letter++ {
		if !s.patentLetter(ctx, data, pass, letter) {
			break
		}
	}
}

func (s *service) patentHistory(ctx context.Context, data *snapshot) error {
	root, err := s.fetch(ctx, gazette+"/", data)
	if err != nil {
		return err
	}
	available, err := issues(root, data.From, data.Through)
	if err != nil {
		return err
	}
	slices.SortFunc(available, func(a, b issue) int { return strings.Compare(b.Date, a.Date) })
	old := s.previousRecords()
	patentWindows(data, available)
	for _, listed := range available {
		s.patentIssue(ctx, data, listed, old)
	}
	if len(data.Records) == 0 && len(old) == 0 {
		return errors.New("no patent records collected")
	}
	data.Limits = append(data.Limits,
		"US utility, design, plant and reissue grants in the available 52-week USPTO Gazette ID archive; "+
			"applications, legal status and worldwide families are outside this collection. "+
			"Earlier dates within the configured history are covered separately by dated USPTO Public Search sweeps.",
		"Public Search verifies grant titles and dates for the indexed IDs; primary documents retain available filing, "+
			"priority and representative claims for configured candidates. Unverified metadata and later certificates "+
			"are excluded from grant counts. Missing priority dates are never replaced with filing or publication dates.",
		"Family deduplication uses explicit USPTO Public Search Family IDs when supplied, otherwise verified priority "+
			"claims or the same US application. Application identities do not establish wider family relationships.")
	return nil
}
