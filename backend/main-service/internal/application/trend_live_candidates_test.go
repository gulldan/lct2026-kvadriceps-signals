package application

import (
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"
)

func TestLiveAssessKeepsMatureDirectionOutOfEarlySignal(t *testing.T) {
	periods := livePeriods(time.Date(2026, 9, 10, 12, 0, 0, 0, time.UTC))
	papers := make([]StudyPaper, 0, 12)
	for i := range 12 {
		// Две авторские группы на всё направление: Groups < 3.
		author := []string{"Old Lab A"}
		if i%2 == 1 {
			author = []string{"Old Lab B"}
		}
		date := periods[0].Start
		if i >= 6 {
			date = periods[len(periods)-2].Start
		}
		papers = append(papers, StudyPaper{ID: fmt.Sprintf("arxiv:m%d", i),
			Title: "Retrieval Augmented Generation", Date: date, Authors: author})
	}
	topic := LiveTopic{StudyTopic: StudyTopic{ID: "mature", Series: liveSeries(periods, papers)}}
	liveAssess(&topic, periods, papers)
	if topic.Groups >= 3 {
		t.Fatalf("проверка бессмысленна без узкого корпуса: групп %d", topic.Groups)
	}
	if topic.Signals.Novelty != studyNoveltyEarlier {
		t.Fatalf("тема обязана быть замечена до окна: %q", topic.Signals.Novelty)
	}
	if topic.Stage != liveStageEstablished {
		t.Fatalf("зрелая тема получила стадию %q вместо established", topic.Stage)
	}
	confidence := liveConfidence(topic, liveAbsoluteBar())
	if confidence.AboveMin {
		t.Fatalf("зрелая тема прошла порог уверенности: %.2f, не хватило %v", confidence.Value, confidence.Missing)
	}
}

func TestLiveSearchCandidatesSplitsQueryIntoTechnologies(t *testing.T) {
	periods := livePeriods(time.Date(2026, 9, 10, 12, 0, 0, 0, time.UTC))
	// Без населения окна рост не измеряется и каждый кандидат уходит в «стадию измерить нельзя».
	for i := range periods {
		periods[i].Population = 10000
	}
	recent := []string{periods[len(periods)-4].Start, periods[len(periods)-3].Start, periods[len(periods)-2].Start}
	papers := []StudyPaper{}
	add := func(prefix, title string, count int) {
		for i := range count {
			papers = append(papers, StudyPaper{ID: fmt.Sprintf("arxiv:%s%d", prefix, i), Title: title,
				Date: recent[i%len(recent)], Authors: []string{fmt.Sprintf("%s Lab %d", prefix, i%5)}})
		}
	}
	add("a", "Quantum Inspired Model Compression for Edge Inference", 6)
	add("b", "Agent Identity and Scoped Permissions for Autonomous Agents", 5)
	add("c", "Neuromorphic Photonic Interconnect for Datacenter Fabric", 4)
	// Фон повторяет форму реальной выдачи: работ тем больше, чем старше месяц.
	for i := range 240 {
		papers = append(papers, StudyPaper{ID: fmt.Sprintf("arxiv:bg%d", i),
			Title: "A Survey of Machine Learning Methods", Date: periods[i%9].Start,
			Authors: []string{fmt.Sprintf("Background %d", i)}})
	}
	umbrella := LiveTopic{StudyTopic: StudyTopic{ID: "search:x"}, Name: "перспективные решения в edge"}
	candidates, rejected := liveSearchCandidates(&umbrella, papers, nil, periods)
	if len(candidates) < 2 {
		t.Fatalf("запрос обязан распасться на технологии, получено %d: %+v", len(candidates), names(candidates))
	}
	if len(candidates) > liveCandidatePool {
		t.Fatalf("пул превысил бюджет проверки: %d", len(candidates))
	}
	for i := 1; i < len(candidates); i++ {
		if candidates[i-1].Confidence.Value < candidates[i].Confidence.Value {
			t.Fatalf("порядок не по уверенности: %v", names(candidates))
		}
	}
	for _, candidate := range candidates {
		if candidate.Name == umbrella.Name || candidate.PapersCount == 0 || candidate.Confidence == nil {
			t.Fatalf("кандидат повторяет запрос или пуст: %+v", candidate.Name)
		}
	}
	for _, row := range rejected {
		if row.Reason == "" || row.Code == "" {
			t.Fatalf("отклонение без причины: %+v", row)
		}
	}
}

