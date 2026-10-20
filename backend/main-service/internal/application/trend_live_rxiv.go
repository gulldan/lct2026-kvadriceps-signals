package application

import (
	"context"
	"crypto/sha256"
	"encoding/json/v2"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	liveServerBioRxiv  = "biorxiv"
	liveServerMedRxiv  = "medrxiv"
	liveServerChemRxiv = "chemrxiv"
	liveRxivBase       = "https://api.biorxiv.org"
	liveRxivWindowKey  = "trends/live/rxiv-window-v1.json.gz"
	// Сервер отдаёт по 30 записей: первый сбор 14 месяцев — около четырёх тысяч страниц, в один поток это час.
	liveRxivWorkers  = 4
	liveRxivAttempts = 4
	liveRxivBackoff  = 15 * time.Second
)

// liveRxivServers — препринты биологии и медицины: в arXiv их почти нет, и без них радар видел медицину только
// через ИИ.
func liveRxivServers() []string {
	return []string{liveServerBioRxiv, liveServerMedRxiv}
}

type rxivEntry struct {
	DOI      string `json:"doi"`
	Title    string `json:"title"`
	Authors  string `json:"authors"`
	Date     string `json:"date"`
	Version  string `json:"version"`
	Category string `json:"category"`
	Abstract string `json:"abstract"`
	Server   string `json:"-"`
}

type rxivPage struct {
	Messages []struct {
		Total string `json:"total"`
	} `json:"messages"`
	Collection []rxivEntry `json:"collection"`
}

func (s *liveSources) rxivPage(ctx context.Context, server, from, until string, cursor int) (rxivPage, error) {
	endpoint := fmt.Sprintf("%s/details/%s/%s/%s/%d/json", s.rxiv, server, from, until, cursor)
	var last error
	for attempt := range liveRxivAttempts {
		if attempt > 0 {
			if err := s.sleep(ctx, time.Duration(attempt)*liveRxivBackoff); err != nil {
				return rxivPage{}, err
			}
		}
		body, _, status, err := s.get(ctx, endpoint, nil)
		switch {
		case err != nil:
			last = err
		case status != http.StatusOK:
			last = fmt.Errorf("status %d", status)
		default:
			var page rxivPage
			if err := json.Unmarshal(body, &page); err != nil {
				return page, fmt.Errorf("%s: parse: %w", server, err)
			}
			return page, nil
		}
	}
	return rxivPage{}, fmt.Errorf("%s: %w", server, last)
}

func (s *liveSources) rxivInterval(ctx context.Context, server string, from, until time.Time) ([]rxivEntry, error) {
	entries := []rxivEntry{}
	for cursor := 0; ; {
		page, err := s.rxivPage(ctx, server, from.Format(time.DateOnly), until.Format(time.DateOnly), cursor)
		if err != nil {
			return nil, err
		}
		for _, entry := range page.Collection {
			entry.Server = server
			entries = append(entries, entry)
		}
		cursor += len(page.Collection)
		total := 0
		if len(page.Messages) > 0 {
			total, _ = strconv.Atoi(page.Messages[0].Total)
		}
		if len(page.Collection) == 0 || cursor >= total {
			return entries, nil
		}
	}
}

// rxivHarvest берёт только первые версии: дата первой версии — появление работы, а ревизия старой работы не
// новость этого месяца.
func (s *liveSources) rxivHarvest(ctx context.Context, periods []LivePeriod, from, until time.Time,
) ([]StudyPaper, error) {
	type job struct {
		server   string
		from, to time.Time
	}
	jobs := []job{}
	for _, server := range liveRxivServers() {
		for start := from; !start.After(until); start = start.AddDate(0, 1, 0) {
			to := start.AddDate(0, 1, -1)
			if to.After(until) {
				to = until
			}
			jobs = append(jobs, job{server: server, from: start, to: to})
		}
	}
	results := make([][]rxivEntry, len(jobs))
	errs := make([]error, len(jobs))
	next := make(chan int)
	var wg sync.WaitGroup
	for range liveRxivWorkers {
		wg.Go(func() {
			for i := range next {
				results[i], errs[i] = s.rxivInterval(ctx, jobs[i].server, jobs[i].from, jobs[i].to)
			}
		})
	}
	for i := range jobs {
		next <- i
	}
	close(next)
	wg.Wait()
	if err := errors.Join(errs...); err != nil {
		return nil, fmt.Errorf("preprints: %w", err)
	}
	papers := []StudyPaper{}
	seen := map[string]bool{}
	for _, entries := range results {
		for _, entry := range entries {
			doi := strings.ToLower(strings.TrimSpace(entry.DOI))
			id := entry.Server + ":" + doi
			if entry.Version != "1" || doi == "" || seen[id] || livePeriodIndex(periods, entry.Date) < 0 {
				continue
			}
			seen[id] = true
			papers = append(papers, s.recordRxiv(id, doi, entry))
		}
	}
	return papers, nil
}

