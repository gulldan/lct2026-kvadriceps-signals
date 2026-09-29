package main

// Лента — скользящая выборка, а не полный архив журнала.
func researchSources() []webSource {
	return []webSource{
		{Name: "PLOS Computational Biology", URL: "https://journals.plos.org/ploscompbiol/feed/atom", Kind: kindFeed,
			Industry: "Computational biology and biotechnology", Pages: 3},
		{Name: "PLOS ONE", URL: "https://journals.plos.org/plosone/feed/atom", Kind: kindFeed,
			Industry: "Scientific research", Pages: 3},
		{Name: "Frontiers in Materials", URL: "https://www.frontiersin.org/journals/materials/rss", Kind: kindFeed,
			Industry: "Materials", Pages: 3},
		{Name: "Frontiers in Energy Research", URL: "https://www.frontiersin.org/journals/energy-research/rss", Kind: kindFeed,
			Industry: "Energy", Pages: 3},
		{Name: "Frontiers in Artificial Intelligence", URL: "https://www.frontiersin.org/journals/artificial-intelligence/rss",
			Kind: kindFeed, Industry: "AI", Pages: 3},
		{Name: "Quantum Journal", URL: "https://quantum-journal.org/feed/", Kind: kindFeed,
			Industry: "Quantum computing", Pages: 3},
		{Name: "Google DeepMind Research", URL: "https://deepmind.google/blog/rss.xml", Kind: kindFeed,
			Industry: "AI", Pages: 3},
		{Name: "NVIDIA Technical Research", URL: "https://developer.nvidia.com/blog/feed/", Kind: kindFeed,
			Industry: "AI, computing and engineering", Pages: 3},
		{Name: "SciML Research Software", URL: "https://sciml.ai/feed.xml", Kind: kindFeed,
			Industry: "Scientific machine learning", Pages: 3},
	}
}
