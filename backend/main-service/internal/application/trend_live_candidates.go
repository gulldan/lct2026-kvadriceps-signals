package application

import (
	"cmp"
	"fmt"
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"
)

// Кандидаты выделяются внутри уже собранного корпуса: сеть повторно не опрашивается.

const (
	liveTopCandidates = 15
	// Общие словарные фразы ограничены отдельно: этот предел не обрезает извлечённые конкретные механизмы.
	// На быстрых маршрутах поиск укладывается в 4–5 минут из двенадцати: из 60 фраз полную карточку получала
	// каждая пятая, и до пятнадцати добирались недописанные. Координатор всё равно останавливается на 15 полных.
	liveCandidatePool = 6 * liveTopCandidates
	// Один прямой источник допустим; малый объём свидетельств отражает уверенность, а не запрет допуска.
	liveCandidateMinDocuments = 1
	// Фразы с двумя документами оказывались шумом; технологии даёт извлечение.
	liveDiscoverMinDocuments = 3

	// Ступени планки свидетельств, в порядке убывания строгости.
	liveEvidenceStrict  = "strict"
	liveEvidenceRelaxed = "relaxed"
	liveEvidenceThin    = "thin"

	// Порог не растёт с корпусом: привязка к объёму не пропускала никого, лишнее отсекают удвоение доли, стадия и уверенность.
	liveCandidateRecentMin = 4

	liveRejectMature     = "mature"
	liveRejectThin       = "thin_evidence"
	liveRejectUnassessed = "unassessed"
	liveRejectRank       = "below_top"
	liveRejectDuplicate  = "duplicate"
	liveRejectOffTopic   = "off_topic"
	liveRejectOneGroup   = "one_group"
	liveRejectBudget     = "qualification_budget"
	liveRejectWellKnown  = "well_known"
	// liveRejectHype не входит в добор до пятнадцати: шум не становится технологией от нехватки выдачи.
	liveRejectHype = "hype"

	// liveLimitRelevanceOff — относимость не измерена: выдача не режется, но и не выдаётся за проверенную.
	liveLimitRelevanceOff = "relevance_unmeasured"

	// liveRelevanceFloor — порог близости запроса к корпусу в процентах, как на экране: технологические
	// запросы дают 31…76 %, заведомо чужой — 21 %, а оборвать честную выдачу дороже.
	liveRelevanceFloor = 25

	// Имена чисел оговорки off_topic: интерфейс подставляет их в перевод.
	liveValueRelevance = "relevance"
	liveValueFloor     = "floor"
)

// LiveRejection несёт причину отказа вместе с измеренным фактом: ТЗ требует показывать логику исключения.
type LiveRejection struct {
	Name         string                  `json:"name"`
	OriginalName string                  `json:"original_name,omitempty"`
	Code         string                  `json:"code"`
	Reason       string                  `json:"reason"`
	Stage        string                  `json:"stage"`
	Papers       int                     `json:"papers"`
	Confidence   int                     `json:"confidence"`
	ModelValue   *float64                `json:"model_value,omitempty"`
	ML           *LiveMLExtractCandidate `json:"ml,omitempty"`
	topic        *LiveTopic
}

// Совпадение фразы не подтверждает ни технологию, ни связь с запросом: это проверяется отдельно до выдачи.
func liveSearchCandidates(umbrella *LiveTopic, papers []StudyPaper, metadata map[string]LivePaperMetadata,
	periods []LivePeriod,
) ([]LiveTopic, []LiveRejection) {
	documents := slices.Concat(papers, liveChannelDocuments(*umbrella))
	// Пороги зрелости — от сбора всего запроса: своего сбора у кандидата нет.
	bar := liveMarketBarOf(*umbrella)
	included := map[string]LiveTopic{}
	refused := map[string]LiveRejection{}
	order := []string{}
	complete := !liveTemporalCoverageLimited(umbrella.Limits)
	tiers := liveEvidenceTiers()
	if !complete {
		// При усечении нет основания ранжировать по ступеням временного роста.
		tiers = tiers[len(tiers)-1:]
	}
	discovered, emergence := liveDiscoverPool(documents, metadata, periods, tiers, complete)
	// Первое пройденное условие сохраняется: ослабление порога не должно молча менять основание допуска.
	for i, tier := range tiers {
		for _, candidate := range discovered[i] {
			if _, done := included[candidate.gram]; done {
				continue
			}
			if _, known := refused[candidate.gram]; !known {
				order = append(order, candidate.gram)
			}
			topic := liveCandidateTopic(*umbrella, candidate, periods, bar, emergence[candidate.gram])
			topic.EvidenceLevel = tier.level
			if code, reason := liveCandidateRefusal(topic, candidate.papers, tier, bar); code != "" {
				refused[candidate.gram] = liveRejection(topic, code, reason)
				continue
			}
			delete(refused, candidate.gram)
			included[candidate.gram] = topic
		}
	}
	selected := make([]LiveTopic, 0, len(included))
	rejected := make([]LiveRejection, 0, len(refused))
	for _, gram := range order {
		if topic, ok := included[gram]; ok {
			selected = append(selected, topic)
			continue
		}
		if row, ok := refused[gram]; ok {
			rejected = append(rejected, row)
		}
	}
	slices.SortFunc(selected, liveCandidateOrder)
	selected, rejected = liveOnTopic(umbrella, selected, rejected)
	for _, topic := range selected[min(len(selected), liveCandidatePool):] {
		rejected = append(rejected, liveRejection(topic, liveRejectBudget, fmt.Sprintf(
			"не проверено по содержанию: в одном поиске проверяется не более %d словарных кандидатов",
			liveCandidatePool)))
	}
	return selected[:min(len(selected), liveCandidatePool)], rejected
}

