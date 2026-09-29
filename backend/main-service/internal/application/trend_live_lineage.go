package application

import (
	"cmp"
	"math"
	"slices"
	"strings"
	"time"
)

func liveSemanticNames(papers []StudyPaper, global map[string]int, population int) []string {
	frequency, surfaces := map[string]int{}, map[string]map[string]int{}
	for _, paper := range papers {
		for _, gram := range liveGramsOf(liveTokens(paper.Title), liveRawTokens(paper.Title), nil) {
			frequency[gram.key]++
			if surfaces[gram.key] == nil {
				surfaces[gram.key] = map[string]int{}
			}
			surfaces[gram.key][gram.surface]++
		}
	}
	type name struct {
		key, surface string
		score        float64
	}
	names := []name{}
	for key, count := range frequency {
		if count < 2 {
			continue
		}
		score := float64(count) * math.Log(1+float64(population)/float64(1+global[key]))
		names = append(names, name{key, liveCommonSurface(surfaces[key], key), score})
	}
	slices.SortFunc(names, func(a, b name) int { return cmp.Or(cmp.Compare(b.score, a.score), cmp.Compare(a.key, b.key)) })
	result, seen := []string{}, map[string]bool{}
	for _, item := range names {
		if len(result) == 4 {
			break
		}
		if seen[item.key] {
			continue
		}
		seen[item.key] = true
		family := liveCandidateFamily(liveCandidate{gram: item.key, surface: item.surface})
		result = append(result, family.name)
	}
	if len(result) == 0 {
		words := strings.Fields(papers[0].Title)
		result = append(result, strings.Join(words[:min(6, len(words))], " "))
	}
	return result
}

func liveDiscoveryQuery(aliases []string) LiveQuery {
	clauses := make([]string, 0, len(aliases))
	for _, alias := range aliases {
		clauses = append(clauses, `ti:"`+strings.ReplaceAll(alias, `"`, "")+`"`)
	}
	return LiveQuery{Arxiv: strings.Join(clauses, " OR "), GitHub: `"` + aliases[0] + `"`,
		HN: aliases[0], Terms: slices.Clone(aliases)}
}

// Подкластеры могут делить широкую фразу: различает их другая засвидетельствованная фраза или заголовок, а не номер.
func liveDiscoveryLabelKey(name string) string {
	words := liveTokens(name)
	for i := range words {
		words[i] = liveStem(words[i])
	}
	return strings.Join(words, " ")
}

func liveDiscoveryLabels(topics []LiveDiscovery, papers []StudyPaper) {
	counts, used := map[string]int{}, map[string]bool{}
	for _, topic := range topics {
		counts[liveDiscoveryLabelKey(topic.Name)]++
	}
	generic := liveGenericTokens(papers)
	for i := range topics {
		topic := &topics[i]
		base := topic.Name
		if counts[liveDiscoveryLabelKey(base)] < 2 {
			used[liveDiscoveryLabelKey(base)] = true
			continue
		}
		chosen := liveDistinctDiscoveryLabel(*topic, papers, generic, used)
		if chosen != "" {
			topic.Name = chosen
		}
		used[liveDiscoveryLabelKey(topic.Name)] = true
	}
}

func liveDistinctDiscoveryLabel(
	topic LiveDiscovery, papers []StudyPaper, generic map[string]bool, used map[string]bool,
) string {
	for _, alias := range topic.Aliases {
		if liveDiscoveryLabelKey(alias) == liveDiscoveryLabelKey(topic.Name) || len(liveGrams(liveTokens(alias), generic)) == 0 {
			continue
		}
		candidate := topic.Name + " · " + alias
		if !used[liveDiscoveryLabelKey(candidate)] {
			return candidate
		}
	}
	for _, paper := range papers {
		if slices.Contains(topic.PaperIDs, paper.ID) && !used[liveDiscoveryLabelKey(paper.Title)] {
			return paper.Title
		}
	}
	return ""
}

func liveLineageDate(req LiveAnalysisRequest) (string, string) {
	end, _ := liveAnalysisDate(req.Snapshot.Periods[len(req.Snapshot.Periods)-1].EndExclusive)
	last := end.AddDate(0, 0, -1).Format(time.DateOnly)
	observed, err := time.Parse(time.RFC3339Nano, req.Snapshot.ObservedAt)
	if err == nil && (!req.StrictAvailability || observed.UTC().Format(time.DateOnly) <= last) {
		return observed.UTC().Format(time.DateOnly), "snapshot-observed-date-v1"
	}
	return last, "cutoff-inclusive-date-v1"
}

