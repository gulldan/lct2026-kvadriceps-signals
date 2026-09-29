package application

import (
	"cmp"
	"fmt"
	"regexp"
	"slices"
	"strconv"
	"strings"
)

// Шкала ТЗ: стадия 1–4 (исследование, прототип, пилот, раннее внедрение), тренд 1–3, балл — их сумма.

const (
	liveEventCardSource = "card"
	// Пороги тренда — отношение потока свежего полугодия к предыдущему с поправкой на рост окна arXiv,
	// подобраны на эталоне; порог «растёт» ниже единицы подписал бы «растёт» под падающей долей.
	liveTZTrendGrows = 1.0
	liveTZTrendFast  = 1.9
)

func liveTZStageName(code int) string {
	return []string{"не установлена", "концепция или исследование", "прототип или PoC",
		liveEventLabel(LiveEventPilot), "раннее внедрение"}[code]
}

func liveEventLabel(kind string) string {
	switch kind {
	case LiveEventGrantEarly:
		return "грант на исследование или PoC"
	case LiveEventGrantLate:
		return "грант на прототип или пилот"
	case LiveEventCodeRelease:
		return "выпуск открытого кода"
	case LiveEventPrototype:
		return "прототип или PoC"
	case LiveEventProductLaunch:
		return "выпуск продукта"
	case LiveEventPilot:
		return "пилот"
	case LiveEventDeployment:
		return "внедрение у заказчика"
	case LiveEventFunding:
		return "раунд финансирования"
	case LiveEventHiring:
		return "вакансии"
	case LiveEventStandard:
		return "стандарт"
	case LiveEventLabResult:
		return "лабораторный результат"
	}
	return "событие"
}

// liveAssessTZ без documents (опоздавшие события) не разбирает кейс карточки: он уже разобран.
func liveAssessTZ(topic *LiveTopic, documents map[string]StudyPaper) {
	topic.Events = liveEventsCompact(topic.Events)
	if documents != nil {
		liveCardCase(topic, documents)
	}
	tz := &LiveTZAssessment{}
	tz.StageCode, tz.StageBasis, tz.StageEvent = liveTZStage(*topic)
	adoption := topic.Adoption
	claimed := adoption != nil && adoption.Rank > tz.StageCode && adoption.Rank < 5 &&
		liveClaim(adoption.Evidence, liveClaimAdoption) != nil
	if claimed {
		tz.StageCode, tz.StageEvent = adoption.Rank, nil
		tz.StageBasis = fmt.Sprintf("стадия %d — %s: %s", adoption.Rank,
			liveTZStageName(adoption.Rank), adoption.Basis)
	}
	tz.TrendCode, tz.TrendBasis = liveTZTrend(*topic, documents)
	if tz.StageCode > 0 && tz.TrendCode > 0 {
		tz.Score = tz.StageCode + tz.TrendCode
	}
	topic.TZ = tz
	switch {
	case claimed:
	// Опоздавшие события не проверены моделью: событие стадии и ссылка на цитату остаются прежними.
	case documents == nil && adoption != nil:
		kept := *adoption
		kept.Basis = tz.StageBasis
		topic.Adoption = &kept
	default:
		topic.Adoption = liveTZAdoption(*topic, documents)
	}
}

func liveCardCase(topic *LiveTopic, documents map[string]StudyPaper) {
	topic.Events = slices.DeleteFunc(slices.Clone(topic.Events), func(event LiveAdoptionEvent) bool {
		return event.Source == liveEventCardSource
	})
	if topic.Card == nil || topic.Card.ExampleID == "" {
		return
	}
	claim := liveClaim(topic.Card.Evidence, liveClaimExample)
	if claim == nil {
		return
	}
	card := *topic.Card
	card.ExampleType = ""
	for _, event := range topic.Events {
		if liveEvidenceID(event.Record) == card.ExampleID && event.Type != "" {
			card.ExampleType = event.Type
		}
	}
	if card.ExampleType == "" {
		// Тип — по цитате источника, а не по русскому кейсу модели: «внедрение ошибок» (fault injection)
		// выдало бы анализ атаки за внедрение.
		kind := liveEventTypeOf(claim.Quote)
		science := !strings.HasPrefix(card.ExampleID, liveRecordPrefix)
		if liveEventBenchmark(claim.Quote) ||
			science && !slices.Contains([]string{LiveEventPilot, LiveEventPrototype, LiveEventDeployment}, kind) {
			kind = LiveEventLabResult
		}
		card.ExampleType = kind
		if document, ok := documents[card.ExampleID]; ok && LiveEventStage(kind) > 0 {
			topic.Events = append(topic.Events, LiveAdoptionEvent{Type: kind, Source: liveEventCardSource,
				Record: LiveSourceRecord{ID: document.ID, Title: document.Title, Date: document.Date, URL: document.URL},
				Quote:  claim.Quote, Verified: true,
				Basis: "кейс-пример карточки: утверждение проверено по цитате отдельным обращением, " +
					"тип события — словарём по этой цитате"})
		}
	}
	topic.Card = &card
}