func names(topics []LiveTopic) []string {
	out := make([]string, 0, len(topics))
	for _, topic := range topics {
		out = append(out, fmt.Sprintf("%s(%.2f)", topic.Name, topic.Confidence.Value))
	}
	return out
}

func TestLiveSearchCandidatesFindsTechnologyWithoutScience(t *testing.T) {
	periods := livePeriods(time.Date(2026, 9, 10, 12, 0, 0, 0, time.UTC))
	for i := range periods {
		periods[i].Population = 10000
	}
	recent := []string{periods[len(periods)-4].Start, periods[len(periods)-3].Start, periods[len(periods)-2].Start}
	papers := make([]StudyPaper, 0, 200)
	for i := range 200 {
		papers = append(papers, StudyPaper{ID: fmt.Sprintf("arxiv:bg%d", i),
			Title: "A Survey of Machine Learning Methods", Date: periods[i%9].Start,
			Authors: []string{fmt.Sprintf("Background %d", i)}})
	}
	records := make([]LiveSourceRecord, 0, 9)
	for i := range 9 {
		records = append(records, LiveSourceRecord{
			ID: fmt.Sprintf("web-%d", i), Title: "Agent Identity Fabric for Autonomous Payments",
			Date: recent[i%len(recent)], URL: fmt.Sprintf("https://techcrunch.com/2026/09/0%d/agent-identity/", i+1),
			Source: fmt.Sprintf("Издание %d", i%5),
		})
	}
	// Канал собирается не по теме: без отбора по переведённым терминам он наполнил бы выдачу чем угодно.
	umbrella := LiveTopic{StudyTopic: StudyTopic{ID: "search:x"}, Name: "финтех",
		Query: LiveQuery{Terms: []string{"agent identity", "autonomous payments"}},
		Web:   &LiveChannel{Source: "web", Records: records, Total: len(records)}}
	candidates, _ := liveSearchCandidates(&umbrella, papers, nil, periods)
	found := slices.IndexFunc(candidates, func(topic LiveTopic) bool {
		return strings.Contains(strings.ToLower(topic.Name), "agent identity")
	})
	if found < 0 {
		t.Fatalf("технология без науки не попала в выдачу: %v", names(candidates))
	}
	topic := candidates[found]
	if topic.PapersCount != 0 || topic.RecordsCount == 0 || topic.DocumentsCount != topic.RecordsCount {
		t.Fatalf("работы и записи посчитаны неверно: работ %d, записей %d, всего %d",
			topic.PapersCount, topic.RecordsCount, topic.DocumentsCount)
	}
	if len(topic.EvidenceIDs) != topic.RecordsCount {
		t.Fatalf("рыночные записи не попали в свидетельства: %d из %d",
			len(topic.EvidenceIDs), topic.RecordsCount)
	}
	// Реестр клинических исследований отдаёт свои двести строк любому направлению.
	foreign := umbrella
	foreign.Registry = &LiveChannel{Source: "registry", Total: 1, Records: []LiveSourceRecord{
		{ID: "ct-1", Title: "Knee Osteoarthritis Rehabilitation Trial", Date: recent[0],
			URL: "https://clinicaltrials.gov/study/1", Source: "ClinicalTrials.gov"},
	}}
	if got := liveChannelDocuments(foreign); slices.ContainsFunc(got, func(p StudyPaper) bool {
		return strings.Contains(p.Title, "Osteoarthritis")
	}) {
		t.Fatal("запись не по теме запроса попала в набор кандидатов")
	}
	// Записи из соцсети — только первичный индикатор: без независимого источника в выдачу не допускаются.
	social := slices.Clone(records)
	for i := range social {
		social[i].URL = fmt.Sprintf("https://www.reddit.com/r/fintech/comments/%d/", i)
	}
	lowered := umbrella
	lowered.Web = &LiveChannel{Source: "web", Records: social, Total: len(social)}
	if got, _ := liveSearchCandidates(&lowered, papers, nil, periods); slices.ContainsFunc(got, func(topic LiveTopic) bool {
		return strings.Contains(strings.ToLower(topic.Name), "agent identity")
	}) {
		t.Fatal("технология допущена по одним источникам пониженного доверия")
	}
}

