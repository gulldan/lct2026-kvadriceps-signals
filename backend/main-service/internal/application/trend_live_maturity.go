package application

import (
	"cmp"
	"fmt"
	"slices"
	"strconv"
	"strings"
)

const (
	liveMarketUnknown = "unknown"
	liveMarketEarly   = "early"
	liveMarketMature  = "mature"
	// liveRejectHistory — факт про науку, а не про зрелость рынка: это разные основания отказа.
	liveRejectHistory = "long_history"

	// Ниже liveHistoryDocuments документов охват месяцев и доля в потоке ничего не измеряют.
	liveHistoryDocuments = 8
	// «Рано» — первая треть окна: треть отделяет присутствие от одиночного попадания.
	liveHistoryEarlyPart = 3
	// «Широко» — не меньше половины месяцев окна.
	liveHistoryWidePart = 2

	liveMaturePatentYears = 3
	liveMatureVendors     = 6
	// liveMatureBarFloor действует при любом объёме сбора: на одной записи доля ничего не измеряет.
	liveMatureBarFloor       = 2
	liveMatureOldYears       = 5
	liveMatureOldMin         = 3
	liveMatureRegistryMonths = 3
	liveMatureFactsShown     = 3
)

type liveMaturity struct {
	known      bool
	mature     bool
	assessment *LiveMarket
	facts      []string
	// Пороги служат диагностикой охвата и не определяют рыночный вердикт.
	checks      []string
	level       string
	formed      bool
	eventFacts  []string
	eventChecks []string
}

type LiveMarket struct {
	Status   string              `json:"status"`
	Known    bool                `json:"known"`
	Mature   bool                `json:"mature"`
	Basis    string              `json:"basis"`
	Facts    []string            `json:"facts,omitempty"`
	Checks   []string            `json:"checks,omitempty"`
	Evidence []LiveClaimEvidence `json:"evidence,omitempty"`
}

type liveMarketBar struct{ vendors, years, months, priorities int }

// liveAbsoluteBar — пороги для темы, ни из чего не выделенной: доля от самой себя набиралась бы всегда.
func liveAbsoluteBar() liveMarketBar {
	return liveMarketBar{vendors: liveMatureVendors, years: liveMaturePatentYears,
		months: liveMatureRegistryMonths, priorities: liveMatureOldMin}
}

func liveMarketBarOf(umbrella LiveTopic) liveMarketBar {
	patents := slices.Concat(liveChannelRecords(umbrella.Patents), liveChannelRecords(umbrella.PatentsCN))
	_, priorities := liveOldPriorities(patents, liveWindowYear(umbrella))
	return liveMarketBar{
		vendors:    liveBarFor(len(liveDistinct(liveChannelRecords(umbrella.Web), livePublisher)), liveMatureVendors),
		years:      liveBarFor(len(liveDistinct(patents, livePatentYear)), liveMaturePatentYears),
		months:     liveBarFor(len(liveDistinct(liveChannelRecords(umbrella.Registry), liveRecordMonth)), liveMatureRegistryMonths),
		priorities: liveBarFor(priorities, liveMatureOldMin),
	}
}

func liveBarFor(collected, absolute int) int {
	return min(absolute, max(liveMatureBarFloor, (collected+1)/2))
}

func liveMarketMaturity(topic LiveTopic, bar liveMarketBar) liveMaturity {
	patents := slices.Concat(liveChannelRecords(topic.Patents), liveChannelRecords(topic.PatentsCN))
	web := liveChannelRecords(topic.Web)
	registry := liveChannelRecords(topic.Registry)
	out := liveMaturity{}
	// Два признака одного патентного канала — не два подтверждения: вердикт допустим лишь с явным
	// утверждением и цитатой.
	if topic.Market != nil && (topic.Market.Status == liveMarketEarly || topic.Market.Status == liveMarketMature) {
		if claim := liveClaim(topic.Market.Evidence, liveClaimMarket); claim != nil &&
			!liveInvestorSource(topic, claim.SourceID) {
			out.known, out.mature = true, topic.Market.Status == liveMarketMature
			out.assessment = topic.Market
		}
	}
	out.level, out.eventFacts, out.eventChecks = liveEventMaturity(topic)
	out.formed = out.level == liveMarketMature
	sign := func(passed bool, fact, check string) {
		if passed {
			out.facts = append(out.facts, fact)
			return
		}
		out.checks = append(out.checks, check)
	}
	years := liveDistinct(patents, livePatentYear)
	sign(len(years) >= bar.years,
		fmt.Sprintf("патентные заявки идут в %d разных годах (%s)", len(years), liveFew(years)),
		fmt.Sprintf("патентных лет %d при пороге %d", len(years), bar.years))
	publishers := liveDistinct(web, livePublisher)
	sign(len(publishers) >= bar.vendors,
		fmt.Sprintf("о технологии пишут %d разных издателей (%s)", len(publishers), liveFew(publishers)),
		fmt.Sprintf("издателей %d при пороге %d", len(publishers), bar.vendors))
	old, total := liveOldPriorities(patents, liveWindowYear(topic))
	sign(total >= bar.priorities && old*2 >= total,
		fmt.Sprintf("у %d патентных записей из %d приоритет старше %d лет", old, total, liveMatureOldYears),
		fmt.Sprintf("давних приоритетов %d из %d при пороге в половину от %d записей", old, total, bar.priorities))
	months := liveDistinct(registry, liveRecordMonth)
	sign(len(months) >= bar.months,
		fmt.Sprintf("в государственных реестрах записи в %d разных месяцах окна (%s)", len(months), liveFew(months)),
		fmt.Sprintf("месяцев с записями в реестрах %d при пороге %d", len(months), bar.months))
	return out
}

