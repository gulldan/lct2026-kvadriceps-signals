package application

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"net/http"

	"github.com/example/main-service/internal/platform/config"
	"github.com/example/main-service/internal/platform/jsonx"
)

// У API Product Hunt нет поиска по тексту: запуски берутся по темам и фильтруются по описанию. Условия API
// запрещают коммерческое использование без согласования, поэтому нужен PRODUCTHUNT_TOKEN владельца.

const (
	liveEventSourcePH     = "producthunt"
	liveProductHuntAPI    = "https://api.producthunt.com/v2/api/graphql"
	liveProductHuntTopics = 2
	liveProductHuntPosts  = 50
)

type liveProductHuntResponse struct {
	Data struct {
		Topics struct {
			Edges []struct {
				Node struct {
					Slug string `json:"slug"`
				} `json:"node"`
			} `json:"edges"`
		} `json:"topics"`
		Posts struct {
			Edges []struct {
				Node struct {
					ID          string `json:"id"`
					Name        string `json:"name"`
					Tagline     string `json:"tagline"`
					Description string `json:"description"`
					URL         string `json:"url"`
					Created     string `json:"createdAt"`
				} `json:"node"`
			} `json:"edges"`
		} `json:"posts"`
	} `json:"data"`
	Errors []struct {
		Message string `json:"message"`
	} `json:"errors"`
}

func liveProductHuntEvents(ctx context.Context, client *http.Client, query LiveEventQuery) ([]LiveAdoptionEvent, error) {
	header := http.Header{"Authorization": {"Bearer " + config.Get("PRODUCTHUNT_TOKEN", "")},
		"Content-Type": {"application/json"}}
	topics, err := liveProductHuntQuery(ctx, client, header,
		`query($q:String!,$n:Int!){topics(query:$q,first:$n){edges{node{slug}}}}`,
		map[string]any{"q": query.Term, "n": liveProductHuntTopics})
	if err != nil {
		return nil, err
	}
	events := []LiveAdoptionEvent{}
	seen := map[string]bool{}
	for _, topic := range topics.Data.Topics.Edges {
		posts, err := liveProductHuntQuery(ctx, client, header,
			`query($t:String!,$n:Int!){posts(topic:$t,first:$n,order:VOTES){edges{node{`+
				`id name tagline description url createdAt}}}}`,
			map[string]any{"t": topic.Node.Slug, "n": liveProductHuntPosts})
		if err != nil {
			return events, err
		}
		for _, edge := range posts.Data.Posts.Edges {
			post := edge.Node
			quote := cmp.Or(liveTermSentence(post.Tagline, query.Term), liveTermSentence(post.Description, query.Term))
			if quote == "" || seen[post.ID] || len(post.Created) < 10 {
				continue
			}
			seen[post.ID] = true
			events = append(events, LiveAdoptionEvent{Type: LiveEventProductLaunch, Source: liveEventSourcePH,
				Record: liveMarketRecord("producthunt:"+post.ID, post.Name+" — "+post.Tagline, post.Created[:10],
					post.URL, liveEventSourcePH, "дата публикации запуска на Product Hunt"),
				Actor: post.Name, Quote: quote})
		}
	}
	return events, nil
}

func liveProductHuntQuery(ctx context.Context, client *http.Client, header http.Header, graphQL string,
	variables map[string]any) (liveProductHuntResponse, error) {
	var out liveProductHuntResponse
	body, err := jsonx.Marshal(struct {
		Query     string         `json:"query"`
		Variables map[string]any `json:"variables"`
	}{graphQL, variables})
	if err != nil {
		return out, fmt.Errorf("producthunt: %w", err)
	}
	raw, err := liveMarketFetch(ctx, client, http.MethodPost, liveProductHuntAPI, header, body, liveMarketMaxJSON)
	if err != nil {
		return out, fmt.Errorf("producthunt: %w", err)
	}
	if err := jsonx.Unmarshal(raw, &out); err != nil {
		return out, fmt.Errorf("producthunt: parse: %w", err)
	}
	if len(out.Errors) > 0 {
		return out, errors.New("producthunt: " + out.Errors[0].Message)
	}
	return out, nil
}