func TestLiveDedupeSelectedDropsRepeatedTechnology(t *testing.T) {
	topic := func(name, label string, ids ...string) LiveTopic {
		return LiveTopic{StudyTopic: StudyTopic{ID: name, PaperIDs: ids}, Name: name, Label: label}
	}
	candidates := []LiveTopic{
		topic("all solid", "Твердотельные аккумуляторы", "p1", "p2", "p3", "p4", "p5"),
		topic("ion conductor", "Ионные проводники", "p6", "p7", "p8", "p9", "p10"),
		// Тот же n-грамм под другим названием модели: различие только в «ё».
		topic("temperature solid", "Твёрдотельные Аккумуляторы", "p11", "p12", "p13"),
		// Другое название, но документы уже показаны первым кандидатом.
		topic("sulfide electrolyte", "Сульфидные электролиты", "p1", "p2", "p3", "p4", "p20"),
	}
	kept, rejected := liveDedupeSelected(candidates, nil)
	if len(kept) != 2 || kept[0].Name != "all solid" || kept[1].Name != "ion conductor" {
		t.Fatalf("дубли остались в выдаче: %d кандидатов, первый %q", len(kept), kept[0].Label)
	}
	if len(rejected) != 2 {
		t.Fatalf("дубли не попали в отклонённые: %+v", rejected)
	}
	for _, row := range rejected {
		if row.Code != liveRejectDuplicate || !strings.Contains(row.Reason, "Твердотельные аккумуляторы") {
			t.Fatalf("причина не называет строку, которая заняла место: %+v", row)
		}
	}
	if !strings.Contains(rejected[1].Reason, "4 из 5") {
		t.Fatalf("причина не измерена: %q", rejected[1].Reason)
	}
}

func TestLiveAnswerDropsUmbrellaAndVariants(t *testing.T) {
	umbrella := LiveTopic{Name: "KV-cache compression", Label: "Сжатие KV-кэша"}
	rows := []struct {
		label, twin string
	}{
		{"Адаптивная квантизация KV-кэша", ""},
		{"KV-кэш сжатие через токен-компрессию", ""},
		{"Эвикция токенов KV-кэша", ""},
		{"KV-кэш сжатие для LLM-агентов", ""},
		{"KV-кэш сжатие через обученные компрессионные токены", "KV-кэш сжатие через токен-компрессию"},
		{"KV-кэш сжатие", "Сжатие KV-кэша"},
		{"KV Caches", "KV-cache compression"},
		{"Методы сжатия KV-кэша", "Сжатие KV-кэша"},
	}
	candidates := make([]LiveTopic, len(rows))
	for i, row := range rows {
		candidates[i] = LiveTopic{StudyTopic: StudyTopic{PaperIDs: []string{row.label}}, Name: row.label, Label: row.label}
	}
	_, rejected := liveDedupeSelected(liveDropUmbrella(umbrella, candidates, nil))
	for _, row := range rows {
		gone := slices.IndexFunc(rejected, func(r LiveRejection) bool { return r.Name == row.label })
		switch {
		case row.twin == "" && gone >= 0:
			t.Errorf("%q снят: %s", row.label, rejected[gone].Reason)
		case row.twin != "" && (gone < 0 || rejected[gone].Code != liveRejectDuplicate ||
			!strings.Contains(rejected[gone].Reason, "«"+row.twin+"»")):
			t.Errorf("%q не снят как повтор %q: %+v", row.label, row.twin, rejected)
		}
	}
}