// Карты Юна — до стадий: класс зависит от медиан всего пула, а стадия — от класса.
func liveDiscoverPool(documents []StudyPaper, metadata map[string]LivePaperMetadata,
	periods []LivePeriod, tiers []liveTier, complete bool,
) ([][]liveCandidate, map[string]*LiveEmergence) {
	discovered := make([][]liveCandidate, len(tiers))
	set := map[string][]StudyPeriod{}
	for i, tier := range tiers {
		discovered[i] = liveDiscoverWithCoverage(documents, metadata, periods, nil, tier.recentMin, tier.earlyDiv, complete)
		for _, candidate := range discovered[i] {
			if _, known := set[candidate.gram]; !known {
				set[candidate.gram] = liveSeries(periods, candidate.dated())
			}
		}
	}
	return discovered, liveEmergenceMap(periods, set)
}

// Места распределяются после всех проверок: отклонённый по содержанию не занимает место даже временно.
func liveSelectCandidates(candidates []LiveTopic, rejected []LiveRejection) ([]LiveTopic, []LiveRejection) {
	slices.SortFunc(candidates, liveCandidateOrder)
	candidates, rejected = liveDedupeSelected(candidates, rejected)
	for _, topic := range candidates[min(len(candidates), liveTopCandidates):] {
		basis := "ступени свидетельств и доле правил"
		if topic.ModelScore != nil {
			basis = "оценке подключённой модели; при равенстве — ступени свидетельств и доле правил"
		}
		rejected = append(rejected, liveRejection(topic, liveRejectRank, fmt.Sprintf(
			"проверено, но ниже первых %d завершённых карточек по %s", liveTopCandidates, basis)))
	}
	return candidates[:min(len(candidates), liveTopCandidates)], rejected
}

// Приёмка требует честного недобора, когда корпус измеренно не о том; отброшенные уходят в отклонённые с числом.
func liveOnTopic(umbrella *LiveTopic, selected []LiveTopic, rejected []LiveRejection) ([]LiveTopic, []LiveRejection) {
	// Направление карты само задаёт корпус выражением поиска: оговорка «не измерено» читалась бы как изъян.
	if umbrella.Curated || umbrella.Source == liveSourceDiscovered {
		return selected, rejected
	}
	similarity, measured := liveQueryRelevance(*umbrella)
	if !measured {
		umbrella.Limits = append(umbrella.Limits, liveLimitOf(liveLimitRelevanceOff,
			"relevance: query similarity not measured; the answer is not screened by it", nil))
		return selected, rejected
	}
	percent := int(similarity*100 + 0.5)
	if percent >= liveRelevanceFloor {
		return selected, rejected
	}
	umbrella.Limits = append(umbrella.Limits, liveLimitOf(liveRejectOffTopic,
		"relevance: the collected corpus is not about the query", map[string]int{
			liveValueRelevance: percent, liveValueFloor: liveRelevanceFloor}))
	for _, topic := range selected {
		rejected = append(rejected, liveRejection(topic, liveRejectOffTopic, fmt.Sprintf(
			"собранное не о том, что спросили: близость запроса к корпусу %d %% при пороге %d %%",
			percent, liveRelevanceFloor)))
	}
	return nil, rejected
}

// liveConcentrationMin — с какого числа документов доля крупнейшей группы что-то различает: на трёх
// документах «больше половины у одной группы» даёт любой общий автор двух работ.
const liveConcentrationMin = 5

// Доля, равная единице, ловится и на малой выборке; спорная доля больше половины требует измеримой выборки.
func liveOneGroup(topic LiveTopic) bool {
	if topic.DocumentsCount < 2 || topic.TopGroupShare == nil || topic.Groups >= 3 {
		return false
	}
	if topic.Groups == 1 {
		return true
	}
	return topic.DocumentsCount >= liveConcentrationMin && *topic.TopGroupShare > liveConfidenceTopGroup
}

// Поле Relevance не годится: оно ищет слова русского запроса в английских заголовках и почти всегда равно нулю.
func liveQueryRelevance(umbrella LiveTopic) (float64, bool) {
	if umbrella.Semantic == nil || umbrella.Semantic.QuerySimilarity == nil {
		return 0, false
	}
	return *umbrella.Semantic.QuerySimilarity, true
}

