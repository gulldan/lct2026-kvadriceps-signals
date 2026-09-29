package application

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/example/main-service/internal/platform/config"
	"github.com/example/main-service/internal/platform/jsonx"
)

const (
	liveStepRegistry     = "registry"
	liveRegistryBase     = "https://clinicaltrials.gov/api/v2/studies"
	liveRegistrySource   = "ClinicalTrials.gov"
	liveRegistryPageSize = 200
	liveRegistryTimeout  = 20 * time.Second
	liveRegistryTerms    = 4
)

type liveRegistryResponse struct {
	TotalCount int `json:"totalCount"`
	Studies    []struct {
		Protocol struct {
			Identification struct {
				NCTID      string `json:"nctId"`
				BriefTitle string `json:"briefTitle"`
			} `json:"identificationModule"`
			Status struct {
				OverallStatus string `json:"overallStatus"`
				StartDate     struct {
					Date string `json:"date"`
				} `json:"startDateStruct"`
			} `json:"statusModule"`
			Design struct {
				Phases []string `json:"phases"`
			} `json:"designModule"`
			Sponsor struct {
				Lead struct {
					Name string `json:"name"`
				} `json:"leadSponsor"`
			} `json:"sponsorCollaboratorsModule"`
		} `json:"protocolSection"`
	} `json:"studies"`
}

// liveRegistry превращает отказ реестра в оговорку, а не ошибку: остальной ответ уже собран.
func liveRegistry(ctx context.Context, src *liveSources, terms []string, periods []LivePeriod) *LiveChannel {
	return liveRegistryAt(ctx, src, terms, periods, config.Get("LIVE_REGISTRY_URL", liveRegistryBase))
}

func liveRegistryAt(ctx context.Context, src *liveSources, terms []string,
	periods []LivePeriod, base string) *LiveChannel {
	if len(periods) == 0 {
		return nil
	}
	phrases := liveScholarPhrases(terms)
	if len(phrases) > liveRegistryTerms {
		phrases = phrases[:liveRegistryTerms]
	}
	observed := time.Now().UTC().Format(time.RFC3339)
	channel := &LiveChannel{
		SourceSet: "registry", Source: liveRegistrySource, ObservedAt: observed,
		Counts: make([]*int, len(periods)), Records: []LiveSourceRecord{}, Limits: []string{},
	}
	if len(phrases) == 0 {
		channel.Limits = append(channel.Limits, "registry: в запросе нет латинских слов для поиска по реестру")
		return channel
	}
	src.step(liveStepRegistry, 0)
	from, through := periods[0].Start, periods[len(periods)-1].EndExclusive
	address := base + "?" + url.Values{
		"query.term":      {strings.Join(phrases, " OR ")},
		"filter.advanced": {fmt.Sprintf("AREA[StartDate]RANGE[%s,%s]", from, through)},
		"pageSize":        {strconv.Itoa(liveRegistryPageSize)},
		"countTotal":      {"true"},
		"fields": {"NCTId|BriefTitle|OverallStatus|Phase|StartDate|" +
			"LeadSponsorName|Condition"},
	}.Encode()

	body, err := liveRegistryFetch(ctx, src, address)
	if err != nil {
		channel.Limits = append(channel.Limits, "registry: "+err.Error())
		return channel
	}
	var parsed liveRegistryResponse
	if err := jsonx.Unmarshal(body, &parsed); err != nil {
		channel.Limits = append(channel.Limits, "registry: ответ реестра не разобран")
		return channel
	}
	counts := make([]int, len(periods))
	for _, study := range parsed.Studies {
		protocol := study.Protocol
		id := protocol.Identification.NCTID
		date := protocol.Status.StartDate.Date
		if id == "" || date == "" {
			continue
		}
		// Реестр отдаёт и «2026-08» без дня: без первого дня запись молча выпадет из всех периодов.
		if len(date) == 7 {
			date += "-01"
		}
		index := livePeriodIndex(periods, date)
		if index < 0 {
			continue
		}
		counts[index]++
		channel.Records = append(channel.Records, LiveSourceRecord{
			ID: id, Title: strings.Join(strings.Fields(protocol.Identification.BriefTitle), " "),
			Date: date, URL: "https://clinicaltrials.gov/study/" + id,
			Source: liveRegistrySource, Language: "en", LanguageBasis: "registry record language",
			PublishedAt: date, ObservedAt: observed,
			DateBasis:  "ClinicalTrials.gov study start date",
			TitleBasis: "registry brief title",
			TextStatus: liveRegistryStage(protocol.Design.Phases, protocol.Status.OverallStatus),
			Application: strings.Join(strings.Fields(
				protocol.Sponsor.Lead.Name), " "),
		})
	}
	for i := range counts {
		channel.Counts[i] = new(counts[i])
	}
	channel.Total = len(channel.Records)
	channel.Partial = parsed.TotalCount > len(parsed.Studies)
	if channel.Partial {
		channel.Limits = append(channel.Limits,
			fmt.Sprintf("registry: взяты %d записей из %d найденных", len(parsed.Studies), parsed.TotalCount))
	}
	channel.Coverage = []LiveSourceCoverage{{
		Source: liveRegistrySource, From: from, Through: through,
		Received: len(parsed.Studies), Titles: len(channel.Records),
		ProviderTotal: new(parsed.TotalCount), ObservedAt: observed,
		Pages: []LiveSourceProvenance{{URL: address}},
	}}
	stampPassports(channel.Records)
	return channel
}

