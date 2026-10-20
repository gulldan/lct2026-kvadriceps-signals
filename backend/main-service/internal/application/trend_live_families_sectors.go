package application

import (
	"slices"
	"strings"
)

// Отраслевые направления подобраны на окне arXiv 18.10.2026: у каждой отрасли каталога dealroom_research_v1
// хотя бы одна узкая технология, которая зародилась внутри окна. Широкая область здесь бесполезна: она
// честно получает «устоявшееся», и отрасль снова выпадает из слабых сигналов.

func liveSectorFamilies() []liveFamily {
	return []liveFamily{
		{id: "datacenter-power", name: "AI data-center power demand", branch: liveBranchEngineering, query: LiveQuery{
			Arxiv: `all:"data center power" OR all:"datacenter power" OR all:"data center energy flexibility" OR ` +
				`all:"ai data center" OR all:"ai data centers"`,
			Terms: []string{"ai data center power", "data center energy flexibility", "datacenter power"}}},
		{id: "grid-forming", name: "Grid-forming inverters", branch: liveBranchEngineering, query: LiveQuery{
			Arxiv: `all:"grid-forming inverter" OR all:"grid-forming inverters" OR all:"grid-forming converter"`,
			Terms: []string{"grid-forming inverter", "grid-forming converter", "gfm inverter"}}},
		{id: "virtual-power-plant", name: "Virtual power plants", branch: liveBranchEngineering, query: LiveQuery{
			Arxiv: `all:"virtual power plant" OR all:"virtual power plants"`,
			Terms: []string{"virtual power plant", "distributed energy aggregation", "vpp"}}},
		{id: "fashion-llm", name: "Multimodal AI for fashion and apparel", branch: liveBranchComputing, query: LiveQuery{
			Arxiv: "(ti:fashion OR ti:garment OR ti:clothing OR ti:outfit OR ti:apparel) AND (all:llm OR all:llms OR " +
				`all:"large language model" OR all:"large language models" OR all:"vision-language" OR ` +
				`all:agentic OR all:"llm agent" OR all:"llm agents")`,
			Terms: []string{"fashion llm", "garment vision-language model", "outfit assistant"}}},
		{id: "video-try-on", name: "Video virtual try-on", branch: liveBranchComputing, query: LiveQuery{
			Arxiv: `all:"video virtual try-on" OR all:"video try-on" OR all:"virtual try-on" AND all:video`,
			Terms: []string{"video virtual try-on", "video try-on", "garment video"}}},
		{id: "prediction-markets", name: "Prediction markets", branch: liveBranchEconomics, query: LiveQuery{
			Arxiv: `all:"prediction market" OR all:"prediction markets" OR all:polymarket OR all:kalshi`,
			Terms: []string{"prediction markets", "polymarket", "kalshi"}}},
		{id: "agentic-payments", name: "Agentic payments and commerce", branch: liveBranchEconomics, query: LiveQuery{
			Arxiv: `all:"agentic commerce" OR all:"agentic payment" OR all:"agent payments" OR all:x402`,
			Terms: []string{"agentic commerce", "agentic payment", "x402"}}},
		{id: "food-ai", name: "AI for food and nutrition", branch: liveBranchLife, query: LiveQuery{
			Arxiv: "(ti:food OR ti:foods OR ti:dish OR ti:dishes OR ti:meal OR ti:meals OR ti:nutrition OR " +
				`ti:nutritional OR ti:dietary) AND (all:"vision-language" OR all:"multimodal large language" OR ` +
				`all:mllm OR all:mllms OR all:agentic OR all:"llm agent" OR all:llm OR all:"large language model")`,
			Terms: []string{"food vision-language model", "nutrition estimation llm", "food ai agent"}}},
		{id: "game-world-models", name: "Playable world models", branch: liveBranchComputing, query: LiveQuery{
			Arxiv: `all:"playable world" OR all:"interactive world model" OR all:"game engine generation" OR ` +
				`all:"generative game"`,
			Terms: []string{"interactive world model", "playable world model", "generative game engine"}}},
		{id: "clinical-agents", name: "Clinical AI agents", branch: liveBranchLife, query: LiveQuery{
			Arxiv: `all:"clinical agent" OR all:"medical agent" OR all:"clinical agents" OR all:"medical agents"`,
			Terms: []string{"clinical ai agent", "medical agent", "healthcare agent"}}},
		{id: "patient-digital-twin", name: "Patient digital twins", branch: liveBranchLife, query: LiveQuery{
			Arxiv: `all:"digital twin of the patient" OR all:"patient digital twin" OR all:"medical digital twin" OR ` +
				`all:"cardiac digital twin"`,
			Terms: []string{"patient digital twin", "medical digital twin", "cardiac digital twin"}}},
		{id: "driving-world-model", name: "World models for autonomous driving", branch: liveBranchEngineering, query: LiveQuery{
			Arxiv: `all:driving AND all:"world model"`,
			Terms: []string{"driving world model", "autonomous driving world model", "generative driving simulation"}}},
		{id: "disaggregated-serving", name: "Disaggregated LLM serving", branch: liveBranchComputing, query: LiveQuery{
			Arxiv: `all:"disaggregated inference" OR all:"prefill-decode disaggregation" OR ` +
				`all:"disaggregated serving" OR all:"pd disaggregation"`,
			Terms: []string{"disaggregated inference", "prefill-decode disaggregation", "disaggregated serving"}}},
		{id: "housing-markets", name: "Housing markets and rents", branch: liveBranchEconomics, query: LiveQuery{
			Arxiv: `ti:housing OR ti:rents OR ti:rental OR ti:"real estate" OR ti:homeownership`,
			Terms: []string{"housing market", "rental market", "real estate"}}},
		{id: "world-action-models", name: "World action models for robots", branch: liveBranchComputing, query: LiveQuery{
			Arxiv: `all:"world action model" OR all:"world-action model" OR all:"action-conditioned world model"`,
			Terms: []string{"world action model", "action-conditioned world model", "robot world model"}}},
	}
}

