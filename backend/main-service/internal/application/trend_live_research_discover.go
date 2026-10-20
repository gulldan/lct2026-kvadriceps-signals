package application

import (
	"context"
	"fmt"
	"slices"
	"strings"
)

const (
	liveResearchDiscoveryMethod = "research-metadata-phrases-v1"
	// С трёх областей из двадцати фраза — общее слово или сложившаяся сквозная технология (3D-печать, облачные
	// вычисления); в двух ещё встречаются смежные отрасли, как очистка сточных вод у воды и материалов.
	liveResearchGeneralDomains = 3
)

// Области задают входной корпус, а названия технологий извлекаются из найденных документов.
type liveResearchDomain struct {
	name, branch string
	terms        []string
}

func liveResearchDomains() []liveResearchDomain {
	return []liveResearchDomain{
		// При ограниченном бюджете первыми проверяем отрасли, которым не хватает профильных источников.
		{"construction", liveBranchEngineering, []string{"construction materials", "concrete", "structural health monitoring"}},
		{"mining", liveBranchEarth, []string{"mineral processing", "hydrometallurgy", "extractive metallurgy"}},
		{"water", liveBranchEarth, []string{"water treatment", "desalination", "wastewater"}},
		{"ecology", liveBranchEarth, []string{"biodiversity monitoring", "ecosystem restoration", "ecotoxicology"}},
		{"food", liveBranchLife, []string{"food engineering", "food biotechnology", "food preservation"}},
		{"veterinary", liveBranchLife, []string{"veterinary diagnostics", "animal vaccines", "aquaculture health"}},
		{"chemistry", liveBranchMaterials, []string{"catalysis", "electrochemistry", "polymer"}},
		{"energy", liveBranchEngineering, []string{"energy storage", "renewable energy", "power grid"}},
		{"agriculture", liveBranchLife, []string{"crop", "soil", "agriculture"}},
		{liveBranchMaterials, liveBranchMaterials, []string{liveBranchMaterials, "metallurgy", "composites"}},
		{"medicine", liveBranchLife, []string{"medicine", "therapeutics", "diagnostics"}},
		{"biology", liveBranchLife, []string{"biology", "biotechnology", "genomics"}},
		{"climate", liveBranchEarth, []string{"climate", "ecology", "water treatment"}},
		{"engineering", liveBranchEngineering, []string{"manufacturing", "robotics", "transport"}},
		{"economics", liveBranchEconomics, []string{"economics", "finance", "supply chain"}},
		{"computer science", liveBranchComputing, []string{"machine learning", "cybersecurity", "distributed computing"}},
		{"transportation", liveBranchEngineering, []string{"railway", "maritime transport", "aviation"}},
		{"education", liveBranchEconomics, []string{"education", "learning sciences", "educational technology"}},
		{"psychology", liveBranchLife, []string{"psychology", "behavioral science", "cognitive science"}},
		{"social sciences", liveBranchEconomics, []string{"sociology", "public policy", "demography"}},
	}
}

// Название исходной области и общие фразы не выдаются за обнаруженную технологию.
func liveResearchCandidates(documents []StudyPaper, metadata map[string]LivePaperMetadata,
	periods []LivePeriod, terms, general []string,
) []liveCandidate {
	known := liveFamilies()
	for _, term := range slices.Concat(terms, general) {
		known = append(known, liveFamily{name: term})
	}
	tiers := liveEvidenceTiers()
	tier := tiers[len(tiers)-1]
	return liveDiscoverWithCoverage(documents, metadata, periods, known, tier.recentMin, tier.earlyDiv, false)
}

// Без ряда во времени выборка ранжирует фразы частотой, и первыми встают самые широкие: «machine learning»
// нашлось в заголовках 18 областей из 20. Общая фраза видна по разбросу между областями, а сама область —
// по названию области в вершине фразы («veterinary medicine», «social sciences»).
func liveResearchGeneral(samples [][]StudyPaper) []string {
	fields := map[string]bool{}
	for _, domain := range liveResearchDomains() {
		words := liveTokens(domain.name)
		fields[liveStem(words[len(words)-1])] = true
	}
	spread := map[string]int{}
	for _, documents := range samples {
		grams := map[string]bool{}
		for _, paper := range documents {
			for _, gram := range liveGrams(liveTokens(paper.Title), nil) {
				grams[gram.key] = true
			}
		}
		for key := range grams {
			spread[key]++
		}
	}
	general := []string{}
	for key, domains := range spread {
		if domains >= liveResearchGeneralDomains || fields[key[strings.LastIndexByte(key, ' ')+1:]] {
			general = append(general, key)
		}
	}
	return general
}

