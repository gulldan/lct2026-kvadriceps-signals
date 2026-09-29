package application

// Запросы английские: окно arXiv англоязычное.

func liveSoftwareFamilies() []liveFamily {
	return []liveFamily{
		{id: "wasm-edge", name: "WebAssembly edge runtimes", query: LiveQuery{
			Arxiv: `all:webassembly OR all:wasm OR all:"edge runtime"`,
			Terms: []string{"webassembly", "wasm runtime", "edge runtime"}}},
		{id: "vector-search", name: "Vector databases and ANN search", query: LiveQuery{
			Arxiv: `all:"vector database" OR all:"approximate nearest neighbor" OR all:"vector search"`,
			Terms: []string{"vector database", "approximate nearest neighbor search", "vector index"}}},
		{id: "on-device-llm", name: "On-device inference", query: LiveQuery{
			Arxiv: `all:"on-device" AND (all:llm OR all:inference OR all:"language model")`,
			Terms: []string{"on-device inference", "edge llm", "quantized inference"}}},
		{id: "program-synthesis", name: "Program synthesis and proof automation", query: LiveQuery{
			Arxiv: `all:"program synthesis" OR all:"proof assistant" OR all:"automated theorem proving"`,
			Terms: []string{"program synthesis", "automated theorem proving", "ai code generation"}}},
		{id: "stream-lakehouse", name: "Streaming and lakehouse engines", query: LiveQuery{
			Arxiv: `all:"stream processing" OR all:lakehouse OR all:"query engine"`,
			Terms: []string{"stream processing", "lakehouse", "columnar query engine"}}},
	}
}

func liveSecurityFamilies() []liveFamily {
	return []liveFamily{
		{id: "confidential-computing", name: "Confidential computing", query: LiveQuery{
			Arxiv: `all:"confidential computing" OR all:"trusted execution environment" OR all:"secure enclave"`,
			Terms: []string{"confidential computing", "trusted execution environment", "secure enclave"}}},
		{id: "supply-chain-security", name: "Software supply-chain security", query: LiveQuery{
			Arxiv: `all:"software supply chain" OR all:sbom OR all:"software bill of materials"`,
			Terms: []string{"software supply chain security", "software bill of materials", "dependency attack"}}},
		{id: "fhe", name: "Homomorphic encryption and MPC", query: LiveQuery{
			Arxiv: `all:"homomorphic encryption" OR all:"secure multiparty computation" OR all:"private inference"`,
			Terms: []string{"fully homomorphic encryption", "secure multiparty computation", "privacy preserving computation"}}},
		{id: "deepfake-detection", name: "Deepfake detection and content provenance", query: LiveQuery{
			Arxiv: `all:deepfake OR all:"synthetic media" OR all:"content provenance" OR all:watermarking`,
			Terms: []string{"deepfake detection", "synthetic media detection", "content provenance"}}},
	}
}

func liveIndustryFamilies() []liveFamily {
	return []liveFamily{
		{id: "humanoid-robots", name: "Humanoid robots", query: LiveQuery{
			Arxiv: `all:"humanoid robot" OR all:"legged locomotion" OR all:"whole-body control"`,
			Terms: []string{"humanoid robot", "legged locomotion", "bipedal robot"}}},
		{id: "industrial-manipulation", name: "Collaborative robots and bin picking", query: LiveQuery{
			Arxiv: `all:"collaborative robot" OR all:"bin picking" OR (all:"robotic manipulation" AND all:industrial)`,
			Terms: []string{"collaborative robot", "robotic bin picking", "industrial manipulation"}}},
		{id: "predictive-maintenance", name: "Predictive maintenance", query: LiveQuery{
			Arxiv: `all:"predictive maintenance" OR all:"prognostics and health management" OR all:"remaining useful life"`,
			Terms: []string{"predictive maintenance", "prognostics and health management", "remaining useful life"}}},
		{id: "green-steel", name: "Low-carbon metallurgy and cement", query: LiveQuery{
			Arxiv: `all:"green steel" OR all:"direct reduced iron" OR all:"low-carbon cement" OR ` +
				`all:"decarbonization of industry"`,
			Terms: []string{"green steel", "hydrogen direct reduction of iron", "low-carbon cement"}}},
	}
}