type liveTier struct {
	level     string
	recentMin int
	earlyDiv  int
	// weak допускает кандидата без измеренной стадии: лучше технология с честной пометкой, чем строка-пустота.
	weak bool
}

func liveEvidenceTiers() []liveTier {
	return []liveTier{
		{liveEvidenceStrict, liveCandidateRecentMin, 2, false},
		{liveEvidenceRelaxed, 3, 2, false},
		{liveEvidenceThin, 2, 1, true},
	}
}

// Зрелость отсекается на любой ступени: ТЗ требует исключать сложившиеся технологии даже ценой короткой выдачи.
func liveRejectReason(topic LiveTopic, tier liveTier, bar liveMarketBar) (code, reason string) {
	market := liveMarketMaturity(topic, bar)
	age := liveScienceAge(topic)
	switch {
	case market.mature:
		return liveRejectMature, market.reason()
	case age.long:
		return liveRejectHistory, liveScienceHistory(topic, market, age)
	// Известная тема по Юну вместе со стартом до окна — сложившаяся технология, планка тут ни при чём.
	case topic.Stage == liveStageEstablished && topic.Emergence.classified(liveEmergenceKnown):
		return liveRejectWellKnown,
			"тема известна: видимость выше медианы выдачи без прироста — по карте появления ключевых слов"
	case topic.DocumentsCount < liveCandidateMinDocuments:
		return liveRejectThin, fmt.Sprintf(
			"недостаточно свидетельств: %d документов в окне, стадию и рост измерить нельзя",
			topic.DocumentsCount)
	// Направление одной группы — работа одной лаборатории, а не рыночный сигнал: рост её доли создан ею же.
	case liveOneGroup(topic):
		return liveRejectOneGroup, fmt.Sprintf(
			"направление ведёт одна группа: %d %% работ у крупнейшей из %d групп при %d документах",
			int(*topic.TopGroupShare*100+0.5), topic.Groups, topic.DocumentsCount)
	case topic.Stage == studyStageUnassessed && !tier.weak:
		return liveRejectUnassessed,
			"стадию измерить не удалось: работы не складываются ни в рост, ни в устойчивую активность"
	}
	return "", ""
}

func liveRejection(topic LiveTopic, code, reason string) LiveRejection {
	confidence := 0
	if topic.Confidence != nil {
		confidence = int(topic.Confidence.Value*100 + 0.5)
	}
	name := cmp.Or(topic.Label, "Кандидат по фрагменту источника")
	if code == liveRejectNotTechnology {
		name = "Фрагмент без подтверждённой технологии"
	}
	rejection := LiveRejection{Name: name, OriginalName: topic.Name, Code: code, Reason: reason, Stage: topic.Stage,
		Papers: topic.PapersCount, Confidence: confidence, ML: topic.ML, topic: &topic}
	if topic.ModelScore != nil && topic.ModelScore.Label != liveClassifierInsufficient && topic.ModelScore.DataGap == "" {
		rejection.ModelValue = new(topic.ModelScore.Value)
	}
	return rejection
}

// Ступени порождают кандидатов независимо, а имя присваивается после отбора, поэтому дубли сворачиваются здесь.
func liveDedupeSelected(candidates []LiveTopic, rejected []LiveRejection) ([]LiveTopic, []LiveRejection) {
	kept := make([]LiveTopic, 0, len(candidates))
	for _, candidate := range candidates {
		own := map[string]bool{}
		for _, id := range liveDuplicateSourceIDs(candidate) {
			own[id] = true
		}
		twin, shared := "", 0
		for _, earlier := range kept {
			count := 0
			for _, id := range liveDuplicateSourceIDs(earlier) {
				if own[id] {
					count++
				}
			}
			// Русские подписи одной технологии модель пишет по-разному: «Доказательства с нулевым разглашением (ZKP)»
			// и «Нулевое разглашение (Zero-Knowledge Proof)» заняли два места добора 19.10.
			sameName := liveSameTechnology(candidate.Label, earlier.Label) ||
				liveVariantLabels(liveCandidateName(candidate), liveCandidateName(earlier)) ||
				liveVariantLabels(candidate.Name, earlier.Name)
			if sameName || (len(own) > 0 && float64(count) >= liveOverlapSkip*float64(len(own)) &&
				!liveDifferentMechanisms(candidate, earlier)) {
				twin, shared = liveCandidateName(earlier), count
				break
			}
		}
		if twin == "" {
			kept = append(kept, candidate)
			continue
		}
		rejected = append(rejected, liveRejection(candidate, liveRejectDuplicate, fmt.Sprintf(
			"то же, что «%s»: общих документов %d из %d", twin, shared, len(own))))
	}
	return kept, rejected
}

