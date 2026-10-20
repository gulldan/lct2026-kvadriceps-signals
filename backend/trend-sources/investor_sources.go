package main

import (
	"net/url"
	"slices"
)

const (
	genreInvestmentThesis   = "investment_thesis"
	genreInvestorCommentary = "investor_commentary"
)

// Жанр источника не может переопределить тип научного документа или превратить тезис фонда в факт финансирования.
func validInvestorGenre(source webSource) bool {
	return source.Genre == "" || isInvestorGenre(source.Genre) &&
		slices.Contains([]string{kindFeed, kindRSS, kindURL, kindURLs}, source.Kind)
}

func isInvestorGenre(genre string) bool {
	return genre == genreInvestmentThesis || genre == genreInvestorCommentary
}

// Курируемые страницы остаются наблюдениями без даты, если издатель не объявил дату публикации.
func investorSources() []webSource {
	return []webSource{
		{Name: "Y Combinator: Requests for Startups",
			URL: "https://www.ycombinator.com/rfs", Kind: kindURL,
			Industry: "Образование; Детские товары и сервисы; Медицина и здоровье; Энергетика; Инженерия и промышленное оборудование",
			Genre:    genreInvestmentThesis, Pages: 1},
		{Name: "a16z: Bio + Health и American Dynamism",
			URL: "https://a16z.com/bio-health/", Kind: kindURLs,
			Industry: "Медицина и здоровье; Биотехнологии; Инженерия и промышленное оборудование; Энергетика",
			Genre:    genreInvestmentThesis, Pages: 2,
			URLs: []string{"https://a16z.com/bio-health/", "https://a16z.com/american-dynamism/"}},
		{Name: "a16z: новости и идеи",
			URL: "https://www.a16z.news/feed", Kind: kindFeed,
			Industry: "Корпоративное ПО и вычисления; Финансовые технологии; Медицина и здоровье",
			Genre:    genreInvestorCommentary, Pages: 1},
		{Name: "Sequoia: направления портфеля",
			URL: "https://sequoiacap.com/our-companies", Kind: kindURL,
			Industry: "Космос и астрономия; Туризм и путешествия; Финансовые технологии; Медиа и цифровой контент",
			Genre:    genreInvestmentThesis, Pages: 1},
		{Name: "Bessemer: дорожная карта наук о жизни",
			URL: "https://www.bvp.com/atlas/roadmap-reinventing-life-sciences-with-ai", Kind: kindURL,
			Industry: "Медицина и здоровье; Биотехнологии",
			Genre:    genreInvestmentThesis, Pages: 1},
		{Name: "GV: инвестиционный подход",
			URL: "https://www.gv.com/about", Kind: kindURL,
			Industry: "Медицина и здоровье; Биотехнологии; Корпоративное ПО и вычисления",
			Genre:    genreInvestmentThesis, Pages: 1},
		{Name: "General Catalyst: преобразование отраслей",
			URL: "https://www.generalcatalyst.com/transformations", Kind: kindURL,
			Industry: "Медицина и здоровье; Корпоративное ПО и вычисления",
			Genre:    genreInvestmentThesis, Pages: 1},
		{Name: "Khosla Ventures: технологические направления",
			URL: "https://www.khoslaventures.com/", Kind: kindURL,
			Industry: "Энергетика; Медицина и здоровье; Химия и материалы; Космос и астрономия",
			Genre:    genreInvestmentThesis, Pages: 1},
		{Name: "Lux Capital: Riskgaming",
			URL: "https://blog.riskgaming.com/feed", Kind: kindFeed,
			Industry: "Инженерия и промышленное оборудование; Кибербезопасность и защита; Полупроводники",
			Genre:    genreInvestorCommentary, Pages: 1},
		{Name: "DCVC: Deep Tech Opportunities",
			URL: "https://www.dcvc.com/reports/deep-tech-opportunities-report-2026/", Kind: kindURL,
			Industry: "Энергетика; Биотехнологии; Робототехника; Инженерия и промышленное оборудование",
			Genre:    genreInvestmentThesis, Pages: 1},
		{Name: "Breakthrough Energy: программы",
			URL: "https://www.breakthroughenergy.org/programs", Kind: kindURL,
			Industry: "Энергетика; Инженерия и промышленное оборудование; Сельское хозяйство; Транспорт и логистика; Недвижимость",
			Genre:    genreInvestmentThesis, Pages: 1},
		{Name: "Lowercarbon: климатический тезис",
			URL: "https://lowercarbon.com/", Kind: kindURL,
			Industry: "Климат и экология; Энергетика; Химия и материалы; Сельское хозяйство",
			Genre:    genreInvestmentThesis, Pages: 1},
		{Name: "Lowercarbon: новости",
			URL: "https://lowercarbon.com/feed/", Kind: kindFeed,
			Industry: "Климат и экология; Энергетика",
			Genre:    genreInvestorCommentary, Pages: 1},
		{Name: "SOSV: новости глубокой технологии",
			URL: "https://sosv.com/feed/", Kind: kindFeed,
			Industry: "Биотехнологии; Климат и экология; Медицина и здоровье; Пищевые технологии",
			Genre:    genreInvestorCommentary, Pages: 1},
		{Name: "SOSV HAX: инженерные технологии",
			URL: "https://hax.co/feed/", Kind: kindFeed,
			Industry: "Инженерия и промышленное оборудование; Робототехника; Энергетика; Мода и текстиль",
			Genre:    genreInvestorCommentary, Pages: 1},
		{Name: "IndieBio: архив публикаций программы",
			URL: "https://indiebio.co/feed/", Kind: kindFeed,
			Industry: "Биотехнологии; Медицина и здоровье; Пищевые технологии; Химия и материалы",
			Genre:    genreInvestorCommentary, Pages: 1},
		{Name: "AgFunderNews: агропродовольственные технологии",
			URL: "https://agfundernews.com/feed", Kind: kindFeed,
			Industry: "Сельское хозяйство; Пищевые технологии; Биотехнологии",
			Genre:    genreInvestorCommentary, Pages: 1},
		{Name: "BlueYard: технологический манифест",
			URL: "https://blueyard.com/put-your-hand-in-the-box-the-spirit-of-blueyard/", Kind: kindURL,
			Industry: "Корпоративное ПО и вычисления; Биотехнологии; Химия и материалы; Космос и астрономия",
			Genre:    genreInvestmentThesis, Pages: 1},
		{Name: "Eclipse: физическая экономика",
			URL: "https://eclipse.capital/about", Kind: kindURL,
			Industry: "Инженерия и промышленное оборудование",
			Genre:    genreInvestmentThesis, Pages: 1},
		{Name: "Space Capital: космическая экономика",
			URL: "https://www.spacecapital.com/about", Kind: kindURL,
			Industry: "Космос и астрономия; Телекоммуникации",
			Genre:    genreInvestmentThesis, Pages: 1},
	}
}

