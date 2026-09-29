package application

import (
	"cmp"
	"context"
	"encoding/json/v2"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"slices"
	"strconv"
	"strings"
	"time"
)

// Пакет засчитывается, только если deps.dev связывает его с репозиторием; описание и звёзды тоже берутся у deps.dev:
// без токена GitHub не отдаёт звёзды (401), а pypi.org со стенда недоступен.

const (
	// liveEventRepos — репозиториев на технологию: без токена у GitHub шестьдесят запросов в час на адрес.
	liveEventRepos      = 3
	liveGitHubAPI       = "https://api.github.com"
	liveGitHubRaw       = "https://raw.githubusercontent.com"
	liveDepsDev         = "https://api.deps.dev/v3/systems"
	liveDepsDevProjects = "https://api.deps.dev/v3/projects"
	livePyPIStats       = "https://pypistats.org/api/packages"
	liveNPMDownloads    = "https://api.npmjs.org/downloads/range/last-year"
	liveHFModels        = "https://huggingface.co/api/models"
	// Дальше пятой модели автора идут дообучения и копии.
	liveHFModelsKept = 5
)

const (
	liveEventSourceGitHub = "github"
	liveEventSourcePyPI   = "pypi"
	liveEventSourceNPM    = "npm"
	liveEventSourceHF     = "huggingface"
	// Скачивания подписаны единицей liveBetKindDownloads: по ней их ищет прогноз.
	liveEventUnitReleases = "releases"
)

// errLiveEventMissing — источник ответил, что объекта нет: это ответ, а не отказ.
var errLiveEventMissing = errors.New("not found")

func liveEventFetch(ctx context.Context, src *liveSources, address string, headers map[string]string,
) ([]byte, http.Header, error) {
	body, header, status, err := src.get(ctx, address, headers)
	switch {
	case err != nil:
		return nil, nil, err
	case status == http.StatusNotFound:
		return nil, header, errLiveEventMissing
	case status != http.StatusOK:
		return nil, header, fmt.Errorf("%s: HTTP %d", passportHost(address), status)
	}
	return body, header, nil
}

func liveEventPresent(err error) error {
	if errors.Is(err, errLiveEventMissing) {
		return nil
	}
	return err
}

func liveEventJSON(ctx context.Context, src *liveSources, address string, headers map[string]string, into any,
) (http.Header, error) {
	body, header, err := liveEventFetch(ctx, src, address, headers)
	if err != nil {
		return header, err
	}
	if err := json.Unmarshal(body, into); err != nil {
		return header, fmt.Errorf("%s: parse: %w", passportHost(address), err)
	}
	return header, nil
}

func liveEventRecord(record LiveSourceRecord, text string) LiveSourceRecord {
	records := []LiveSourceRecord{record}
	stampPassports(records)
	record = records[0]
	record.ObservedAt = time.Now().UTC().Format(time.RFC3339)
	switch script := liveTextScript(text); {
	case script != "":
		record.Language, record.LanguageBasis = script, "определён по преобладающему алфавиту текста"
	case liveLatin(text):
		record.Language, record.LanguageBasis = "en", "язык не заявлен источником; описание на латинице"
	}
	return record
}

func liveStampDay(stamp string) string {
	if len(stamp) < len(time.DateOnly) {
		return ""
	}
	return stamp[:len(time.DateOnly)]
}

func liveEventRepoNames(repos []string) []string {
	names := []string{}
	for _, repo := range repos {
		repo = strings.TrimSpace(repo)
		for _, prefix := range []string{"https://", "http://", "www."} {
			repo = strings.TrimPrefix(repo, prefix)
		}
		parts := strings.Split(strings.Trim(strings.TrimPrefix(repo, "github.com/"), "/"), "/")
		if len(parts) < 2 || strings.Contains(parts[0], ".") {
			continue
		}
		name := parts[0] + "/" + strings.TrimSuffix(parts[1], ".git")
		if !livePackageName(name) || slices.Contains(names, name) {
			continue
		}
		names = append(names, name)
		if len(names) == liveEventRepos {
			break
		}
	}
	return names
}

