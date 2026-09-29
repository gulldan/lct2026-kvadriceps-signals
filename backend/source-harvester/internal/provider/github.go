package provider

import (
	"context"
	"encoding/base64"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"path"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/example/source-harvester/internal/platform/sourcedoc"
)

// GitHub читает метаданные публичных репозиториев и README без клонирования, запуска кода и переадресаций download_url.
type GitHub struct {
	base    string
	fetcher *fetcher
}

// NewGitHub: необязательный токен оператора только на чтение; ответ всё равно обязан описывать публичный репозиторий.
func NewGitHub(token string) *GitHub {
	// Интервал укладывается и в десять запросов в минуту поиска без авторизации; паузу длиннее задают заголовки квоты.
	f := newFetcher(6 * time.Second)
	f.token = token
	f.accept = "application/vnd.github+json"
	return &GitHub{base: "https://api.github.com", fetcher: f}
}

type githubRepository struct {
	ID          int64      `json:"id"`
	FullName    string     `json:"full_name"`
	Private     *bool      `json:"private"`
	HTMLURL     string     `json:"html_url"`
	Description string     `json:"description"`
	Fork        *bool      `json:"fork"`
	Archived    *bool      `json:"archived"`
	Created     time.Time  `json:"created_at"`
	Updated     time.Time  `json:"updated_at"`
	Pushed      *time.Time `json:"pushed_at"`
	Stars       *int64     `json:"stargazers_count"`
	Forks       *int64     `json:"forks_count"`
	Source      *struct {
		ID int64 `json:"id"`
	} `json:"source"`
	License *struct {
		SPDX string `json:"spdx_id"`
	} `json:"license"`
}

type githubCommit struct {
	SHA    string `json:"sha"`
	Commit struct {
		Committer struct {
			Date time.Time `json:"date"`
		} `json:"committer"`
	} `json:"commit"`
}

type githubReadme struct {
	SHA      string `json:"sha"`
	Path     string `json:"path"`
	Size     int    `json:"size"`
	Encoding string `json:"encoding"`
	Content  string `json:"content"`
}

var repositoryName = regexp.MustCompile(`^[A-Za-z0-9_.-]{1,39}/[A-Za-z0-9_.-]{1,100}$`)
var gitObjectSHA = regexp.MustCompile(`^(?:[0-9a-f]{40}|[0-9a-f]{64})$`)

// Fetch: поиск GitHub режется на 1000 результатов, поэтому неполнота пишется, даже если страницы кончились.
func (g *GitHub) Fetch(ctx context.Context, scope Scope, cursor string) (Page, error) {
	if err := scope.ValidateFor("github"); err != nil {
		return Page{}, err
	}
	if scope.PageSize > 10 {
		return Page{}, errors.New("github page_size is limited to 10 enriched repositories")
	}
	sort := scope.Sort
	if sort == "" {
		sort = "updated" // Смысл уже сохранённых курсоров не меняется.
	}
	if sort != "updated" && sort != "stars" {
		return Page{}, errors.New("unsupported github collection sort")
	}
	if cursor == "" {
		cursor = "1"
	}
	n, err := strconv.Atoi(cursor)
	if err != nil || n < 1 || (n-1)*scope.PageSize >= 1000 {
		return Page{}, errors.New("github cursor exceeds search coverage")
	}
	query := scope.Query + " in:name,description,readme is:public fork:true created:" + scope.From + ".." + scope.Through
	q := url.Values{"q": {query},
		"sort": {sort}, "order": {"desc"}, "per_page": {strconv.Itoa(scope.PageSize)}, "page": {cursor}}
	raw, observed, err := g.fetcher.get(ctx, g.base+"/search/repositories?"+q.Encode())
	if err != nil {
		return Page{}, err
	}
	p, repos, decodeErr := decodeGithubSearch(raw, observed, scope, n)
	if decodeErr != nil {
		return Page{}, decodeErr
	}
	for _, repo := range repos {
		if repo.Private == nil || *repo.Private || repo.ID <= 0 {
			p.Rejected = append(p.Rejected, Rejected{NativeID: strconv.FormatInt(repo.ID, 10), Reason: "non-public repository"})
			continue
		}
		record, err := g.repository(ctx, repo.ID, scope)
		if err != nil {
			if collectionRequestFailed(err) {
				return Page{}, err
			}
			p.Rejected = append(p.Rejected, Rejected{NativeID: strconv.FormatInt(repo.ID, 10), Reason: err.Error()})
			continue
		}
		p.Records = append(p.Records, record)
	}
	return p, nil
}

