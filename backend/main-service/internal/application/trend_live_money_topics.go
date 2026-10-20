package application

import (
	"cmp"
	"hash/fnv"
	"maps"
	"slices"
	"strings"
	"sync"
)

// Ранние деньги у каждого направления обзора — маркер «инвесторы уже заметили». Сверка всех направлений
// со всеми событиями доски стоит секунды, поэтому она считается в фоне один раз на пару «доска — набор
// направлений», а обзор отдаёт готовое и не ждёт.

const liveTopicMoneyInvestors = 3

type liveTopicMoneyIndex struct {
	mu        sync.Mutex
	key       uint64
	counts    map[string]LiveMapMoney
	computing bool
}

func (s *TrendLiveService) topicMoney(topics []LiveTopic) map[string]LiveMapMoney {
	board, _ := s.moneyBoard()
	if board == nil {
		return nil
	}
	hash := fnv.New64a()
	_, _ = hash.Write([]byte(board.BuiltAt.String()))
	probes := make(map[string][]string, len(topics))
	for _, topic := range topics {
		probes[topic.ID] = liveMoneyProbes(slices.Concat([]string{topic.Name}, topic.Query.Terms))
		_, _ = hash.Write([]byte(topic.ID))
	}
	index := &s.eventData.topicMoney
	index.mu.Lock()
	defer index.mu.Unlock()
	if key := hash.Sum64(); index.key != key && !index.computing {
		index.computing = true
		go func() {
			counts := liveMoneyByTopic(board.Events, probes)
			index.mu.Lock()
			defer index.mu.Unlock()
			index.key, index.counts, index.computing = key, counts, false
		}()
	}
	return index.counts
}

func liveCountInvestors(named map[string]int, investors string) map[string]int {
	for name := range strings.SplitSeq(investors, ",") {
		if name = strings.TrimSpace(name); name != "" {
			if named == nil {
				named = map[string]int{}
			}
			named[name]++
		}
	}
	return named
}

func liveMoneyProbes(terms []string) []string {
	probes := []string{}
	for _, term := range terms {
		if probe := strings.ToLower(strings.Join(strings.Fields(term), " ")); len([]rune(probe)) >= 3 {
			probes = append(probes, probe)
		}
	}
	return probes
}

// liveMoneyByTopic — то же правило совпадения, что у MoneyMatch, но текст события приводится один раз на все направления.
func liveMoneyByTopic(events []LiveMoneyEvent, probes map[string][]string) map[string]LiveMapMoney {
	counts := map[string]LiveMapMoney{}
	investors := map[string]map[string]int{}
	for _, event := range events {
		haystack := " " + strings.ToLower(event.Title+" "+event.Text+" "+event.Company) + " "
		for id, list := range probes {
			if !slices.ContainsFunc(list, func(probe string) bool { return liveWordPrefix(haystack, probe) }) {
				continue
			}
			money := counts[id]
			if money.Kinds == nil {
				money.Kinds, money.Stages = map[string]int{}, map[string]int{}
			}
			money.Total++
			money.Kinds[event.Kind]++
			money.Stages[event.Stage]++
			counts[id] = money
			// Имена фондов есть только у раундов: у грантов в этом поле программа, а не инвестор.
			if event.Kind == LiveMoneyRound {
				investors[id] = liveCountInvestors(investors[id], event.Investors)
			}
		}
	}
	for id, named := range investors {
		names := slices.Collect(maps.Keys(named))
		slices.SortFunc(names, func(a, b string) int { return cmp.Or(cmp.Compare(named[b], named[a]), strings.Compare(a, b)) })
		money := counts[id]
		money.Investors = names[:min(len(names), liveTopicMoneyInvestors)]
		counts[id] = money
	}
	return counts
}