// livePackageName отсеивает шаблон cookiecutter и переменную вместо имени пакета.
func livePackageName(name string) bool {
	if name == "" || len(name) > 214 {
		return false
	}
	for _, r := range name {
		switch {
		case r|0x20 >= 'a' && r|0x20 <= 'z', r >= '0' && r <= '9', strings.ContainsRune("._-@/", r):
		default:
			return false
		}
	}
	return true
}

func liveGitHubHeaders() map[string]string {
	headers := map[string]string{"Accept": "application/vnd.github+json"}
	if token := os.Getenv("GITHUB_TOKEN"); token != "" {
		headers["Authorization"] = "Bearer " + token
	}
	return headers
}

// liveLastPage: при одной записи на странице число страниц — это число релизов, а первая страница — последний релиз.
func liveLastPage(link string, fallback int) int {
	for part := range strings.SplitSeq(link, ",") {
		target, rel, ok := strings.Cut(part, ";")
		if !ok || !strings.Contains(rel, `rel="last"`) {
			continue
		}
		address, err := url.Parse(strings.Trim(strings.TrimSpace(target), "<>"))
		if err != nil {
			continue
		}
		if page, err := strconv.Atoi(address.Query().Get("page")); err == nil && page > 0 {
			return page
		}
	}
	return fallback
}

type liveGitHubRelease struct {
	Tag       string `json:"tag_name"`
	Name      string `json:"name"`
	URL       string `json:"html_url"`
	Published string `json:"published_at"`
}

func liveGitHubEvents(ctx context.Context, client *http.Client, query LiveEventQuery) ([]LiveAdoptionEvent, error) {
	src := &liveSources{http: client}
	headers := liveGitHubHeaders()
	events := []LiveAdoptionEvent{}
	for _, repo := range liveEventRepoNames(query.Repos) {
		var releases []liveGitHubRelease
		header, err := liveEventJSON(ctx, src, liveGitHubAPI+"/repos/"+repo+"/releases?per_page=1", headers, &releases)
		if err != nil && !errors.Is(err, errLiveEventMissing) {
			return events, fmt.Errorf("github: %w", err)
		}
		if len(releases) == 0 {
			continue
		}
		var meta struct {
			Description string `json:"description"`
			Stars       int    `json:"starsCount"`
		}
		if _, err := liveEventJSON(ctx, src, liveDepsDevProjects+"/"+url.PathEscape("github.com/"+repo), nil, &meta); err != nil &&
			!errors.Is(err, errLiveEventMissing) {
			return events, fmt.Errorf("github: %w", err)
		}
		release := releases[0]
		owner, _, _ := strings.Cut(repo, "/")
		count := liveLastPage(header.Get("Link"), len(releases))
		date := liveStampDay(release.Published)
		record := liveEventRecord(LiveSourceRecord{
			ID: "github:" + repo + "@" + release.Tag, Title: repo + " " + release.Tag,
			Date: date, URL: release.URL, Source: "GitHub", PublishedAt: release.Published,
			TitleBasis: "имя репозитория и метка последнего релиза", DateBasis: "дата публикации последнего релиза",
			Excerpt: strings.TrimSpace(fmt.Sprintf("%s Релизов: %d, последний — %s от %s. Звёзд: %d.",
				meta.Description, count, release.Tag, date, meta.Stars)),
		}, meta.Description)
		events = append(events, LiveAdoptionEvent{Type: LiveEventCodeRelease, Source: liveEventSourceGitHub, Record: record,
			Actor: owner, Phase: release.Tag, Quote: cmp.Or(meta.Description, release.Name), Count: &count,
			Unit: liveEventUnitReleases})
	}
	return events, nil
}

func livePythonManifests() []string { return []string{"pyproject.toml", "setup.cfg", "setup.py"} }

// livePythonPackage разбирает манифест построчно, без библиотеки TOML: нужны только имя и описание.
func livePythonPackage(file, body string) (name, description string) {
	if file == "setup.py" {
		at := strings.Index(body, "setup(")
		if at < 0 {
			return "", ""
		}
		return liveSetupArgument(body[at:], "name"), liveSetupArgument(body[at:], "description")
	}
	section := ""
	for line := range strings.Lines(body) {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "[") {
			section = strings.Trim(line, "[] ")
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		if !ok || (section != "project" && section != "tool.poetry" && section != "metadata") {
			continue
		}
		value = strings.Trim(strings.TrimSpace(value), `"'`)
		switch strings.TrimSpace(key) {
		case "name":
			name = cmp.Or(name, value)
		case "description":
			description = cmp.Or(description, value)
		}
	}
	return name, description
}