func (g *GitHub) repository(ctx context.Context, id int64, scope Scope) (Record, error) {
	raw, observed, err := g.fetcher.get(ctx, g.base+"/repositories/"+strconv.FormatInt(id, 10))
	if err != nil {
		return Record{}, err
	}
	var repo githubRepository
	if decodeErr := json.Unmarshal(raw, &repo); decodeErr != nil {
		return Record{}, fmt.Errorf("decode github repository: %w", decodeErr)
	}
	if repo.ID != id || repo.Private == nil || *repo.Private || repo.Fork == nil || repo.Archived == nil ||
		!repositoryName.MatchString(repo.FullName) ||
		repo.HTMLURL != "https://github.com/"+repo.FullName {
		return Record{}, errors.New("github repository identity, URL or visibility changed")
	}
	if repo.Created.IsZero() || repo.Created.UTC().Format(time.DateOnly) < scope.From ||
		repo.Created.UTC().Format(time.DateOnly) > scope.Through {
		return Record{}, errors.New("repository created_at is outside the requested interval")
	}
	base := g.base + "/repos/" + repo.FullName
	commitQuery := url.Values{"per_page": {"1"}, "until": {scope.Through + "T23:59:59Z"}}
	commitRaw, _, err := g.fetcher.get(ctx, base+"/commits?"+commitQuery.Encode())
	if err != nil {
		return Record{}, err
	}
	var commits []githubCommit
	if decodeErr := json.Unmarshal(commitRaw, &commits); decodeErr != nil {
		return Record{}, fmt.Errorf("decode github commit: %w", decodeErr)
	}
	if len(commits) != 1 || !gitObjectSHA.MatchString(commits[0].SHA) || commits[0].Commit.Committer.Date.IsZero() ||
		commits[0].Commit.Committer.Date.UTC().Format(time.DateOnly) > scope.Through {
		return Record{}, errors.New("no pinned commit available at requested cutoff")
	}
	commit := commits[0]
	readme, readmeRaw, readmeText, err := g.readme(ctx, base, commit.SHA)
	if err != nil {
		return Record{}, err
	}
	releases, releaseRaw, limited, err := g.releases(ctx, base, scope.Through)
	if err != nil {
		return Record{}, err
	}
	combined, err := json.Marshal(map[string]jsontext.Value{
		"repository": raw, "commit": commitRaw, "readme": readmeRaw, "releases": releaseRaw})
	if err != nil {
		return Record{}, fmt.Errorf("encode github raw metadata: %w", err)
	}
	return githubRecord(repo, commit, readme, readmeText, releases, limited, observed, combined)
}

func (g *GitHub) readme(ctx context.Context, base, sha string) (*githubReadme, []byte, string, error) {
	raw, _, err := g.fetcher.get(ctx, base+"/readme?"+url.Values{"ref": {sha}}.Encode())
	if httpErr, ok := errors.AsType[*HTTPError](err); ok && httpErr.Status == http.StatusNotFound {
		return nil, []byte("null"), "", nil
	}
	if err != nil {
		return nil, nil, "", err
	}
	var readme githubReadme
	if decodeErr := json.Unmarshal(raw, &readme); decodeErr != nil {
		return nil, nil, "", fmt.Errorf("decode github README: %w", decodeErr)
	}
	if readme.Size < 0 || readme.Size > 1<<20 || readme.Encoding != "base64" {
		return nil, nil, "", errors.New("unsupported or oversized README")
	}
	if !gitObjectSHA.MatchString(readme.SHA) || readme.Path == "" || len(readme.Path) > 4096 ||
		path.Clean(readme.Path) != readme.Path || path.IsAbs(readme.Path) || strings.HasPrefix(readme.Path, "../") {
		return nil, nil, "", errors.New("README lacks immutable blob SHA or path")
	}
	text, err := base64.StdEncoding.DecodeString(strings.ReplaceAll(readme.Content, "\n", ""))
	if err != nil || len(text) != readme.Size || !utf8.Valid(text) {
		return nil, nil, "", errors.New("README length, base64 or UTF-8 mismatch")
	}
	return &readme, raw, string(text), nil
}

func (g *GitHub) releases(ctx context.Context, base, cutoff string) ([]Release, []byte, bool, error) {
	raw, _, err := g.fetcher.get(ctx, base+"/releases?per_page=100")
	if err != nil {
		return nil, nil, false, err
	}
	var items []struct {
		ID        int64      `json:"id"`
		URL       string     `json:"html_url"`
		Tag       string     `json:"tag_name"`
		Published *time.Time `json:"published_at"`
		Draft     bool       `json:"draft"`
		Body      string     `json:"body"`
	}
	if err := json.Unmarshal(raw, &items); err != nil {
		return nil, nil, false, fmt.Errorf("decode github releases: %w", err)
	}
	if items == nil || len(items) > 100 {
		return nil, nil, false, errors.New("invalid github release list")
	}
	var releases []Release
	for _, r := range items {
		if r.Draft || r.Published == nil || r.Published.UTC().Format(time.DateOnly) > cutoff {
			continue
		}
		if r.ID <= 0 || r.Published.IsZero() ||
			!strings.HasPrefix(r.URL, "https://github.com/"+strings.TrimPrefix(base, g.base+"/repos/")+"/releases/") {
			return nil, nil, false, errors.New("invalid published github release")
		}
		releases = append(releases, Release{ID: r.ID, URL: r.URL, Tag: r.Tag, Published: *r.Published, Body: r.Body})
	}
	slices.SortFunc(releases, func(a, b Release) int { return b.Published.Compare(a.Published) })
	return releases, raw, len(items) == 100, nil
}