// Совпадение слов широкого направления с узким методом не заменяет совпадения свидетельств.
func liveSameTechnology(label, earlier string) bool {
	if label == "" || earlier == "" {
		return false
	}
	return liveLabelKey(label) == liveLabelKey(earlier)
}

// Модель пишет «ё»/«е», регистр и знаки как придётся, а liveDiscoveryLabelKey от кириллицы не оставляет ничего.
func liveLabelKey(label string) string {
	return strings.Join(liveWords(strings.ReplaceAll(strings.ToLower(label), "ё", "е")), " ")
}

func liveVariantLabels(label, earlier string) bool {
	short, long := liveLabelRoots(label), liveLabelRoots(earlier)
	if len(short) > len(long) {
		short, long = long, short
	}
	return len(short) >= 2 && 2*len(short) > len(long) && liveRootsWithin(short, long)
}

// Повтор направления занимает место ответа; код — дубль, потому что добор дубли не берёт.
func liveDropUmbrella(umbrella LiveTopic, candidates []LiveTopic, rejected []LiveRejection,
) ([]LiveTopic, []LiveRejection) {
	kept := make([]LiveTopic, 0, len(candidates))
	for _, candidate := range candidates {
		if phrase := liveUmbrellaRestated(umbrella, candidate); phrase != "" {
			rejected = append(rejected, liveRejection(candidate, liveRejectDuplicate, fmt.Sprintf(
				"повторяет само направление «%s», а не называет технологию внутри него", phrase)))
			continue
		}
		kept = append(kept, candidate)
	}
	return kept, rejected
}

// Каждая формулировка сравнивается отдельно, чтобы русское сверялось с русским без переводчика.
func liveUmbrellaRestated(umbrella, topic LiveTopic) string {
	name := liveLabelRoots(liveCandidateName(topic))
	if len(name) == 0 {
		return ""
	}
	phrases := []string{umbrella.Label, umbrella.Name}
	// Первый термин перевода — сам запрос; остальные называют части области, и кандидат с таким именем — ответ.
	if len(umbrella.Query.Terms) > 0 {
		phrases = append(phrases, umbrella.Query.Terms[0])
	}
	for _, phrase := range phrases {
		roots := liveLabelRoots(phrase)
		if len(roots) > 0 && 2*len(name) >= len(roots) && liveRootsWithin(name, roots) {
			return phrase
		}
	}
	return ""
}

// Слова-оболочки выпадают: «метод сжатия KV-кэша» — то же, что «сжатие KV-кэша».
func liveLabelRoots(label string) []string {
	roots := slices.DeleteFunc(liveRoots(label), func(root string) bool {
		return slices.ContainsFunc([]string{"метод", "подход", "способ", "техник", "технологи", "technology"},
			func(generic string) bool { return liveRootsMatch(root, generic) })
	})
	slices.Sort(roots)
	return slices.Compact(roots)
}

func liveRootsWithin(short, long []string) bool {
	return !slices.ContainsFunc(short, func(root string) bool {
		return !slices.ContainsFunc(long, func(other string) bool { return liveRootsMatch(root, other) })
	})
}

// Основа не короче четырёх букв и не больше чем на три короче, чтобы «сет» не совпал с «сетчатк».
func liveRootsMatch(a, b string) bool {
	if a == b {
		return true
	}
	short, long := utf8.RuneCountInString(a), utf8.RuneCountInString(b)
	if short > long {
		a, b, short, long = b, a, long, short
	}
	return short >= 4 && long-short <= 3 && strings.HasPrefix(b, a)
}

// Громкость противоположна слабому сигналу, поэтому не число документов; имя — последний ключ для стабильного порядка.
func liveCandidateOrder(a, b LiveTopic) int {
	// Метод одной статьи не стоит выше подтверждённого тренда ни при какой оценке модели: вердикта «слабый
	// сигнал» у него нет (signalScore.ts), а модель ставит малоизвестным фразам высокую оценку за редкость.
	if order := liveConfirmedOrder(a, b); order != 0 {
		return order
	}
	// Два заполненных поля не должны вытеснять карточку, прошедшую весь обязательный контракт.
	if order := cmp.Compare(liveRequiredCardFields(b), liveRequiredCardFields(a)); order != 0 {
		return order
	}
	// Решение выше воздержания и отсутствующей оценки; кандидаты без решения сравниваются по правилам.
	decidedA := liveModelDecision(a.ModelScore)
	decidedB := liveModelDecision(b.ModelScore)
	switch {
	case decidedA != decidedB:
		if !decidedA {
			return 1
		}
		return -1
	case decidedA:
		if order := -cmp.Compare(a.ModelScore.Value, b.ModelScore.Value); order != 0 {
			return order
		}
	}
	value := func(topic LiveTopic) float64 {
		if topic.Confidence == nil {
			return 0
		}
		return topic.Confidence.Value
	}
	spread := func(topic LiveTopic) float64 {
		if topic.TopGroupShare == nil {
			return 1
		}
		return *topic.TopGroupShare
	}
	return cmp.Or(
		cmp.Compare(liveEvidenceRank(a.EvidenceLevel), liveEvidenceRank(b.EvidenceLevel)),
		-cmp.Compare(value(a), value(b)),
		cmp.Compare(spread(a), spread(b)),
		cmp.Compare(liveEmergenceRank(a.Emergence), liveEmergenceRank(b.Emergence)),
		cmp.Compare(a.Name, b.Name))
}

