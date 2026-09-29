package application

import "testing"

func TestLiveCountersCountDistinctAddresses(t *testing.T) {
	topic := LiveTopic{
		Registry: &LiveChannel{Records: []LiveSourceRecord{
			{URL: "https://clinicaltrials.gov/study/NCT01", Trust: TrustHigh},
		}},
		Patents: &LiveChannel{Records: []LiveSourceRecord{
			{URL: "https://uspto.gov/a", Trust: TrustHigh},
			{URL: "https://uspto.gov/a", Trust: TrustHigh},
			{URL: "", Trust: TrustHigh},
		}},
		Web: &LiveChannel{Records: []LiveSourceRecord{
			{URL: "https://nplus1.ru/b", Trust: TrustMedium},
			{URL: "https://habr.com/c", Trust: TrustLowered},
		}},
		External: []StudyEvent{{URL: "https://github.com/o/r", Trust: TrustLowered}},
	}
	papers := []StudyPaper{{ID: "p1", URL: "https://arxiv.org/abs/1"}}
	metadata := map[string]LivePaperMetadata{"p1": {Trust: TrustMedium}}

	got := liveCounters(topic, papers, metadata, 7)
	if got.SourcesProcessed != 6 {
		t.Fatalf("обработано источников %d, ожидали 6: %+v", got.SourcesProcessed, got)
	}
	if got.Papers != 1 || got.Registry != 1 || got.Patents != 1 || got.Web != 2 || got.External != 1 {
		t.Errorf("разбивка по каналам неверна: %+v", got)
	}
	if got.TrustedSources != 4 || got.LoweredSources != 2 {
		t.Errorf("разбивка по доверию неверна: %+v", got)
	}
	if got.Candidates != 7 {
		t.Errorf("кандидатов %d, ожидали 7", got.Candidates)
	}
}

func TestLiveVerdictNeedsIndependentEvidence(t *testing.T) {
	score := &LiveModelScore{Label: liveClassifierWeak, Value: 0.96, Threshold: 0.37}
	single := LiveTopic{StudyTopic: StudyTopic{Groups: 1}, PapersCount: 1, ModelScore: score}
	covered := single
	covered.Web = &LiveChannel{Records: []LiveSourceRecord{
		{URL: "https://www.reuters.com/a"}, {URL: "https://reuters.com/b"},
	}}
	if liveModelAbove(single) || !liveModelAbove(covered) {
		t.Fatalf("метод одной статьи посчитан сигналом или второй издатель не засчитан: %d и %d",
			liveIndependentSources(single), liveIndependentSources(covered))
	}
	hype := LiveTopic{StudyTopic: StudyTopic{ID: "h"}, Web: &LiveChannel{Records: []LiveSourceRecord{
		{URL: "https://www.prnewswire.com/news-releases/a", Title: "Vendor announces platform"},
		{URL: "https://x.com/vendor/status/1", Title: "Our platform"},
	}}}
	patent := LiveTopic{StudyTopic: StudyTopic{ID: "p"}, Patents: &LiveChannel{Records: []LiveSourceRecord{
		{URL: "https://patents.google.com/patent/CN1", Title: "Способ хранения"},
	}}}
	_, rejected := liveAdmitCards([]LiveTopic{hype, patent}, nil)
	if len(rejected) != 2 || rejected[0].Code != liveRejectHype || rejected[1].Code == liveRejectHype {
		t.Fatalf("пресс-релиз и соцсеть не отсечены как шум или патент принят за шум: %+v", rejected)
	}
}

func TestLiveCountersClassifyUnstampedPaper(t *testing.T) {
	got := liveCounters(LiveTopic{}, []StudyPaper{{ID: "p", URL: "https://news.mit.edu/x"}},
		map[string]LivePaperMetadata{}, 0)
	if got.SourcesProcessed != 1 || got.TrustedSources != 1 {
		t.Fatalf("неразмеченная статья посчитана неверно: %+v", got)
	}
}
