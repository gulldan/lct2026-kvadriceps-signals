package application

import (
	"cmp"
	"context"
	"encoding/json/v2"
	"net/url"
	"os"
	"regexp"
	"slices"
	"strings"
	"sync"
	"time"
	"unicode"

	"github.com/example/main-service/internal/platform/openalex"
)

type LivePriorArt struct {
	Status       string          `json:"status"`
	Query        string          `json:"query,omitempty"`
	Before       string          `json:"before,omitempty"`
	Note         string          `json:"note"`
	SourceURL    string          `json:"source_url,omitempty"`
	SourceSHA256 string          `json:"source_sha256,omitempty"`
	ObservedAt   time.Time       `json:"observed_at,omitzero"`
	Total        *int            `json:"total,omitempty"`
	Works        []LivePriorWork `json:"works,omitempty"`
}

type LivePriorWork struct {
	ID       string `json:"id"`
	Title    string `json:"title"`
	Date     string `json:"date"`
	DOI      string `json:"doi,omitempty"`
	URL      string `json:"url"`
	Abstract string `json:"abstract,omitempty"`
}

type livePriorBudget struct {
	mu        sync.Mutex
	remaining int
	slots     chan struct{}
}

type livePriorKey struct{}

const (
	livePriorUnavailable = "unavailable"
	livePriorSort        = "sort"
	livePriorPage        = "per_page"
)

var livePriorWorkID = regexp.MustCompile(`^https://openalex.org/W[1-9][0-9]+$`)

func liveWithPriorArt(ctx context.Context) context.Context {
	if _, ok := ctx.Value(livePriorKey{}).(*livePriorBudget); ok {
		return ctx
	}
	return context.WithValue(ctx, livePriorKey{}, &livePriorBudget{remaining: liveTopCandidates, slots: make(chan struct{}, 2)})
}

// Только окончательная выдача расходует запросы: отброшенные кандидаты не лишают её проверки истории.
func (s *TrendLiveService) finalPriorArt(ctx context.Context, candidates []LiveTopic, documents []StudyPaper) {
	if os.Getenv("RADAR_PRIOR_ART") == livePaperDisabled {
		return
	}
	ctx = liveWithPriorArt(ctx)
	papers := make(map[string]StudyPaper, len(documents))
	for _, paper := range documents {
		papers[paper.ID] = paper
	}
	count := min(len(candidates), liveTopCandidates)
	for i := range count {
		if candidates[i].Card == nil {
			candidates[i].Card = &LiveCard{}
		}
		candidates[i].Card.PriorArt = &LivePriorArt{Status: livePriorUnavailable,
			Note: "Поиск ранних аналогов не выполнен: общий срок завершился или поиск отменён."}
	}
	s.eachConcurrently(ctx, count, func(i int) {
		candidates[i].Card.PriorArt = s.priorArt(ctx, candidates[i], papers)
	})
}