func liveConfirmedOrder(a, b LiveTopic) int {
	return -cmp.Compare(min(liveIndependentSources(a), liveMinIndependent), min(liveIndependentSources(b), liveMinIndependent))
}

func liveEvidenceRank(level string) int {
	return slices.IndexFunc(liveEvidenceTiers(), func(tier liveTier) bool { return tier.level == level })
}

// Запись зонтика наследуется, только если сама называет кандидата: патент о другой технологии его не подтверждает.
func liveCandidateTopic(umbrella LiveTopic, candidate liveCandidate, periods []LivePeriod,
	bar liveMarketBar, emergence *LiveEmergence,
) LiveTopic {
	family := liveCandidateFamily(candidate)
	works, records := liveSplitDocuments(candidate.papers)
	topic := LiveTopic{
		StudyTopic: StudyTopic{
			ID: family.id, PaperIDs: make([]string, 0, len(works)),
			Series: liveSeries(periods, candidate.dated()), Stage: studyStageUnassessed,
			Momentum: studyUnknown, Screening: liveCoherenceQuery, EvidenceIDs: []string{},
			External: []StudyEvent{},
		},
		Name: family.name, Query: family.query, Source: "candidate", Curated: false,
		PapersCount: len(works), DocumentsCount: len(candidate.papers), RecordsCount: len(records),
		Limits: liveTemporalCoverageLimits(umbrella.Limits), Branch: umbrella.Branch, Emergence: emergence,
	}
	for _, paper := range works {
		topic.PaperIDs = append(topic.PaperIDs, paper.ID)
	}
	for _, record := range records {
		topic.EvidenceIDs = append(topic.EvidenceIDs, record.ID)
	}
	tokens := liveWords(cmp.Or(candidate.surface, candidate.gram))
	for _, event := range umbrella.External {
		if liveMentions(event.Title, tokens) {
			topic.External = append(topic.External, event)
		}
	}
	// Запись, на которую опирается кандидат, остаётся в канале и без его имени в заголовке: иначе её издатель
	// не считался бы независимым подтверждением, а паспорт — проверкой на шум.
	cited := make(map[string]bool, len(records))
	for _, record := range records {
		cited[record.ID] = true
	}
	topic.Patents = liveChannelMentions(umbrella.Patents, tokens, cited)
	topic.PatentsCN = liveChannelMentions(umbrella.PatentsCN, tokens, cited)
	topic.Registry = liveChannelMentions(umbrella.Registry, tokens, cited)
	topic.Web = liveChannelMentions(umbrella.Web, tokens, cited)
	topic.Research = liveChannelMentions(umbrella.Research, tokens, cited)
	liveResearchCounts(topic.Research, periods)
	liveAssess(&topic, periods, candidate.dated())
	// Правило одной группы спрашивает обо всех коллективах за технологией, а не о вынесших фразу в заголовок.
	topic.Groups, topic.TopGroupShare = studyConcentration(candidate.papers, len(candidate.papers) > 0)
	topic.Comparison = liveCompare(periods, candidate.dated())
	topic.Adoption = liveAdoptionOf(liveTitles(candidate.papers))
	topic.Market = liveMarketMaturity(topic, bar).field()
	topic.Confidence = new(liveConfidence(topic, bar))
	return topic
}

// liveMentions сравнивает целые токены, а не подстроки: «agent» не должен совпасть с «agentic».
func liveMentions(title string, tokens []string) bool {
	if len(tokens) == 0 {
		return false
	}
	words := liveWords(title)
	stems := make([]string, len(words))
	for i, word := range words {
		stems[i] = liveStem(word)
	}
	for _, token := range tokens {
		if !slices.Contains(stems, liveStem(token)) {
			return false
		}
	}
	return true
}

// Итог зонтика приписал бы кандидату чужие свидетельства, поэтому итоги пересчитываются по оставленным записям.
func liveChannelMentions(channel *LiveChannel, tokens []string, cited map[string]bool) *LiveChannel {
	if channel == nil {
		return nil
	}
	records := []LiveSourceRecord{}
	for _, record := range channel.Records {
		if liveMentions(record.Title, tokens) || cited[liveEvidenceID(record)] {
			records = append(records, record)
		}
	}
	if len(records) == 0 {
		return nil
	}
	return &LiveChannel{SourceSet: channel.SourceSet, Source: channel.Source, ObservedAt: channel.ObservedAt,
		Total: len(records), Partial: channel.Partial, Records: records, Counts: []*int{},
		Limits: append(slices.Clone(channel.Limits), "записи отобраны из общего сбора по запросу: в названии "+
			"встречается "+strings.Join(tokens, " ")+" либо запись названа источником кандидата")}
}