func liveHealthFamilies() []liveFamily {
	return []liveFamily{
		{id: "mrna-therapeutics", name: "mRNA therapeutics and delivery", query: LiveQuery{
			Arxiv: `all:"mRNA vaccine" OR all:"lipid nanoparticle" OR all:"mRNA therapeutic"`,
			Terms: []string{"mrna therapeutics", "lipid nanoparticle delivery", "mrna vaccine"}}},
		{id: "ai-drug-discovery", name: "AI-driven drug discovery", query: LiveQuery{
			Arxiv: `all:"drug discovery" AND (all:"machine learning" OR all:"generative model" OR all:"molecular design")`,
			Terms: []string{"ai drug discovery", "generative molecular design", "virtual screening"}}},
		{id: "medical-foundation-models", name: "Medical foundation models", query: LiveQuery{
			Arxiv: `(all:"medical imaging" OR all:clinical) AND ` +
				`(all:"foundation model" OR all:"vision-language" OR all:"large language model")`,
			Terms: []string{"medical foundation model", "clinical language model", "medical imaging ai"}}},
		{id: "wearable-biosensors", name: "Wearable biosensors", query: LiveQuery{
			Arxiv: `all:"wearable sensor" OR all:biosensor OR all:"continuous glucose"`,
			Terms: []string{"wearable biosensor", "continuous health monitoring", "continuous glucose monitoring"}}},
		{id: "organoids", name: "Organoids and organ-on-chip", query: LiveQuery{
			Arxiv: `all:organoid OR all:"organ-on-chip" OR all:"microphysiological system"`,
			Terms: []string{"organoid", "organ-on-a-chip", "microphysiological system"}}},
		{id: "synthetic-biology", name: "Synthetic biology", query: LiveQuery{
			Arxiv: `all:"synthetic biology" OR all:"metabolic engineering" OR all:"cell-free expression"`,
			Terms: []string{"synthetic biology", "metabolic engineering", "engineered microbe"}}},
	}
}

func liveSpaceFamilies() []liveFamily {
	return []liveFamily{
		{id: "smallsat-constellations", name: "Small-satellite constellations", query: LiveQuery{
			Arxiv: `all:"satellite constellation" OR all:cubesat OR all:smallsat`,
			Terms: []string{"satellite constellation", "cubesat", "small satellite"}}},
		{id: "earth-observation", name: "Earth observation analytics", query: LiveQuery{
			Arxiv: `all:"earth observation" OR all:"satellite imagery" OR all:"remote sensing foundation model"`,
			Terms: []string{"earth observation", "satellite imagery analytics", "remote sensing foundation model"}}},
		{id: "on-orbit-servicing", name: "On-orbit servicing and debris removal", query: LiveQuery{
			Arxiv: `all:"on-orbit servicing" OR all:"space debris" OR all:"in-space manufacturing" OR ` +
				`all:"rendezvous and docking"`,
			Terms: []string{"on-orbit servicing", "space debris removal", "in-space manufacturing"}}},
		{id: "electric-propulsion", name: "Electric spacecraft propulsion", query: LiveQuery{
			Arxiv: `all:"electric propulsion" OR all:"hall thruster" OR all:"ion thruster"`,
			Terms: []string{"electric propulsion", "hall thruster", "spacecraft propulsion"}}},
		{id: "optical-space-links", name: "Optical inter-satellite links", query: LiveQuery{
			Arxiv: `all:"free-space optical" OR (all:"laser communication" AND all:satellite) OR all:"inter-satellite link"`,
			Terms: []string{"free-space optical communication", "optical inter-satellite link", "satellite laser link"}}},
	}
}

func liveEnergyFamilies() []liveFamily {
	return []liveFamily{
		{id: "small-modular-reactors", name: "Small modular reactors", query: LiveQuery{
			Arxiv: `all:"small modular reactor" OR all:"molten salt reactor" OR all:"advanced nuclear"`,
			Terms: []string{"small modular reactor", "molten salt reactor", "advanced nuclear reactor"}}},
		{id: "long-duration-storage", name: "Long-duration energy storage", query: LiveQuery{
			Arxiv: `all:"long-duration energy storage" OR all:"flow battery" OR all:"grid-scale storage"`,
			Terms: []string{"long duration energy storage", "flow battery", "grid-scale storage"}}},
		{id: "geothermal", name: "Enhanced geothermal systems", query: LiveQuery{
			Arxiv: `all:"enhanced geothermal" OR all:"geothermal energy" OR all:"deep drilling"`,
			Terms: []string{"enhanced geothermal system", "geothermal energy", "deep geothermal drilling"}}},
	}
}