func liveRegistryStage(phases []string, status string) string {
	stage := "фаза не указана"
	for _, phase := range phases {
		switch strings.ToUpper(phase) {
		case "EARLY_PHASE1":
			stage = "ранняя первая фаза"
		case "PHASE1":
			stage = "первая фаза"
		case "PHASE2":
			stage = "вторая фаза"
		case "PHASE3":
			stage = "третья фаза"
		case "PHASE4":
			stage = "четвёртая фаза"
		}
	}
	switch strings.ToUpper(status) {
	case "TERMINATED", "WITHDRAWN", "SUSPENDED":
		return stage + ", испытание остановлено"
	}
	return stage
}

func liveRegistryFetch(ctx context.Context, src *liveSources, address string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, liveRegistryTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, address, nil)
	if err != nil {
		return nil, fmt.Errorf("создать запрос к реестру: %w", err)
	}
	req.Header.Set("Accept", "application/json")
	response, err := src.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("реестр недоступен: %w", err)
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("реестр ответил HTTP %d", response.StatusCode)
	}
	raw, err := io.ReadAll(io.LimitReader(response.Body, liveMaxBody))
	if err != nil {
		return nil, fmt.Errorf("прочитать ответ реестра: %w", err)
	}
	return raw, nil
}

// liveRegistryOnTopic требует фразу направления в названии: ClinicalTrials.gov ищет полнотекстово с расширением
// слов. Правило стоит на выдаче, чтобы чистить и старые снимки.
func liveRegistryOnTopic(topic LiveTopic, periods []LivePeriod) LiveTopic {
	if topic.Registry == nil || len(topic.Registry.Records) == 0 {
		return topic
	}
	phrases := liveScholarPhrases(liveTopicTerms(&topic))
	if len(phrases) == 0 {
		return topic
	}
	for i := range phrases {
		phrases[i] = strings.ToLower(phrases[i])
	}
	kept := make([]LiveSourceRecord, 0, len(topic.Registry.Records))
	for _, record := range topic.Registry.Records {
		title := strings.ToLower(record.Title)
		if slices.ContainsFunc(phrases, func(phrase string) bool {
			return strings.Contains(title, phrase)
		}) {
			kept = append(kept, record)
		}
	}
	dropped := len(topic.Registry.Records) - len(kept)
	if dropped == 0 {
		return topic
	}
	channel := *topic.Registry
	channel.Records = kept
	channel.Total = len(kept)
	// Показано ровно то, что прошло правило, поэтому выборка уже не частичная.
	channel.Partial = false
	counts := make([]*int, len(periods))
	for i := range counts {
		counts[i] = new(int)
	}
	for _, record := range kept {
		if index := livePeriodIndex(periods, record.Date); index >= 0 {
			*counts[index]++
		}
	}
	channel.Counts = counts
	channel.Limits = append(slices.Clone(channel.Limits),
		fmt.Sprintf("registry: отброшено %d записей без фразы направления в названии", dropped))
	topic.Registry = &channel
	return topic
}
