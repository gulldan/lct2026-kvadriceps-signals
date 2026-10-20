package application

import (
	"cmp"
	"context"
	"fmt"
	"net/http"
	"regexp"
	"strings"
)

// Vinnova — инновационное агентство Швеции; открытые данные агентства — общественное достояние без ограничений.
// Типа организации в выгрузке нет, поэтому компанию узнаём по форме собственности в названии: координатор-компания
// и есть получатель гранта.

const (
	liveMoneyVinnovaURL     = "https://data.vinnova.se/api/projekt/"
	liveMoneyVinnovaSource  = "Vinnova: гранты компаниям Швеции"
	liveMoneyVinnovaLicense = "Public Domain Mark, Vinnova"
)

type liveMoneyVinnovaProject struct {
	ID          string  `json:"Diarienummer"`
	Title       string  `json:"Ärenderubrik"`
	TitleEN     string  `json:"ÄrenderubrikEngelska"`
	Goal        string  `json:"MalSvenska"`
	GoalEN      string  `json:"MalEngelska"`
	Amount      float64 `json:"BeviljatBidrag"`
	Start       string  `json:"ProjektStart"`
	Coordinator string  `json:"KoordinatorOrg"`
}

// liveMoneyVinnova спрашивает проекты, изменённые с начала окна: проект меняется при решении и старте,
// а запрос на полгода раньше дал те же 794 старта в окне (замер 18.10).
func liveMoneyVinnova(ctx context.Context, client *http.Client, rubrics []liveRubric,
	from, through string) ([]LiveMoneyEvent, int, error) {
	// Сервер собирает ответ 10–30 с до первого байта: сторож замирания liveMarketFetch оборвал бы каждую попытку.
	var projects []liveMoneyVinnovaProject
	if err := liveMoneyJSON(ctx, client, http.MethodGet, liveMoneyVinnovaURL+from, nil, &projects); err != nil {
		return nil, 0, fmt.Errorf("vinnova: %w", err)
	}
	events, records := liveMoneyVinnovaEvents(rubrics, projects, from, through)
	return events, records, nil
}

// Аббревиатура формы собственности — компания; но Chalmers и RISE тоже AB, а это вуз и институт.
var (
	liveMoneyVinnovaCompany  = regexp.MustCompile(`\b(?:AB|HB|KB)\b|(?i:aktiebolag)|\(publ\)`)
	liveMoneyVinnovaAcademic = regexp.MustCompile(`(?i)institut|högskol|universitet|science park`)
)

func liveMoneyVinnovaEvents(rubrics []liveRubric, projects []liveMoneyVinnovaProject,
	from, through string) ([]LiveMoneyEvent, int) {
	events, records := []LiveMoneyEvent{}, 0
	for _, project := range projects {
		date := project.Start[:min(len(project.Start), 10)]
		if date < from || date > through {
			continue
		}
		records++
		if !liveMoneyVinnovaCompany.MatchString(project.Coordinator) || liveMoneyVinnovaAcademic.MatchString(project.Coordinator) {
			continue
		}
		event := liveMoneyGrant(rubrics, liveMoneyVinnovaSource, cmp.Or(project.TitleEN, project.Title),
			cmp.Or(project.GoalEN, project.Goal))
		event.ID, event.Company, event.Date = liveMoneyID("vinnova", project.ID), strings.TrimSpace(project.Coordinator), date
		event.Region, event.Country, event.Investors = liveMoneyRegionEU, "SE", "Vinnova"
		event.URL, event.License = liveMoneyVinnovaURL+project.ID, liveMoneyVinnovaLicense
		if project.Amount > 0 {
			event.Amount = liveMoneyIn("SEK ", project.Amount)
		}
		events = append(events, event)
	}
	return events, records
}
