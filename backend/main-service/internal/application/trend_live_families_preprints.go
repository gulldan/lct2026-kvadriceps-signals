package application

// Направления подобраны 19.10.2026 на окне arXiv вместе с препринтами bioRxiv, medRxiv и Crossref: раньше
// медицина, биология, химия и общественные отрасли попадали в радар почти только через статьи об ИИ. Каждое
// зародилось внутри окна — давняя область честно получила бы «устоявшееся».
func livePreprintFamilies() []liveFamily {
	return []liveFamily{
		{id: "bundibugyo", name: "Bundibugyo ebolavirus outbreak", branch: liveBranchLife, query: LiveQuery{
			Arxiv: `all:bundibugyo OR all:bdbv`,
			Terms: []string{"bundibugyo virus", "bundibugyo ebolavirus", "bdbv outbreak"}}},
		{id: "glp1-drugs", name: "GLP-1 drugs beyond diabetes", branch: liveBranchLife, query: LiveQuery{
			Arxiv: `all:semaglutide OR all:tirzepatide OR all:retatrutide OR all:orforglipron OR ` +
				`all:"glp-1 receptor agonist" OR all:"glp-1 receptor agonists" OR all:"glp-1 ras"`,
			Terms: []string{"semaglutide", "tirzepatide", "glp-1 receptor agonist"}}},
		{id: "social-prescribing", name: "Social prescribing", branch: liveBranchLife, query: LiveQuery{
			Arxiv: `all:"social prescribing" OR all:"social prescription" OR all:"nature prescription" OR ` +
				`all:"green prescription"`,
			Terms: []string{"social prescribing", "nature prescription", "link worker"}}},
		{id: "bridge-recombinase", name: "Bridge recombinases and RNA-guided transposons", branch: liveBranchLife, query: LiveQuery{
			Arxiv: `all:"bridge recombinase" OR all:"bridge rna" OR all:"crispr-associated transposase" OR ` +
				`all:"crispr-associated transposon" OR all:"rna-guided transposition"`,
			Terms: []string{"bridge recombinase", "bridge rna", "crispr-associated transposon"}}},
		{id: "rna-biopesticides", name: "RNA-based biopesticides", branch: liveBranchLife, query: LiveQuery{
			Arxiv: `all:"spray-induced gene silencing" OR all:"rna-based biopesticide" OR all:"rna biopesticide" OR ` +
				`all:"dsrna-based" OR all:"rna pesticide"`,
			Terms: []string{"spray-induced gene silencing", "rna biopesticide", "dsrna pest control"}}},
		{id: "precision-fermentation", name: "Precision fermentation", branch: liveBranchLife, query: LiveQuery{
			Arxiv: `all:"precision fermentation" OR all:"recombinant milk" OR all:"animal-free dairy" OR ` +
				`all:"recombinant food protein"`,
			Terms: []string{"precision fermentation", "animal-free dairy", "recombinant food protein"}}},
		{id: "nuclear-clock", name: "Thorium nuclear clocks", branch: liveBranchPhysics, query: LiveQuery{
			Arxiv: `all:"nuclear clock" OR all:"nuclear clocks" OR all:229th OR all:229mth OR all:"thorium isomer" OR ` +
				`all:"thorium nuclear"`,
			Terms: []string{"nuclear clock", "thorium-229", "229th isomer"}}},
		{id: "higgsino-lz", name: "Higgsino dark matter and the LZ event", branch: liveBranchPhysics, query: LiveQuery{
			Arxiv: `all:higgsino OR all:higgsinos OR all:"lz experiment" OR all:"lz collaboration"`,
			Terms: []string{"higgsino dark matter", "lz experiment", "high-recoil event"}}},
		{id: "lithium-tantalate", name: "Thin-film lithium tantalate photonics", branch: liveBranchPhysics, query: LiveQuery{
			Arxiv: `all:"lithium tantalate" OR all:"thin-film lithium tantalate" OR all:tflt`,
			Terms: []string{"thin-film lithium tantalate", "lithium tantalate modulator", "tflt photonics"}}},
		{id: "cfet", name: "Complementary FET and 2D transistors", branch: liveBranchEngineering, query: LiveQuery{
			Arxiv: `ti:cfet OR all:"complementary fet" OR all:"stacked nanosheet" OR all:"2d transistors" OR ` +
				`all:"2d semiconductor transistors"`,
			Terms: []string{"cfet", "stacked nanosheet transistor", "2d transistor"}}},
		{id: "nitrate-to-ammonia", name: "Electrochemical nitrate-to-ammonia", branch: liveBranchMaterials, query: LiveQuery{
			Arxiv: `all:"nitrate reduction" OR all:"nitrate electroreduction" OR all:"no3rr" OR ` +
				`all:"nitrate-to-ammonia"`,
			Terms: []string{"nitrate reduction to ammonia", "nitrate electroreduction", "no3rr"}}},
		{id: "lithium-extraction", name: "Direct lithium extraction", branch: liveBranchMaterials, query: LiveQuery{
			Arxiv: `all:"lithium extraction" OR all:"direct lithium extraction" OR all:"lithium recovery" OR ` +
				`all:"lithium brine" OR all:"lithium brines"`,
			Terms: []string{"direct lithium extraction", "lithium recovery from brine", "lithium-selective membrane"}}},
		{id: "natural-hydrogen", name: "Natural (geologic) hydrogen", branch: liveBranchEarth, query: LiveQuery{
			Arxiv: `all:"natural hydrogen" OR all:"geologic hydrogen" OR all:"geological hydrogen" OR ` +
				`all:"white hydrogen" OR all:"native hydrogen"`,
			Terms: []string{"natural hydrogen", "geologic hydrogen", "white hydrogen"}}},
		{id: "perovskite-tandem", name: "Perovskite tandem solar cells", branch: liveBranchMaterials, query: LiveQuery{
			Arxiv: `(ti:perovskite AND ti:tandem) OR all:"perovskite/silicon tandem" OR ` +
				`all:"perovskite-silicon tandem"`,
			Terms: []string{"perovskite tandem solar cell", "perovskite silicon tandem", "all-perovskite tandem"}}},
		{id: "deep-sea-mining", name: "Deep-sea mining", branch: liveBranchEarth, query: LiveQuery{
			Arxiv: `all:"deep-sea mining" OR all:"deep sea mining" OR all:"polymetallic nodules" OR ` +
				`all:"polymetallic nodule" OR all:"seabed mining"`,
			Terms: []string{"deep-sea mining", "polymetallic nodules", "seabed mining"}}},
		{id: "phase-field-fracture", name: "Phase-field fracture modelling", branch: liveBranchMaterials, query: LiveQuery{
			Arxiv: `all:"phase-field fracture" OR all:"phase field fracture" OR all:"phase-field damage"`,
			Terms: []string{"phase-field fracture", "phase-field damage model", "brittle fracture simulation"}}},
		{id: "smart-rings", name: "Smart rings and wearables", branch: liveBranchEngineering, query: LiveQuery{
			Arxiv: `all:"smart ring" OR all:"smart rings" OR all:oura`,
			Terms: []string{"smart ring", "oura ring", "ring wearable"}}},
		{id: "influencer-marketing", name: "Influencer marketing", branch: liveBranchEconomics, query: LiveQuery{
			Arxiv: `all:"influencer marketing" OR all:"social media influencers" OR all:"influencer economy" OR ` +
				`all:"creator economy"`,
			Terms: []string{"influencer marketing", "creator economy", "social media influencers"}}},
		{id: "climate-anxiety", name: "Climate anxiety", branch: liveBranchEconomics, query: LiveQuery{
			Arxiv: `all:"climate anxiety" OR all:"eco-anxiety" OR all:"climate distress" OR all:"ecological grief"`,
			Terms: []string{"climate anxiety", "eco-anxiety", "climate distress"}}},
		{id: "cognitive-offloading", name: "Cognitive offloading in learning", branch: liveBranchEconomics, query: LiveQuery{
			Arxiv: `all:"cognitive offloading" OR all:"metacognitive laziness" OR all:"cognitive debt" OR ` +
				`all:deskilling`,
			Terms: []string{"cognitive offloading", "metacognitive laziness", "deskilling"}}},
		{id: "datacenter-water", name: "Water footprint of data centers", branch: liveBranchEngineering, query: LiveQuery{
			Arxiv: `(all:"water footprint" OR all:"water consumption" OR all:"water use" OR ` +
				`all:"water withdrawal") AND (all:"data center" OR all:"data centers" OR all:datacenter OR ` +
				`all:datacenters)`,
			Terms: []string{"data center water footprint", "ai water consumption", "datacenter water use"}}},
		{id: "liquid-cooling", name: "Liquid and immersion cooling for data centers", branch: liveBranchEngineering, query: LiveQuery{
			Arxiv: `(all:"immersion cooling" OR all:"liquid cooling" OR all:"direct-to-chip" OR ` +
				`all:"two-phase cooling") AND (all:"data center" OR all:"data centers" OR all:datacenter OR ` +
				`all:server OR all:servers OR all:gpu OR all:gpus)`,
			Terms: []string{"immersion cooling", "liquid cooling data center", "direct-to-chip cooling"}}},
		{id: "datacenter-siting", name: "Data-center siting and land use", branch: liveBranchEconomics, query: LiveQuery{
			Arxiv: `all:"data center siting" OR all:"data centre siting" OR all:"siting of data centers" OR ` +
				`all:"data center location" OR all:"data center construction" OR all:"data center development"`,
			Terms: []string{"data center siting", "data center land use", "data center construction"}}},
	}
}
