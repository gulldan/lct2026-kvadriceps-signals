package application

import (
	"slices"
	"strings"
)

// Научные ветви объединяют темы радара; отраслевые рубрики корпуса задаются отдельно.
const (
	liveBranchComputing   = "computing"
	liveBranchQuantum     = "quantum"
	liveBranchMaterials   = "materials"
	liveBranchPhysics     = "physics"
	liveBranchSpace       = "space"
	liveBranchLife        = "life"
	liveBranchEarth       = "earth"
	liveBranchMath        = "math"
	liveBranchEngineering = "engineering"
	liveBranchEconomics   = "economics"
	liveDiscoverPerBranch = 5
)

// LiveDiscoveryRequestLimit вдвое больше бюджета эмбеддингов: анализ ещё раз прореживает выборку по областям.
const LiveDiscoveryRequestLimit = 2 * liveSemanticDiscoveryLimit

// Предмет заданного семейства не меняется из-за преобладания вычислительных методов в выборке статей.
func liveCuratedBranch(topic LiveTopic) string {
	if !topic.Curated || topic.Source != "curated" {
		return ""
	}
	families := liveFamilies()
	if i := slices.IndexFunc(families, func(family liveFamily) bool { return family.id == topic.ID }); i >= 0 {
		return families[i].branch
	}
	return ""
}

func liveBranchOrder() []string {
	return []string{liveBranchComputing, liveBranchQuantum, liveBranchMaterials, liveBranchPhysics, liveBranchSpace,
		liveBranchLife, liveBranchEarth, liveBranchMath, liveBranchEngineering, liveBranchEconomics}
}

// Неизвестная категория вне cs.* — пустая область, чтобы её можно было не ранжировать.
func liveBranch(category string) string {
	category = strings.TrimSpace(category)
	archive, sub, _ := strings.Cut(category, ".")
	switch archive {
	case "cs":
		return liveBranchComputing
	case "stat":
		if sub == "ML" {
			return liveBranchComputing
		}
		return liveBranchMath
	case "math", "math-ph":
		return liveBranchMath
	case "quant-ph":
		return liveBranchQuantum
	case "cond-mat":
		return liveBranchMaterials
	case "astro-ph":
		return liveBranchSpace
	case "q-bio", liveServerBioRxiv, liveServerMedRxiv:
		return liveBranchLife
	case liveServerChemRxiv:
		return liveBranchMaterials
	case "eartharxiv", "essoar":
		return liveBranchEarth
	case "engrxiv":
		return liveBranchEngineering
	case "psyarxiv", "socarxiv", "edarxiv":
		return liveBranchEconomics
	case "eess":
		return liveBranchEngineering
	case "q-fin", "econ":
		return liveBranchEconomics
	case "hep-th", "hep-ph", "hep-ex", "hep-lat", "nucl-th", "nucl-ex", "gr-qc", "nlin":
		return liveBranchPhysics
	case "physics":
		switch sub {
		case "chem-ph", "app-ph":
			return liveBranchMaterials
		case "bio-ph", "med-ph":
			return liveBranchLife
		case "ao-ph", "geo-ph":
			return liveBranchEarth
		case "space-ph":
			return liveBranchSpace
		}
		return liveBranchPhysics
	}
	return ""
}

// При равенстве побеждает область раньше в liveBranchOrder, чтобы ответ не менялся между прогонами.
func liveTopicBranch(papers []StudyPaper, metadata map[string]LivePaperMetadata) string {
	counts := map[string]int{}
	for _, paper := range papers {
		if branch := liveBranch(metadata[paper.ID].Category); branch != "" {
			counts[branch]++
		}
	}
	best, bestCount := "", 0
	for _, branch := range liveBranchOrder() {
		if counts[branch] > bestCount {
			best, bestCount = branch, counts[branch]
		}
	}
	return best
}

// Радар ищет технологии: чистая математика и записи без категории получают ноль.
func liveBranchWeight(branch string) int {
	switch branch {
	case liveBranchComputing, liveBranchLife, liveBranchEngineering,
		liveBranchMaterials, liveBranchEconomics, liveBranchQuantum:
		return 3
	case liveBranchSpace, liveBranchEarth:
		return 2
	case liveBranchPhysics:
		return 1
	}
	return 0
}

func liveBranchSample(papers []StudyPaper, metadata map[string]LivePaperMetadata, limit int) []StudyPaper {
	groups := map[string][]StudyPaper{}
	for _, paper := range papers {
		groups[liveBranch(metadata[paper.ID].Category)] = append(groups[liveBranch(metadata[paper.ID].Category)], paper)
	}
	branches := slices.Sorted(func(yield func(string) bool) {
		for branch := range groups {
			if !yield(branch) {
				return
			}
		}
	})
	weight := 0
	for _, branch := range branches {
		weight += liveBranchWeight(branch)
	}
	result := []StudyPaper{}
	remaining := limit
	for _, branch := range branches {
		share := liveBranchWeight(branch)
		if share == 0 || weight == 0 {
			continue
		}
		quota := remaining * share / weight
		picked := liveStratifiedPapers(groups[branch], metadata, min(quota, len(groups[branch])))
		result = append(result, picked...)
		remaining -= len(picked)
		weight -= share
	}
	return result
}

func liveMetadataFor(metadata map[string]LivePaperMetadata, groups ...[]StudyPaper) map[string]LivePaperMetadata {
	out := map[string]LivePaperMetadata{}
	for _, papers := range groups {
		for _, paper := range papers {
			if meta, ok := metadata[paper.ID]; ok {
				out[paper.ID] = meta
			}
		}
	}
	return out
}