// liveSetupArgument читает только строковый литерал: значение переменной известно лишь при исполнении setup.py.
func liveSetupArgument(call, key string) string {
	for rest := call; ; {
		at := strings.Index(rest, key)
		if at < 0 {
			return ""
		}
		before := rest[:at]
		rest = rest[at+len(key):]
		if before != "" && liveIdentByte(before[len(before)-1]) {
			continue
		}
		value := strings.TrimLeft(rest, " \t")
		if !strings.HasPrefix(value, "=") {
			continue
		}
		value = strings.TrimLeft(value[1:], " \t")
		if value == "" || (value[0] != '"' && value[0] != '\'') {
			continue
		}
		if end := strings.IndexByte(value[1:], value[0]); end >= 0 {
			return value[1 : end+1]
		}
		return ""
	}
}

func liveIdentByte(b byte) bool {
	return b == '_' || (b|0x20 >= 'a' && b|0x20 <= 'z') || (b >= '0' && b <= '9')
}

func livePyPIName(name string) string {
	return strings.Join(strings.FieldsFunc(strings.ToLower(name), func(r rune) bool {
		return r == '-' || r == '_' || r == '.'
	}), "-")
}

type liveDepsRelease struct {
	Key struct {
		Version string `json:"version"`
	} `json:"versionKey"`
	PublishedAt string `json:"publishedAt"`
	IsDefault   bool   `json:"isDefault"`
}

type liveDepsVersion struct {
	Links []struct {
		Label string `json:"label"`
		URL   string `json:"url"`
	} `json:"links"`
	Related []struct {
		Project struct {
			ID string `json:"id"`
		} `json:"projectKey"`
	} `json:"relatedProjects"`
}

func liveDepsDevRepo(version liveDepsVersion, repo string) bool {
	want := "github.com/" + strings.ToLower(repo)
	for _, related := range version.Related {
		if strings.ToLower(related.Project.ID) == want {
			return true
		}
	}
	for _, link := range version.Links {
		address := strings.ToLower(link.URL)
		for _, prefix := range []string{"git+", "https://", "http://", "ssh://", "git://", "git@", "www."} {
			address = strings.TrimPrefix(address, prefix)
		}
		address = strings.TrimSuffix(strings.TrimSuffix(strings.Replace(address, ":", "/", 1), "/"), ".git")
		if address == want || strings.HasPrefix(address, want+"/") {
			return true
		}
	}
	return false
}

// liveDepsDevConfirm: без связи deps.dev с репозиторием имя пакета принадлежит кому-то другому.
func liveDepsDevConfirm(ctx context.Context, src *liveSources, system, name, repo string) (string, string, error) {
	base := liveDepsDev + "/" + system + "/packages/" + url.PathEscape(name)
	var pkg struct {
		Versions []liveDepsRelease `json:"versions"`
	}
	if _, err := liveEventJSON(ctx, src, base, nil, &pkg); err != nil {
		return "", "", liveEventPresent(err)
	}
	at := slices.IndexFunc(pkg.Versions, func(v liveDepsRelease) bool { return v.IsDefault })
	if at < 0 {
		return "", "", nil
	}
	current := pkg.Versions[at]
	var version liveDepsVersion
	if _, err := liveEventJSON(ctx, src, base+"/versions/"+url.PathEscape(current.Key.Version), nil, &version); err != nil {
		return "", "", liveEventPresent(err)
	}
	if !liveDepsDevRepo(version, repo) {
		return "", "", nil
	}
	return current.Key.Version, current.PublishedAt, nil
}

// liveMonthDownloads считает от последнего дня ряда: реестры отдают скачивания с задержкой в сутки-двое.
func liveMonthDownloads(days map[string]int) (last, previous int, through string) {
	for day := range days {
		through = max(through, day)
	}
	end, err := time.Parse(time.DateOnly, through)
	if err != nil {
		return 0, 0, ""
	}
	lastFrom := end.AddDate(0, 0, -29).Format(time.DateOnly)
	previousFrom := end.AddDate(0, 0, -59).Format(time.DateOnly)
	for day, n := range days {
		switch {
		case day >= lastFrom:
			last += n
		case day >= previousFrom:
			previous += n
		}
	}
	return last, previous, through
}