func liveSectorFamiliesMore() []liveFamily {
	return []liveFamily{
		{id: "geo-search", name: "Generative engine optimization", branch: liveBranchComputing, query: LiveQuery{
			Arxiv: `all:"generative engine optimization" OR all:"ai search visibility" OR ` +
				`all:"llm search engine optimization"`,
			Terms: []string{"generative engine optimization", "ai search visibility", "llm seo"}}},
		{id: "ai-video-detection", name: "AI-generated video detection", branch: liveBranchComputing, query: LiveQuery{
			Arxiv: `all:"ai-generated video" OR all:"generated video detection" OR all:"fake video detection"`,
			Terms: []string{"ai-generated video detection", "fake video detection", "synthetic video detection"}}},
		{id: "multi-shot-video", name: "Multi-shot and long video generation", branch: liveBranchComputing, query: LiveQuery{
			Arxiv: `all:"multi-shot video" OR all:"long video generation" OR all:"streaming video generation"`,
			Terms: []string{"multi-shot video generation", "long video generation", "streaming video generation"}}},
		{id: "legal-benchmarks", name: "Legal LLM benchmarks", branch: liveBranchComputing, query: LiveQuery{
			Arxiv: "(ti:legal OR ti:law OR ti:judicial) AND ti:benchmark",
			Terms: []string{"legal llm benchmark", "law benchmark", "judicial benchmark"}}},
		{id: "wireless-world-model", name: "World models for wireless networks", branch: liveBranchEngineering, query: LiveQuery{
			Arxiv: `(ti:wireless OR ti:6g OR ti:mimo OR ti:beamforming OR ti:channel) AND all:"world model"`,
			Terms: []string{"wireless world model", "6g world model", "channel world model"}}},
		{id: "ai-labor-exposure", name: "AI exposure of occupations", branch: liveBranchEconomics, query: LiveQuery{
			Arxiv: `all:"occupational exposure to ai" OR all:"ai exposure" OR all:"labor market effects of ai" OR ` +
				`all:"generative ai and employment"`,
			Terms: []string{"ai exposure of occupations", "labor market effects of ai", "generative ai and employment"}}},
		{id: "household-robots", name: "Household robots", branch: liveBranchComputing, query: LiveQuery{
			Arxiv: `all:"household robot" OR all:"home robot" OR all:"household tasks" OR ` +
				`all:"mobile manipulation in homes"`,
			Terms: []string{"household robot", "home robot", "household tasks"}}},
		{id: "pedagogical-agents", name: "Pedagogical AI agents", branch: liveBranchComputing, query: LiveQuery{
			Arxiv: "(ti:tutor OR ti:tutoring OR ti:pedagogical OR ti:classroom OR ti:educational OR ti:education) AND " +
				`(all:agentic OR all:"llm agent" OR all:"llm agents" OR all:"ai agent" OR all:"ai agents")`,
			Terms: []string{"pedagogical agent", "ai tutor agent", "educational ai agent"}}},
		{id: "ai-companionship", name: "AI companionship", branch: liveBranchComputing, query: LiveQuery{
			Arxiv: `all:"ai companionship" OR all:"emotional reliance" OR all:"emotional dependence" OR ` +
				`all:"companion app"`,
			Terms: []string{"ai companionship", "emotional reliance on ai", "companion app"}}},
		{id: "live-commerce", name: "Live-stream commerce", branch: liveBranchComputing, query: LiveQuery{
			Arxiv: `all:"live streaming" AND (all:commerce OR all:e-commerce OR all:sales) OR all:"live commerce"`,
			Terms: []string{"live commerce", "live-stream shopping", "livestream e-commerce"}}},
		{id: "eda-agents", name: "AI agents for chip design", branch: liveBranchEngineering, query: LiveQuery{
			Arxiv: `(ti:eda OR ti:rtl OR ti:verilog OR ti:"chip design" OR ti:"hardware design" OR ti:hls) AND ` +
				`(all:agentic OR all:"llm agent" OR all:"llm agents" OR all:"ai agent" OR all:"ai agents")`,
			Terms: []string{"eda agent", "rtl generation agent", "chip design llm agent"}}},
		{id: "ai-psychosis", name: "Chatbot mental-health risks", branch: liveBranchLife, query: LiveQuery{
			Arxiv: `all:"ai psychosis" OR all:"chatbot psychosis" OR all:delusion AND all:chatbot`,
			Terms: []string{"ai psychosis", "chatbot psychosis", "ai-induced delusion"}}},
		{id: "child-online-safety", name: "Child online safety", branch: liveBranchComputing, query: LiveQuery{
			Arxiv: `all:"child safety" OR all:"child sexual abuse material" OR all:"age verification" OR ` +
				`all:"minors online"`,
			Terms: []string{"child online safety", "age verification", "child safety ai"}}},
		{id: "song-generation", name: "Full-song generation", branch: liveBranchComputing, query: LiveQuery{
			Arxiv: `all:"song generation" OR all:"lyrics-to-song" OR all:"full-song"`,
			Terms: []string{"song generation", "lyrics-to-song", "full-song generation"}}},
		{id: "orbital-datacenters", name: "Orbital data centers", branch: liveBranchSpace, query: LiveQuery{
			Arxiv: `all:"orbital data center" OR all:"space-based data center" OR all:"in-orbit computing" OR ` +
				`all:"orbital computing"`,
			Terms: []string{"orbital data center", "space-based data center", "in-orbit computing"}}},
	}
}

