package application

import (
	"cmp"
	"context"
	"log/slog"
	"slices"
	"strings"
)

// Решение владельца: ответ всегда из пятнадцати, недобор волн закрывают отказы с явной пометкой «ниже планки».
// Список белый: новый код исключения по умолчанию не добирается, иначе размывается логика исключения из ТЗ.
// Число — близость к проходу: прошедший всё, кроме ранга, раньше непроверенного; -1 — не добирается.
func liveTopUpRank(code string) int {
	switch code {
	case liveRejectRank:
		return 0
	case liveLimitModelFailed:
		return 1
	case liveRejectModel:
		return 2
	case liveRejectCard:
		return 3
	case liveRejectBudget, liveRejectUnqualified:
		return 4
	case liveRejectThin, liveRejectOneGroup, liveRejectUnassessed:
		return 5
	}
	return -1
}

// Чужой по теме корпус не добирается: пустая выдача на нерелевантный запрос входит в приёмку.
func liveTopUp(umbrella LiveTopic, selected []LiveTopic, rejected []LiveRejection,
	metadata map[string]LivePaperMetadata,
) ([]LiveTopic, []LiveRejection) {
	if len(selected) >= liveTopCandidates || liveOffTopicAnswer(umbrella) {
		return selected, rejected
	}
	order := []int{}
	for i, rejection := range rejected {
		if liveTopUpAllowed(umbrella, rejection) {
			order = append(order, i)
		}
	}
	slices.SortStableFunc(order, func(a, b int) int {
		x, y := *rejected[a].topic, *rejected[b].topic
		return cmp.Or(cmp.Compare(liveWithoutCard(x), liveWithoutCard(y)), liveConfirmedOrder(x, y),
			// Названные извлечением и направления карты раньше n-грамм: n-граммы — широкие области и обрывки фраз.
			cmp.Compare(liveTopUpPhrase(x), liveTopUpPhrase(y)),
			cmp.Compare(liveTopUpUnverified(x), liveTopUpUnverified(y)),
			cmp.Compare(liveTopUpRank(rejected[a].Code), liveTopUpRank(rejected[b].Code)),
			cmp.Compare(liveTopUpLate(x), liveTopUpLate(y)), liveCandidateOrder(x, y))
	})
	start, taken := len(selected), map[int]bool{}
	for _, i := range order {
		if len(selected) >= liveTopCandidates {
			break
		}
		candidate := *rejected[i].topic
		candidate.Shortfall = &LiveShortfall{Code: rejected[i].Code, Reason: rejected[i].Reason}
		// Оборванная до ответа модели проверка пуста: на экране она читалась бы как пройденная без причины.
		if candidate.Qualification != nil && candidate.Qualification.Scope == nil {
			candidate.Qualification = nil
		}
		if kept, _ := liveDedupeSelected(append(slices.Clone(selected), candidate), nil); len(kept) > len(selected) {
			selected, taken[i] = kept, true
		}
	}
	selected = liveFillFifteen(umbrella, selected, rejected, taken)
	liveFallbackSummaries(selected[start:], metadata)
	kept := make([]LiveRejection, 0, len(rejected)-len(taken))
	for i, rejection := range rejected {
		if !taken[i] {
			kept = append(kept, rejection)
		}
	}
	return selected, kept
}

// Непроверенная n-грамма — обрывок словосочетания, непроверенное направление карты — лишь пересечение корпусов, а
// зрелость и повтор направления ТЗ исключает при любом отказе.
func liveTopUpAllowed(umbrella LiveTopic, rejection LiveRejection) bool {
	topic := rejection.topic
	if liveTopUpRank(rejection.Code) < 0 || topic == nil {
		return false
	}
	// Замер 27.09: непроверенное извлечение добирало общие понятия («большие языковые модели», «свёрточные сети»):
	// без проверки сущности его зрелость неизвестна. Направление карты измерено радаром по полному окну, а
	// непроверенное извлечение добирается, только если модель этапа 1 признала его слабым сигналом.
	if liveTopUpUnverified(*topic) == 1 && !liveFromDirection(*topic) && !liveModelDecision(topic.ModelScore) {
		return false
	}
	mature := topic.Stage == liveStageEstablished || topic.Market != nil && topic.Market.Status == liveMarketMature ||
		topic.ModelScore != nil && topic.ModelScore.Label == liveClassifierMature
	return !mature && liveUmbrellaRestated(umbrella, *topic) == ""
}

