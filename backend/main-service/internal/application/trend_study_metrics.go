package application

import (
	"math"
	"regexp"
	"slices"
	"strconv"
	"strings"
)

type StudyPeriod struct {
	Period         string   `json:"period"`
	Count          int      `json:"count"`
	Population     int      `json:"population"`
	Share          *float64 `json:"share"`
	Groups         int      `json:"groups"`
	NewGroups      int      `json:"new_groups"`
	RetainedGroups int      `json:"retained_groups"`
}

// StudyGrowth сравнивает два последних полных года семейства.
type StudyGrowth struct {
	Prior      StudyPeriod `json:"prior"`
	Recent     StudyPeriod `json:"recent"`
	DeltaPP    *float64    `json:"delta_pp"`
	ShareRatio *float64    `json:"share_ratio"`
	CountRatio *float64    `json:"count_ratio"`
}

// StudySignals — шесть отдельных уровней отбора без общего балла.
type StudySignals struct {
	Novelty       string `json:"novelty"`
	Momentum      string `json:"momentum"`
	Coherence     string `json:"coherence"`
	Spread        string `json:"spread"`
	Application   string `json:"application"`
	Concentration string `json:"concentration"`
}

type StudyEvent struct {
	Topic string `json:"topic"`
	// Тип издателя и уровень доверия обязательны у каждой ссылки в выдаче.
	PublisherType string `json:"publisher_type,omitempty"`
	Trust         string `json:"trust,omitempty"`
	TrustBasis    string `json:"trust_basis,omitempty"`
	Kind          string `json:"kind"`
	Community     string `json:"community"`
	Date          string `json:"date"`
	Title         string `json:"title"`
	Org           string `json:"org"`
	URL           string `json:"url"`
	Stars         int    `json:"stars,omitzero"`
}

type StudyTopic struct {
	ID               string        `json:"id"`
	PaperIDs         []string      `json:"paper_ids"`
	Series           []StudyPeriod `json:"series"`
	Stage            string        `json:"stage"`
	Momentum         string        `json:"momentum"`
	Growth           StudyGrowth   `json:"growth"`
	Reviewed         bool          `json:"reviewed"`
	Screening        string        `json:"screening"`
	EvidenceIDs      []string      `json:"evidence_ids"`
	FirstYear        string        `json:"first_year"`
	GrowthStart      string        `json:"growth_start"`
	ActiveYears      int           `json:"active_years"`
	Groups           int           `json:"groups"`
	TopGroupShare    *float64      `json:"top_group_share"`
	Signals          StudySignals  `json:"signals"`
	External         []StudyEvent  `json:"external"`
	EarliestExternal string        `json:"earliest_external"`
}

type studyFamily struct {
	id      string
	pattern *regexp.Regexp
}

func studyFamilies() []studyFamily {
	return []studyFamily{
		{studyGraphFamily, regexp.MustCompile(`(?i)graph|\bgrag\b`)},
		{"adaptive", regexp.MustCompile(`(?i)adaptiv|self-rag|correctiv|active retrieval`)},
		{"multimodal", regexp.MustCompile(`(?i)multimodal|multi-modal|visual|vision`)},
		{"agentic", regexp.MustCompile(`(?i)agent`)},
		{"memory", regexp.MustCompile(`(?i)memory|long-term|lifelong`)},
	}
}

func graphStudyMilestones() []string {
	return []string{"2024.acl-long.115", "2025.findings-naacl.232", "2025.naacl-long.449", "2025.findings-acl.856",
		"2025.emnlp-main.1793"}
}

const (
	studyUnknown            = "unknown"
	studyKindImplementation = "implementation"
	studyNoveltyEarlier     = "earlier"
	studyNoveltyNew         = "new"
	studyScreeningNone      = "none"
	studyScreeningTitles    = "titles"
	studyStageUnassessed    = "unassessed"
	studyGraphFamily        = "graph"
)

