package main

import "net/url"

const (
	industryHealth      = "Медицина и здоровье"
	industryBiotech     = "Биотехнологии"
	industryAgriculture = "Сельское хозяйство и продовольствие"
	industryEnvironment = "Экология и климат"
	industryMaterials   = "Материалы и химия"
	industrySociety     = "Экономика и общество"
	industryWater       = "Водные ресурсы и океан"
)

// Отдельные запросы дают каждому домену место в выборке независимо от размера отраслевого каталога.
func domainArchiveSources() []webSource {
	sources := []webSource{}
	for _, domain := range []struct{ name, industry, query string }{
		{"PubMed: медицина и диагностика", industryHealth,
			`SRC:MED AND (TITLE_ABS:diagnostic OR TITLE_ABS:vaccine OR TITLE_ABS:therapy)`},
		{"Europe PMC: биотехнологии", industryBiotech,
			`TITLE_ABS:"gene editing" OR TITLE_ABS:"synthetic biology" OR TITLE_ABS:bioprinting`},
		{"Europe PMC: агротехнологии и питание", industryAgriculture,
			`TITLE_ABS:agriculture OR TITLE_ABS:"food security" OR TITLE_ABS:aquaculture`},
		{"Europe PMC: экология и климат", industryEnvironment,
			`TITLE_ABS:bioremediation OR TITLE_ABS:biodiversity OR TITLE_ABS:microplastics`},
		{"Europe PMC: нейронауки", "Нейронауки и психология",
			`TITLE_ABS:neurostimulation OR TITLE_ABS:neuroprosthetics OR TITLE_ABS:neuroscience`},
		{"Europe PMC: биоматериалы", industryMaterials,
			`TITLE_ABS:biomaterials OR TITLE_ABS:biosensor OR TITLE_ABS:biopolymer`},
	} {
		sources = append(sources, webSource{Name: domain.name, Industry: domain.industry, Kind: kindEuropePMC,
			URL:   "https://www.ebi.ac.uk/europepmc/webservices/rest/search?" + url.Values{publicationQueryKey: {domain.query}}.Encode(),
			Pages: 2})
	}
	for _, domain := range []struct{ name, industry, query string }{
		{"Crossref: энергетика", "Энергетика", "renewable energy hydrogen battery"},
		{"Crossref: материалы и химия", industryMaterials, "materials catalysts polymers"},
		{"Crossref: сельское хозяйство", industryAgriculture, "agriculture food aquaculture"},
		{"Crossref: климат и экология", industryEnvironment, "climate biodiversity environment"},
		{"Crossref: транспорт", "Транспорт и логистика", "transport mobility logistics"},
		{"Crossref: промышленность", "Промышленность и производство", "manufacturing metallurgy recycling"},
		{"Crossref: космос и физика", "Космос и физика", "astronomy astrophysics space"},
		{"Crossref: экономика", industrySociety, "economics development finance"},
		{"Crossref: образование", "Образование и культура", "education learning pedagogy"},
		{"Crossref: общество и демография", industrySociety, "demography sociology migration"},
		{"Crossref: вода и океан", industryWater, "water ocean desalination"},
		{"Crossref: строительство и города", "Города и инфраструктура", "construction urban infrastructure"},
	} {
		sources = append(sources, webSource{Name: domain.name, Industry: domain.industry, Kind: kindCrossref,
			URL: "https://api.crossref.org/works?" + url.Values{
				"query.title": {domain.query}, publicationFilterKey: {"type:journal-article"}}.Encode(), Pages: 2})
	}
	for _, domain := range []struct{ name, industry, query string }{
		{"DataCite: климатические данные", industryEnvironment, "climate OR ecology OR biodiversity"},
		{"DataCite: данные о воде и океане", industryWater, "ocean OR hydrology OR freshwater"},
		{"DataCite: данные о Земле", "Геология и природные ресурсы", "geology OR geophysics OR seismic"},
		{"DataCite: аграрные данные", industryAgriculture, "agriculture OR soil OR crops"},
		{"DataCite: данные о материалах", industryMaterials, "materials OR chemistry OR crystallography"},
		{"DataCite: социальные данные", industrySociety, "economics OR demography OR sociology"},
	} {
		sources = append(sources, webSource{Name: domain.name, Industry: domain.industry, Kind: kindDataCite,
			URL: "https://api.datacite.org/dois?" + url.Values{
				publicationQueryKey: {"titles.title:(" + domain.query + ")"}, "resource-type-id": {"dataset"}}.Encode(), Pages: 2})
	}
	return append(sources,
		webSource{Name: "ClinicalTrials.gov: клинические исследования", Kind: kindClinicalTrials,
			URL: "https://clinicaltrials.gov/api/v2/studies", Industry: industryHealth, Pages: 2},
		webSource{Name: "bioRxiv: архив препринтов", Kind: kindBiorxiv,
			URL: "https://api.biorxiv.org/details/biorxiv", Industry: industryBiotech, Pages: 3},
		webSource{Name: "medRxiv: архив препринтов", Kind: kindBiorxiv,
			URL: "https://api.biorxiv.org/details/medrxiv", Industry: industryHealth, Pages: 3},
		webSource{Name: "Zenodo: междисциплинарные данные", Kind: kindZenodo,
			URL: "https://zenodo.org/api/records?type=dataset", Industry: "Междисциплинарные исследования", Pages: 2},
		webSource{Name: "Figshare: научные данные", Kind: kindFigshare,
			URL: "https://api.figshare.com/v2/articles?item_type=3", Industry: "Междисциплинарные исследования", Pages: 2},
		webSource{Name: "BioStudies: биологические эксперименты", Kind: kindBioStudies,
			URL: "https://www.ebi.ac.uk/biostudies/api/v1/search", Industry: industryBiotech, Pages: 2})
}

func domainResearchFeeds() []webSource {
	return []webSource{
		{Name: "Nature Climate Change", URL: "https://www.nature.com/nclimate.rss", Kind: kindFeed,
			Industry: industryEnvironment, Pages: 1},
		{Name: "Nature Energy", URL: "https://www.nature.com/nenergy.rss", Kind: kindFeed,
			Industry: "Энергетика", Pages: 1},
		{Name: "Nature Food", URL: "https://www.nature.com/natfood.rss", Kind: kindFeed,
			Industry: industryAgriculture, Pages: 1},
		{Name: "Nature Sustainability", URL: "https://www.nature.com/natsustain.rss", Kind: kindFeed,
			Industry: industryEnvironment, Pages: 1},
		{Name: "Nature Medicine", URL: "https://www.nature.com/nm.rss", Kind: kindFeed,
			Industry: industryHealth, Pages: 1},
		{Name: "Nature Human Behaviour", URL: "https://www.nature.com/nathumbehav.rss", Kind: kindFeed,
			Industry: industrySociety, Pages: 1},
		{Name: "PLOS Biology", URL: "https://journals.plos.org/plosbiology/feed/atom", Kind: kindFeed,
			Industry: industryBiotech, Pages: 1},
		{Name: "PLOS Climate", URL: "https://journals.plos.org/climate/feed/atom", Kind: kindFeed,
			Industry: industryEnvironment, Pages: 1},
		{Name: "PLOS Water", URL: "https://journals.plos.org/water/feed/atom", Kind: kindFeed,
			Industry: industryWater, Pages: 1},
		{Name: "PLOS Global Public Health", URL: "https://journals.plos.org/globalpublichealth/feed/atom", Kind: kindFeed,
			Industry: industryHealth, Pages: 1},
	}
}