// Первый проход наследования — по английской n-грамме; русское название появляется после отбора, отсюда второй проход.

const (
	// Одиночное «система» стоит в половине заголовков лент.
	liveLabelMinWords = 2
	// liveRootMinBytes — три кириллические буквы в UTF-8: более короткая основа совпадает с половиной языка.
	liveRootMinBytes = 6
)

func liveInheritByLabel(umbrella LiveTopic, candidates []LiveTopic,
	rejected []LiveRejection, bar liveMarketBar,
) ([]LiveTopic, []LiveRejection) {
	kept := make([]LiveTopic, 0, len(candidates))
	for i := range candidates {
		liveInheritRecords(umbrella, &candidates[i])
		market := liveMarketMaturity(candidates[i], bar)
		candidates[i].Market = market.field()
		// Уверенность — после наследования, иначе «рынок ещё не сложился» считалось бы по нулевому рынку.
		candidates[i].Confidence = new(liveConfidence(candidates[i], bar))
		if market.mature {
			rejected = append(rejected, liveRejection(candidates[i], liveRejectMature, market.reason()))
			continue
		}
		kept = append(kept, candidates[i])
	}
	return kept, rejected
}

// Стадия, рост и уверенность не пересчитываются: они измерены по окну научных работ.
func liveInheritRecords(umbrella LiveTopic, candidate *LiveTopic) {
	tokens := liveRoots(candidate.Label)
	if len(tokens) < liveLabelMinWords {
		return
	}
	titles := []string{}
	for _, pair := range []struct {
		from *LiveChannel
		into **LiveChannel
	}{
		{umbrella.Patents, &candidate.Patents}, {umbrella.PatentsCN, &candidate.PatentsCN},
		{umbrella.Registry, &candidate.Registry}, {umbrella.Web, &candidate.Web},
		{umbrella.Research, &candidate.Research},
	} {
		for _, record := range liveLabelRecords(pair.from, pair.into, candidate.Label, tokens) {
			candidate.EvidenceIDs = append(candidate.EvidenceIDs, liveRecordPrefix+cmp.Or(record.ID, record.URL))
			titles = append(titles, record.Title)
		}
	}
	candidate.RecordsCount += len(titles)
	candidate.DocumentsCount += len(titles)
	// Русская новость про пилот — ровно тот документ, что поднимает стадию внедрения.
	if len(titles) > 0 {
		candidate.Adoption = liveAdoptionMerge(candidate.Adoption, liveAdoptionOf(titles))
	}
}

func liveLabelRecords(from *LiveChannel, into **LiveChannel, label string, tokens []string) []LiveSourceRecord {
	if from == nil {
		return nil
	}
	channel := LiveChannel{SourceSet: from.SourceSet, Source: from.Source, ObservedAt: from.ObservedAt,
		Partial: from.Partial, Counts: []*int{}, Limits: from.Limits, Records: []LiveSourceRecord{}}
	if *into != nil {
		channel = **into
	}
	have := make(map[string]bool, len(channel.Records))
	for _, record := range channel.Records {
		have[liveRecordKey(record)] = true
	}
	added := []LiveSourceRecord{}
	for _, record := range from.Records {
		if !have[liveRecordKey(record)] && liveNamed(record.Title, tokens) {
			added = append(added, record)
		}
	}
	if len(added) == 0 {
		return nil
	}
	channel.Records = append(slices.Clone(channel.Records), added...)
	channel.Total = len(channel.Records)
	channel.Limits = append(slices.Clone(channel.Limits), fmt.Sprintf(
		"ещё %d записей отобраны по русскому названию «%s»: в заголовке по основе слова встречается %s",
		len(added), label, strings.Join(tokens, " ")))
	*into = &channel
	return added
}

func liveRecordKey(record LiveSourceRecord) string {
	return cmp.Or(record.ID, record.URL, record.Title)
}

func liveNamed(title string, tokens []string) bool {
	if len(tokens) == 0 {
		return false
	}
	roots := liveRoots(title)
	for _, token := range tokens {
		if !slices.Contains(roots, token) {
			return false
		}
	}
	return true
}

// «ё» сводится к «е»: ленты пишут его как придётся.
func liveRoots(text string) []string {
	roots := []string{}
	for _, word := range liveWords(strings.ReplaceAll(strings.ToLower(text), "ё", "е")) {
		if liveStopword(word) || liveRussianFunctionWord(word) {
			continue
		}
		roots = append(roots, liveRoot(word))
	}
	return roots
}

