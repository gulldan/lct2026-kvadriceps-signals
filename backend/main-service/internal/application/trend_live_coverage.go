package application

import "slices"

func liveTemporalCoverageLimited(limits []LiveLimit) bool {
	return slices.ContainsFunc(limits, liveTemporalCoverageLimit)
}

func liveTemporalCoverageLimit(limit LiveLimit) bool {
	return limit.Code == liveLimitArxivKept || limit.Code == liveLimitScholarCapped
}

// Удаление чужих документов после отбора не восстанавливает пропущенные месяцы общего сбора.
func liveTemporalCoverageLimits(limits []LiveLimit) []LiveLimit {
	out := []LiveLimit{}
	for _, limit := range limits {
		if liveTemporalCoverageLimit(limit) {
			out = append(out, limit)
		}
	}
	return out
}

// Без полного временного покрытия числа выборки не дают ни ранней стадии, ни роста, ни отказа за спад.
func liveApplyTemporalCoverage(topic *LiveTopic) bool {
	if !liveTemporalCoverageLimited(topic.Limits) {
		return false
	}
	topic.Stage, topic.Momentum, topic.GrowthStart = studyStageUnassessed, studyUnknown, ""
	topic.Signals.Novelty, topic.Signals.Momentum = studyUnknown, studyUnknown
	topic.Emergence = nil
	topic.Growth.DeltaPP, topic.Growth.ShareRatio, topic.Growth.CountRatio = nil, nil, nil
	topic.Growth.Prior.Share, topic.Growth.Recent.Share = nil, nil
	for i := range topic.Series {
		topic.Series[i].Share = nil
	}
	return true
}