// Чинится только известный дефект даты создания первой v5, с сохранённым снимком как доказательством;
// исходное значение остаётся в событии, строгие исторические запросы не переписываются.
func liveCorrectLegacyCreated(topic LiveTopic, snapshot LiveSnapshot, strict bool) *LiveDiscovery {
	if topic.Analysis == nil || topic.Analysis.Discovery == nil {
		return nil
	}
	discovery := topic.Analysis.Discovery
	if strict || snapshot.Method != "live-signals-v5" || discovery.Method != "dated-semantic-leader-v2" ||
		len(snapshot.Periods) == 0 || len(discovery.History) != 1 || discovery.ID != topic.ID || discovery.Name != topic.Name ||
		!slices.Equal(liveSortedUnique(discovery.PaperIDs), liveSortedUnique(topic.PaperIDs)) {
		return discovery
	}
	event := discovery.History[0]
	cutoff := snapshot.Periods[len(snapshot.Periods)-1].EndExclusive
	observed, err := time.Parse(time.RFC3339Nano, snapshot.ObservedAt)
	if err != nil || observed.UTC().AddDate(0, 0, 1).Format(time.DateOnly) != cutoff || event.Kind != "created" ||
		event.At != cutoff || event.Name != discovery.Name || len(event.Parents) != 0 || event.OldName != "" ||
		event.DateBasis != "" || event.PreviousAt != "" {
		return discovery
	}
	corrected := *discovery
	corrected.History = slices.Clone(discovery.History)
	corrected.History[0].At = observed.UTC().Format(time.DateOnly)
	corrected.History[0].PreviousAt = event.At
	corrected.History[0].DateBasis = "snapshot-observed-date-v1"
	return &corrected
}

func liveDiscoveryLineage(current, previous []LiveDiscovery, cutoff, dateBasis string) {
	matches := liveDiscoveryMatches(current, previous, cutoff)
	usedCurrent := liveContinueDiscoveries(current, previous, matches, cutoff, dateBasis)
	for i := range current {
		topic := &current[i]
		parents := []string{}
		for _, m := range matches {
			if m.current == i {
				parents = append(parents, previous[m.old].ID)
			}
		}
		parents = liveSortedUnique(parents)
		kind := "created"
		switch {
		case len(parents) > 1:
			kind = "merge"
		case len(parents) == 1 && parents[0] != topic.ID:
			kind = "split"
		case usedCurrent[i]:
			continue
		}
		topic.History = append(topic.History, LiveLineageEvent{
			Kind: kind, At: cutoff, DateBasis: dateBasis, Parents: parents, Name: topic.Name,
		})
	}
}

type liveLineageMatch struct {
	old, current, shared int
	score                float64
}

func liveDiscoveryMatches(current, previous []LiveDiscovery, cutoff string) []liveLineageMatch {
	matches := []liveLineageMatch{}
	for i, topic := range current {
		for j, old := range previous {
			// Будущая родословная не может переименовать или слить историческую тему.
			if slices.ContainsFunc(old.History, func(e LiveLineageEvent) bool { return e.At > cutoff }) {
				continue
			}
			shared := liveSharedDiscoveryPapers(topic.PaperIDs, old.PaperIDs)
			score := float64(shared) / float64(max(1, min(len(topic.PaperIDs), len(old.PaperIDs))))
			if shared >= 2 && score >= .5 {
				matches = append(matches, liveLineageMatch{j, i, shared, score})
			}
		}
	}
	slices.SortFunc(matches, func(a, b liveLineageMatch) int {
		return cmp.Or(cmp.Compare(b.shared, a.shared), cmp.Compare(b.score, a.score),
			cmp.Compare(previous[a.old].ID, previous[b.old].ID), cmp.Compare(current[a.current].ID, current[b.current].ID))
	})
	return matches
}

func liveSharedDiscoveryPapers(current, previous []string) int {
	shared := 0
	for _, id := range current {
		if slices.Contains(previous, id) {
			shared++
		}
	}
	return shared
}

func liveContinueDiscoveries(
	current, previous []LiveDiscovery, matches []liveLineageMatch, cutoff, dateBasis string,
) map[int]bool {
	usedOld, usedCurrent := map[int]bool{}, map[int]bool{}
	for _, m := range matches {
		if usedOld[m.old] || usedCurrent[m.current] {
			continue
		}
		topic, old := &current[m.current], previous[m.old]
		topic.ID, topic.History = old.ID, slices.Clone(old.History)
		topic.Aliases = liveSortedUnique(slices.Concat([]string{topic.Name, old.Name}, topic.Aliases, old.Aliases))
		if topic.Name != old.Name {
			topic.History = append(topic.History, LiveLineageEvent{
				Kind: "rename", At: cutoff, DateBasis: dateBasis, OldName: old.Name, Name: topic.Name,
			})
		}
		usedOld[m.old], usedCurrent[m.current] = true, true
	}
	return usedCurrent
}
