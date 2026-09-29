package trendsemantic

import (
	"cmp"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json/v2"
	"fmt"
	"io"
	"maps"
	"net/http"
	"net/url"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/example/main-service/internal/application"
)

// GitHubLineage читает только явные утверждения fork-сети; удачный ответ «не форк» — ещё не проверка
// независимости.
type GitHubLineage struct {
	URL, Token string
	HTTP       *http.Client
	Now        func() time.Time
}

func NewGitHubLineage(endpoint, token string) *GitHubLineage {
	return &GitHubLineage{URL: strings.TrimRight(endpoint, "/"), Token: token, Now: time.Now,
		HTTP: &http.Client{Timeout: 10 * time.Second}}
}

var githubName = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.-]*$`)

func githubRepository(raw string) string {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" || !strings.EqualFold(u.Host, "github.com") || u.User != nil {
		return ""
	}
	parts := strings.Split(strings.Trim(u.Path, "/"), "/")
	if len(parts) < 2 || !githubName.MatchString(parts[0]) || !githubName.MatchString(parts[1]) {
		return ""
	}
	return "https://github.com/" + strings.ToLower(parts[0]) + "/" + strings.TrimSuffix(strings.ToLower(parts[1]), ".git")
}

func (c *GitHubLineage) Enrich(
	ctx context.Context, input application.LiveAnalysisRequest, budget int,
) (map[string]application.LivePaperMetadata, []application.LiveLimit) {
	metadata := maps.Clone(input.Metadata)
	if metadata == nil {
		metadata = map[string]application.LivePaperMetadata{}
	}
	if !input.EnrichMetadata || input.EvidenceOnly || input.StrictAvailability {
		return metadata, nil
	}
	refs := githubReferences(input.Snapshot)
	repositories := slices.Sorted(maps.Keys(refs))
	checked := func(repository string) string {
		return latestLineageCheck(refs[repository], metadata)
	}
	slices.SortFunc(repositories, func(a, b string) int { return cmp.Or(cmp.Compare(checked(a), checked(b)), cmp.Compare(a, b)) })
	attempted, pending := 0, 0
	for _, repository := range repositories {
		at, err := time.Parse(time.RFC3339Nano, checked(repository))
		if err == nil && c.Now().Sub(at) >= 0 && c.Now().Sub(at) < 7*24*time.Hour {
			continue
		}
		if attempted >= max(0, min(32, budget)) {
			pending++
			continue
		}
		if attempted > 0 {
			select {
			case <-ctx.Done():
				return metadata, []application.LiveLimit{{Code: application.LiveLimitLineageOff,
					Text: "github lineage: collection interrupted; prior observations retained"}}
			case <-time.After(time.Second):
			}
		}
		attempted++
		links, hash, status := c.lookup(ctx, repository)
		if status == providerUnavailable || status == "rate_limited" {
			return metadata, []application.LiveLimit{{Code: application.LiveLimitLineageOff,
				Text: "github lineage: " + status + "; prior observations retained"}}
		}
		observed := c.Now().UTC().Format(time.RFC3339Nano)
		for i := range links {
			links[i].ObservedAt, links[i].SourceHash = observed, hash
		}
		updateRepositoryLineage(metadata, refs[repository], repository, links, observed, hash, status)
	}
	if pending > 0 {
		return metadata, []application.LiveLimit{{Code: application.LiveLimitLineageBudget,
			Values: map[string]int{"pending": pending, "checked": attempted},
			Text: fmt.Sprintf("github lineage: %d unchecked repository refs remain after %d lookups",
				pending, attempted)}}
	}
	return metadata, nil
}

func latestLineageCheck(refs []string, metadata map[string]application.LivePaperMetadata) string {
	latest := ""
	for _, ref := range refs {
		latest = max(latest, metadata[ref].LineageCheckedAt)
	}
	return latest
}

func githubReferences(snapshot application.LiveSnapshot) map[string][]string {
	refs := map[string][]string{}
	for _, topic := range slices.Concat(snapshot.Topics, snapshot.Discovered) {
		for _, event := range topic.External {
			if repository := githubRepository(event.URL); repository != "" {
				refs[repository] = append(refs[repository], event.URL)
			}
		}
	}
	return refs
}

func updateRepositoryLineage(
	metadata map[string]application.LivePaperMetadata, refs []string, repository string,
	links []application.LiveEvidenceLink, observed, hash, status string,
) {
	sourceURL := "https://api.github.com/repos/" + strings.TrimPrefix(repository, "https://github.com/")
	for _, ref := range refs {
		meta := metadata[ref]
		// Неизвестные или «не форк» текущие метаданные не опровергают подтверждённое ранее происхождение:
		// датированное доказательство живёт до нового утверждения о форке.
		meta.Links = slices.Clone(meta.Links)
		if status == "provider_linked" {
			meta.Links = slices.DeleteFunc(meta.Links, func(link application.LiveEvidenceLink) bool {
				return link.Kind == "fork" && link.SourceURL == sourceURL
			})
		}
		meta.Links = append(meta.Links, links...)
		meta.LineageCheckedAt, meta.LineageStatus, meta.LineageHash = observed, status, hash
		metadata[ref] = meta
	}
}

func (c *GitHubLineage) lookup(ctx context.Context, repository string) ([]application.LiveEvidenceLink, string, string) {
	name := strings.TrimPrefix(repository, "https://github.com/")
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, c.URL+"/repos/"+name, http.NoBody)
	if err != nil {
		return nil, "", providerUnavailable
	}
	request.Header.Set("Accept", "application/vnd.github+json")
	request.Header.Set("X-Github-Api-Version", "2022-11-28")
	request.Header.Set("User-Agent", "rag-test-radar-analysis/1.0")
	if c.Token != "" {
		request.Header.Set("Authorization", "Bearer "+c.Token)
	}
	response, err := c.HTTP.Do(request)
	if err != nil {
		return nil, "", providerUnavailable
	}
	defer func() { _ = response.Body.Close() }()
	limited := response.Header.Get("X-Ratelimit-Remaining") == "0" || response.Header.Get("Retry-After") != ""
	if response.StatusCode == http.StatusTooManyRequests || (response.StatusCode == http.StatusForbidden && limited) {
		return nil, "", "rate_limited"
	}
	if response.StatusCode != http.StatusOK && response.StatusCode != http.StatusNotFound {
		return nil, "", providerUnavailable
	}
	raw, err := io.ReadAll(io.LimitReader(response.Body, (2<<20)+1))
	if err != nil || len(raw) > 2<<20 {
		return nil, "", providerUnavailable
	}
	digest := sha256.Sum256(raw)
	hash := hex.EncodeToString(digest[:])
	if response.StatusCode == http.StatusNotFound {
		return nil, hash, "not_found"
	}
	type ref struct {
		ID      int64  `json:"id"`
		HTMLURL string `json:"html_url"`
	}
	var record struct {
		ref
		Fork   bool `json:"fork"`
		Parent ref  `json:"parent"`
		Source ref  `json:"source"`
	}
	if json.Unmarshal(raw, &record) != nil || record.ID <= 0 || githubRepository(record.HTMLURL) != repository {
		return nil, hash, "identity_mismatch"
	}
	if !record.Fork {
		return nil, hash, "not_fork_not_independence"
	}
	links := []application.LiveEvidenceLink{}
	seen := map[string]bool{}
	for _, parent := range []ref{record.Parent, record.Source} {
		u := githubRepository(parent.HTMLURL)
		if parent.ID > 0 && parent.ID != record.ID && u != "" && u != repository && !seen[u] {
			seen[u] = true
			links = append(links, application.LiveEvidenceLink{
				Kind: "fork", URL: repository, ParentURL: u, SourceURL: "https://api.github.com/repos/" + name,
			})
		}
	}
	if len(links) == 0 {
		return nil, hash, "fork_parent_unknown"
	}
	return links, hash, "provider_linked"
}