func (s *TrendLiveService) collectResearchDiscoveries(ctx context.Context, src *liveSources, snapshot *LiveSnapshot) {
	seen, topics := map[string]bool{}, map[string]bool{}
	for _, paper := range snapshot.Papers {
		seen[paper.ID] = true
	}
	for _, topic := range slices.Concat(snapshot.Topics, snapshot.Discovered) {
		topics[topic.ID] = true
	}
	domains := liveResearchDomains()
	umbrellas := make([]*LiveTopic, len(domains))
	samples := make([][]StudyPaper, len(domains))
	for i, domain := range domains {
		if ctx.Err() != nil {
			return
		}
		channel := src.researchSearch(ctx, domain.terms, snapshot.Periods)
		if channel == nil {
			continue
		}
		liveResearchDedup(channel, snapshot.Papers, snapshot.PaperMetadata)
		liveResearchCounts(channel, snapshot.Periods)
		umbrellas[i] = &LiveTopic{Research: channel, Branch: domain.branch, Query: LiveQuery{Terms: domain.terms}}
		liveResearchLimits(umbrellas[i])
		snapshot.Limits = append(snapshot.Limits, umbrellas[i].Limits...)
		// Поисковик уже отобрал корпус по заголовку и аннотации; повторный фильтр только по заголовку терял бы данные.
		samples[i] = liveResearchDocuments(channel)
		livePrepareAnswerMetadata([]LiveTopic{*umbrellas[i]}, samples[i], src.metadata)
	}
	// Общность фразы видна только по всем областям сразу, поэтому отбор идёт вторым проходом.
	general := liveResearchGeneral(samples)
	for i, domain := range domains {
		if umbrellas[i] == nil {
			continue
		}
		candidates := liveResearchCandidates(samples[i], src.metadata, snapshot.Periods, domain.terms, general)
		kept := 0
		for _, candidate := range candidates {
			topic := liveCandidateTopic(*umbrellas[i], candidate, snapshot.Periods, liveAbsoluteBar(), nil)
			if topics[topic.ID] {
				continue
			}
			topics[topic.ID] = true
			topic.Source, topic.Curated = liveSourceDiscovered, false
			topic.Analysis = &LiveTopicAnalysis{Discovery: &LiveDiscovery{ID: topic.ID, Name: topic.Name,
				Query: topic.Query, PaperIDs: topic.EvidenceIDs, Sampled: true, Method: liveResearchDiscoveryMethod}}
			liveApplyTemporalCoverage(&topic)
			topic.Confidence = new(liveConfidence(topic, liveAbsoluteBar()))
			snapshot.addTopic(topic, candidate.papers, seen, true)
			kept++
			if kept == liveDiscoverPerBranch {
				break
			}
		}
	}
	snapshot.PaperMetadata = liveMergeMetadata(src.metadata, snapshot.PaperMetadata, snapshot.Papers)
}

func liveResearchDocuments(channel *LiveChannel) []StudyPaper {
	documents := []StudyPaper{}
	for _, record := range liveChannelRecords(channel) {
		if document, ok := liveRecordDocument(record); ok {
			documents = append(documents, document)
		}
	}
	return documents
}

func liveResearchCounts(channel *LiveChannel, periods []LivePeriod) {
	if channel == nil {
		return
	}
	channel.Counts = make([]*int, len(periods))
	for _, record := range channel.Records {
		if i := livePeriodIndex(periods, record.Date); i >= 0 {
			if channel.Counts[i] == nil {
				channel.Counts[i] = new(0)
			}
			*channel.Counts[i]++
		}
	}
}

func liveResearchLimits(topic *LiveTopic) {
	if topic.Research == nil {
		return
	}
	if len(topic.Research.Records) > 0 {
		topic.Limits = append(topic.Limits, liveLimitOf(liveResearchSample,
			fmt.Sprintf("исследовательские каталоги: %d записей в ограниченной выборке; рост не измерен",
				len(topic.Research.Records)), map[string]int{"records": len(topic.Research.Records)}))
	}
	for _, limit := range topic.Research.Limits {
		topic.Limits = append(topic.Limits, liveLimitOf(liveResearchOff, limit, nil))
	}
}

func liveResearchDOI(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	for _, prefix := range []string{"https://doi.org/", "http://doi.org/", "https://dx.doi.org/", "doi:"} {
		value = strings.TrimPrefix(value, prefix)
	}
	if !strings.HasPrefix(value, "10.") || !strings.Contains(value, "/") {
		return ""
	}
	return value
}