func liveSectorFamiliesRest() []liveFamily {
	return []liveFamily{
		{id: "team-sports-ai", name: "Foundation models for team sports", branch: liveBranchComputing, query: LiveQuery{
			Arxiv: `(all:football OR all:soccer OR all:basketball) AND (all:"foundation model" OR ` +
				`all:"large language model" OR all:llm)`,
			Terms: []string{"football foundation model", "soccer ai", "basketball ai"}}},
		{id: "sports-betting", name: "Sports betting analytics", branch: liveBranchEconomics, query: LiveQuery{
			Arxiv: `all:"sports betting" OR all:"betting markets" OR all:sportsbook`,
			Terms: []string{"sports betting", "betting markets", "sportsbook"}}},
		{id: "proactive-assistant", name: "Proactive AI assistants", branch: liveBranchComputing, query: LiveQuery{
			Arxiv: `all:"proactive assistant" OR all:"proactive agent" OR all:"always-on assistant"`,
			Terms: []string{"proactive assistant", "proactive agent", "always-on assistant"}}},
		{id: "agent-security", name: "Security of AI agents", branch: liveBranchComputing, query: LiveQuery{
			Arxiv: `all:"agent security" OR all:"tool poisoning" OR all:"memory poisoning" OR all:"agent hijacking" OR ` +
				`all:"securing ai agents"`,
			Terms: []string{"ai agent security", "tool poisoning", "memory poisoning"}}},
		{id: "materials-agents", name: "AI agents for materials and chemistry", branch: liveBranchMaterials, query: LiveQuery{
			Arxiv: "(ti:materials OR ti:chemistry OR ti:chemical OR ti:catalyst OR ti:catalysis OR ti:crystal OR " +
				`ti:alloy) AND (all:agentic OR all:"llm agent" OR all:"llm agents" OR all:"ai agent" OR ` +
				`all:"ai agents")`,
			Terms: []string{"materials discovery agent", "chemistry llm agent", "agentic materials design"}}},
		{id: "generative-crystals", name: "Generative crystal design", branch: liveBranchMaterials, query: LiveQuery{
			Arxiv: `all:mattergen OR all:"crystal structure generation" OR all:"generative materials design" OR ` +
				`all:"crystal generation"`,
			Terms: []string{"crystal structure generation", "generative materials design", "mattergen"}}},
		{id: "text-to-cad", name: "Text-to-CAD generation", branch: liveBranchEngineering, query: LiveQuery{
			Arxiv: `all:"text-to-cad" OR all:"cad code" OR all:"cad generation" OR all:cadquery OR all:"parametric cad"`,
			Terms: []string{"text-to-cad", "cad code generation", "parametric cad generation"}}},
		{id: "agri-agents", name: "AI agents for agriculture", branch: liveBranchLife, query: LiveQuery{
			Arxiv: "(ti:agriculture OR ti:agricultural OR ti:farm OR ti:farming OR ti:farmers OR ti:crop OR ti:crops OR " +
				`ti:livestock) AND (all:agentic OR all:"llm agent" OR all:"llm agents" OR all:"ai agent" OR ` +
				`all:"ai agents")`,
			Terms: []string{"agricultural ai agent", "farm advisory agent", "crop management agent"}}},
		{id: "water-ai", name: "AI for water and floods", branch: liveBranchEarth, query: LiveQuery{
			Arxiv: "(ti:water OR ti:hydrology OR ti:hydrological OR ti:flood OR ti:wastewater OR ti:groundwater) AND " +
				`(all:llm OR all:llms OR all:"large language model" OR all:"large language models" OR ` +
				`all:"vision-language" OR all:agentic OR all:"llm agent" OR all:"llm agents")`,
			Terms: []string{"water llm", "flood ai", "wastewater ai"}}},
		{id: "microplastics", name: "Microplastics monitoring", branch: liveBranchEarth, query: LiveQuery{
			Arxiv: "all:microplastic OR all:microplastics OR all:nanoplastics",
			Terms: []string{"microplastics", "nanoplastics", "microplastic detection"}}},
		{id: "geo-foundation", name: "Geospatial foundation models", branch: liveBranchEarth, query: LiveQuery{
			Arxiv: `all:alphaearth OR all:"geospatial foundation model" OR all:"earth observation foundation model" OR ` +
				`all:terramind OR all:"remote sensing foundation model"`,
			Terms: []string{"geospatial foundation model", "earth observation foundation model", "alphaearth"}}},
		{id: "biodiversity-ai", name: "Foundation models for biodiversity", branch: liveBranchEarth, query: LiveQuery{
			Arxiv: `all:bioclip OR all:"biodiversity foundation model" OR all:"camera trap" AND all:"foundation model"`,
			Terms: []string{"biodiversity foundation model", "bioclip", "camera trap ai"}}},
		{id: "bio-agents", name: "AI agents for biology and drug discovery", branch: liveBranchLife, query: LiveQuery{
			Arxiv: `(ti:biology OR ti:biological OR ti:"drug discovery" OR ti:biomedical OR ti:protein OR ` +
				`ti:molecular OR ti:genomics) AND (all:agentic OR all:"llm agent" OR all:"llm agents" OR ` +
				`all:"ai agent" OR all:"ai agents")`,
			Terms: []string{"ai agents for drug discovery", "biomedical ai agent", "agentic biology"}}},
		{id: "ai-scientist", name: "AI scientist agents", branch: liveBranchComputing, query: LiveQuery{
			Arxiv: `all:"ai scientist" OR all:"autonomous research" OR all:"research agent" OR ` +
				`all:"automated scientific discovery"`,
			Terms: []string{"ai scientist", "autonomous research agent", "automated scientific discovery"}}},
		{id: "tourism-ai", name: "AI in tourism and hospitality", branch: liveBranchComputing, query: LiveQuery{
			Arxiv: "ti:tourism OR ti:tourist OR ti:tourists OR ti:hotel OR ti:hotels",
			Terms: []string{"tourism ai", "hotel analytics", "tourist behaviour model"}}},
		{id: "cultural-heritage-ai", name: "AI for museums and cultural heritage", branch: liveBranchComputing, query: LiveQuery{
			Arxiv: `(ti:heritage OR ti:museum OR ti:museums) AND ` +
				`(all:llm OR all:llms OR all:"large language model" OR all:"large language models" OR ` +
				`all:"vision-language" OR all:agentic OR all:"llm agent" OR all:"llm agents")`,
			Terms: []string{"cultural heritage ai", "museum llm", "heritage tourism ai"}}},
		{id: "poi-llm", name: "LLM recommendations of places to visit", branch: liveBranchComputing, query: LiveQuery{
			Arxiv: `(ti:poi OR ti:"point-of-interest" OR ti:"next location") AND ` +
				`(all:llm OR all:llms OR all:"large language model" OR all:"large language models" OR ` +
				`all:"vision-language" OR all:agentic OR all:"llm agent" OR all:"llm agents")`,
			Terms: []string{"llm poi recommendation", "next poi recommendation llm", "travel place recommendation"}}},
	}
}