func liveTZStage(topic LiveTopic) (int, string, *LiveAdoptionEvent) {
	var best *LiveAdoptionEvent
	for i, event := range topic.Events {
		if !event.Verified || event.Quote == "" || LiveEventStage(event.Type) == 0 {
			continue
		}
		if best == nil || cmp.Or(cmp.Compare(LiveEventStage(event.Type), LiveEventStage(best.Type)),
			cmp.Compare(liveEventDate(event), liveEventDate(*best))) > 0 {
			best = &topic.Events[i]
		}
	}
	searched := liveTZSearched(topic)
	if best == nil {
		// Патент — изобретение, а не продукт: кандидат из одних патентов — исследование, а не «не установлена».
		documents := max(topic.DocumentsCount, topic.PapersCount)
		if documents > 0 {
			return 1, fmt.Sprintf("стадия 1 — %s: о технологии найдены научные работы и патенты (%d), "+
				"но не подтверждённые пилоты и внедрения; %s", liveTZStageName(1), documents, searched), nil
		}
		return 0, "стадия не установлена: документов о технологии нет; " + searched, nil
	}
	stage := LiveEventStage(best.Type)
	event := *best
	basis := fmt.Sprintf("стадия %d — %s: %s", stage, liveTZStageName(stage), liveEventLabel(best.Type))
	if best.Actor != "" {
		basis += " (" + best.Actor + ")"
	}
	if date := liveEventDate(event); date != "" {
		basis += ", " + date[:min(len(date), 10)]
	}
	basis += fmt.Sprintf(", источник %s: «%s»", liveEventSourceName(best.Source), best.Quote)
	if stage < 3 {
		basis += "; " + searched
	}
	return stage, basis, &event
}

func liveTZSearched(topic LiveTopic) string {
	if len(topic.eventSources) == 0 {
		return "рыночные источники о технологии не ответили или не спрашивались"
	}
	names := make([]string, len(topic.eventSources))
	for i, name := range topic.eventSources {
		names[i] = liveEventSourceName(name)
	}
	return fmt.Sprintf("пилотов и внедрений не найдено в %d рыночных источниках (%s)", len(names),
		strings.Join(names, ", "))
}

const liveHackerNews = "Hacker News"

func liveEventSourceName(name string) string {
	switch name {
	case liveEventWebSource:
		return "веб-ленты и Bing News"
	case liveEventCardSource:
		return "кейс-пример карточки"
	case liveEventWikipedia:
		return "Википедия"
	case liveEventIETF:
		return "IETF Datatracker"
	case "sbir":
		return "SBIR"
	case "cordis":
		return "CORDIS"
	case "hn":
		return liveHackerNews
	case "edgar":
		return "SEC EDGAR"
	case "producthunt":
		return "Product Hunt"
	case "github":
		return "GitHub"
	case "pypi":
		return "PyPI"
	case "huggingface":
		return "Hugging Face"
	case "bing":
		return "TAdviser, CNews и Хабр через Bing"
	case "zakupki":
		return "ЕИС закупки"
	case "web_ru":
		return "русские ленты сборщика"
	}
	return name
}

// liveTZTrend нормирует работы на объём окна: поток arXiv растёт сам по себе. Пустой год различается по
// датам работ кандидата, иначе сглаживание дало бы «растёт» при нуле.
func liveTZTrend(topic LiveTopic, documents map[string]StudyPaper) (int, string) {
	growth := topic.Growth
	prior, recent := liveTZHalf(growth.Prior.Period), liveTZHalf(growth.Recent.Period)
	if prior[0] == "" || recent[0] == "" || growth.Prior.Population == 0 || growth.Recent.Population == 0 {
		return 0, "тренд не установлен: поток публикаций за два полных полугодия не измерен"
	}
	priorEvents, recentEvents := 0, 0
	for _, event := range topic.Events {
		month := liveEventDate(event)
		if event.Type == "" || event.Count != nil || event.Source == liveEventCardSource || len(month) < 7 {
			continue
		}
		switch month = month[:7]; {
		case month >= prior[0] && month <= prior[1]:
			priorEvents++
		case month >= recent[0] && month <= recent[1]:
			recentEvents++
		}
	}
	if growth.Recent.Count+recentEvents+growth.Prior.Count+priorEvents == 0 {
		return liveTZTrendFromZero(topic, documents, prior, recent)
	}
	scale := float64(growth.Prior.Population) / float64(growth.Recent.Population)
	ratio := (float64(growth.Recent.Count)*scale + float64(recentEvents) + 1) / float64(growth.Prior.Count+priorEvents+1)
	var code int
	var word string
	switch {
	case ratio >= liveTZTrendFast:
		code, word = 3, "быстро растёт"
	case ratio >= liveTZTrendGrows:
		code, word = 2, "растёт"
	default:
		code, word = 1, "стабильно (поток не растёт)"
	}
	return code, fmt.Sprintf("тренд %d — %s: за %s работ %d и рыночных событий %d против %d и %d за %s; "+
		"с поправкой на рост всего потока arXiv отношение %s", code, word, growth.Recent.Period, growth.Recent.Count,
		recentEvents, growth.Prior.Count, priorEvents, growth.Prior.Period, strconv.FormatFloat(ratio, 'f', 2, 64))
}

