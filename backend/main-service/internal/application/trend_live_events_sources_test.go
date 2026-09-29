package application

import (
	"encoding/json/v2"
	"slices"
	"testing"
)

func TestLiveEventSourceParsing(t *testing.T) {
	repos := liveEventRepoNames([]string{"https://github.com/state-spaces/mamba.git", "gitlab.com/x/y",
		"github.com/huggingface/peft/tree/main/src", "state-spaces/mamba", "{{org}}/{{repo}}"})
	if !slices.Equal(repos, []string{"state-spaces/mamba", "huggingface/peft"}) {
		t.Errorf("репозитории: %v", repos)
	}

	link := `<https://api.github.com/repositories/599547518/releases?per_page=1&page=2>; rel="next", ` +
		`<https://api.github.com/repositories/599547518/releases?per_page=1&page=106>; rel="last"`
	if got := liveLastPage(link, 1); got != 106 {
		t.Errorf("релизов по Link: %d", got)
	}

	for _, tc := range []struct{ file, body, name, description string }{
		{"pyproject.toml", "[build-system]\nrequires = [\"setuptools\"]\n\n[project]\nname = \"mamba_ssm\"\n" +
			"description = \"Mamba state-space model\"\n", "mamba_ssm", "Mamba state-space model"},
		{"pyproject.toml", "[tool.poetry]\nname = 'peft'\n[tool.poetry.dependencies]\nname = \"x\"\n", "peft", ""},
		{"setup.cfg", "[metadata]\nname = vllm\ndescription = A high-throughput engine\n", "vllm", "A high-throughput engine"},
		{"setup.py", "import setuptools\nsetuptools.setup(\n    long_description=readme,\n    name = \"dream-robot\",\n" +
			"    description='DREAM: real-to-sim transfer',\n)\n", "dream-robot", "DREAM: real-to-sim transfer"},
		{"setup.py", "setup(name=NAME)\n", "", ""},
	} {
		name, description := livePythonPackage(tc.file, tc.body)
		if name != tc.name || description != tc.description {
			t.Errorf("%s: получили %q/%q, ожидали %q/%q", tc.file, name, description, tc.name, tc.description)
		}
	}
	if got := livePyPIName("Mamba_SSM"); got != "mamba-ssm" {
		t.Errorf("нормальная форма имени PyPI: %s", got)
	}

	var version liveDepsVersion
	if err := json.Unmarshal([]byte(`{"versionKey":{"system":"PYPI","name":"peft","version":"0.13.2"},`+
		`"links":[{"label":"SOURCE_REPO","url":"https://github.com/huggingface/peft"}],"relatedProjects":`+
		`[{"projectKey":{"id":"github.com/huggingface/peft"},"relationProvenance":"UNVERIFIED_METADATA",`+
		`"relationType":"SOURCE_REPO"}]}`), &version); err != nil {
		t.Fatal(err)
	}
	if !liveDepsDevRepo(version, "HuggingFace/peft") || liveDepsDevRepo(version, "someone/peft") {
		t.Error("deps.dev должен связывать пакет только с его репозиторием")
	}

	last, previous, through := liveMonthDownloads(map[string]int{"2026-09-21": 10, "2026-08-23": 5,
		"2026-08-22": 7, "2026-07-24": 3, "2026-07-23": 100})
	if last != 15 || previous != 10 || through != "2026-09-21" {
		t.Errorf("скачивания за месяц: %d, за предыдущий: %d, по %s", last, previous, through)
	}

	query := LiveEventQuery{Term: "LangGraph", Russian: "LangGraph-оркестрация агентов для цифровых двойников"}
	names := liveRussianNames(query)
	if len(names) != 2 || names[0].query != "LangGraph-оркестрация агентов" || names[1].query != `"LangGraph"` {
		t.Errorf("запросы к поисковику: %+v", names)
	}
	if plain := liveRussianNames(LiveEventQuery{Term: "Mamba"}); len(plain) != 0 {
		t.Errorf("обычное слово ищется как имя: %+v", plain)
	}
	if got := liveNameCore("CRYSTALS-Kyber for Key Exchange"); got != "CRYSTALS-Kyber" {
		t.Errorf("ядро названия: %s", got)
	}
	if got := liveNameCore("ИИ в финтех-экосистемах (AI in Fintech)"); got != "ИИ в финтех-экосистемах" {
		t.Errorf("ядро из одного слова не остаётся: %s", got)
	}
	rss := `<?xml version="1.0" encoding="utf-8" ?><rss version="2.0"><channel><title>Bing</title>` +
		`<item><title>Мы пытались оптимизировать граф агентов в LangGraph — и в итоге написали gMAS</title>` +
		`<link>http://www.bing.com/news/apiclick.aspx?ref=FexRss&amp;aid=&amp;` +
		`url=https%3a%2f%2fhabr.com%2fru%2fcompanies%2fsberbank%2farticles%2f1083832%2f&amp;c=1</link>` +
		`<description>Команда Сбера рассказывает о мультиагентной системе.</description>` +
		`<pubDate>Fri, 18 Sep 2026 09:00:00 GMT</pubDate></item>` +
		`<item><title>LangGraph 1.0 released</title>` +
		`<link>http://www.bing.com/news/apiclick.aspx?url=https%3a%2f%2fexample.com%2fa</link>` +
		`<description>English note.</description><pubDate>Thu, 17 Sep 2026 09:00:00 GMT</pubDate></item>` +
		`<item><title>Как моделировали дорожное движение до ИИ</title>` +
		`<link>http://www.bing.com/news/apiclick.aspx?url=https%3a%2f%2fhabr.com%2fru%2farticles%2f1%2f</link>` +
		`<description>Обзор агентов и графов.</description><pubDate>Thu, 17 Sep 2026 09:00:00 GMT</pubDate></item>` +
		`</channel></rss>`
	records, err := liveBingRecords([]byte(rss), names)
	if err != nil {
		t.Fatal(err)
	}
	if len(records) != 1 || records[0].URL != "https://habr.com/ru/companies/sberbank/articles/1083832/" ||
		records[0].Date != "2026-09-18" || records[0].Language != "ru" || records[0].PublisherType != PublisherBlog {
		t.Errorf("русские записи выдачи: %+v", records)
	}
	if events := liveRussianEventsOf(liveEventSourceBing, records, names); len(events) != 1 || events[0].Quote != "" {
		t.Errorf("название в заголовке, цитату выбирает словарь конвейера: %+v", events)
	}
}