func liveWithoutCard(topic LiveTopic) int {
	if topic.Card != nil && topic.Card.Summary != "" {
		return 0
	}
	return 1
}

func liveTopUpPhrase(topic LiveTopic) int {
	if liveNamedCandidate(topic) {
		return 0
	}
	return 1
}

func liveTopUpUnverified(topic LiveTopic) int {
	if liveVerifiedMechanism(topic) {
		return 0
	}
	return 1
}

func liveTopUpLate(topic LiveTopic) int {
	if topic.Stage == liveStageEarly || topic.Stage == liveStageEmerging {
		return 0
	}
	return 1
}

// scoreTopUpPool оценивает моделью этапа 1 лучших непроверенных кандидатов извлечения, когда волн не хватило до
// пятнадцати: их признаки (история термина, зрелость) отсекают общие и давние понятия. Срок поиска к этому моменту
// почти исчерпан, поэтому у оценки свой срок стадии; её сбой не помечает весь ответ «модель недоступна».
func (s *TrendLiveService) scoreTopUpPool(ctx context.Context, query string, umbrella LiveTopic, need int,
	rejected []LiveRejection, documents []StudyPaper, metadata map[string]LivePaperMetadata,
) []LiveRejection {
	if need <= 0 || s.classifier == nil || liveOffTopicAnswer(umbrella) {
		return rejected
	}
	pool := []LiveTopic{}
	for _, rejection := range rejected {
		topic := rejection.topic
		if topic != nil && topic.ModelScore == nil && liveTopUpRank(rejection.Code) >= 0 &&
			strings.HasPrefix(topic.ID, liveExtractPrefix) && liveTopUpUnverified(*topic) == 1 &&
			len(pool) < 2*need+2 {
			pool = append(pool, *topic)
		}
	}
	if len(pool) == 0 {
		return rejected
	}
	work, cancel := context.WithTimeout(context.WithoutCancel(ctx), liveClassifierStage)
	defer cancel()
	input := liveClassifierInput(query, s.classifierVersion, pool, documents, metadata)
	output, err := s.classifier(work, input)
	var scores map[string]LiveModelScore
	if err == nil {
		scores, err = liveValidatePredictions(input, output)
	}
	if err != nil {
		slog.Warn("radar top-up scores", "candidates", len(pool), "error", err)
		return rejected
	}
	return liveScoreRejected(rejected, scores)
}

// Решение владельца 27.09: в выдаче всегда пятнадцать. Если разрешённых отказов не хватило, добор идёт любыми
// оставшимися кандидатами, кроме повтора самого запроса; причина отказа видна на плашке каждой строки.
func liveFillFifteen(umbrella LiveTopic, selected []LiveTopic, rejected []LiveRejection, taken map[int]bool,
) []LiveTopic {
	rest := []int{}
	for i, rejection := range rejected {
		if !taken[i] && rejection.topic != nil && liveUmbrellaRestated(umbrella, *rejection.topic) == "" {
			rest = append(rest, i)
		}
	}
	slices.SortStableFunc(rest, func(a, b int) int { return liveCandidateOrder(*rejected[a].topic, *rejected[b].topic) })
	for _, i := range rest {
		if len(selected) >= liveTopCandidates {
			break
		}
		candidate := *rejected[i].topic
		candidate.Shortfall = &LiveShortfall{Code: rejected[i].Code, Reason: rejected[i].Reason}
		if candidate.Qualification != nil && candidate.Qualification.Scope == nil {
			candidate.Qualification = nil
		}
		if kept, _ := liveDedupeSelected(append(slices.Clone(selected), candidate), nil); len(kept) > len(selected) {
			selected, taken[i] = kept, true
		}
	}
	return selected
}
