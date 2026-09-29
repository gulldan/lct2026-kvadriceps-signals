package application

import (
	"cmp"
	"context"
	"fmt"
	"log/slog"
	"slices"
)

func liveQualificationBatch(candidates []LiveTopic, documents []StudyPaper, metadata map[string]LivePaperMetadata,
) []liveQualificationTask {
	byID := make(map[string]StudyPaper, len(documents))
	for _, document := range documents {
		byID[document.ID] = document
	}
	tasks := make([]liveQualificationTask, len(candidates))
	for i, candidate := range candidates {
		tasks[i] = liveQualificationTask{index: i, candidate: candidate, documents: byID,
			inputs: liveQualificationInputs(candidate, byID, metadata)}
	}
	return tasks
}

func liveApplyQualifications(umbrella *LiveTopic, ready []liveQualificationResult,
	periods []LivePeriod, rejected []LiveRejection,
) ([]LiveTopic, []LiveRejection) {
	slices.SortFunc(ready, func(a, b liveQualificationResult) int { return cmp.Compare(a.task.index, b.task.index) })
	kept := make([]LiveTopic, 0, len(ready))
	bar := liveMarketBarOf(*umbrella)
	unavailable := 0
	for _, result := range ready {
		candidate, qualification := result.task.candidate, result.qualification
		candidate.Qualification = &qualification
		if result.err != nil {
			slog.Warn("radar candidate qualification unavailable", "candidate_id", candidate.ID, "error", result.err)
			unavailable++
			rejected = append(rejected, liveRejection(candidate, liveRejectUnqualified,
				"проверка сущности и связи с запросом не завершена; кандидат не включён в готовую выдачу"))
			continue
		}
		code, reason := liveQualificationRejection(qualification)
		if code != "" {
			if code == liveRejectUnqualified {
				unavailable++
			}
			rejected = append(rejected, liveRejection(candidate, code, reason))
			continue
		}
		// Проверка и для подменённого генератора: флаг допуска без цитат не должен сохранить прежние счётчики.
		if err := liveValidateQualification(qualification, result.task.inputs); err != nil {
			unavailable++
			rejected = append(rejected, liveRejection(candidate, liveRejectUnqualified,
				"не удалось проверить ссылки и дословные фрагменты, подтверждающие одну технологию"))
			continue
		}
		candidate = liveQualifiedTopic(candidate, qualification, result.task.documents, periods, bar)
		tier := liveEvidenceTiers()[liveEvidenceRank(candidate.EvidenceLevel)]
		if code, reason := liveRejectReason(candidate, tier, bar); code != "" {
			rejected = append(rejected, liveRejection(candidate, code, reason))
			continue
		}
		kept = append(kept, candidate)
	}
	if unavailable > 0 {
		umbrella.Limits = append(umbrella.Limits, liveLimitOf(liveRejectUnqualified,
			"проверка сущности и источников части кандидатов не завершена", map[string]int{"candidates": unavailable}))
	}
	return kept, rejected
}

type liveQualificationTask struct {
	index     int
	candidate LiveTopic
	inputs    []LiveQualificationDocument
	documents map[string]StudyPaper
}

type liveQualificationResult struct {
	task          liveQualificationTask
	qualification LiveQualification
	err           error
}

// Срок относится к допущенной работе; следующая группа не наследует истёкший срок предыдущей.
func (p *liveCandidatePipeline) qualify(ctx context.Context, service *TrendLiveService, query string,
	candidates []LiveTopic, documents []StudyPaper, metadata map[string]LivePaperMetadata,
) {
	tasks := liveQualificationBatch(candidates, documents, metadata)
	p.qualifying += len(tasks)
	p.inFlight += len(tasks)
	slots := service.workerSlots(ctx)
	for _, task := range tasks {
		task.index = p.started
		p.started++
		p.work.Go(func() {
			stage, cancel := context.WithTimeout(ctx, liveQualificationStage)
			defer cancel()
			result := liveQualificationResult{task: task}
			result.qualification, result.err = service.qualifyCandidate(stage, slots, query, task.candidate.Name, task.inputs)
			if result.err == nil && result.qualification.Verified {
				liveNotifyFound(ctx, result.qualification.Label)
			}
			p.updates <- liveCandidateUpdate{qualification: &result}
		})
	}
}

func (s *TrendLiveService) qualifyCandidate(ctx context.Context, slots liveWorkerSlots, query, name string,
	inputs []LiveQualificationDocument,
) (LiveQualification, error) {
	if s.qualify == nil {
		return LiveQualification{}, errLLMNotConfigured
	}
	if !slots.acquire(ctx) {
		return LiveQualification{}, fmt.Errorf("qualification wait: %w", ctx.Err())
	}
	defer slots.release()
	return s.qualify(ctx, query, name, inputs)
}

func liveQualificationRejection(qualification LiveQualification) (code, reason string) {
	reason = cmp.Or(liveRussianText(qualification.Reason), "подтверждений для допуска кандидата недостаточно")
	switch {
	case qualification.Model == "" || qualification.Method == "" || liveRussianText(qualification.Reason) == "":
		return liveRejectUnqualified, "проверка сущности по исходным документам не завершена"
	case !qualification.Technology:
		return liveRejectNotTechnology, reason
	case !qualification.Relevant:
		return liveRejectOffTopic, reason
	case !qualification.Coherent:
		return liveRejectIncoherent, reason
	case !qualification.Verified || qualification.Model == "" || qualification.VerifierModel == "":
		return liveRejectUnqualified, "проверка сущности по исходным документам не завершена"
	}
	return "", ""
}