func (s *liveSources) recordRxiv(id, doi string, entry rxivEntry) StudyPaper {
	if s.metadata == nil {
		s.metadata = map[string]LivePaperMetadata{}
	}
	title := strings.Join(strings.Fields(entry.Title), " ")
	abstract := strings.Join(strings.Fields(entry.Abstract), " ")
	authors := []string{}
	for name := range strings.SplitSeq(entry.Authors, ";") {
		if name = strings.TrimSpace(name); name != "" {
			authors = append(authors, name)
		}
	}
	address := "https://doi.org/" + doi
	s.metadata[id] = LivePaperMetadata{
		Abstract: abstract, DOI: doi,
		Category:    entry.Server + "." + strings.Join(strings.Fields(strings.ToLower(entry.Category)), "-"),
		AvailableAt: entry.Date, ObservedAt: time.Now().UTC().Format(time.RFC3339), SourceURL: address,
		ContentHash: fmt.Sprintf("%x", sha256.Sum256([]byte(title+"\n"+abstract+"\n"+entry.Date))),
	}
	return StudyPaper{ID: id, Title: title, Date: entry.Date, CanonicalID: id, ContentHash: "", URL: address,
		Authors: authors, DocumentID: "", SnapshotID: ""}
}

const (
	liveCrossrefBase      = "https://api.crossref.org"
	liveCrossrefWindowKey = "trends/live/crossref-window-v1.json.gz"
	liveCrossrefRows      = 1000
)

// liveCrossrefServer — препринт-сервер, чьи работы Crossref отдаёт по префиксу DOI: у ChemRxiv свой API закрыт
// проверкой Cloudflare, а у OSF-серверов его нет вовсе.
type liveCrossrefServer struct {
	name, prefix, title string
}

// liveCrossrefServers закрывают области, которых в arXiv почти нет: химию, науки о Земле, психологию, общество,
// образование и инженерию. Research Square не взят: 6–7 тысяч работ в месяц раздули бы окно, а медицину уже даёт
// medRxiv.
func liveCrossrefServers() []liveCrossrefServer {
	return []liveCrossrefServer{{liveServerChemRxiv, "10.26434", "ChemRxiv"}, {"eartharxiv", "10.31223", "EarthArXiv"},
		{"essoar", "10.22541", "ESS Open Archive"}, {"psyarxiv", "10.31234", "PsyArXiv"},
		{"socarxiv", "10.31235", "SocArXiv"}, {"edarxiv", "10.35542", "EdArXiv"}, {"engrxiv", "10.31224", "engrXiv"},
		{"preprints", "10.20944", "Preprints.org"}, {"osf", "10.31219", "OSF Preprints"}}
}

// livePreprintServerName — название сервера препринтов для паспорта работы; пусто — источник не из окна препринтов.
func livePreprintServerName(source string) string {
	switch source {
	case liveServerBioRxiv:
		return "bioRxiv"
	case liveServerMedRxiv:
		return "medRxiv"
	}
	for _, server := range liveCrossrefServers() {
		if server.name == source {
			return server.title
		}
	}
	return ""
}

// livePreprintDOI узнаёт серверы препринтов по префиксу DOI; bioRxiv и medRxiv с 2026 года выдают и 10.64898.
func livePreprintDOI(rawURL string) bool {
	path := strings.TrimPrefix(strings.ToLower(rawURL), "https://doi.org/")
	path = strings.TrimPrefix(path, "http://doi.org/")
	prefix, _, _ := strings.Cut(path, "/")
	if prefix == "10.1101" || prefix == "10.64898" {
		return true
	}
	for _, server := range liveCrossrefServers() {
		if prefix == server.prefix {
			return true
		}
	}
	return false
}

func liveCrossrefSets() []string {
	servers := liveCrossrefServers()
	sets := make([]string, 0, len(servers))
	for _, server := range servers {
		sets = append(sets, server.name)
	}
	return sets
}

type crossrefWork struct {
	DOI        string   `json:"DOI"`
	Title      []string `json:"title"`
	Abstract   string   `json:"abstract"`
	GroupTitle string   `json:"group-title"`
	Author     []struct {
		Given  string `json:"given"`
		Family string `json:"family"`
	} `json:"author"`
	Posted struct {
		DateParts [][]int `json:"date-parts"`
	} `json:"posted"`
}