// Предмет исследования от технологии отличает голова фразы: «pressure sensor» выживает, «stack pressure» отсеивается.
func TestLiveGramsRejectObjectOfStudyNames(t *testing.T) {
	for _, phrase := range []string{
		"stack pressure", "electrode deformation", "phase transitions", "slurry rheology",
		"interfacial properties", "lithium dynamics", "cycling stability", "cell performance",
		"capacity degradation", "dendrite growth", "thermal behaviour", "deformation measurements",
	} {
		if grams := liveGrams(liveTokens(phrase), nil); len(grams) > 0 {
			t.Fatalf("%q названо технологией: %+v", phrase, grams)
		}
	}
	for _, phrase := range []string{"pressure sensor", "solid electrolyte", "sodium ion battery"} {
		if len(liveGrams(liveTokens(phrase), nil)) == 0 {
			t.Fatalf("технология %q отсеяна вместе с предметами исследования", phrase)
		}
	}
}

func TestLiveDiscoverDropsPhrasesThatNeverStandAlone(t *testing.T) {
	periods := livePeriods(time.Date(2026, 9, 10, 12, 0, 0, 0, time.UTC))
	recent := []string{periods[len(periods)-4].Start, periods[len(periods)-3].Start, periods[len(periods)-2].Start}
	papers := []StudyPaper{}
	add := func(prefix, title string, count int) {
		for i := range count {
			papers = append(papers, StudyPaper{ID: fmt.Sprintf("arxiv:%s%d", prefix, i), Title: title,
				Date: recent[i%len(recent)], Authors: []string{fmt.Sprintf("%s Lab %d", prefix, i)}})
		}
	}
	// Заголовки разные: доля считается по формулировкам, и повтор одной строки — одно наблюдение.
	add("a", "Large Scale Distributed Edge Inference", 2)
	add("b", "Large Scale Training of Vision Transformers", 2)
	add("c", "Large Scale Retrieval Pipelines", 2)
	add("d", "Large Scale Sensor Fusion", 2)
	add("e", "Edge Inference for Wearable Devices", 2)
	add("f", "Quantised Edge Inference", 2)
	// Предмет здесь — тройка: «cyber threat» без продолжения не встречается.
	add("g", "Cyber Threat Intelligence for Industrial Control", 2)
	add("h", "Toward Cyber Threat Intelligence", 2)
	add("i", "Sharing of Cyber Threat Intelligence", 2)
	found := liveDiscover(papers, nil, periods, nil, 2, 1)
	grams := make([]string, 0, len(found))
	for _, candidate := range found {
		grams = append(grams, candidate.gram)
	}
	for _, fragment := range []string{"large scale", "cyber threat", "threat intelligence"} {
		if slices.Contains(grams, fragment) {
			t.Fatalf("обрывок %q назван технологией: %v", fragment, grams)
		}
	}
	for _, phrase := range []string{"edge inference", "cyber threat intelligence"} {
		if !slices.Contains(grams, phrase) {
			t.Fatalf("название %q отсеяно вместе с обрывками: %v", phrase, grams)
		}
	}
}

func TestLiveGenreLowersTrustOfAnnouncement(t *testing.T) {
	records := []LiveSourceRecord{
		{URL: "https://www.nasa.gov/press-release/new-lab", Title: "NASA announces a new lab"},
		{URL: "https://www.nasa.gov/mission/report", Title: "Mission measurement report"},
	}
	stampPassports(records)
	if records[0].Trust != TrustLowered || records[0].Genre != GenreAnnouncement {
		t.Fatalf("пресс-релиз сохранил доверие домена: %+v", records[0])
	}
	if !strings.Contains(records[0].TrustBasis, "пресс-релиз") ||
		!strings.Contains(records[0].TrustBasis, "издатель") {
		t.Fatalf("основание не называет и жанр, и издателя: %q", records[0].TrustBasis)
	}
	if liveTrustOrder(records[1].Trust) <= liveTrustOrder(TrustLowered) {
		t.Fatalf("обычный материал ведомства понижен без причины: %+v", records[1])
	}
}

