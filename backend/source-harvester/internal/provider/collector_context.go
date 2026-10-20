package provider

import (
	"errors"
	"time"

	"github.com/example/source-harvester/internal/platform/sourcedoc"
)

func collectorInvestorContext(source *sourcedoc.Source, row collectorRecord, scope Scope) error {
	selected := scope.WorkType == sourcedoc.GenreInvestmentThesis
	if selected && (source.Provider != providerWeb || row.Genre != sourcedoc.GenreInvestmentThesis) {
		return errors.New("collector record does not match investment_thesis scope")
	}
	if row.Genre != sourcedoc.GenreInvestmentThesis && row.Genre != sourcedoc.GenreInvestorCommentary {
		return nil
	}
	source.Genre = row.Genre
	at, err := time.Parse(time.RFC3339, row.ObservedAt)
	if err != nil || at.After(source.ObservedAt) {
		return errors.New("investor context requires an original, non-future observation")
	}
	source.ObservedAt = at
	if selected && row.Date == "" {
		// Наблюдение ограничивает отбор контекста, но не становится датой публикации.
		source.Publication = nil
		day := at.UTC().Format(time.DateOnly)
		if day < scope.From || day > scope.Through {
			return errors.New("undated investor observation is outside the scope")
		}
	}
	return nil
}