// liveRawFile читает файл по raw-адресу: он не расходует лимит API GitHub.
func liveRawFile(ctx context.Context, src *liveSources, repo, file string) (string, error) {
	body, _, err := liveEventFetch(ctx, src, liveGitHubRaw+"/"+repo+"/HEAD/"+file, nil)
	return string(body), liveEventPresent(err)
}

type livePackageRegistry struct {
	source, registry, system string
	page                     func(name string) string
	manifest                 func(ctx context.Context, src *liveSources, repo string) (string, string, error)
	downloads                func(ctx context.Context, src *liveSources, name string) (map[string]int, error)
}

func (r livePackageRegistry) fetch(ctx context.Context, client *http.Client, query LiveEventQuery,
) ([]LiveAdoptionEvent, error) {
	src := &liveSources{http: client}
	events := []LiveAdoptionEvent{}
	for _, repo := range liveEventRepoNames(query.Repos) {
		name, description, err := r.manifest(ctx, src, repo)
		if err != nil {
			return events, fmt.Errorf("%s: %w", r.source, err)
		}
		if !livePackageName(name) {
			continue
		}
		version, published, err := liveDepsDevConfirm(ctx, src, r.system, name, repo)
		if err != nil {
			return events, fmt.Errorf("%s: %w", r.source, err)
		}
		if version == "" {
			continue
		}
		days, err := r.downloads(ctx, src, name)
		if err != nil {
			return events, fmt.Errorf("%s: %w", r.source, err)
		}
		last, previous, through := liveMonthDownloads(days)
		if last == 0 {
			continue
		}
		delta := last - previous
		record := liveEventRecord(LiveSourceRecord{
			ID: r.source + ":" + name, Title: name + " " + version + " — " + r.registry,
			Date: liveStampDay(published), URL: r.page(name), Source: r.registry, PublishedAt: published,
			TitleBasis: "имя пакета из манифеста репозитория " + repo + "; связь пакета с репозиторием подтверждает deps.dev",
			DateBasis:  "дата публикации версии по умолчанию по deps.dev",
			Excerpt: strings.TrimSpace(fmt.Sprintf("%s Скачиваний из %s за 30 дней по %s: %d, за предыдущие 30 дней: %d.",
				description, r.registry, through, last, previous)),
		}, description)
		owner, _, _ := strings.Cut(repo, "/")
		events = append(events, LiveAdoptionEvent{Type: LiveEventCodeRelease, Source: r.source, Record: record,
			Actor: owner, Phase: version, Quote: cmp.Or(description, name), Count: &last, Delta: &delta,
			Unit: liveBetKindDownloads})
	}
	return events, nil
}

// livePyPI: скачивания без зеркал — зеркала выкачивают каждую версию целиком и к использованию отношения не имеют.
func livePyPI() livePackageRegistry {
	return livePackageRegistry{source: liveEventSourcePyPI, registry: "PyPI", system: "pypi",
		page: func(name string) string { return "https://pypi.org/project/" + name + "/" },
		manifest: func(ctx context.Context, src *liveSources, repo string) (string, string, error) {
			for _, file := range livePythonManifests() {
				body, err := liveRawFile(ctx, src, repo, file)
				if err != nil {
					return "", "", err
				}
				if name, description := livePythonPackage(file, body); name != "" {
					return livePyPIName(name), description, nil
				}
			}
			return "", "", nil
		},
		downloads: func(ctx context.Context, src *liveSources, name string) (map[string]int, error) {
			var stats struct {
				Data []struct {
					Category  string `json:"category"`
					Date      string `json:"date"`
					Downloads int    `json:"downloads"`
				} `json:"data"`
			}
			// Из сети стенда второй запрос к pypistats по тому же соединению зависает, а по новому отвечает за полсекунды.
			fresh := &liveSources{http: &http.Client{Timeout: src.http.Timeout,
				Transport: &http.Transport{Proxy: http.ProxyFromEnvironment, DisableKeepAlives: true}}}
			if _, err := liveEventJSON(ctx, fresh, livePyPIStats+"/"+name+"/overall?mirrors=false", nil, &stats); err != nil {
				return nil, liveEventPresent(err)
			}
			days := map[string]int{}
			for _, row := range stats.Data {
				if row.Category == "without_mirrors" {
					days[row.Date] += row.Downloads
				}
			}
			return days, nil
		},
	}
}