type crossrefWorks struct {
	Message struct {
		NextCursor string         `json:"next-cursor"`
		Total      int            `json:"total-results"`
		Items      []crossrefWork `json:"items"`
	} `json:"message"`
}

var (
	liveJATSTag         = regexp.MustCompile(`<[^>]+>`)
	livePreprintVersion = regexp.MustCompile(`(?i)[_./-]v([0-9]+)$`)
)

func (s *liveSources) crossrefPage(ctx context.Context, server liveCrossrefServer, from, until, cursor string,
) (crossrefWorks, error) {
	values := url.Values{oaFilter: {"type:posted-content,from-posted-date:" + from + ",until-posted-date:" + until},
		"rows": {strconv.Itoa(liveCrossrefRows)}, "cursor": {cursor},
		oaSelect: {"DOI,title,abstract,author,posted,group-title"}}
	endpoint := s.crossref + "/prefixes/" + server.prefix + "/works?" + values.Encode()
	var last error
	for attempt := range liveRxivAttempts {
		if attempt > 0 {
			if err := s.sleep(ctx, time.Duration(attempt)*liveRxivBackoff); err != nil {
				return crossrefWorks{}, err
			}
		}
		body, _, status, err := s.get(ctx, endpoint, nil)
		switch {
		case err != nil:
			last = err
		case status != http.StatusOK:
			last = fmt.Errorf("status %d", status)
		default:
			var page crossrefWorks
			if err := json.Unmarshal(body, &page); err != nil {
				return page, fmt.Errorf("%s: parse: %w", server.name, err)
			}
			return page, nil
		}
	}
	return crossrefWorks{}, fmt.Errorf("%s: %w", server.name, last)
}

// crossrefHarvest идёт по месяцам в один поток: общий пул Crossref без ключа терпит одно соединение.
func (s *liveSources) crossrefHarvest(ctx context.Context, periods []LivePeriod, from, until time.Time,
) ([]StudyPaper, error) {
	papers := []StudyPaper{}
	seen := map[string]bool{}
	for _, server := range liveCrossrefServers() {
		for start := from; !start.After(until); start = start.AddDate(0, 1, 0) {
			to := start.AddDate(0, 1, -1)
			if to.After(until) {
				to = until
			}
			works, err := s.crossrefMonth(ctx, server, start, to)
			if err != nil {
				return nil, fmt.Errorf("preprints: %w", err)
			}
			for _, work := range works {
				if paper, ok := s.recordCrossref(server.name, work, periods); ok && !seen[paper.ID] {
					seen[paper.ID] = true
					papers = append(papers, paper)
				}
			}
		}
	}
	return papers, nil
}

func (s *liveSources) crossrefMonth(ctx context.Context, server liveCrossrefServer, from, until time.Time,
) ([]crossrefWork, error) {
	works := []crossrefWork{}
	for cursor := "*"; ; {
		page, err := s.crossrefPage(ctx, server, from.Format(time.DateOnly), until.Format(time.DateOnly), cursor)
		if err != nil {
			return nil, err
		}
		works = append(works, page.Message.Items...)
		if len(page.Message.Items) == 0 || len(works) >= page.Message.Total || page.Message.NextCursor == "" {
			return works, nil
		}
		cursor = page.Message.NextCursor
	}
}

// recordCrossref берёт только первую версию: у этих серверов ревизия получает свой DOI с суффиксом версии.
func (s *liveSources) recordCrossref(server string, work crossrefWork, periods []LivePeriod) (StudyPaper, bool) {
	doi := strings.ToLower(strings.TrimSpace(work.DOI))
	if match := livePreprintVersion.FindStringSubmatch(doi); doi == "" || len(work.Title) == 0 ||
		len(work.Posted.DateParts) == 0 || len(work.Posted.DateParts[0]) < 2 || match != nil && match[1] != "1" {
		return StudyPaper{}, false
	}
	parts, day := work.Posted.DateParts[0], 1
	if len(parts) > 2 {
		day = max(parts[2], 1)
	}
	date := fmt.Sprintf("%04d-%02d-%02d", parts[0], parts[1], day)
	if livePeriodIndex(periods, date) < 0 {
		return StudyPaper{}, false
	}
	authors := []string{}
	for _, author := range work.Author {
		if name := strings.TrimSpace(author.Given + " " + author.Family); name != "" {
			authors = append(authors, name)
		}
	}
	abstract := strings.Join(strings.Fields(liveJATSTag.ReplaceAllString(work.Abstract, " ")), " ")
	return s.recordRxiv(server+":"+doi, doi, rxivEntry{Title: work.Title[0], Date: date, Category: work.GroupTitle,
		Abstract: abstract, Server: server, Authors: strings.Join(authors, ";")}), true
}
