package application

import (
	"cmp"
	"math"
	"slices"
)

// Счётчики панели считаются по уже собранному ответу, а не отдельным проходом, поэтому всегда согласованы с экраном.

// LiveCounters делит источники по доверию, чтобы было видно, сколько ответа держится на самопубликациях.
type LiveCounters struct {
	SourcesProcessed    int `json:"sources_processed"`
	Papers              int `json:"papers"`
	Registry            int `json:"registry"`
	Patents             int `json:"patents"`
	Web                 int `json:"web"`
	External            int `json:"external"`
	TrustedSources      int `json:"trusted_sources"`
	LoweredSources      int `json:"lowered_sources"`
	Candidates          int `json:"candidates"`
	Selected            int `json:"selected"`
	Rejected            int `json:"rejected"`
	AboveThreshold      int `json:"above_threshold"`
	ModelScored         int `json:"model_scored"`
	ModelAboveThreshold int `json:"model_above_threshold"`
	TopUp               int `json:"top_up,omitempty"`
}

// liveMinIndependent: метод одной статьи — наблюдение, а не сигнал, какой бы высокой ни была оценка модели.
const liveMinIndependent = 2

// Тот же округлённый процент, что на карточке; при «мало данных» и без второго независимого подтверждения
// технология не входит — по тому же правилу экран не ставит ей вердикт «слабый сигнал» (signalScore.ts).
func liveModelAbove(candidate LiveTopic) bool {
	score := candidate.ModelScore
	return score != nil && score.Label != liveClassifierInsufficient && math.Round(score.Value*100) > 75 &&
		liveIndependentSources(candidate) >= liveMinIndependent
}

// Рыночные записи дописываются после подсчёта авторских групп, поэтому их издатели считаются отдельно, а все
// научные работы при одной группе — одним источником.
func liveIndependentSources(topic LiveTopic) int {
	records := slices.Concat(liveChannelRecords(topic.Patents), liveChannelRecords(topic.PatentsCN),
		liveChannelRecords(topic.Registry), liveChannelRecords(topic.Web))
	return max(topic.Groups, min(topic.PapersCount, 1)+len(liveDistinct(records, livePublisher)))
}

// Запись в двух каналах — один источник, иначе охват завышен.
func liveCounters(topic LiveTopic, papers []StudyPaper, metadata map[string]LivePaperMetadata,
	candidates int) LiveCounters {
	counters := LiveCounters{Candidates: candidates}
	if topic.Confidence != nil && topic.Confidence.AboveMin {
		counters.AboveThreshold++
	}
	tally := &liveSourceTally{seen: make(map[string]bool, len(papers)*2), counters: &counters}
	for _, paper := range papers {
		meta := metadata[paper.ID]
		address := cmp.Or(paper.URL, meta.SourceURL)
		if tally.add(address, cmp.Or(meta.Trust, ClassifySource(address).Trust)) {
			counters.Papers++
		}
	}
	for _, channel := range []struct {
		channel *LiveChannel
		into    *int
	}{
		{topic.Registry, &counters.Registry},
		{topic.Patents, &counters.Patents},
		{topic.PatentsCN, &counters.Patents},
		{topic.Web, &counters.Web},
	} {
		tally.addChannel(channel.channel, channel.into)
	}
	for _, event := range topic.External {
		if tally.add(event.URL, event.Trust) {
			counters.External++
		}
	}
	return counters
}

type liveSourceTally struct {
	seen     map[string]bool
	counters *LiveCounters
}

func (t *liveSourceTally) add(address, trust string) bool {
	if address == "" || t.seen[address] {
		return false
	}
	t.seen[address] = true
	t.counters.SourcesProcessed++
	switch trust {
	case TrustHigh, TrustMedium:
		t.counters.TrustedSources++
	case TrustLowered:
		t.counters.LoweredSources++
	}
	return true
}

func (t *liveSourceTally) addChannel(channel *LiveChannel, into *int) {
	if channel == nil {
		return
	}
	for _, record := range channel.Records {
		if t.add(record.URL, record.Trust) {
			*into++
		}
	}
}