func liveOffTopicFixture() ([]StudyPaper, []LivePeriod) {
	periods := livePeriods(time.Date(2026, 9, 10, 12, 0, 0, 0, time.UTC))
	for i := range periods {
		periods[i].Population = 10000
	}
	recent := []string{periods[len(periods)-4].Start, periods[len(periods)-3].Start, periods[len(periods)-2].Start}
	papers := []StudyPaper{}
	add := func(prefix, title string, count int) {
		for i := range count {
			papers = append(papers, StudyPaper{ID: fmt.Sprintf("arxiv:%s%d", prefix, i), Title: title,
				Date: recent[i%len(recent)], Authors: []string{fmt.Sprintf("%s Lab %d", prefix, i%5)}})
		}
	}
	add("a", "Quantum Inspired Model Compression for Edge Inference", 6)
	add("b", "Agent Identity and Scoped Permissions for Autonomous Agents", 5)
	add("c", "Neuromorphic Photonic Interconnect for Datacenter Fabric", 4)
	for i := range 240 {
		papers = append(papers, StudyPaper{ID: fmt.Sprintf("arxiv:bg%d", i),
			Title: "A Survey of Machine Learning Methods", Date: periods[i%9].Start,
			Authors: []string{fmt.Sprintf("Background %d", i)}})
	}
	return papers, periods
}

func TestLiveSearchCandidatesRefusesOffTopicQuery(t *testing.T) {
	papers, periods := liveOffTopicFixture()
	umbrella := LiveTopic{StudyTopic: StudyTopic{ID: "search:x"}, Name: "мурмурация скворцов",
		Semantic: &LiveSemantic{QuerySimilarity: new(0.214)}}
	candidates, rejected := liveSearchCandidates(&umbrella, papers, nil, periods)
	if len(candidates) != 0 {
		t.Fatalf("нерелевантный запрос всё равно добит до выдачи: %v", names(candidates))
	}
	off := 0
	for _, row := range rejected {
		if row.Code != liveRejectOffTopic {
			continue
		}
		off++
		if !strings.Contains(row.Reason, "21 %") || !strings.Contains(row.Reason, "25 %") {
			t.Fatalf("причина без измеренного числа и порога: %q", row.Reason)
		}
	}
	if off == 0 {
		t.Fatalf("отклонённые не названы нерелевантными: %+v", rejected)
	}
	limit := slices.IndexFunc(umbrella.Limits, func(l LiveLimit) bool { return l.Code == liveRejectOffTopic })
	if limit < 0 || umbrella.Limits[limit].Values[liveValueRelevance] != 21 {
		t.Fatalf("оговорка не несёт измеренную относимость: %+v", umbrella.Limits)
	}
}

func TestLiveSearchCandidatesKeepsUnmeasuredRelevance(t *testing.T) {
	papers, periods := liveOffTopicFixture()
	umbrella := LiveTopic{StudyTopic: StudyTopic{ID: "search:x"}, Name: "перспективные решения в edge"}
	candidates, _ := liveSearchCandidates(&umbrella, papers, nil, periods)
	if len(candidates) == 0 {
		t.Fatalf("неизмеренная относимость отсекла выдачу")
	}
	if !slices.ContainsFunc(umbrella.Limits, func(l LiveLimit) bool { return l.Code == liveLimitRelevanceOff }) {
		t.Fatalf("неизмеренная относимость не названа оговоркой: %+v", umbrella.Limits)
	}
}