func (m liveMaturity) field() *LiveMarket {
	out := &LiveMarket{Status: liveMarketUnknown, Known: m.known, Mature: m.mature, Facts: m.facts, Checks: m.checks,
		Basis: "зрелость рынка не проверена: количество публикаций, патентов и записей реестров " +
			"не доказывает ни массовое внедрение, ни раннюю стадию"}
	switch {
	// Цитата карточки о ранней стадии не отменяет двух измеренных признаков сформированного рынка.
	case m.assessment != nil && !m.formed:
		out.Status, out.Basis, out.Evidence = m.assessment.Status, m.assessment.Basis, m.assessment.Evidence
	case m.level != "":
		out.Status, out.Known, out.Mature = m.level, true, m.formed
		out.Facts, out.Checks = slices.Concat(m.eventFacts, m.facts), slices.Concat(m.eventChecks, m.checks)
		switch m.level {
		case liveMarketMature:
			out.Basis = "рынок сформирован: " + strings.Join(m.eventFacts, "; ")
		case liveMarketForming:
			out.Basis = "рынок формируется: " + strings.Join(m.eventFacts, "; ") +
				"; второго признака сформированного рынка нет"
		default:
			out.Basis = "признаков сформированного рынка не найдено: " + strings.Join(m.eventChecks, "; ")
		}
	}
	return out
}

func (m liveMaturity) reason() string {
	if m.formed {
		return "рынок сформирован: " + strings.Join(m.eventFacts, "; ")
	}
	if m.assessment != nil {
		return "зрелая технология по найденному источнику: " + m.assessment.Basis
	}
	return "зрелость рынка не подтверждена"
}

type liveHistory struct {
	long  bool
	facts []string
}

// liveScienceAge отказывает только при «давно, широко и без роста»: направление со старыми корнями, доля
// которого растёт сейчас, — ровно то, что ищет радар.
func liveScienceAge(topic LiveTopic) liveHistory {
	out := liveHistory{}
	if liveTemporalCoverageLimited(topic.Limits) {
		return out
	}
	// Рост считается по доле в потоке, а не по числу работ: корпус окна растёт
	// сам по себе. Неизмеренный рост не даёт отказать — это то же молчание.
	if topic.Growth.DeltaPP == nil || topic.Momentum == liveMomentumUp {
		return out
	}
	if topic.DocumentsCount < liveHistoryDocuments || len(topic.Series) == 0 {
		return out
	}
	first := slices.IndexFunc(topic.Series, func(row StudyPeriod) bool { return row.Count > 0 })
	if first < 0 {
		return out
	}
	established := topic.Stage == liveStageEstablished
	early := first*liveHistoryEarlyPart < len(topic.Series)
	wide := topic.ActiveYears*liveHistoryWidePart >= len(topic.Series)
	if !established && (!early || !wide) {
		return out
	}
	if early {
		out.facts = append(out.facts, fmt.Sprintf("первое упоминание в %s — это %d-й месяц окна из %d",
			topic.Series[first].Period, first+1, len(topic.Series)))
	}
	if wide {
		out.facts = append(out.facts, fmt.Sprintf("активных месяцев %d из %d — это ровный поток, а не всплеск",
			topic.ActiveYears, len(topic.Series)))
	}
	// «-0.000 п.п.» — знак округления, а не падение: на трёх знаках величина уже ноль.
	delta := *topic.Growth.DeltaPP
	shown := fmt.Sprintf("%+.3f", delta)
	switch shown {
	case "+0.000", "-0.000":
		out.facts = append(out.facts, fmt.Sprintf(
			"доля в потоке не меняется: разница за свежие полгода меньше 0,001 п.п., документов %d против %d в предыдущие полгода",
			topic.Growth.Recent.Count, topic.Growth.Prior.Count))
	default:
		trend := "держится ровно"
		if delta < 0 {
			trend = "падает"
		}
		out.facts = append(out.facts, fmt.Sprintf(
			"доля в потоке не растёт, а %s: %s п.п. за свежие полгода, документов %d против %d в предыдущие полгода",
			trend, shown, topic.Growth.Recent.Count, topic.Growth.Prior.Count))
	}
	out.long = true
	return out
}