// Раннее совпадение — материал для сравнения механизма, а не автоматическое доказательство зрелого рынка.
func (s *TrendLiveService) priorArt(ctx context.Context, candidate LiveTopic, papers map[string]StudyPaper) *LivePriorArt {
	if os.Getenv("RADAR_PRIOR_ART") == livePaperDisabled {
		return nil
	}
	result := &LivePriorArt{Status: livePriorUnavailable, Note: "Поиск ранних аналогов не выполнен."}
	budget, _ := ctx.Value(livePriorKey{}).(*livePriorBudget)
	if budget == nil || s.oa == nil || !s.oa.Configured() {
		return result
	}
	phrase := candidate.Name
	if candidate.Qualification != nil {
		phrase = cmp.Or(candidate.Qualification.CanonicalName, phrase)
	}
	phrase = strings.Join(strings.Fields(phrase), " ")
	if strings.ContainsAny(phrase, `,"|`) || len(strings.Fields(phrase)) < 2 ||
		!strings.ContainsFunc(phrase, func(r rune) bool { return unicode.Is(unicode.Latin, r) }) {
		result.Note = "Для поиска ранних аналогов не найдено точное исходное название технологии."
		return result
	}
	before := ""
	for _, id := range slices.Concat(candidate.PaperIDs, candidate.EvidenceIDs) {
		date := papers[id].Date
		if _, err := time.Parse(time.DateOnly, date); err == nil && (before == "" || date < before) {
			before = date
		}
	}
	if before == "" {
		result.Note = "Дата исходных работ неизвестна: границу поиска ранних аналогов установить нельзя."
		return result
	}
	result.Query, result.Before = phrase, before
	budget.mu.Lock()
	available := budget.remaining > 0
	if available {
		budget.remaining--
	}
	budget.mu.Unlock()
	if !available {
		result.Note = "Поиск ранних аналогов не завершён: исчерпан общий бюджет запросов."
		return result
	}
	select {
	case budget.slots <- struct{}{}:
		defer func() { <-budget.slots }()
	case <-ctx.Done():
		result.Note = "Поиск ранних аналогов отменён или общий срок поиска завершился."
		return result
	}
	// Ожидание свободного места не сокращает срок самого обращения к каталогу.
	work, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	date, _ := time.Parse(time.DateOnly, before)
	filter := `title.search:"` + phrase + `",to_publication_date:` + date.AddDate(0, 0, -1).Format(time.DateOnly)
	query := url.Values{oaFilter: {filter},
		livePriorSort: {"publication_date:asc"}, livePriorPage: {"10"},
		"select": {"id,doi,title,type,publication_date,abstract_inverted_index"}}
	result.SourceURL = liveOpenAlexBase + "/works?" + query.Encode()
	raw, err := s.oa.Get(work, "/works", query)
	if err != nil {
		result.Note = "Источник ранних работ недоступен или его лимит исчерпан; отсутствие аналогов не установлено."
		return result
	}
	return liveReadPriorArt(raw, result, time.Now().UTC())
}

func liveReadPriorArt(raw []byte, result *LivePriorArt, now time.Time) *LivePriorArt {
	var response struct {
		Meta struct {
			Count *int `json:"count"`
		} `json:"meta"`
		Results []openalex.Work `json:"results"`
	}
	if json.Unmarshal(raw, &response) != nil || response.Meta.Count == nil || response.Results == nil ||
		*response.Meta.Count < len(response.Results) {
		result.Note = "Источник вернул неполный ответ: наличие ранних аналогов неизвестно."
		return result
	}
	result.Total, result.SourceSHA256, result.ObservedAt = response.Meta.Count, liveCardTextHash(string(raw)), now
	seen := map[string]bool{}
	phrase := liveWords(result.Query)
	for _, paper := range response.Results {
		doi := liveNormalizePaperDOI(paper.PlainDOI())
		id := cmp.Or(doi, paper.ID)
		if _, err := time.Parse(time.DateOnly, paper.PublicationDate); err != nil || paper.PublicationDate >= result.Before ||
			!livePriorWorkID.MatchString(paper.ID) || seen[id] ||
			len(phrase) < 2 || !livePhraseIn(liveWords(paper.Title), phrase) ||
			!slices.Contains([]string{livePaperArticle, liveMLExtractPreprint, "proceedings-article", "book-chapter"}, paper.Type) {
			continue
		}
		seen[id] = true
		address := paper.ID
		if doi != "" {
			address = "https://doi.org/" + doi
		}
		result.Works = append(result.Works, LivePriorWork{ID: paper.ID, Title: paper.Title, Date: paper.PublicationDate,
			DOI: doi, URL: address, Abstract: liveClip(paper.Abstract(), liveCardAbstract)})
		if len(result.Works) == 3 {
			break
		}
	}
	result.Status = "not_found"
	result.Note = "В проверенном поиске более ранние работы не найдены; это не доказывает новизну технологии."
	if len(result.Works) > 0 {
		result.Status = "candidates"
		result.Note = "Найдены более ранние работы с исходным названием технологии в заголовке. " +
			"Тождество механизма и применимости требует проверки; " +
			"зрелость рынка этим не установлена."
	} else if *result.Total > 0 {
		result.Status = livePriorUnavailable
		result.Note = "Каталог вернул совпадения, но их заголовки, даты или типы не позволяют подтвердить ранние аналоги."
	}
	return result
}