// Отдельные запросы сохраняют место для отраслей, которые широкая научная выборка вытесняет.
func ventureResearchSources() []webSource {
	sources := make([]webSource, 0, 11)
	for _, domain := range []struct{ name, industry, query string }{
		{"Crossref: финансовые технологии", "Финансовые технологии", "financial technology payments inclusion"},
		{"Crossref: телекоммуникации", "Телекоммуникации", "telecommunications wireless networks"},
		{"Crossref: полупроводники", "Полупроводники", "semiconductor fabrication photonics"},
		{"Crossref: робототехника", "Робототехника", "robotics manipulation actuators"},
		{"Crossref: правовые технологии", "Правовые технологии", "law legal regulation technology"},
		{"Crossref: медиа и контент", "Медиа и цифровой контент", "media journalism digital content"},
		{"Crossref: мода и текстиль", "Мода и текстиль", "textiles wearable fibers"},
		{"Crossref: спорт", "Спорт", "sports exercise performance"},
		{"Crossref: туризм", "Туризм и путешествия", "tourism hospitality sustainable travel"},
		{"Crossref: дети и семейные сервисы", "Детские товары и сервисы", "childcare parenting child development"},
		{"Crossref: музыка", "Музыка", "music acoustics musical instruments"},
	} {
		sources = append(sources, webSource{Name: domain.name, Industry: domain.industry, Kind: kindCrossref,
			URL: "https://api.crossref.org/works?" + url.Values{
				"query.title": {domain.query}, publicationFilterKey: {"type:journal-article"}}.Encode(), Pages: 1})
	}
	return sources
}