func liveTZTrendFromZero(topic LiveTopic, documents map[string]StudyPaper, prior, recent [2]string) (int, string) {
	if documents == nil && topic.TZ != nil && topic.TZ.TrendCode > 0 {
		return topic.TZ.TrendCode, topic.TZ.TrendBasis
	}
	fresh := 0
	for _, id := range topic.PaperIDs {
		if date := documents[id].Date; len(date) >= 7 && date[:7] > recent[1] {
			fresh++
		}
	}
	if fresh == 0 {
		return 1, fmt.Sprintf("тренд 1 — стабильно: за %s–%s работ и рыночных событий о технологии нет, "+
			"поток не растёт", prior[0], recent[1])
	}
	return 2, fmt.Sprintf("тренд 2 — растёт с нуля: работ после %s — %d, за %s–%s работ и рыночных событий не было; "+
		"быстрым рост не считается, пока нет второго полугодия для сравнения", recent[1], fresh, prior[0], recent[1])
}

// liveTZHalf — первый и последний месяц полугодия «2025-03–2025-08».
func liveTZHalf(period string) [2]string {
	first, last, ok := strings.Cut(period, "–")
	if !ok || len(first) < 7 || len(last) < 7 {
		return [2]string{}
	}
	return [2]string{first[:7], last[:7]}
}

// liveTZAdoption прикладывает цитату ссылкой, только если её документ есть в ответе.
func liveTZAdoption(topic LiveTopic, documents map[string]StudyPaper) *LiveAdoption {
	codes := []string{liveAdoptionUnknown, liveAdoptionConcept, liveAdoptionPrototype, liveAdoptionPilot,
		liveAdoptionEarly}
	out := &LiveAdoption{Code: codes[topic.TZ.StageCode], Rank: topic.TZ.StageCode, Basis: topic.TZ.StageBasis}
	if topic.Adoption != nil {
		out.Documents, out.Matched = topic.Adoption.Documents, topic.Adoption.Matched
	}
	if event := topic.TZ.StageEvent; event != nil {
		id := event.Record.ID
		if event.Source != liveEventCardSource {
			id = liveEvidenceID(event.Record)
		}
		if _, ok := documents[id]; ok {
			out.Evidence = []LiveClaimEvidence{{Field: liveClaimAdoption, SourceID: id, Quote: event.Quote,
				Verified: event.Verified}}
		}
	}
	return out
}

const (
	// liveMarketForming — один признак рынка из четырёх: исключать технологию рано.
	liveMarketForming = "forming"
	// liveRejectMarketFormed требует двух признаков: стандарт, число компаний, продукт крупного вендора, статья Википедии.
	liveRejectMarketFormed = "market_formed"
	liveMatureActors       = 10
	// Статья Википедии одна рынок не доказывает: у сигнала эталона «Solid-state battery» статья 2009 года.
	liveWikiYears = 5
	liveWikiViews = 5000
	// Свежий стандарт в эталоне стоит рядом с ранним внедрением, а не с рынком.
	liveStandardYears = 3
)

// liveEventMarketTypes не считает гранты: SBIR у малых фирм — исследования под заказ, а не рынок.
func liveEventMarketTypes() []string {
	return []string{LiveEventFunding, LiveEventPilot, LiveEventDeployment, LiveEventProductLaunch}
}