// Снимается одно окончание: чередования в корне не сводятся, а стеммер тянет зависимость.
// То же правило повторено в backend/trend-sources — общего модуля нет.
func liveRoot(token string) string {
	first, _ := utf8.DecodeRuneInString(token)
	if !unicode.Is(unicode.Cyrillic, first) {
		return liveStem(token)
	}
	for size := 6; size >= 2; size -= 2 {
		if len(token)-size >= liveRootMinBytes && liveRussianEnding(token[len(token)-size:]) {
			return token[:len(token)-size]
		}
	}
	return token
}

// Название технологии — именная группа: спрягаемые формы сюда не входят.
func liveRussianEnding(suffix string) bool {
	switch suffix {
	case "ами", "ями", "ыми", "ими", "ого", "его", "ому", "ему":
		return true
	case "ов", "ев", "ей", "ой", "ый", "ий", "ая", "яя", "ое", "ее", "ые", "ие", "ых", "их", "ым", "им",
		"ую", "юю", "ам", "ям", "ах", "ях", "ом", "ем", "ью":
		return true
	case "а", "я", "ы", "и", "о", "е", "у", "ю", "ь", "й":
		return true
	}
	return false
}

// Служебное слово ничего не называет: требовать его в заголовке значит не найти ничего.
func liveRussianFunctionWord(token string) bool {
	switch token {
	case "и", "а", "но", "или", "же", "не", "ни", "то", "это", "как", "что", "чтобы",
		"в", "во", "на", "с", "со", "к", "ко", "о", "об", "обо", "по", "за", "из", "изо",
		"от", "до", "у", "при", "для", "про", "через", "над", "под", "без", "между", "их", "его", "ее":
		return true
	}
	return false
}

func liveCandidateName(topic LiveTopic) string {
	if topic.Label != "" {
		return topic.Label
	}
	return topic.Name
}

func liveWorkLine(paper StudyPaper, meta LivePaperMetadata) string {
	line := paper.Title
	if len(paper.Date) >= 4 {
		line += " (" + paper.Date[:4] + ")"
	}
	if meta.Abstract != "" {
		abstract := meta.Abstract
		if utf8.RuneCountInString(abstract) > liveCardAbstract {
			abstract = string([]rune(abstract)[:liveCardAbstract])
		}
		line += ": " + abstract
	}
	return line
}

// Эталон почти не опирается на науку: записи каналов приводятся к виду работы и порождают кандидатов наравне с ней.

const liveRecordPrefix = "rec:"

// Издатель стоит на месте авторов, поэтому независимые издатели считаются независимыми группами.
func liveChannelDocuments(topic LiveTopic) []StudyPaper {
	// Каналы собираются не по запросу, и без отбора по теме реестр отдаёт свои записи любому направлению.
	terms := liveQueryTerms(topic)
	documents := []StudyPaper{}
	for _, channel := range []*LiveChannel{topic.Patents, topic.PatentsCN, topic.Registry, topic.Web, topic.Corpus, topic.Research} {
		if channel == nil {
			continue
		}
		for _, record := range channel.Records {
			text := record.Title
			if channel == topic.Research {
				text += " " + record.Excerpt
			}
			if document, ok := liveRecordDocument(record); ok && liveAboutQuery(text, terms) {
				documents = append(documents, document)
			}
		}
	}
	for _, event := range topic.External {
		if event.Title == "" || event.Date == "" || !liveAboutQuery(event.Title, terms) {
			continue
		}
		documents = append(documents, StudyPaper{
			ID: liveRecordPrefix + cmp.Or(event.URL, event.Title), Title: event.Title,
			Date: event.Date[:min(len(event.Date), 10)], URL: event.URL,
			Authors: []string{cmp.Or(event.Org, event.Community)},
		})
	}
	return documents
}

func liveRecordDocument(record LiveSourceRecord) (StudyPaper, bool) {
	// Позиция инвестора полезна в контексте, но не подтверждает существование или работоспособность технологии.
	if liveInvestorContext(record.Genre) {
		return StudyPaper{}, false
	}
	authors := record.Authors
	if len(authors) == 0 {
		authors = []string{cmp.Or(record.Source, passportHost(record.URL))}
		if liveJournalProvider(record.Source) != "" {
			// Два агрегатора не подтверждают существование двух авторских коллективов.
			authors = []string{"research:unknown-authors"}
		}
	}
	date := cmp.Or(record.Date, record.PublishedAt, record.AvailableAt)
	if record.Title == "" || date == "" {
		return StudyPaper{}, false
	}
	return StudyPaper{
		ID: liveRecordPrefix + cmp.Or(record.ID, record.URL), Title: record.Title,
		Date: date[:min(len(date), 10)], URL: record.URL,
		Authors: authors, CanonicalID: liveResearchDOI(record.DOI),
	}, true
}