// liveNPM: корневой package.json монорепозитория помечен private и пакетом не является.
func liveNPM() livePackageRegistry {
	return livePackageRegistry{source: liveEventSourceNPM, registry: "npm", system: "npm",
		page: func(name string) string { return "https://www.npmjs.com/package/" + name },
		manifest: func(ctx context.Context, src *liveSources, repo string) (string, string, error) {
			body, err := liveRawFile(ctx, src, repo, "package.json")
			if err != nil {
				return "", "", err
			}
			var manifest struct {
				Name        string `json:"name"`
				Description string `json:"description"`
				Private     bool   `json:"private"`
			}
			// Нечитаемый package.json — не отказ источника, а репозиторий без пакета.
			if body == "" || json.Unmarshal([]byte(body), &manifest) != nil {
				manifest.Name = ""
			}
			if manifest.Private {
				return "", "", nil
			}
			return manifest.Name, manifest.Description, nil
		},
		downloads: func(ctx context.Context, src *liveSources, name string) (map[string]int, error) {
			var series struct {
				Downloads []struct {
					Day       string `json:"day"`
					Downloads int    `json:"downloads"`
				} `json:"downloads"`
			}
			if _, err := liveEventJSON(ctx, src, liveNPMDownloads+"/"+name, nil, &series); err != nil {
				return nil, liveEventPresent(err)
			}
			days := map[string]int{}
			for _, row := range series.Downloads {
				days[row.Day] += row.Downloads
			}
			return days, nil
		},
	}
}

type liveHFModel struct {
	ID        string `json:"id"`
	Downloads int    `json:"downloads"`
	Likes     int    `json:"likes"`
	CreatedAt string `json:"createdAt"`
}

// liveHFEvents берёт модели того же автора, что и репозиторий: поиск по одному имени находил дообучения чужих людей.
func liveHFEvents(ctx context.Context, client *http.Client, query LiveEventQuery) ([]LiveAdoptionEvent, error) {
	src := &liveSources{http: client}
	events := []LiveAdoptionEvent{}
	for _, repo := range liveEventRepoNames(query.Repos) {
		owner, name, _ := strings.Cut(repo, "/")
		values := url.Values{"author": {owner}, "search": {name}, "sort": {"downloads"},
			"limit": {strconv.Itoa(liveHFModelsKept)}}
		var models []liveHFModel
		if _, err := liveEventJSON(ctx, src, liveHFModels+"?"+values.Encode(), nil, &models); err != nil {
			if errors.Is(err, errLiveEventMissing) {
				continue
			}
			return events, fmt.Errorf("huggingface: %w", err)
		}
		models = slices.DeleteFunc(models, func(model liveHFModel) bool { return model.Downloads == 0 })
		if len(models) == 0 {
			continue
		}
		downloads, likes := 0, 0
		for _, model := range models {
			downloads += model.Downloads
			likes += model.Likes
		}
		top := models[0]
		record := liveEventRecord(LiveSourceRecord{
			ID: "huggingface:" + top.ID, Title: top.ID + " — Hugging Face", Date: liveStampDay(top.CreatedAt),
			URL: "https://huggingface.co/" + top.ID, Source: "Hugging Face", PublishedAt: top.CreatedAt,
			TitleBasis: "самая скачиваемая модель автора " + owner + " с «" + name + "» в имени",
			DateBasis:  "дата публикации карточки модели",
			Excerpt: fmt.Sprintf("Моделей %s с «%s» в имени: %d, скачиваний за 30 дней: %d, отметок «нравится»: %d. "+
				"Самая скачиваемая — %s.", owner, name, len(models), downloads, likes, top.ID),
		}, top.ID)
		events = append(events, LiveAdoptionEvent{Type: LiveEventCodeRelease, Source: liveEventSourceHF, Record: record,
			Actor: owner, Quote: top.ID, Count: &downloads, Unit: liveBetKindDownloads})
	}
	return events, nil
}
