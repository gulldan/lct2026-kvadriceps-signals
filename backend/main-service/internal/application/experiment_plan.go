package application

import (
	"errors"
	"fmt"
	"strings"

	"github.com/example/main-service/internal/platform/jsonx"
)

const (
	timeDays   = "days"
	timeWeeks  = "weeks"
	timeMonths = "months"
)

const (
	expTypeNewAlloy         = "new_alloy"
	expTypeProcessRoute     = "process_route"
	expTypeCoatingCorrosion = "coating_corrosion"
	expTypeBatteryMaterial  = "battery_material"
	expTypeOreBeneficiation = "ore_beneficiation"
	expTypeMetallurgy       = "metallurgy_process"
	expTypeGeneric          = "generic"
)

const (
	stageProcess  = "процесс"
	stageProperty = "свойство"
)

// experimentPlan: стоимость и срок намеренно грубые (low/medium/high, days/weeks/months), без выдуманной точности.
type experimentPlan struct {
	ExperimentType   string              `json:"experiment_type"`
	Materials        []string            `json:"materials"`
	Sections         []experimentSection `json:"sections"`
	ProcessParams    []experimentParam   `json:"process_parameters"`
	Characterization []string            `json:"characterization_methods"`
	TestMethods      []string            `json:"test_methods"`
	Controls         []string            `json:"controls"`
	SuccessCriteria  []string            `json:"success_criteria"`
	EstimatedCost    string              `json:"estimated_cost"`
	EstimatedTime    string              `json:"estimated_time"`
	Risks            []string            `json:"risks"`
}

type experimentSection struct {
	Title   string   `json:"title"`
	Purpose string   `json:"purpose"`
	Items   []string `json:"items"`
}

type experimentParam struct {
	Name  string `json:"name"`
	Range string `json:"range"`
}

func parseExperimentPlan(answer string) (experimentPlan, error) {
	var p experimentPlan
	if err := decodeJSONObject(answer, &p, jsonx.Unmarshal); err != nil {
		if errors.Is(err, errNoJSONObject) {
			return experimentPlan{}, errors.New("no JSON object in experiment-plan output")
		}
		return experimentPlan{}, fmt.Errorf("parse experiment plan: %w", err)
	}
	return p, nil
}

// normalizeCostLevel: неизвестная метка даёт "", и интерфейс не показывает плашку.
func normalizeCostLevel(s string) string {
	switch strings.ToLower(compactText(s)) {
	case levelLow, "низкая", "низкий", "низко":
		return levelLow
	case levelMedium, "средняя", "средний", "средне":
		return levelMedium
	case levelHigh, "высокая", "высокий", "высоко":
		return levelHigh
	default:
		return ""
	}
}

// normalizeTimeScale: неизвестная метка даёт "", и интерфейс не показывает плашку.
func normalizeTimeScale(s string) string {
	switch strings.ToLower(compactText(s)) {
	case timeDays, "дни", "день", "сутки":
		return timeDays
	case timeWeeks, "недели", "неделя":
		return timeWeeks
	case timeMonths, "месяцы", "месяц":
		return timeMonths
	default:
		return ""
	}
}

func cleanExperimentParams(in []experimentParam) []map[string]string {
	out := make([]map[string]string, 0, len(in))
	for _, p := range in {
		name, rng := compactText(p.Name), compactText(p.Range)
		if name == "" && rng == "" {
			continue
		}
		out = append(out, map[string]string{keyName: name, "range": rng})
	}
	return out
}

func normalizeExperimentType(s string) string {
	switch strings.ToLower(compactText(s)) {
	case expTypeNewAlloy, "alloy", "сплав", "новый сплав":
		return expTypeNewAlloy
	case expTypeProcessRoute, "process", "route", stageProcess, "технологический маршрут":
		return expTypeProcessRoute
	case expTypeCoatingCorrosion, "coating", "corrosion", "покрытие", "коррозия":
		return expTypeCoatingCorrosion
	case expTypeBatteryMaterial, "battery", "cell", "катод", "анод", "батарейный материал":
		return expTypeBatteryMaterial
	case expTypeOreBeneficiation, "beneficiation", "флотация", "обогащение", "обогащение руды":
		return expTypeOreBeneficiation
	case expTypeMetallurgy, "metallurgy", "металлургия", "металлургический передел":
		return expTypeMetallurgy
	case expTypeGeneric, "общий":
		return expTypeGeneric
	default:
		return ""
	}
}

func cleanExperimentSections(in []experimentSection) []map[string]any {
	out := make([]map[string]any, 0, len(in))
	for _, s := range in {
		title, purpose := compactText(s.Title), compactText(s.Purpose)
		items := compactSlice(s.Items)
		if title == "" && purpose == "" && len(items) == 0 {
			continue
		}
		out = append(out, map[string]any{
			"title":   title,
			"purpose": purpose,
			keyItems:  items,
		})
	}
	return out
}
