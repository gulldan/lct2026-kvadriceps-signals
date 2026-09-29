package analysisdoc

import (
	"time"

	"github.com/example/llm-service/internal/platform/sourcedoc"
)

// EligibleSource: позднее наблюдение допустимо только в ретроспективном режиме, который прямо говорит,
// что состав текущий, а окно историческое.
func EligibleSource(source sourcedoc.Source, request Request) (sourcedoc.Source, []string, string) {
	if (source.TestRunID != "" && source.TestRunID != request.TestRunID) || source.Kind == "hypothesis" {
		return source, nil, "non_product_source"
	}
	cutoff, err := time.Parse(time.RFC3339, request.Cutoff)
	if err != nil {
		return source, nil, "invalid_cutoff"
	}
	if source.Publication != nil {
		_, end := DateInterval(*source.Publication)
		if end.IsZero() || end.Add(-time.Second).After(cutoff) {
			return source, nil, "publication_interval_after_cutoff"
		}
	}
	available := source.ObservedAt
	if source.AvailableAt != nil {
		available = *source.AvailableAt
	}
	if available.After(cutoff) || (source.ArXiv != nil && source.ArXiv.VersionUpdated.After(cutoff)) {
		return source, nil, "revision_unavailable_at_cutoff"
	}
	limits := []string{}
	if source.TestRunID != "" {
		limits = append(limits, "test_source_in_isolated_analysis")
	}
	if source.ObservedAt.After(cutoff) {
		if NormalizeMode(request.Mode) != ModeRetrospective {
			return source, nil, "not_observed_at_cutoff"
		}
		limits = append(limits, "historical_reconstruction_with_current_models")
		// Пометки DOI и журнала в сегодняшнем Atom не несут исторических дат: более поздняя журнальная
		// идентичность не должна попасть в ранний запуск.
		if source.ArXiv != nil {
			arxiv := *source.ArXiv
			arxiv.DOI, arxiv.JournalRef = "", ""
			source.ArXiv = &arxiv
			limits = append(limits, "undated_later_doi_and_journal_annotations_removed")
		}
	}
	if source.Publication == nil && source.Kind != "software_repository" {
		limits = append(limits, "publication_date_unknown; excluded_from_temporal_support")
	}
	return source, limits, ""
}

// DateInterval: начало интервала нельзя показывать как известный день публикации.
func DateInterval(date sourcedoc.Date) (time.Time, time.Time) {
	if date.Validate() != nil {
		return time.Time{}, time.Time{}
	}
	layout := map[string]string{"year": "2006", "month": "2006-01", "day": time.DateOnly}[date.Precision]
	start, _ := time.Parse(layout, date.Value)
	switch date.Precision {
	case "year":
		return start, start.AddDate(1, 0, 0)
	case "month":
		return start, start.AddDate(0, 1, 0)
	default:
		return start, start.AddDate(0, 0, 1)
	}
}
