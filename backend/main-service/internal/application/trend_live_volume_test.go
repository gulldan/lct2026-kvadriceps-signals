package application

import (
	"testing"
	"time"

	"github.com/example/main-service/internal/platform/aiclients"
)

func TestLiveVolumeCountsTextAndModelOnce(t *testing.T) {
	detail := LiveDetail{
		Papers: []StudyPaper{{ID: "p1", URL: "https://arxiv.org/abs/1", Title: "Кубиты"}},
		Topic: LiveTopic{
			Web: &LiveChannel{Records: []LiveSourceRecord{
				{URL: "https://cnews.ru/a", Title: "Новость"},
				{URL: "https://cnews.ru/a", Title: "Новость"},
			}},
			Corpus: &LiveChannel{Records: []LiveSourceRecord{{URL: "doc://1", Title: "Отчёт"}}},
		},
	}
	metadata := map[string]LivePaperMetadata{"p1": {Abstract: "Аннотация"}}
	ctx, tally := liveWithTally(t.Context())
	liveTallyOf(ctx).addLLM("qualify_candidate", aiclients.GenRequest{System: "s", Query: "q"},
		aiclients.GenResponse{Usage: aiclients.Usage{PromptTokens: 4350, CompletionTokens: 371}}, true, 65*time.Second)
	liveTallyOf(ctx).addLLM("review_candidate", aiclients.GenRequest{System: "s", Query: "q"},
		aiclients.GenResponse{}, false, 100*time.Second)
	liveTallyOf(ctx).addAnalysis(2906, 2777289, 20*time.Second)
	liveTallyOf(ctx).addClassifier(LiveClassifierRequest{Candidates: []LiveClassifierCandidate{
		{ID: "c1", Name: "Квантовый отжиг", Sources: []LiveClassifierSource{{ID: "p1", Title: "Кубиты", Abstract: "Аннотация"}}},
	}}, false)

	got := liveVolume(detail, metadata, 374183, tally)
	if got.Scanned != 374183 || got.Fetched != 4 || got.Processed != 3 {
		t.Fatalf("воронка неверна: %+v", got)
	}
	// «Кубиты» + пробел + «Аннотация» = 16, «Новость» = 7, «Отчёт» = 5.
	if got.Chars != 28 || got.Pages != 1 || got.Tokens != 7 {
		t.Errorf("объём текста неверен: %+v", got)
	}
	if got.Stored != 2 {
		t.Errorf("в базу уходит %d записей, ожидали 2 без своих документов", got.Stored)
	}
	if len(got.Channels) != 3 {
		t.Errorf("разбивка по каналам неверна: %+v", got.Channels)
	}
	if got.Model.Calls != 2 || got.Model.Failed != 1 || got.Model.Unreported != 1 {
		t.Errorf("вызовы модели посчитаны неверно: %+v", got.Model)
	}
	if got.Model.PromptTokens != 4350 || got.Model.CompletionTokens != 371 || got.Model.SlowestMs != 100000 {
		t.Errorf("токены и длительность модели неверны: %+v", got.Model)
	}
	if len(got.Model.Operations) != 2 || got.Model.Operations[0].Operation != "qualify_candidate" {
		t.Errorf("разбивка по операциям неверна: %+v", got.Model.Operations)
	}
	if got.Analysis.Papers != 2906 || got.Analysis.Tokens != 2777289/liveCharsPerToken {
		t.Errorf("нагрузка на сервис анализа неверна: %+v", got.Analysis)
	}
	// Вход классификатора меряется и при отключённой модели: 15 + 6 + 9 знаков.
	if got.Classifier.Candidates != 1 || got.Classifier.Sources != 1 || got.Classifier.Chars != 30 ||
		got.Classifier.Connected || got.Classifier.Bytes == 0 {
		t.Errorf("вход классификатора неверен: %+v", got.Classifier)
	}
	// Второй готовый кандидат не должен стирать уже измеренный пакет, даже если источник общий.
	tally.addClassifier(LiveClassifierRequest{Candidates: []LiveClassifierCandidate{
		{ID: "c2", Name: "Квантовый отжиг", Sources: []LiveClassifierSource{{ID: "p1", Title: "Кубиты", Abstract: "Аннотация"}}},
	}}, true)
	_, _, total := tally.read()
	if total.Candidates != 2 || total.Sources != 2 || total.Chars != 60 || total.Tokens != 14 ||
		!total.Connected || total.Bytes != 2*got.Classifier.Bytes {
		t.Errorf("готовые пакеты классификатора потеряны: %+v", total)
	}
}