var (
	liveEventGA = regexp.MustCompile(`(?i)\bgenerally available\b|\bgeneral availability\b|\bnow (generally )?available to all\b|` +
		`общедоступн`)
	// У крупных вендоров общедоступный выпуск — массовый продукт, а не первая версия стартапа.
	liveEventBigVendor = regexp.MustCompile(`(?i)\b(aws|amazon|microsoft|azure|google|oracle|ibm|alibaba|salesforce|` +
		`sap|nvidia|apple|meta|cloudflare|databricks|snowflake)\b|яндекс|сбер|\bvk\b|мтс`)
)

// liveEventMaturity возвращает пустой уровень, если рыночные источники о технологии не ответили.
func liveEventMaturity(topic LiveTopic) (level string, facts, checks []string) {
	if len(topic.eventSources) == 0 {
		return "", nil, nil
	}
	sign := func(passed bool, fact, check string) {
		if passed {
			facts = append(facts, fact)
			return
		}
		checks = append(checks, check)
	}
	sign(liveStandardMaturity(topic.Events))
	actors := []string{}
	for _, event := range topic.Events {
		actor := strings.ToLower(strings.TrimSpace(event.Actor))
		market := slices.Contains(liveEventMarketTypes(), event.Type) || event.Type == "" && event.Count == nil
		if actor != "" && market && !slices.Contains(actors, actor) {
			actors = append(actors, actor)
		}
	}
	sign(len(actors) > liveMatureActors,
		fmt.Sprintf("в рыночных событиях %d разных компаний (%s)", len(actors), liveFew(actors)),
		fmt.Sprintf("компаний в рыночных событиях %d при пороге больше %d", len(actors), liveMatureActors))
	ga := slices.IndexFunc(topic.Events, func(event LiveAdoptionEvent) bool {
		text := event.Record.Title + " " + event.Quote
		return liveEventGA.MatchString(text) && liveEventBigVendor.MatchString(text+" "+event.Actor)
	})
	fact := ""
	if ga >= 0 {
		fact = fmt.Sprintf("общедоступный продукт крупного вендора: «%s»", topic.Events[ga].Record.Title)
	}
	sign(ga >= 0, fact, "общедоступных продуктов крупных вендоров не найдено")
	sign(liveWikiMaturity(topic.Events))
	switch {
	case len(facts) >= 2:
		return liveMarketMature, facts, checks
	case len(facts) == 1:
		return liveMarketForming, facts, checks
	}
	return liveMarketEarly, facts, checks
}

func liveWikiMaturity(events []LiveAdoptionEvent) (passed bool, fact, check string) {
	wiki := slices.IndexFunc(events, func(event LiveAdoptionEvent) bool { return event.Source == liveEventWikipedia })
	if wiki < 0 {
		return false, "", "статьи Википедии с этим названием нет"
	}
	event := events[wiki]
	created, observed := liveYear(event.Record.Date), liveYear(cmp.Or(event.Record.ObservedAt, event.Record.Date))
	from, fromErr := strconv.Atoi(created)
	to, toErr := strconv.Atoi(observed)
	views := 0
	if event.Count != nil {
		views = *event.Count
	}
	passed = fromErr == nil && toErr == nil && to-from >= liveWikiYears && views >= liveWikiViews
	return passed, fmt.Sprintf("статья Википедии «%s» существует с %s года, просмотров за месяц %d",
			event.Record.Title, created, views),
		fmt.Sprintf("статья Википедии «%s» с %s года, просмотров за месяц %d — ниже порога в %d лет и %d просмотров",
			event.Record.Title, created, views, liveWikiYears, liveWikiViews)
}

func liveStandardMaturity(events []LiveAdoptionEvent) (passed bool, fact, check string) {
	check = "стандартов с этим именем не найдено"
	for _, event := range events {
		if event.Type != LiveEventStandard || !event.Verified && !event.typed {
			continue
		}
		published := liveYear(liveEventDate(event))
		from, fromErr := strconv.Atoi(published)
		to, toErr := strconv.Atoi(liveYear(event.Record.ObservedAt))
		if fromErr == nil && toErr == nil && to-from >= liveStandardYears {
			return true, fmt.Sprintf("стандарт «%s» действует с %s года (%s)", event.Record.Title, published,
				event.Source), check
		}
		check = fmt.Sprintf("стандарт «%s» вышел в %s году — свежий стандарт говорит о формировании рынка, "+
			"а не о сложившемся", event.Record.Title, published)
	}
	return false, "", check
}

// liveEventsCompact снимает с записей фрагмент и текст: цитата уже выписана, а аннотации утяжелили бы ответ
// на сотни килобайт.
func liveEventsCompact(events []LiveAdoptionEvent) []LiveAdoptionEvent {
	out := slices.Clone(events)
	for i := range out {
		out[i].Record.Excerpt, out[i].Record.Text, out[i].Record.Provenance = "", "", nil
	}
	return out
}