func studyTopics(papers []StudyPaper, complete bool, events []StudyEvent) []StudyTopic {
	families := studyFamilies()
	decisions := studyDecisions()
	topics := make([]StudyTopic, 0, len(families))
	for _, family := range families {
		members := studyMembers(family.pattern, papers, decisions[family.id])
		screening := studyScreening(family, papers, decisions[family.id])
		topic := StudyTopic{ID: family.id, PaperIDs: make([]string, 0, len(members)), Series: studySeries(papers, members, complete),
			Stage: studyStageUnassessed, Momentum: studyUnknown, Reviewed: screening != studyScreeningNone,
			Screening: screening, EvidenceIDs: []string{}, External: []StudyEvent{}}
		for _, paper := range members {
			topic.PaperIDs = append(topic.PaperIDs, paper.ID)
		}
		topic.FirstYear, topic.ActiveYears = studyActivity(topic.Series)
		topic.Groups, topic.TopGroupShare = studyConcentration(members, complete)
		topic.Growth = studyGrowth(topic.Series[4], topic.Series[5], complete)
		topic.Momentum = studyMomentum(topic.Growth)
		topic.GrowthStart = studyGrowthStart(topic.Series)
		topic.External, topic.EarliestExternal = studyExternal(family.id, events)
		topic.Stage = studyStage(topic, complete)
		topic.Signals = studySignals(topic, complete)
		if family.id == studyGraphFamily {
			for _, id := range graphStudyMilestones() {
				if slices.Contains(topic.PaperIDs, id) {
					topic.EvidenceIDs = append(topic.EvidenceIDs, id)
				}
			}
		}
		topics = append(topics, topic)
	}
	return topics
}

func studyMembers(pattern *regexp.Regexp, papers []StudyPaper, decisions map[string]studyDecision) []StudyPaper {
	members := []StudyPaper{}
	for _, paper := range papers {
		decision, screened := decisions[paper.ID]
		if screened && decision.ContentHash == paper.ContentHash && !decision.Included {
			continue
		}
		if pattern.MatchString(paper.Title) {
			members = append(members, paper)
		}
	}
	return members
}

func studyActivity(series []StudyPeriod) (string, int) {
	first, years := "", 0
	for _, period := range series {
		if period.Count > 0 {
			years++
			if first == "" {
				first = period.Period
			}
		}
	}
	return first, years
}

func studyConcentration(members []StudyPaper, complete bool) (int, *float64) {
	groups := studyGroups(members)
	if !complete || len(members) == 0 {
		return len(groups), nil
	}
	largest := 0
	for _, group := range groups {
		largest = max(largest, len(group))
	}
	return len(groups), new(float64(largest) / float64(len(members)))
}

func studyExternal(topic string, events []StudyEvent) ([]StudyEvent, string) {
	external, earliest := []StudyEvent{}, ""
	for _, event := range events {
		if event.Topic != topic {
			continue
		}
		external = append(external, event)
		if earliest == "" || event.Date < earliest {
			earliest = event.Date
		}
	}
	return external, earliest
}

// Независимые группы — связные компоненты работ с общим автором: замена независимости, пока нет
// аффилиаций.
func studyGroups(papers []StudyPaper) [][]StudyPaper {
	parent := make([]int, len(papers))
	for i := range parent {
		parent[i] = i
	}
	find := func(i int) int {
		for parent[i] != i {
			parent[i] = parent[parent[i]]
			i = parent[i]
		}
		return i
	}
	byAuthor := map[string]int{}
	for i, paper := range papers {
		for _, author := range paper.Authors {
			if first, seen := byAuthor[author]; seen {
				parent[find(i)] = find(first)
			} else {
				byAuthor[author] = i
			}
		}
	}
	members := map[int][]StudyPaper{}
	order := []int{}
	for i, paper := range papers {
		root := find(i)
		if _, seen := members[root]; !seen {
			order = append(order, root)
		}
		members[root] = append(members[root], paper)
	}
	groups := make([][]StudyPaper, 0, len(order))
	for _, root := range order {
		groups = append(groups, members[root])
	}
	return groups
}

func studySeries(population, members []StudyPaper, complete bool) []StudyPeriod {
	groups := studyGroups(members)
	firstYear := make([]string, len(groups))
	for i, group := range groups {
		firstYear[i] = "9999"
		for _, paper := range group {
			firstYear[i] = min(firstYear[i], paper.Date[:4])
		}
	}
	series := make([]StudyPeriod, 0, 6)
	for year := 2020; year <= 2025; year++ {
		period := StudyPeriod{Period: strconv.Itoa(year)}
		inPeriod := func(paper StudyPaper) bool { return strings.HasPrefix(paper.Date, period.Period) }
		period.Population = studyCount(population, inPeriod)
		period.Count = studyCount(members, inPeriod)
		for i, group := range groups {
			if !slices.ContainsFunc(group, inPeriod) {
				continue
			}
			period.Groups++
			if firstYear[i] == period.Period {
				period.NewGroups++
			} else {
				period.RetainedGroups++
			}
		}
		if complete && period.Population > 0 {
			period.Share = new(float64(period.Count) / float64(period.Population))
		}
		series = append(series, period)
	}
	return series
}