// Рост на одних самопубликациях — не основание.
func liveCandidateRefusal(topic LiveTopic, documents []StudyPaper, tier liveTier,
	bar liveMarketBar,
) (code, reason string) {
	if code, reason = liveRejectReason(topic, tier, bar); code != "" {
		return code, reason
	}
	if liveNoiseOnly(documents) {
		return liveRejectHype, liveHypeReason(len(documents))
	}
	if !liveLoweredOnly(documents) {
		return "", ""
	}
	return liveRejectThin, fmt.Sprintf("недостаточно независимых свидетельств: все %d документов — источники "+
		"пониженного доверия (домены вне реестра доверия, самопубликации кода); они годятся как первичный "+
		"индикатор, но не как единственное основание", len(documents))
}

// liveNoise — то, что ТЗ называет лишь первичным индикатором: соцсеть, блог, агрегатор, пресс-релиз. Домен вне
// реестра доверия шумом не считается: его не проверили, а не уличили.
func liveNoise(address, title string) bool {
	passport, genre := liveApplyGenre(ClassifySource(address), address, title)
	return passport.Trust == TrustLowered && (slices.Contains([]string{PublisherSocial, PublisherBlog,
		PublisherAggregator, PublisherVendor, PublisherMedia}, passport.Publisher) ||
		slices.Contains([]string{GenreAnnouncement, GenreBlog, GenreOpinion}, genre))
}

// Научная работа — независимое основание, поэтому шумом считается только выдача из одних записей.
func liveNoiseOnly(documents []StudyPaper) bool {
	return len(documents) > 0 && !slices.ContainsFunc(documents, func(document StudyPaper) bool {
		return !strings.HasPrefix(document.ID, liveRecordPrefix) || !liveNoise(document.URL, document.Title)
	})
}

// К допуску карточки каналы уже дополнены рыночными записями: проверяется всё, на что опирается кандидат.
func liveTopicEvidence(topic LiveTopic) []StudyPaper {
	documents := make([]StudyPaper, 0, len(topic.PaperIDs)+len(topic.External))
	for _, id := range topic.PaperIDs {
		documents = append(documents, StudyPaper{ID: id})
	}
	for _, channel := range []*LiveChannel{topic.Patents, topic.PatentsCN, topic.Registry, topic.Web} {
		for _, record := range liveChannelRecords(channel) {
			documents = append(documents, StudyPaper{ID: liveEvidenceID(record), URL: record.URL, Title: record.Title})
		}
	}
	for _, event := range topic.External {
		documents = append(documents, StudyPaper{ID: liveRecordPrefix + event.URL, URL: event.URL, Title: event.Title})
	}
	return documents
}

func liveHypeReason(documents int) string {
	return fmt.Sprintf("маркетинговый шум: все %d свидетельств — пресс-релизы, соцсети, блоги или агрегаторы; "+
		"ТЗ допускает их как первичный индикатор, но не как единственное основание, а научной работы или "+
		"записи обычного доверия нет", documents)
}

// ТЗ разрешает записи пониженного доверия лишь как первичный индикатор; научная работа — независимое основание.
func liveLoweredOnly(documents []StudyPaper) bool {
	for _, document := range documents {
		if !strings.HasPrefix(document.ID, liveRecordPrefix) {
			return false
		}
		if passport, _ := liveApplyGenre(ClassifySource(document.URL), document.URL, document.Title); passport.Trust !=
			TrustLowered {
			return false
		}
	}
	return len(documents) > 0
}

// Счёт вперемешку приписал бы поиску науку, которой нет.
func liveSplitDocuments(documents []StudyPaper) (papers, records []StudyPaper) {
	for _, document := range documents {
		if strings.HasPrefix(document.ID, liveRecordPrefix) {
			records = append(records, document)
			continue
		}
		papers = append(papers, document)
	}
	return papers, records
}

// Переведённые термины точнее исходной формулировки: запрос русский, а записи каналов английские.
func liveQueryTerms(topic LiveTopic) [][]string {
	phrases := topic.Query.Terms
	if len(phrases) == 0 {
		phrases = []string{topic.Name}
	}
	terms := make([][]string, 0, len(phrases))
	for _, phrase := range phrases {
		words := []string{}
		for _, word := range liveWords(phrase) {
			if !liveStopword(word) {
				words = append(words, liveStem(word))
			}
		}
		if len(words) > 0 {
			terms = append(terms, words)
		}
	}
	return terms
}

// Без фраз не допускается ничего, иначе нефильтрованный канал заполнит выдачу.
func liveAboutQuery(title string, terms [][]string) bool {
	if len(terms) == 0 {
		return false
	}
	words := liveWords(title)
	stems := make([]string, len(words))
	for i, word := range words {
		stems[i] = liveStem(word)
	}
	for _, phrase := range terms {
		matched := true
		for _, word := range phrase {
			if !slices.Contains(stems, word) {
				matched = false
				break
			}
		}
		if matched {
			return true
		}
	}
	return false
}

// Заголовок — единственный текст, который есть у работы arXiv, патента, записи реестра и новости одинаково.
func liveTitles(documents []StudyPaper) []string {
	titles := make([]string, 0, len(documents))
	for _, document := range documents {
		if document.Title != "" {
			titles = append(titles, document.Title)
		}
	}
	return titles
}
