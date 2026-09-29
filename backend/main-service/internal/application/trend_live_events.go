package application

import (
	"context"
	"net/http"

	"github.com/example/main-service/internal/platform/config"
)

const (
	LiveEventGrantEarly    = "grant_early"
	LiveEventGrantLate     = "grant_late"
	LiveEventCodeRelease   = "code_release"
	LiveEventPrototype     = "prototype"
	LiveEventProductLaunch = "product_launch"
	LiveEventPilot         = "pilot"
	LiveEventDeployment    = "deployment"
	LiveEventFunding       = "funding"
	LiveEventHiring        = "hiring"
	LiveEventStandard      = "standard"
	LiveEventLabResult     = "lab_result"
)

// LiveAdoptionEvent без дословной цитаты Quote не привязывается к технологии.
type LiveAdoptionEvent struct {
	// Type пуст, если источник не знает тип сам: его ставит словарь по заголовку и фрагменту.
	Type   string           `json:"type"`
	Source string           `json:"source"`
	Record LiveSourceRecord `json:"record"`
	Actor  string           `json:"actor,omitempty"`
	Amount string           `json:"amount,omitempty"`
	Phase  string           `json:"phase,omitempty"`
	Quote  string           `json:"quote,omitempty"`
	// Count и Delta (изменение за месяц) равны nil, когда источник этого не измеряет.
	Count    *int   `json:"count,omitempty"`
	Delta    *int   `json:"delta,omitempty"`
	Unit     string `json:"unit,omitempty"`
	Verified bool   `json:"verified"`
	Basis    string `json:"basis,omitempty"`
	// typed: тип поставил сам источник по своей структуре, поэтому проверка моделью его не меняет.
	typed bool
}

// LiveTZAssessment с кодом 0 означает «не установлено»: тогда Basis объясняет, что искали и не нашли.
type LiveTZAssessment struct {
	StageCode  int                `json:"stage_code"`
	StageBasis string             `json:"stage_basis"`
	StageEvent *LiveAdoptionEvent `json:"stage_event,omitempty"`
	TrendCode  int                `json:"trend_code"`
	TrendBasis string             `json:"trend_basis"`
	// Score — сумма кодов стадии и тренда (2–7); ноль — неизвестен любой из кодов.
	Score int `json:"score"`
}

// LiveEventStage не даёт стадии раунду, вакансиям и стандарту: деньги и люди идут и в идею.
func LiveEventStage(eventType string) int {
	switch eventType {
	case LiveEventLabResult:
		return 1
	case LiveEventCodeRelease, LiveEventProductLaunch, LiveEventGrantEarly, LiveEventPrototype:
		return 2
	case LiveEventPilot, LiveEventGrantLate:
		return 3
	case LiveEventDeployment:
		return 4
	}
	return 0
}

// LiveEventQuery несёт репозитории работ: имя пакета берётся из них, а не угадывается.
type LiveEventQuery struct {
	Term    string
	Russian string
	Repos   []string
}

type LiveEventSource struct {
	Name  string
	Fetch func(ctx context.Context, client *http.Client, query LiveEventQuery) ([]LiveAdoptionEvent, error)
}

func (s *TrendLiveService) eventSources() []LiveEventSource {
	sources := []LiveEventSource{
		liveIETFSource(s.now),
		liveWikipediaSource(s.now),
		{Name: liveEventSourceGitHub, Fetch: liveGitHubEvents},
		{Name: liveEventSourcePyPI, Fetch: livePyPI().fetch},
		{Name: liveEventSourceNPM, Fetch: liveNPM().fetch},
		{Name: liveEventSourceHF, Fetch: liveHFEvents},
		{Name: liveEventSourceBing, Fetch: liveRussianBingEvents},
		{Name: liveEventSourceWebRU, Fetch: liveRussianFeedEvents},
		{Name: liveEventSourceSBIR, Fetch: s.sbirEvents},
		{Name: liveEventSourceCORDIS, Fetch: s.cordisEvents},
		{Name: liveEventSourceHN, Fetch: liveHNEvents},
		{Name: liveEventSourceEDGAR, Fetch: s.edgarEvents},
	}
	// Без токена владельца Product Hunt выключен намеренно: его отказ в каждом ответе выглядел поломкой канала.
	if config.Get("PRODUCTHUNT_TOKEN", "") != "" {
		sources = append(sources, LiveEventSource{Name: liveEventSourcePH, Fetch: liveProductHuntEvents})
	}
	return sources
}