func liveQualificationInputs(candidate LiveTopic, byID map[string]StudyPaper,
	metadata map[string]LivePaperMetadata,
) []LiveQualificationDocument {
	papers := []StudyPaper{}
	seen := map[string]bool{}
	// Сначала первоисточники сущности, затем рыночный контекст и остальные научные работы.
	for _, id := range slices.Concat(candidate.qualificationAnchors, candidate.EvidenceIDs, candidate.PaperIDs) {
		paper, ok := byID[id]
		key := cmp.Or(paper.CanonicalID, paper.URL, paper.ID)
		if !ok || seen[key] {
			continue
		}
		seen[key] = true
		papers = append(papers, paper)
	}
	// Вторая работа той же группы авторов или того же издателя не даёт независимого подтверждения, поэтому
	// в предел сначала идёт по одному документу от каждой группы в прежнем порядке, затем вторые и далее.
	groups := studyGroups(papers)
	inputs := []LiveQualificationDocument{}
	for row := 0; len(inputs) < min(len(papers), liveQualificationDocuments); row++ {
		for _, group := range groups {
			if row < len(group) && len(inputs) < liveQualificationDocuments {
				inputs = append(inputs, LiveQualificationDocument{ID: group[row].ID,
					Text: liveWorkLine(group[row], metadata[group[row].ID])})
			}
		}
	}
	return inputs
}

func liveQualifiedTopic(candidate LiveTopic, qualification LiveQualification, byID map[string]StudyPaper,
	periods []LivePeriod, bar liveMarketBar,
) LiveTopic {
	ids := make(map[string]bool, len(qualification.Evidence))
	documents := make([]StudyPaper, 0, len(qualification.Evidence))
	for _, evidence := range qualification.Evidence {
		ids[evidence.SourceID] = true
		documents = append(documents, byID[evidence.SourceID])
	}
	works, records := liveSplitDocuments(documents)
	direction := candidate
	external := []StudyEvent{}
	for _, event := range candidate.External {
		if ids[liveRecordPrefix+cmp.Or(event.URL, event.Title)] {
			external = append(external, event)
		}
	}
	candidate.StudyTopic = StudyTopic{ID: candidate.ID, Series: liveSeries(periods, documents),
		Stage: studyStageUnassessed, Momentum: studyUnknown, Screening: liveQualificationMethod, External: external,
		PaperIDs: []string{}, EvidenceIDs: []string{}}
	for _, paper := range works {
		candidate.PaperIDs = append(candidate.PaperIDs, paper.ID)
	}
	for _, record := range records {
		candidate.EvidenceIDs = append(candidate.EvidenceIDs, record.ID)
	}
	candidate.Label, candidate.Qualification = qualification.Label, &qualification
	candidate.qualificationAnchors = nil
	candidate.PapersCount, candidate.RecordsCount, candidate.DocumentsCount = len(works), len(records), len(documents)
	candidate.Card, candidate.ModelScore, candidate.Analysis, candidate.Semantic = nil, nil, nil, nil
	candidate.Relevance, candidate.QueryRelevance, candidate.Uniqueness = nil, nil, nil
	for _, channel := range []**LiveChannel{&candidate.Patents, &candidate.PatentsCN, &candidate.Web, &candidate.Registry} {
		*channel = liveQualifiedChannel(*channel, ids)
	}
	// Карта Юна — против прежних медиан: набор после квалификации не меняется, меняется только тема.
	if candidate.Emergence != nil {
		candidate.Emergence = liveEmergenceOf(periods, candidate.Series, candidate.Emergence.Set)
	}
	liveAssess(&candidate, periods, documents)
	candidate.Comparison = liveCompare(periods, documents)
	// Проверка подтверждает технологию цитатами, а тренд измерен по всему направлению: ряд по одним цитатам
	// снова сделал бы из направления метод одной статьи.
	if liveFromDirection(candidate) {
		liveDirectionMeasure(&candidate, direction)
	}
	candidate.Adoption = liveAdoptionOf(liveTitles(documents))
	candidate.Market = liveMarketMaturity(candidate, bar).field()
	candidate.Confidence = new(liveConfidence(candidate, bar))
	candidate.EvidenceLevel = liveEvidenceThin
	// Ступень старой группы не переносится на более узкую сущность: рост и свежесть проверяются заново.
	growing := candidate.Growth.Prior.Count == 0 ||
		candidate.Growth.ShareRatio != nil && *candidate.Growth.ShareRatio >= 2
	if candidate.Stage != studyStageUnassessed && growing {
		switch {
		case candidate.Growth.Recent.Count >= liveCandidateRecentMin:
			candidate.EvidenceLevel = liveEvidenceStrict
		case candidate.Growth.Recent.Count >= 3:
			candidate.EvidenceLevel = liveEvidenceRelaxed
		}
	}
	return candidate
}

func liveQualifiedChannel(channel *LiveChannel, ids map[string]bool) *LiveChannel {
	if channel == nil {
		return nil
	}
	qualified := *channel
	qualified.Records = nil
	for _, record := range channel.Records {
		if ids[liveRecordPrefix+cmp.Or(record.ID, record.URL)] {
			qualified.Records = append(qualified.Records, record)
		}
	}
	if len(qualified.Records) == 0 {
		return nil
	}
	qualified.Total, qualified.Counts = len(qualified.Records), nil
	return &qualified
}
