package application

import (
	"bytes"
	"cmp"
	"context"
	"fmt"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/example/main-service/internal/platform/config"
	"github.com/example/main-service/internal/platform/jsonx"
)

// Полнотекстовый поиск EDGAR не отдаёт фрагментов: цитата берётся из самого документа.

const (
	liveEventSourceEDGAR = "edgar"
	liveEDGARSearch      = "https://efts.sec.gov/LATEST/search-index"
	liveEDGARForms       = "8-K,10-K,10-Q,S-1,20-F,6-K"
	// liveEDGARAgent: без контакта в User-Agent SEC отвечает 403; настоящий задаётся SEC_EDGAR_USER_AGENT.
	liveEDGARAgent = "SignalGraph radar radar@signalgraph.invalid"
	// liveEDGARPace — правило SEC: не больше десяти запросов в секунду с адреса.
	liveEDGARPace  = 110 * time.Millisecond
	liveEDGARDocs  = 4
	liveEDGARYears = 3
	liveEDGARBody  = 6 << 20
)

type liveEDGARResponse struct {
	Hits struct {
		Hits []struct {
			ID     string `json:"_id"`
			Source struct {
				CIKs        []string `json:"ciks"`
				Names       []string `json:"display_names"`
				FileDate    string   `json:"file_date"`
				Form        string   `json:"form"`
				ADSH        string   `json:"adsh"`
				FileType    string   `json:"file_type"`
				Description string   `json:"file_description"`
			} `json:"_source"`
		} `json:"hits"`
	} `json:"hits"`
}

type liveEDGARFiling struct {
	id, actor, form, date, title, url string
}

func (s *TrendLiveService) edgarEvents(ctx context.Context, client *http.Client, query LiveEventQuery,
) ([]LiveAdoptionEvent, error) {
	header := http.Header{"User-Agent": {config.Get("SEC_EDGAR_USER_AGENT", liveEDGARAgent)}}
	now := s.now()
	address := liveEDGARSearch + "?" + url.Values{
		"q": {`"` + query.Term + `"`}, "forms": {liveEDGARForms}, "dateRange": {"custom"},
		"startdt": {now.AddDate(-liveEDGARYears, 0, 0).Format(time.DateOnly)}, "enddt": {now.Format(time.DateOnly)},
	}.Encode()
	if err := s.eventData.edgar.wait(ctx, liveEDGARPace); err != nil {
		return nil, err
	}
	body, err := liveMarketFetch(ctx, client, http.MethodGet, address, header, nil, liveMarketMaxJSON)
	if err != nil {
		return nil, fmt.Errorf("edgar: %w", err)
	}
	filings, err := liveEDGARFilings(body)
	if err != nil {
		return nil, err
	}
	// Параллельно: сеть стенда рвёт каждый второй ответ, по очереди не уложиться в срок; темп SEC держит общий счётчик.
	found := make([]*LiveAdoptionEvent, len(filings))
	var wg sync.WaitGroup
	for i, filing := range filings {
		wg.Go(func() {
			if s.eventData.edgar.wait(ctx, liveEDGARPace) != nil {
				return
			}
			document, err := liveMarketFetch(ctx, client, http.MethodGet, filing.url, header, nil, liveEDGARBody)
			if err != nil {
				return
			}
			text, err := SourceHTMLText(bytes.NewReader(document))
			if err != nil {
				return
			}
			if eventType, quote := liveEDGARMention(text, query.Term); quote != "" {
				found[i] = &LiveAdoptionEvent{Type: eventType, Source: liveEventSourceEDGAR,
					Record: liveMarketRecord(filing.id, filing.title, filing.date, filing.url, liveEventSourceEDGAR, "дата подачи в SEC EDGAR"),
					Actor:  filing.actor, Phase: filing.form, Quote: quote}
			}
		})
	}
	wg.Wait()
	events := []LiveAdoptionEvent{}
	for _, event := range found {
		if event != nil {
			events = append(events, *event)
		}
	}
	return events, nil
}

// liveEDGARFilings берёт один документ на компанию: иначе выпуски одного эмитента вытеснили бы остальных.
func liveEDGARFilings(body []byte) ([]liveEDGARFiling, error) {
	var parsed liveEDGARResponse
	if err := jsonx.Unmarshal(body, &parsed); err != nil {
		return nil, fmt.Errorf("edgar: parse: %w", err)
	}
	filings := []liveEDGARFiling{}
	seen := map[string]bool{}
	for _, hit := range parsed.Hits.Hits {
		source := hit.Source
		_, file, ok := strings.Cut(hit.ID, ":")
		if !ok || len(source.CIKs) == 0 || len(source.Names) == 0 || seen[source.CIKs[0]] {
			continue
		}
		cik, err := strconv.Atoi(source.CIKs[0])
		if err != nil {
			continue
		}
		seen[source.CIKs[0]] = true
		actor, _, _ := strings.Cut(source.Names[0], "  (")
		form := source.Form
		if source.FileType != "" && source.FileType != source.Form {
			form += " " + source.FileType
		}
		filings = append(filings, liveEDGARFiling{
			id: "edgar:" + hit.ID, actor: strings.TrimSpace(actor), form: form, date: source.FileDate,
			title: cmp.Or(source.Description, source.Form) + " — " + strings.TrimSpace(actor),
			url: fmt.Sprintf("https://www.sec.gov/Archives/edgar/data/%d/%s/%s", cik,
				strings.ReplaceAll(source.ADSH, "-", ""), file),
		})
		if len(filings) == liveEDGARDocs {
			break
		}
	}
	return filings, nil
}

func liveEDGARMention(text, term string) (string, string) {
	sentences := liveSourceExcerpts(text)
	first := ""
	pilot := ""
	for i, sentence := range sentences {
		sentence = liveTermSentence(sentence, term)
		if sentence == "" {
			continue
		}
		first = cmp.Or(first, sentence)
		window := sentence
		if i+1 < len(sentences) {
			if next := strings.Join(strings.Fields(sentences[i+1]), " "); len(next) <= liveSentenceMax {
				window += " " + next
			}
		}
		switch liveEDGARKind(window) {
		case LiveEventDeployment:
			return LiveEventDeployment, window
		case LiveEventPilot:
			pilot = cmp.Or(pilot, window)
		}
	}
	if pilot != "" {
		return LiveEventPilot, pilot
	}
	return "", first
}

func liveEDGARKind(text string) string {
	words := liveWords(text)
	lowered := strings.Join(words, " ")
	switch {
	case slices.ContainsFunc(words, func(w string) bool { return strings.HasPrefix(w, "deploy") }) ||
		strings.Contains(lowered, "purchase order"):
		return LiveEventDeployment
	case slices.ContainsFunc(words, func(w string) bool { return strings.HasPrefix(w, "pilot") }) ||
		strings.Contains(lowered, "proof of concept"):
		return LiveEventPilot
	}
	return ""
}
