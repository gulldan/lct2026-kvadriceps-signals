package application

import (
	"slices"
	"strings"
	"testing"
)

func liveWithoutTopUp(selected []LiveTopic) []LiveTopic {
	return slices.DeleteFunc(slices.Clone(selected), func(topic LiveTopic) bool { return topic.Shortfall != nil })
}

// Добор закрывает недобор волн до пятнадцати, но исключения ТЗ не размывает, а ответ с ним проходит границу хранилища.
func TestLiveTopUpFillsFifteenWithinExclusions(t *testing.T) {
	const query = "квантовое измерение"
	service, candidates, documents, _ := liveWaveFixture(t, 30)
	for i := range candidates {
		qualification, err := service.qualify(t.Context(), query, candidates[i].Name,
			[]LiveQualificationDocument{{ID: documents[i].ID, Text: documents[i].Title}})
		if err != nil {
			t.Fatal(err)
		}
		candidates[i].Qualification = &qualification
	}
	umbrella := LiveTopic{Label: "Квантовое измерение"}
	established, marketMature, unverified, restated := candidates[3], candidates[4], candidates[6], candidates[7]
	established.Stage = liveStageEstablished
	marketMature.Market = &LiveMarket{Status: liveMarketMature}
	unverified.Qualification = nil
	restated.Label = "Квантовое измерение"
	rejected := make([]LiveRejection, 0, len(candidates))
	rejected = append(rejected, liveRejection(established, liveRejectThin, "мало свидетельств"),
		liveRejection(marketMature, liveRejectRank, "ниже первых"),
		liveRejection(candidates[5], liveRejectNotTechnology, "не технология"),
		liveRejection(unverified, liveRejectBudget, "не проверено"),
		liveRejection(restated, liveRejectRank, "ниже первых"))
	for _, candidate := range candidates[8:20] {
		rejected = append(rejected, liveRejection(candidate, liveRejectCard, "нет кейса"))
	}
	// Непроверенные направления карты идут раньше n-грамм, а пустая проверка не выдаётся за пройденную.
	for _, candidate := range candidates[20:] {
		candidate.ID = liveDirectionPrefix + strings.ToLower(candidate.Name)
		candidate.Qualification = &LiveQualification{}
		rejected = append(rejected, liveRejection(candidate, liveRejectUnqualified, "проверка не завершена"))
	}
	got, kept := liveTopUp(umbrella, slices.Clone(candidates[:3]), rejected, nil)
	if len(got) != liveTopCandidates || len(liveWithoutTopUp(got)) != 3 || len(got)+len(kept) != 3+len(rejected) {
		t.Fatalf("top-up did not fill fifteen: selected=%d strict=%d rejected=%d",
			len(got), len(liveWithoutTopUp(got)), len(kept))
	}
	for i, candidate := range got[3:] {
		extracted := liveFromDirection(candidate)
		if candidate.Shortfall == nil || extracted != (i < 10) || extracted && candidate.Qualification != nil ||
			!extracted && (candidate.Shortfall.Code != liveRejectCard || candidate.Card == nil || candidate.Card.Summary == "") {
			t.Fatalf("top-up %d broke order, mark or summary: %s %+v", i, candidate.ID, candidate.Shortfall)
		}
	}
	// Непроверенное извлечение может быть общим понятием («большие языковые модели») и не добирается.
	general := candidates[21]
	general.ID, general.Qualification = liveExtractPrefix+"large language models", nil
	rejected = append(rejected[:5:5], liveRejection(general, liveRejectUnqualified, "проверка не завершена"))
	// Исключённые проверяются напрямую: при запасе допустимых они и так не дошли бы до очереди.
	for _, excluded := range rejected {
		if liveTopUpAllowed(umbrella, excluded) {
			t.Fatalf("excluded candidate is eligible for top-up: %s %s", excluded.Code, excluded.topic.ID)
		}
	}
	// Решение модели этапа 1 «слабый сигнал» допускает непроверенное извлечение к добору.
	decided := general
	decided.ModelScore = &LiveModelScore{Label: liveClassifierWeak, Value: 0.9, Threshold: 0.23}
	if !liveTopUpAllowed(umbrella, liveRejection(decided, liveRejectUnqualified, "проверка не завершена")) {
		t.Fatal("an unverified extraction the model called a weak signal was not eligible for top-up")
	}
	scope := liveFixtureQueryScope(query)
	if !liveValidAnswerScope(scope, query, got) {
		t.Fatal("an answer with marked top-ups was refused by the store boundary")
	}
	foreign := *got[14].Qualification
	foreign.Scope = liveFixtureQueryScope("другой запрос")
	got[14].Qualification = &foreign
	if liveValidAnswerScope(scope, query, got) {
		t.Fatal("a top-up checked against another scope was accepted")
	}
}