func collectionRequestFailed(err error) bool {
	if _, ok := errors.AsType[*RequestError](err); !ok {
		return false
	}
	httpErr, isHTTP := errors.AsType[*HTTPError](err)
	return !isHTTP || httpErr.Status != http.StatusNotFound
}

func decodeGithubSearch(raw []byte, observed time.Time, scope Scope, n int) (Page, []githubRepository, error) {
	var search struct {
		Total      *int               `json:"total_count"`
		Incomplete bool               `json:"incomplete_results"`
		Items      []githubRepository `json:"items"`
	}
	if err := json.Unmarshal(raw, &search); err != nil {
		return Page{}, nil, fmt.Errorf("decode github search: %w", err)
	}
	if search.Total == nil || *search.Total < 0 || search.Items == nil || len(search.Items) > scope.PageSize {
		return Page{}, nil, errors.New("invalid github search envelope")
	}
	count := (n-1)*scope.PageSize + len(search.Items)
	p := Page{Provider: "github", Scope: scope, Cursor: strconv.Itoa(n), Total: *search.Total, Received: len(search.Items),
		Exhausted: count >= *search.Total || count >= 1000, Observed: observed, Raw: raw, RawHash: digest(raw)}
	if search.Incomplete {
		p.Limitations = append(p.Limitations, "github_incomplete_results")
	}
	if *search.Total > 1000 {
		p.Limitations = append(p.Limitations, "github_search_capped_at_1000")
	}
	if !p.Exhausted && len(search.Items) < scope.PageSize {
		p.Exhausted = true
		p.Limitations = append(p.Limitations, "github_short_page_before_total")
	}
	if !p.Exhausted {
		p.Next = strconv.Itoa(n + 1)
	}
	return p, search.Items, nil
}

func githubRecord(repo githubRepository, commit githubCommit, readme *githubReadme, readmeText string,
	releases []Release, limited bool, observed time.Time, raw []byte) (Record, error) {
	id := repo.ID
	text, status := repo.FullName+"\n\n"+repo.Description, "repository_metadata_only"
	limitations := []string{"historical_reconstruction_from_current_api", "stars_before_observation_unknown",
		"commit_date_is_not_public_availability"}
	if readme != nil {
		text = readmeText
		status = "pinned_readme"
	} else {
		limitations = append(limitations, "readme_unavailable")
	}
	if limited {
		limitations = append(limitations, "release_listing_truncated")
	}
	if len(releases) == 0 {
		limitations = append(limitations, "no_published_release_observed_in_window")
	}
	github := &sourcedoc.GitHub{RepositoryID: id, CommitSHA: commit.SHA, Fork: *repo.Fork, Archived: *repo.Archived,
		CreatedAt: repo.Created, UpdatedAt: repo.Updated, PushedAt: repo.Pushed, Stars: repo.Stars, Forks: repo.Forks}
	if repo.License != nil {
		github.License = repo.License.SPDX
	}
	if repo.Source != nil {
		github.RootID = new(repo.Source.ID)
	}
	if *repo.Fork && repo.Source == nil {
		limitations = append(limitations, "fork_root_unknown")
	}
	if readme != nil {
		github.BlobSHA = readme.SHA
		github.ReadmePath = readme.Path
	}
	if len(releases) > 0 {
		github.ReleaseID = new(releases[0].ID)
		github.ReleasedAt = new(releases[0].Published)
	}
	source := sourcedoc.Source{Version: 1, Kind: "software_repository", Provider: "github",
		NativeID: strconv.FormatInt(id, 10), URL: repo.HTMLURL,
		CanonicalID: "repository:" + strconv.FormatInt(id, 10), ObservedAt: observed,
		Revision:    commit.SHA + "@" + observed.Format(time.RFC3339Nano),
		ContentHash: digest([]byte(text)), HashAlgorithm: hashSHA256, AccessScope: scopePublic, GitHub: github}
	if err := source.Validate(); err != nil {
		return Record{}, err
	}
	contentURL := ""
	if readme != nil {
		permalink := url.URL{Scheme: schemeHTTPS, Host: "github.com",
			Path: "/" + repo.FullName + "/blob/" + commit.SHA + "/" + readme.Path}
		contentURL = permalink.String()
	}
	return Record{Source: source, Title: repo.FullName, Text: text, TextStatus: status, Raw: raw,
		Limitations: limitations, ContentURL: contentURL, Releases: releases}, nil
}