// liveScienceHistory называет, что измерили рыночные каналы: их молчание — не второй довод за зрелость.
func liveScienceHistory(topic LiveTopic, market liveMaturity, age liveHistory) string {
	// Числа — у самого кандидата: одинаковая фраза у двадцати пяти отказов непроверяема.
	measured := fmt.Sprintf("за всё окно: документов %d, научных работ %d, независимых групп %d",
		topic.DocumentsCount, topic.PapersCount, topic.Groups)
	verdict := market.field().Basis
	return "давняя научная история без роста — " + strings.Join(age.facts, "; ") +
		"; " + measured + "; " + verdict
}

func liveChannelRecords(channel *LiveChannel) []LiveSourceRecord {
	if channel == nil {
		return nil
	}
	return channel.Records
}

func liveDistinct(records []LiveSourceRecord, of func(LiveSourceRecord) string) []string {
	out := []string{}
	for _, record := range records {
		if value := of(record); value != "" && !slices.Contains(out, value) {
			out = append(out, value)
		}
	}
	slices.Sort(out)
	return out
}

// livePatentYear датирует заявку приоритетом или подачей: по дате публикации давняя заявка выглядела бы
// свежей.
func livePatentYear(record LiveSourceRecord) string {
	return liveYear(cmp.Or(record.Priority, record.Filing, record.Date, record.PublishedAt))
}

// livePublisher берёт хост адреса, а не имя ленты: одно издание приходит из разных лент под разными именами.
func livePublisher(record LiveSourceRecord) string {
	if liveInvestorContext(record.Genre) {
		return ""
	}
	return cmp.Or(passportHost(cmp.Or(record.URL, record.CanonicalURL)), record.Source)
}

// Сохранённая цитата фонда остаётся его позицией, даже если раньше попала в подтверждённое поле карточки.
func liveInvestorSource(topic LiveTopic, sourceID string) bool {
	for _, channel := range []*LiveChannel{topic.Patents, topic.PatentsCN, topic.Registry, topic.Web,
		topic.Corpus, topic.Research} {
		for _, record := range liveChannelRecords(channel) {
			if liveInvestorContext(record.Genre) && sourceID != "" &&
				(sourceID == record.ID || sourceID == record.URL || sourceID == record.CanonicalURL ||
					sourceID == liveEvidenceID(record)) {
				return true
			}
		}
	}
	return false
}

func liveRecordMonth(record LiveSourceRecord) string {
	date := cmp.Or(record.Date, record.PublishedAt, record.AvailableAt)
	if liveYear(date) == "" || len(date) < 7 {
		return ""
	}
	return date[:7]
}

func liveYear(date string) string {
	if len(date) < 4 {
		return ""
	}
	if _, err := strconv.Atoi(date[:4]); err != nil {
		return ""
	}
	return date[:4]
}

func liveOldPriorities(records []LiveSourceRecord, window int) (old, total int) {
	if window == 0 {
		return 0, 0
	}
	for _, record := range records {
		year := liveYear(cmp.Or(record.Priority, record.Filing))
		if year == "" {
			continue
		}
		total++
		if value, err := strconv.Atoi(year); err == nil && window-value >= liveMatureOldYears {
			old++
		}
	}
	return old, total
}

func liveWindowYear(topic LiveTopic) int {
	if len(topic.Series) == 0 {
		return 0
	}
	year, err := strconv.Atoi(liveYear(topic.Series[len(topic.Series)-1].Period))
	if err != nil {
		return 0
	}
	return year
}

func liveFew(values []string) string {
	if len(values) <= liveMatureFactsShown {
		return strings.Join(values, ", ")
	}
	return strings.Join(values[:liveMatureFactsShown], ", ") +
		" и ещё " + strconv.Itoa(len(values)-liveMatureFactsShown)
}