func TestLiveInheritByLabelGivesRussianRecordToCandidate(t *testing.T) {
	umbrella := LiveTopic{StudyTopic: StudyTopic{ID: "search:x"}, Name: "антифрод и биометрия",
		Web: &LiveChannel{Source: "web", Total: 3, Records: []LiveSourceRecord{
			{ID: "hab-1", Title: "Биометрическую верификацию в банках научили считать живость",
				Date: "2026-08-14", URL: "https://habr.com/ru/news/1", Source: "Хабр, информационная безопасность"},
			{ID: "hab-2", Title: "Как устроена биометрическая верификация платежей",
				Date: "2026-08-20", URL: "https://habr.com/ru/news/2", Source: "Хабр, информационная безопасность"},
			{ID: "rsp-1", Title: "Реестр операторов связи пополнился",
				Date: "2026-08-21", URL: "https://rspectr.com/news/3", Source: "RSpectr"},
		}}}
	candidate := LiveTopic{StudyTopic: StudyTopic{ID: "c1", EvidenceIDs: []string{}},
		Name: "Biometric Verification", Label: "Биометрическая верификация"}
	kept, rejected := liveInheritByLabel(umbrella, []LiveTopic{candidate}, nil, liveMarketBarOf(umbrella))
	if len(kept) != 1 || len(rejected) != 0 {
		t.Fatalf("кандидат пропал из выдачи: оставлено %d, отклонено %+v", len(kept), rejected)
	}
	got := kept[0]
	if got.Web == nil || got.Web.Total != 2 || len(got.Web.Records) != 2 {
		t.Fatalf("русские записи не достались кандидату: %+v", got.Web)
	}
	if got.RecordsCount != 2 || got.DocumentsCount != 2 || len(got.EvidenceIDs) != 2 {
		t.Fatalf("счётчики не пересчитаны: записей %d, документов %d, свидетельств %d",
			got.RecordsCount, got.DocumentsCount, len(got.EvidenceIDs))
	}
	last := got.Web.Limits[len(got.Web.Limits)-1]
	if !strings.Contains(last, "по русскому названию") || !strings.Contains(last, "Биометрическая верификация") {
		t.Fatalf("оговорка не называет правило, по которому запись досталась: %q", last)
	}
}

func TestLiveInheritByLabelKeepsOutSharedWordBeginning(t *testing.T) {
	umbrella := LiveTopic{StudyTopic: StudyTopic{ID: "search:x"}, Name: "оптика",
		Web: &LiveChannel{Source: "web", Total: 2, Records: []LiveSourceRecord{
			{ID: "w-1", Title: "Оптическая когерентная томография сетчатки вышла в клиники",
				Date: "2026-08-14", URL: "https://media.example/1", Source: "Медиа"},
			{ID: "w-2", Title: "Банк внедрил систему мониторинга платежей",
				Date: "2026-08-15", URL: "https://media.example/2", Source: "Медиа"},
		}}}
	candidates := []LiveTopic{
		{StudyTopic: StudyTopic{ID: "c1", EvidenceIDs: []string{}}, Name: "optical network",
			Label: "Оптическая сеть"},
		{StudyTopic: StudyTopic{ID: "c2", EvidenceIDs: []string{}}, Name: "system", Label: "Система"},
	}
	kept, _ := liveInheritByLabel(umbrella, candidates, nil, liveMarketBarOf(umbrella))
	for _, got := range kept {
		if got.Web != nil || got.RecordsCount != 0 || len(got.EvidenceIDs) != 0 {
			t.Fatalf("посторонняя запись досталась кандидату %q: %+v", got.Label, got.Web)
		}
	}
}

func TestLiveOneGroupNeedsMeasurableSample(t *testing.T) {
	t.Parallel()
	share := func(value float64) *float64 { return &value }
	cases := []struct {
		name    string
		topic   LiveTopic
		refused bool
	}{
		{"две работы из трёх у одной группы — совпадение", LiveTopic{
			StudyTopic: StudyTopic{Groups: 2, TopGroupShare: share(0.67)}, DocumentsCount: 3}, false},
		{"все работы у одной группы", LiveTopic{
			StudyTopic: StudyTopic{Groups: 1, TopGroupShare: share(1)}, DocumentsCount: 3}, true},
		{"больше половины на измеримой выборке", LiveTopic{
			StudyTopic: StudyTopic{Groups: 2, TopGroupShare: share(0.8)}, DocumentsCount: 6}, true},
		{"три группы — не одна лаборатория", LiveTopic{
			StudyTopic: StudyTopic{Groups: 3, TopGroupShare: share(0.9)}, DocumentsCount: 9}, false},
	}
	for _, row := range cases {
		if got := liveOneGroup(row.topic); got != row.refused {
			t.Errorf("%s: отказ = %v, ожидали %v", row.name, got, row.refused)
		}
	}
}
