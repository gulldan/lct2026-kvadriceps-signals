package application

import "fmt"

// Покрытие правил отделено от обученного классификатора: постоянный знаменатель не даёт процента выше
// после потери данных, а неизвестные признаки перечисляются отдельно.
const (
	liveConfidenceMethod    = "evidence-coverage-rules-v4"
	liveConfidenceThreshold = 0.75
	// liveConfidenceTopGroup — доля крупнейшей авторской группы, выше которой направление — работа одной
	// лаборатории; граница та же, что у studyConcentrationLevel, чтобы карточка не спорила сама с собой.
	liveConfidenceTopGroup = 0.5
	liveStageEarly         = "early_signal"
	liveStageEmerging      = "emerging_signal"
	liveStageEstablished   = "established"
	liveMomentumUp         = "up"
)

// LiveConfidence показывает долю подтверждённых правил, а не вероятность класса.
type LiveConfidence struct {
	Value  float64 `json:"value"`
	Passed int     `json:"passed"`
	// Старые сохранённые ответы считали иначе и могут иметь другой знаменатель.
	Total    int    `json:"total"`
	Measured int    `json:"measured"`
	Method   string `json:"method"`
	// Threshold приезжает числом, а не словами перевода: сдвинется константа — подпись и выгрузка не соврут.
	Threshold float64 `json:"threshold"`
	Basis     string  `json:"basis"`
	// Reasons — выполненные проверки: ТЗ требует объяснять и положительное решение.
	Reasons []string `json:"reasons,omitempty"`
	Missing []string `json:"missing,omitempty"`
	// Непроверенные — словами, а не счётчиком: иначе «6 из 6» на тонком корпусе выглядит как на полном.
	Unmeasured []string `json:"unmeasured,omitempty"`
	AboveMin   bool     `json:"above_threshold"`
}

func liveConfidence(topic LiveTopic, bar liveMarketBar) LiveConfidence {
	type rule struct {
		name   string
		gap    string
		passed bool
	}
	// Стадии мало: узкий корпус даёт раннюю стадию и ровному потоку через всё окно, поэтому проверка сверяется
	// с той же историей, по которой строится отказ.
	limited := liveTemporalCoverageLimited(topic.Limits)
	young := !limited && (topic.Stage == liveStageEmerging || topic.Stage == liveStageEarly)
	recent := young && !liveScienceAge(topic).long
	// Неизмеренная стадия — «измерить не удалось», а не минус за карточкой.
	stageKnown := !limited && (young || topic.Stage == liveStageEstablished)
	stageGap := liveGap(stageKnown, "стадия активности внутри окна не измерена; время появления технологии неизвестно")
	growthGap := liveGap(topic.Momentum != "" && topic.Momentum != studyUnknown,
		"рост доли не измерен: в окне нет двух полных полугодий, которые можно сравнить")
	if limited {
		stageGap = "стадия активности не измерена: сбор усечён, месячные количества описывают только выборку"
		growthGap = "рост доли не измерен: сбор усечён, сравнение временных окон не подтверждено"
	}
	// У зонтика поиска набора для карты Юна нет, а усечённый сбор даёт доли выборки, а не месяцев.
	emergenceGap := ""
	switch {
	case limited:
		emergenceGap = "карта появления ключевых слов не измерена: сбор усечён, доли месяцев описывают только выборку"
	case topic.Emergence == nil:
		emergenceGap = "карта появления ключевых слов не построена: направление измерено вне набора, медианы сравнивать не с чем"
	case topic.Emergence.Class == liveEmergenceUnmeasured:
		emergenceGap = "карта появления ключевых слов " + topic.Emergence.Basis
	}
	market := liveMarketMaturity(topic, bar)
	rules := []rule{
		{"направление имеет признаки роста внутри окна наблюдения",
			stageGap,
			topic.FirstYear != "" && recent},
		{"работы выходят не в одном периоде", "", topic.ActiveYears >= 2},
		{"доля направления растёт", growthGap, !limited && topic.Momentum == liveMomentumUp},
		{"не меньше трёх авторских групп", "", topic.Groups >= 3},
		// Распределение работ по авторским группам различает тему одной лаборатории и тему, за которую взялись
		// независимо; «есть свежая научная поддержка» выполнялось бы у всех по построению.
		{"работы не собраны в одной авторской группе",
			liveGap(topic.TopGroupShare != nil, "распределение работ по авторским группам не измерено"),
			topic.TopGroupShare != nil && *topic.TopGroupShare <= liveConfidenceTopGroup},
		// Слабый и сильный сигнал по Юну оба растут быстрее набора, различает их только уровень видимости.
		{"по карте появления ключевых слов направление — слабый или сильный сигнал", emergenceGap,
			topic.Emergence.classified(liveEmergenceWeak) || topic.Emergence.classified(liveEmergenceStrong)},
		// Зрелость рынка — отдельный факт, измеренный по другим данным, а не повтор проверки стадии.
		{"ранняя стадия технологии подтверждена источником",
			liveGap(market.known,
				"зрелость рынка не подтверждена прямым свидетельством; количество публикаций её не определяет"),
			market.known && !market.mature},
		{"есть подтверждающие свидетельства вне науки", "", len(topic.External) > 0 ||
			liveChannelHasRecords(topic.Patents) || liveChannelHasRecords(topic.PatentsCN) ||
			liveChannelHasRecords(topic.Registry) || liveChannelHasRecords(topic.Web)},
	}
	out := LiveConfidence{Method: liveConfidenceMethod, Threshold: liveConfidenceThreshold}
	for _, r := range rules {
		switch {
		case r.gap != "":
			out.Unmeasured = append(out.Unmeasured, r.gap)
		case r.passed:
			out.Passed++
			out.Reasons = append(out.Reasons, r.name)
		default:
			out.Missing = append(out.Missing, r.name)
		}
	}
	out.Total = len(rules)
	out.Measured = out.Total - len(out.Unmeasured)
	if out.Total > 0 {
		out.Value = float64(out.Passed) / float64(out.Total)
	}
	out.AboveMin = out.Value > liveConfidenceThreshold
	out.Basis = fmt.Sprintf("подтверждено %d из %d правил; измерено %d", out.Passed, out.Total, out.Measured)
	// Число неизмеренных — в основании: оно объясняет одинаковую долю при разном корпусе.
	if len(out.Unmeasured) > 0 {
		out.Basis += fmt.Sprintf("; %d правил не измерено и не засчитано", len(out.Unmeasured))
	}
	out.Basis += "; это доля пройденных правил, а не вероятность"
	return out
}

func liveGap(measured bool, gap string) string {
	if measured {
		return ""
	}
	return gap
}

func liveChannelHasRecords(channel *LiveChannel) bool {
	return channel != nil && len(channel.Records) > 0
}
