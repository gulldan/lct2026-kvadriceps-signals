package pubsearch

import (
	"context"
	"net/url"
	"strconv"
)

func (c *Client) searchOpenAlex(ctx context.Context, query string, limit, recentYears int) ([]Work, error) {
	client := c.oa.With(c.apiKey(ctx), c.mailto(ctx))
	if !client.Configured() {
		return nil, nil
	}
	q := url.Values{"search": {query}, "per-page": {strconv.Itoa(limit)}}
	if from := c.fromDate(recentYears); from != "" {
		q.Set("filter", "from_publication_date:"+from)
	}
	page, err := client.Works(ctx, q)
	if err != nil {
		return nil, err
	}
	works := make([]Work, 0, len(page.Results))
	for _, r := range page.Results {
		works = append(works, Work{Title: r.Title, Year: r.PublicationYear, DOI: r.PlainDOI(), Venue: r.Venue(),
			Abstract: r.Abstract(), Source: SourceOpenAlex})
	}
	return works, nil
}