func studyCount(papers []StudyPaper, match func(StudyPaper) bool) int {
	count := 0
	for _, paper := range papers {
		if match(paper) {
			count++
		}
	}
	return count
}

func studyGrowth(prior, recent StudyPeriod, complete bool) StudyGrowth {
	growth := StudyGrowth{Prior: prior, Recent: recent}
	if !complete || prior.Share == nil || recent.Share == nil {
		return growth
	}
	growth.DeltaPP = new((*recent.Share - *prior.Share) * 100)
	if *prior.Share > 0 {
		growth.ShareRatio = new(*recent.Share / *prior.Share)
	}
	if prior.Count > 0 {
		growth.CountRatio = new(float64(recent.Count) / float64(prior.Count))
	}
	if math.Abs(*growth.DeltaPP) < 1e-12 {
		growth.DeltaPP = new(0.0)
	}
	return growth
}

func studyMomentum(growth StudyGrowth) string {
	switch {
	case growth.DeltaPP == nil:
		return studyUnknown
	case *growth.DeltaPP > 0:
		return "up"
	case *growth.DeltaPP < 0:
		return "down"
	default:
		return "level"
	}
}

// Текущий подъём: от последнего полного года назад, пока доля растёт.
func studyGrowthStart(series []StudyPeriod) string {
	start := ""
	for i := len(series) - 1; i > 0; i-- {
		current, previous := series[i].Share, series[i-1].Share
		if current == nil || previous == nil || *current <= *previous {
			break
		}
		if series[i].Count > 0 {
			start = series[i].Period
		}
	}
	return start
}

// Правило отбора для этого ограниченного корпуса, а не проверенный классификатор; свидетельства
// применения стадию не ограничивают — у молодых направлений их может не быть.
func studyStage(topic StudyTopic, complete bool) string {
	switch {
	case !complete || len(topic.PaperIDs) == 0:
		return studyStageUnassessed
	case topic.Groups < 3:
		return "early_signal"
	case topic.FirstYear <= "2022":
		return "established"
	case topic.Screening != studyScreeningNone && topic.FirstYear >= "2023" && topic.ActiveYears >= 2 && topic.Momentum == "up":
		return "emerging_signal"
	default:
		return studyStageUnassessed
	}
}

func studySignals(topic StudyTopic, complete bool) StudySignals {
	signals := StudySignals{Novelty: studyUnknown, Momentum: topic.Momentum, Coherence: "unscreened",
		Spread: studyUnknown, Application: "none_observed", Concentration: studyUnknown}
	switch topic.Screening {
	case "abstracts":
		signals.Coherence = "screened"
	case studyScreeningTitles:
		signals.Coherence = studyScreeningTitles
	}
	for _, event := range topic.External {
		if event.Kind == "release" {
			signals.Application = "releases"
		} else if event.Kind == studyKindImplementation && signals.Application != "releases" {
			signals.Application = "implementations"
		}
	}
	if !complete || len(topic.PaperIDs) == 0 {
		return signals
	}
	switch {
	case topic.FirstYear >= "2024":
		signals.Novelty = studyNoveltyNew
	case topic.FirstYear == "2023":
		signals.Novelty = "recent"
	default:
		signals.Novelty = studyNoveltyEarlier
	}
	signals.Spread = studySpreadLevel(topic.Series[len(topic.Series)-1].NewGroups)
	signals.Concentration = studyConcentrationLevel(topic.TopGroupShare)
	return signals
}

func studySpreadLevel(newGroups int) string {
	switch {
	case newGroups >= 10:
		return "broad"
	case newGroups >= 3:
		return "several"
	case newGroups > 0:
		return "single"
	default:
		return "none"
	}
}

func studyConcentrationLevel(share *float64) string {
	switch {
	case share == nil:
		return studyUnknown
	case *share <= 0.2:
		return "low"
	case *share <= 0.5:
		return "medium"
	default:
		return "high"
	}
}