// liveFamilyIndustryTable — отрасль каталога dealroom_research_v1 у курируемых направлений: строка «отрасль: id …».
// Научная область (branch) говорит, где публикуются статьи, а инвестору нужна отрасль, куда идут деньги.
// Таблица строкой, а не картой: имена отраслей уже встречаются в пакете, и карта упёрлась бы в goconst.
const liveFamilyIndustryTable = `
computing: deep-research speculative-decoding mcp test-time-scaling rlvr kv-cache mamba-ssm diffusion-lm gui-agents
computing: agent-memory graphrag wasm-edge vector-search on-device-llm program-synthesis stream-lakehouse
computing: neutral-atom-qubits quantum-error-correction
robotics: vla humanoid-robots industrial-manipulation warehouse-automation world-action-models
security: prompt-injection confidential-computing supply-chain-security fhe deepfake-detection post-quantum-crypto
security: zero-knowledge agent-security
semiconductors: neuromorphic photonic-computing eda-agents
science: quantum-sensing ai-scientist
energy: fusion-energy perovskite solid-state-batteries sodium-ion hydrogen small-modular-reactors
energy: long-duration-storage geothermal smart-grid datacenter-power grid-forming virtual-power-plant
environment: carbon-capture ai-weather climate-adaptation methane-monitoring marine-carbon geo-foundation
environment: biodiversity-ai
materials: high-entropy-alloys metamaterials mof superconductors thermoelectrics materials-agents generative-crystals
biotechnology: gene-editing protein-design single-cell synthetic-biology ai-drug-discovery organoids bio-agents
medicine: brain-computer-interface bioprinting medical-foundation-models wearable-biosensors mrna-therapeutics
medicine: clinical-agents patient-digital-twin
industry: additive-manufacturing digital-twin predictive-maintenance green-steel text-to-cad
telecom: wireless-6g wireless-world-model
fintech: llm-finance fraud-detection algorithmic-trading credit-risk-ml portfolio-rl defi payments digital-currency
fintech: rwa-tokenization embedded-finance insurtech prediction-markets agentic-payments
economics: climate-finance
space: smallsat-constellations earth-observation on-orbit-servicing electric-propulsion optical-space-links
space: orbital-datacenters
transport: autonomous-driving ev-charging clean-aviation uav-logistics driving-world-model
agriculture: precision-agriculture vertical-farming crop-genomics agri-agents
food: alternative-protein food-ai
fashion: fashion-llm video-try-on
gaming: game-world-models
hosting: disaggregated-serving
real_estate: housing-markets
marketing: geo-search
media: ai-video-detection multi-shot-video
legal: legal-benchmarks
jobs_recruitment: ai-labor-exposure
home_living: household-robots
education: pedagogical-agents
dating: ai-companionship
event_tech: live-commerce
wellness_beauty: ai-psychosis
kids: child-online-safety
music: song-generation
sports: team-sports-ai sports-betting
consumer_electronics: proactive-assistant
water: water-ai microplastics
travel: tourism-ai cultural-heritage-ai poi-llm
agriculture: rna-biopesticides
biotechnology: bridge-recombinase
consumer_electronics: smart-rings
education: cognitive-offloading
energy: lithium-extraction natural-hydrogen perovskite-tandem
environment: nitrate-to-ammonia
food: precision-fermentation
hosting: liquid-cooling
industry: phase-field-fracture
marketing: influencer-marketing
materials: deep-sea-mining
medicine: bundibugyo glp1-drugs
real_estate: datacenter-siting
science: nuclear-clock higgsino-lz
semiconductors: lithium-tantalate cfet
water: datacenter-water
wellness_beauty: social-prescribing climate-anxiety
`

func liveFamilyIndustries() map[string]string {
	industries := map[string]string{}
	for line := range strings.Lines(liveFamilyIndustryTable) {
		industry, ids, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		for id := range strings.FieldsSeq(ids) {
			industries[id] = strings.TrimSpace(industry)
		}
	}
	return industries
}

// liveTopicIndustry — отрасль из таблицы для курируемого направления, иначе по каталогу рубрик по имени и терминам.
func liveTopicIndustry(topic LiveTopic, industries map[string]string, rubrics []liveRubric) string {
	if industry := industries[topic.ID]; topic.Curated && industry != "" {
		return industry
	}
	text := strings.Join(slices.Concat([]string{topic.Name, topic.Label}, topic.Query.Terms), " ")
	return liveRubricOf(rubrics, text)
}