func liveMobilityFamilies() []liveFamily {
	return []liveFamily{
		{id: "autonomous-driving", name: "Autonomous driving", query: LiveQuery{
			Arxiv: `all:"autonomous driving" OR all:"self-driving" OR all:"end-to-end driving"`,
			Terms: []string{"autonomous driving", "self-driving vehicle", "end-to-end driving policy"}}},
		{id: "ev-charging", name: "EV charging and vehicle-to-grid", query: LiveQuery{
			Arxiv: `all:"electric vehicle charging" OR all:"vehicle-to-grid" OR all:"fast charging"`,
			Terms: []string{"electric vehicle charging", "vehicle-to-grid", "fast charging infrastructure"}}},
		{id: "clean-aviation", name: "Sustainable and electric aviation", query: LiveQuery{
			Arxiv: `all:"sustainable aviation fuel" OR all:"electric aircraft" OR all:"hydrogen aviation" OR ` +
				`all:"urban air mobility"`,
			Terms: []string{"sustainable aviation fuel", "electric aircraft", "urban air mobility"}}},
		{id: "warehouse-automation", name: "Warehouse automation", query: LiveQuery{
			Arxiv: `all:"warehouse automation" OR all:"autonomous mobile robot" OR all:"logistics optimization"`,
			Terms: []string{"warehouse automation", "autonomous mobile robot", "logistics optimization"}}},
		{id: "uav-logistics", name: "Drone delivery and UAV traffic", query: LiveQuery{
			Arxiv: `all:"unmanned aerial" AND (all:delivery OR all:"traffic management" OR all:logistics)`,
			Terms: []string{"drone delivery", "uav traffic management", "unmanned aerial logistics"}}},
	}
}

func liveAgroFamilies() []liveFamily {
	return []liveFamily{
		{id: "precision-agriculture", name: "Precision agriculture", query: LiveQuery{
			Arxiv: `all:"precision agriculture" OR all:"crop monitoring" OR all:"agricultural robot"`,
			Terms: []string{"precision agriculture", "crop monitoring", "agricultural robotics"}}},
		{id: "alternative-protein", name: "Alternative proteins", query: LiveQuery{
			Arxiv: `all:"cultivated meat" OR all:"cultured meat" OR all:"precision fermentation" OR ` +
				`all:"plant-based protein"`,
			Terms: []string{"cultivated meat", "precision fermentation", "alternative protein"}}},
		{id: "vertical-farming", name: "Controlled-environment agriculture", query: LiveQuery{
			Arxiv: `all:"vertical farming" OR all:"controlled environment agriculture" OR all:hydroponic`,
			Terms: []string{"vertical farming", "controlled environment agriculture", "hydroponics"}}},
		{id: "crop-genomics", name: "Crop genomics and breeding", query: LiveQuery{
			Arxiv: `all:"plant genomics" OR all:"crop breeding" OR all:"gene-edited crop" OR all:"genomic selection"`,
			Terms: []string{"crop genomics", "genomic selection in breeding", "gene edited crop"}}},
	}
}

func liveClimateFamilies() []liveFamily {
	return []liveFamily{
		{id: "climate-adaptation", name: "Climate adaptation and hazard forecasting", query: LiveQuery{
			Arxiv: `all:"climate adaptation" OR all:"flood forecasting" OR all:"wildfire prediction"`,
			Terms: []string{"climate adaptation", "flood forecasting", "wildfire prediction"}}},
		{id: "methane-monitoring", name: "Methane and emissions monitoring", query: LiveQuery{
			Arxiv: `all:"methane emission" OR all:"greenhouse gas monitoring" OR all:"emission detection"`,
			Terms: []string{"methane detection", "greenhouse gas monitoring", "emissions satellite"}}},
		{id: "marine-carbon", name: "Ocean carbon removal", query: LiveQuery{
			Arxiv: `all:"ocean alkalinity" OR all:"marine carbon dioxide removal" OR all:"ocean carbon"`,
			Terms: []string{"marine carbon dioxide removal", "ocean alkalinity enhancement", "ocean carbon removal"}}},
	}
}
