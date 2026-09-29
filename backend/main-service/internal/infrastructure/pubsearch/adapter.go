package pubsearch

import (
	"context"
	"strings"

	"github.com/example/main-service/internal/application"
	"github.com/example/main-service/internal/platform/runtimecfg"
)

type Adapter struct{ c *Client }

func NewAdapter(ovr *runtimecfg.Overrides) *Adapter { return &Adapter{c: New(ovr)} }

// SearchWorks при пустом запросе от LLM берёт детерминированный словарь обогащения руд.
func (a *Adapter) SearchWorks(
	ctx context.Context, query, goal, constraints string, limit int,
) ([]application.ExternalWork, error) {
	q := strings.TrimSpace(query)
	if q == "" {
		q = QueryFromRu(goal, constraints)
	}
	if q == "" {
		return nil, nil
	}
	works, err := a.c.Search(ctx, q, limit)
	if err != nil {
		return nil, err
	}
	out := make([]application.ExternalWork, 0, len(works))
	for _, w := range works {
		out = append(out, application.ExternalWork{
			Title: w.Title, Year: w.Year, DOI: w.DOI, Venue: w.Venue,
			Abstract: w.Abstract, Source: w.Source,
		})
	}
	return out, nil
}
