package application

import (
	"bytes"
	"cmp"
	"context"
	"encoding/json/v2"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"maps"
	"net/http"
	"slices"
	"strings"
	"sync"
	"time"
)

const liveSourceDiscovered = "discovered"

type liveFamily struct {
	id, name string
	query    LiveQuery
	plan     *LiveQueryPlan
}

// Названия и запросы семейств английские: окно arXiv англоязычное.
func liveFamilies() []liveFamily {
	families := slices.Concat(liveComputingFamilies(), livePhysicalFamilies(),
		liveLifeAndEconomyFamilies(), liveFinanceFamilies(), liveSoftwareFamilies(),
		liveSecurityFamilies(), liveIndustryFamilies(), liveHealthFamilies(),
		liveSpaceFamilies(), liveEnergyFamilies(), liveMobilityFamilies(),
		liveAgroFamilies(), liveClimateFamilies())
	for i := range families {
		query := &families[i].query
		if len(query.Terms) == 0 {
			continue
		}
		if query.GitHub == "" {
			query.GitHub = query.Terms[0]
		}
		if query.HN == "" {
			query.HN = query.Terms[0]
		}
	}
	return families
}

func liveComputingFamilies() []liveFamily {
	return []liveFamily{
		{id: liveGraphRAG, name: "GraphRAG", query: LiveQuery{
			Arxiv:  `all:"graph rag" OR all:graphrag OR all:"graph-based retrieval-augmented"`,
			GitHub: liveGraphRAG, HN: liveGraphRAG,
			Terms: []string{"graph rag", "graphrag", "knowledge graph retrieval"}}},
		{id: "deep-research", name: "Deep research agents", query: LiveQuery{
			Arxiv:  `all:"deep research" OR all:"agentic rag" OR all:"agentic retrieval" OR all:"search agent"`,
			GitHub: `"deep research" agent`, HN: "deep research",
			Terms: []string{"deep research agent", "agentic retrieval", "autonomous research agent"}}},
		{id: "speculative-decoding", name: "Speculative decoding", query: LiveQuery{
			Arxiv:  `all:"speculative decoding"`,
			GitHub: `"speculative decoding"`, HN: "speculative decoding",
			Terms: []string{"speculative decoding", "draft model inference"}}},
		{id: "mcp", name: "Model Context Protocol", query: LiveQuery{
			Arxiv:  `all:"model context protocol"`,
			GitHub: `"model context protocol"`, HN: "model context protocol",
			Terms: []string{"model context protocol", "agent tool protocol"}}},
		{id: "test-time-scaling", name: "Test-time scaling", query: LiveQuery{
			Arxiv:  `all:"test-time scaling" OR all:"test-time compute"`,
			GitHub: `"test-time scaling"`, HN: "test-time compute",
			Terms: []string{"test-time scaling", "inference time compute", "reasoning model"}}},
		{id: "rlvr", name: "RL with verifiable rewards", query: LiveQuery{
			Arxiv:  `all:"verifiable rewards" OR all:"verifiable reward" OR all:RLVR`,
			GitHub: `"verifiable rewards"`, HN: "verifiable rewards",
			Terms: []string{"verifiable rewards", "reinforcement learning from verifiable rewards"}}},
		{id: "kv-cache", name: "KV-cache compression", query: LiveQuery{
			Arxiv:  `all:"kv cache" AND (all:compression OR all:eviction OR all:quantization)`,
			GitHub: `"kv cache" compression`, HN: "kv cache",
			Terms: []string{"kv cache compression", "attention cache quantization"}}},
		{id: "mamba-ssm", name: "State space models (Mamba)", query: LiveQuery{
			Arxiv:  `all:mamba OR all:"state space model"`,
			GitHub: `mamba "state space"`, HN: "state space model",
			Terms: []string{"state space model", "mamba architecture", "linear attention"}}},
		{id: "diffusion-lm", name: "Diffusion language models", query: LiveQuery{
			Arxiv:  `all:"diffusion language model" OR all:"diffusion llm" OR all:"masked diffusion model"`,
			GitHub: `"diffusion language model"`, HN: "diffusion language model",
			Terms: []string{"diffusion language model", "masked diffusion model"}}},
		{id: "gui-agents", name: "GUI and computer-use agents", query: LiveQuery{
			Arxiv:  `all:"gui agent" OR all:"computer use agent" OR all:"computer-use agent"`,
			GitHub: `"computer use" agent`, HN: "computer use agent",
			Terms: []string{"gui agent", "computer use agent", "screen automation agent"}}},
		{id: "vla", name: "Vision-language-action models", query: LiveQuery{
			Arxiv:  `all:"vision-language-action"`,
			GitHub: `"vision-language-action"`, HN: "vision-language-action",
			Terms: []string{"vision language action model", "robot foundation model"}}},
		{id: "agent-memory", name: "Agent memory", query: LiveQuery{
			Arxiv:  `all:"agent memory" OR (all:"long-term memory" AND all:agents)`,
			GitHub: `"agent memory" llm`, HN: "agent memory",
			Terms: []string{"agent memory", "long-term memory for agents"}}},
		{id: "prompt-injection", name: "Agent and prompt-injection security", query: LiveQuery{
			Arxiv:  `all:"prompt injection" OR all:jailbreak OR all:"indirect prompt"`,
			GitHub: `"prompt injection"`, HN: "prompt injection",
			Terms: []string{"prompt injection", "llm jailbreak", "agent security"}}},
		{id: "neuromorphic", name: "Neuromorphic computing", query: LiveQuery{
			Arxiv:  `all:neuromorphic OR all:"spiking neural network"`,
			GitHub: `"spiking neural network"`, HN: "neuromorphic computing",
			Terms: []string{"neuromorphic computing", "spiking neural network"}}},
		{id: "photonic-computing", name: "Photonic computing", query: LiveQuery{
			Arxiv:  `all:"photonic computing" OR all:"optical neural network" OR all:"photonic accelerator"`,
			GitHub: `"photonic computing"`, HN: "optical computing",
			Terms: []string{"photonic computing", "optical neural network", "silicon photonics"}}},
	}
}

func livePhysicalFamilies() []liveFamily {
	return []liveFamily{
		{id: "post-quantum-crypto", name: "Post-quantum cryptography", query: LiveQuery{
			Arxiv: `all:"post-quantum cryptography" OR all:"post-quantum" OR all:"lattice-based cryptography"`,
			Terms: []string{"post-quantum cryptography", "lattice-based cryptography", "quantum-safe encryption"}}},
		{id: "quantum-error-correction", name: "Quantum error correction", query: LiveQuery{
			Arxiv: `all:"quantum error correction" OR all:"surface code" OR all:"logical qubit"`,
			Terms: []string{"quantum error correction", "surface code", "logical qubit"}}},
		{id: "neutral-atom-qubits", name: "Neutral-atom quantum computers", query: LiveQuery{
			Arxiv: `(all:"neutral atom" OR all:"rydberg") AND (all:qubit OR all:"quantum computing" OR all:"quantum processor")`,
			Terms: []string{"neutral atom quantum computer", "rydberg atom array"}}},
		{id: "quantum-sensing", name: "Quantum sensing", query: LiveQuery{
			Arxiv: `all:"quantum sensing" OR all:"quantum sensor" OR all:"quantum metrology"`,
			Terms: []string{"quantum sensing", "quantum sensor", "quantum metrology"}}},
		{id: "fusion-energy", name: "Fusion energy", query: LiveQuery{
			Arxiv: `all:tokamak OR all:stellarator OR all:"inertial confinement fusion" OR all:"fusion reactor"`,
			Terms: []string{"fusion energy", "tokamak", "inertial confinement fusion"}}},
		{id: "perovskite", name: "Perovskite solar cells", query: LiveQuery{
			Arxiv: `all:perovskite AND (all:solar OR all:photovoltaic)`, Terms: []string{perovskiteSolar, "perovskite"}}},
		{id: "solid-state-batteries", name: "Solid-state batteries", query: LiveQuery{
			Arxiv: `all:"solid state battery" OR all:"solid-state batteries"`,
			Terms: []string{"solid state battery", "solid state batteries"}}},
		{id: "sodium-ion", name: "Sodium-ion batteries", query: LiveQuery{
			Arxiv: `all:"sodium ion" OR all:"sodium-ion"`, Terms: []string{"sodium ion"}}},
		{id: "hydrogen", name: "Hydrogen production", query: LiveQuery{
			Arxiv: `all:"green hydrogen" OR all:"hydrogen production"`,
			Terms: []string{"hydrogen production", "hydrogen generation", "green hydrogen", "water electrolysis"}}},
		{id: "carbon-capture", name: "Carbon capture", query: LiveQuery{
			Arxiv: `all:"carbon capture" OR all:"direct air capture"`, Terms: []string{"carbon capture", "direct air capture"}}},
		{id: "high-entropy-alloys", name: "High-entropy alloys", query: LiveQuery{
			Arxiv: `all:"high entropy alloy" OR all:"high-entropy alloys"`,
			Terms: []string{"high entropy alloy", "multi-principal element alloy"}}},
		{id: "metamaterials", name: "Metamaterials and metasurfaces", query: LiveQuery{
			Arxiv: `all:metamaterial OR all:metasurface`,
			Terms: []string{"metamaterial", "metasurface", "optical metasurface"}}},
		{id: "mof", name: "Metal-organic frameworks", query: LiveQuery{
			Arxiv: `all:"metal-organic framework" OR all:"metal organic frameworks" OR all:"covalent organic framework"`,
			Terms: []string{"metal-organic framework", "covalent organic framework", "porous sorbent"}}},
		{id: "superconductors", name: "High-temperature superconductors", query: LiveQuery{
			Arxiv: `all:"high-temperature superconduct" OR all:"room temperature superconduct" OR all:"superconducting tape"`,
			Terms: []string{"high temperature superconductor", "superconducting tape", "rebco"}}},
		{id: "thermoelectrics", name: "Thermoelectric materials", query: LiveQuery{
			Arxiv: `all:thermoelectric AND (all:material OR all:"figure of merit" OR all:generator)`,
			Terms: []string{"thermoelectric material", "thermoelectric generator", "waste heat recovery"}}},
	}
}

func liveLifeAndEconomyFamilies() []liveFamily {
	return []liveFamily{
		{id: "gene-editing", name: "Gene editing (CRISPR)", query: LiveQuery{
			Arxiv: `all:CRISPR OR all:"gene editing"`, Terms: []string{"CRISPR", "gene editing"}}},
		{id: "protein-design", name: "Computational protein design", query: LiveQuery{
			Arxiv: `all:"protein design" OR all:"protein structure prediction" OR all:alphafold OR all:"protein language model"`,
			Terms: []string{"de novo protein design", "protein structure prediction", "protein language model"}}},
		{id: "brain-computer-interface", name: "Brain-computer interfaces", query: LiveQuery{
			Arxiv: `all:"brain-computer interface" OR all:"brain computer interface" OR all:"neural decoding"`,
			Terms: []string{"brain-computer interface", "neural decoding", "neuroprosthesis"}}},
		{id: "single-cell", name: "Single-cell and spatial omics", query: LiveQuery{
			Arxiv: `all:"single-cell" AND (all:transcriptomic OR all:sequencing OR all:omics OR all:spatial)`,
			Terms: []string{"single-cell transcriptomics", "spatial omics", "single cell sequencing"}}},
		{id: "bioprinting", name: "3D bioprinting", query: LiveQuery{Arxiv: `all:bioprinting`, Terms: []string{"bioprinting"}}},
		{id: "additive-manufacturing", name: "Additive manufacturing", query: LiveQuery{
			Arxiv: `all:"additive manufacturing"`,
			Terms: []string{"additive manufacturing", "3d printing", "three dimensional printing"}}},
		{id: "digital-twin", name: "Digital twins", query: LiveQuery{
			Arxiv: `all:"digital twin"`,
			Terms: []string{"digital twin", "digital twin of production"}}},
		{id: "wireless-6g", name: "6G wireless networks", query: LiveQuery{
			Arxiv: `all:"6g" AND (all:network OR all:wireless OR all:communication)`,
			Terms: []string{"6g network", "terahertz communication", "reconfigurable intelligent surface"}}},
		{id: "smart-grid", name: "Smart and inverter-based grids", query: LiveQuery{
			Arxiv: `all:"smart grid" OR all:"grid-forming" OR all:"virtual power plant"`,
			Terms: []string{"smart grid", "grid-forming inverter", "virtual power plant"}}},
		{id: "ai-weather", name: "AI weather and climate forecasting", query: LiveQuery{
			Arxiv: `(all:"weather forecasting" OR all:"climate model") AND ` +
				`(all:"machine learning" OR all:"neural network" OR all:"foundation model")`,
			Terms: []string{"machine learning weather forecasting", "ai climate model", "data-driven weather prediction"}}},
	}
}

// Финансы — отдельным блоком: по архивам они размазаны (q-fin, cs.LG, econ).
func liveFinanceFamilies() []liveFamily {
	return []liveFamily{
		{id: "llm-finance", name: "Language models in finance", query: LiveQuery{
			Arxiv: `all:"large language model" AND (all:financial OR all:finance OR all:banking)`,
			Terms: []string{"large language model in finance", "financial language model", "llm for banking"}}},
		{id: "fraud-detection", name: "Fraud and money-laundering detection", query: LiveQuery{
			Arxiv: `all:"fraud detection" OR all:"money laundering" OR all:"transaction monitoring"`,
			Terms: []string{"fraud detection", "anti-money laundering", "transaction monitoring"}}},
		{id: "algorithmic-trading", name: "Algorithmic trading and market microstructure", query: LiveQuery{
			Arxiv: `all:"algorithmic trading" OR all:"limit order book" OR all:"market making"`,
			Terms: []string{"algorithmic trading", "limit order book", "market making"}}},
		{id: "credit-risk-ml", name: "Credit scoring by machine learning", query: LiveQuery{
			Arxiv: `all:"credit scoring" OR all:"credit risk" OR all:"default prediction"`,
			Terms: []string{"credit scoring", "credit risk model", "loan default prediction"}}},
		{id: "portfolio-rl", name: "Portfolio management by reinforcement learning", query: LiveQuery{
			Arxiv: `all:"portfolio optimization" OR all:"portfolio management" OR all:"asset allocation"`,
			Terms: []string{"portfolio optimization", "reinforcement learning for trading", "asset allocation"}}},
		{id: "defi", name: "Decentralised finance", query: LiveQuery{
			Arxiv: `all:"decentralized finance" OR all:defi OR all:"automated market maker" OR all:"smart contract"`,
			Terms: []string{"decentralized finance", "automated market maker", "smart contract audit"}}},
		{id: "zero-knowledge", name: "Zero-knowledge proofs", query: LiveQuery{
			Arxiv: `all:"zero-knowledge proof" OR all:"zero knowledge" OR all:"succinct argument"`,
			Terms: []string{"zero-knowledge proof", "zk-snark", "verifiable computation"}}},
		{id: "payments", name: "Payment rails and cross-border transfers", query: LiveQuery{
			Arxiv: `all:"payment system" OR all:"cross-border payment" OR all:"retail payment" OR all:"payment network"`,
			Terms: []string{"payment system", "cross-border payment", "instant payments"}}},
		{id: "climate-finance", name: "Climate risk in finance", query: LiveQuery{
			Arxiv: `all:"climate risk" AND (all:financial OR all:portfolio OR all:bank OR all:insurance)`,
			Terms: []string{"climate financial risk", "transition risk", "green finance"}}},
		{id: "digital-currency", name: "Digital currencies and stablecoins", query: LiveQuery{
			Arxiv: `all:"central bank digital currency" OR all:cbdc OR all:stablecoin OR all:"tokenized deposit"`,
			Terms: []string{"central bank digital currency", "stablecoin", "tokenized deposit"}}},
		{id: "rwa-tokenization", name: "Tokenised real-world assets", query: LiveQuery{
			Arxiv: `all:tokenization AND (all:asset OR all:security OR all:bond OR all:settlement)`,
			Terms: []string{"asset tokenization", "tokenized securities", "real-world asset tokenization"}}},
		{id: "embedded-finance", name: "Open banking and embedded finance", query: LiveQuery{
			Arxiv: `all:"embedded finance" OR all:"open banking" OR all:"banking as a service"`,
			Terms: []string{"embedded finance", "open banking api", "banking as a service"}}},
		{id: "insurtech", name: "Parametric insurance and risk pricing", query: LiveQuery{
			Arxiv: `all:"parametric insurance" OR all:"insurance pricing" OR all:"actuarial model"`,
			Terms: []string{"parametric insurance", "insurance pricing model", "insurtech"}}},
	}
}

// Реестр и внешние каналы обязаны спрашивать одно и то же.
func liveTopicTerms(topic *LiveTopic) []string {
	if len(topic.Query.Terms) > 0 {
		return topic.Query.Terms
	}
	if phrase := strings.TrimSpace(topic.Query.HN); phrase != "" {
		return []string{phrase}
	}
	return nil
}

func (s *TrendLiveService) collectAll(ctx context.Context, src *liveSources, snapshot *LiveSnapshot, window []StudyPaper) error {
	families := liveFamilies()
	members, err := liveMatchFamilies(families, window, src.metadata)
	if err != nil {
		return err
	}
	// Ряды всех семейств — до первой темы: медианы карт Юна по набору, и найденные позже направления сравниваются с ними.
	src.emergence = liveFamilyMedians(snapshot.Periods, members)
	seen := map[string]bool{}
	if err := s.collectFamilies(ctx, src, snapshot, families, members, seen); err != nil {
		return err
	}
	previous, categories, metadata := s.previousAnalysis()
	snapshot.PaperMetadata = liveMergeMetadata(src.metadata, metadata, snapshot.Papers)
	if s.analyzer != nil {
		// Всё окно не влезает в один запрос к анализу: уходит детерминированная выборка по областям, а полный
		// размер окна передаётся числом, чтобы ответ назвал себя выборочным.
		sample := liveBranchSample(window, src.metadata, LiveDiscoveryRequestLimit)
		request := LiveAnalysisRequest{Snapshot: *snapshot, DiscoveryPapers: sample, DiscoveryTotal: len(window),
			Metadata: liveMetadataFor(src.metadata, sample, snapshot.Papers), Previous: previous,
			PreviousCategories: categories, Discover: true}
		// Поиск новых направлений — дополнение: его отказ становится ограничением, а наблюдение публикуется без него.
		if err := s.collectSemanticDiscoveries(ctx, src, snapshot, request, window, seen); err != nil {
			slog.Warn("radar discovery unavailable", "error", err)
			snapshot.Limits = append(snapshot.Limits, liveLimitFail(liveLimitDiscoveryFailed, "discovery", err))
		}
		if err := ctx.Err(); err != nil {
			return fmt.Errorf("live: discovery canceled: %w", err)
		}
		s.collectCitations(ctx, src, snapshot, window)
		snapshot.PaperMetadata = liveMergeMetadata(src.metadata, metadata, snapshot.Papers)
		return nil
	}
	for _, branch := range liveBranchOrder() {
		subset := []StudyPaper{}
		for _, paper := range window {
			if liveBranch(src.metadata[paper.ID].Category) == branch {
				subset = append(subset, paper)
			}
		}
		candidates := liveDiscover(subset, src.metadata, snapshot.Periods, liveFamilies(), liveDiscoverRecent, 10)
		for _, candidate := range candidates[:min(len(candidates), liveDiscoverPerBranch)] {
			if ctx.Err() != nil {
				return fmt.Errorf("live: discovery canceled: %w", ctx.Err())
			}
			topic := liveBuild(ctx, src, liveCandidateFamily(candidate), snapshot.Periods, candidate.papers, false)
			topic.Source = liveSourceDiscovered
			topic.Curated = false
			topic.Label = s.topicLabel(ctx, candidate.papers)
			snapshot.addTopic(topic, candidate.papers, seen, true)
		}
	}
	s.collectCitations(ctx, src, snapshot, window)
	snapshot.PaperMetadata = liveMergeMetadata(src.metadata, metadata, snapshot.Papers)
	return nil
}

func (snapshot *LiveSnapshot) addTopic(topic LiveTopic, papers []StudyPaper, seen map[string]bool, discovered bool) {
	if discovered {
		snapshot.Discovered = append(snapshot.Discovered, topic)
	} else {
		snapshot.Topics = append(snapshot.Topics, topic)
	}
	for _, paper := range papers {
		if !seen[paper.ID] {
			seen[paper.ID] = true
			snapshot.Papers = append(snapshot.Papers, paper)
		}
	}
}

func (s *TrendLiveService) collectFamilies(ctx context.Context, src *liveSources, snapshot *LiveSnapshot,
	families []liveFamily, members [][]StudyPaper, seen map[string]bool,
) error {
	for i, family := range families {
		if err := ctx.Err(); err != nil {
			return fmt.Errorf("live: collection canceled: %w", err)
		}
		// Семейство без статей в окне — промах запроса, а не слабый сигнал: на карте был бы пустой кружок.
		if len(members[i]) == 0 {
			slog.Info("radar family has no papers in window", "topic", family.id)
			continue
		}
		slog.Info("collecting radar family", "topic", family.id, "papers", len(members[i]))
		snapshot.addTopic(liveBuild(ctx, src, family, snapshot.Periods, members[i], true), members[i], seen, false)
	}
	return nil
}

func (s *TrendLiveService) collectSemanticDiscoveries(
	ctx context.Context, src *liveSources, snapshot *LiveSnapshot, request LiveAnalysisRequest, window []StudyPaper,
	seen map[string]bool,
) error {
	result, err := s.analyzer.Analyze(ctx, request)
	if err != nil {
		return fmt.Errorf("live discovery: %w", err)
	}
	byID := make(map[string]StudyPaper, len(request.DiscoveryPapers))
	for _, paper := range request.DiscoveryPapers {
		byID[paper.ID] = paper
	}
	dropped := 0
	for _, discovery := range result.Discoveries {
		members := make([]StudyPaper, 0, len(discovery.PaperIDs))
		for _, id := range discovery.PaperIDs {
			paper, ok := byID[id]
			if !ok {
				return fmt.Errorf("live discovery: unknown paper %q", id)
			}
			members = append(members, paper)
		}
		if len(members) == 0 {
			continue
		}
		discovery.Name = liveUsableName(discovery)
		family := liveFamily{id: discovery.ID, name: discovery.Name, query: discovery.Query}
		// Кластер найден на выборке, но считается по всему окну: стадия — по полным числам, а не по доле выборки.
		sampled := discovery.Sampled
		if full, err := liveMatchFamilies([]liveFamily{family}, window, src.metadata); err == nil && len(full[0]) >= len(members) {
			members, sampled = full[0], false
		}
		if len(members) < liveDiscoverMinPapers {
			dropped++
			continue
		}
		topic := liveBuild(ctx, src, family, snapshot.Periods, members, false)
		topic.Source, topic.Curated = liveSourceDiscovered, false
		topic.Analysis = &LiveTopicAnalysis{Version: result.Version, Discovery: new(discovery)}
		if sampled {
			topic.Limits = append(topic.Limits, liveLimitOf(liveLimitPopulationSample,
				"semantic discovery: deterministic population sample; counts are sample observations", nil))
			topic.Stage = "unassessed"
		}
		topic.Label = s.topicLabel(ctx, members)
		snapshot.addTopic(topic, members, seen, true)
	}
	if dropped > 0 {
		snapshot.Limits = append(snapshot.Limits, liveLimitOf(liveLimitBelowFloor, fmt.Sprintf(
			"discovery: %d candidate directions with fewer than %d papers on the full window were left out",
			dropped, liveDiscoverMinPapers),
			map[string]int{"directions": dropped, liveValuePapers: liveDiscoverMinPapers}))
	}
	snapshot.Limits = append(snapshot.Limits, result.Limits...)
	return nil
}

// Сервис анализа отдаёт и буквальные «undefined»/«null» вместо имени.
func liveUsableName(discovery LiveDiscovery) string {
	if liveJunkQuery(discovery.Name) {
		return cmp.Or(strings.Join(discovery.Query.Terms, ", "), discovery.ID)
	}
	return strings.TrimSpace(discovery.Name)
}

// Остальные источники отвечают за секунды и не должны ждать недоступный arXiv.
func liveCollect(ctx context.Context, src *liveSources, family liveFamily, periods []LivePeriod,
) (LiveTopic, []StudyPaper, error) {
	from, through := liveDay(periods[0].Start), liveDay(periods[len(periods)-1].EndExclusive).AddDate(0, 0, -1)
	papers, total, truncated, arxivFailure := src.arxivPapers(ctx, arxivQuery(family.query.Arxiv, liveCategories, from, through))
	if err := ctx.Err(); err != nil {
		return LiveTopic{}, nil, fmt.Errorf("live: search canceled: %w", err)
	}
	papers = slices.DeleteFunc(papers, func(p StudyPaper) bool { return livePeriodIndex(periods, p.Date) < 0 })
	papers, scholarLimits := src.withScholar(ctx, papers, src.metadata, family.query.Terms, periods)
	topic := liveBuild(ctx, src, family, periods, papers, false)
	topic.Limits = append(topic.Limits, scholarLimits...)
	if err := ctx.Err(); err != nil {
		return LiveTopic{}, nil, fmt.Errorf("live: search canceled: %w", err)
	}
	switch {
	case arxivFailure != nil:
		topic.Limits = append(topic.Limits, liveLimitFail(liveLimitArxivOff, "arxiv", arxivFailure))
	case truncated:
		topic.Limits = append(topic.Limits, liveLimitOf(liveLimitArxivKept,
			fmt.Sprintf("arxiv: %d of %d papers enumerated", len(papers), total),
			map[string]int{"kept": len(papers), liveValueFound: total}))
	}
	if liveApplyTemporalCoverage(&topic) {
		topic.Confidence = new(liveConfidence(topic, liveAbsoluteBar()))
	}
	return topic, papers, nil
}

func liveBuild(ctx context.Context, src *liveSources, family liveFamily, periods []LivePeriod, papers []StudyPaper,
	history bool) LiveTopic {
	topic := LiveTopic{ID: family.id, PaperIDs: make([]string, 0, len(papers)), Series: liveSeries(periods, papers),
		Stage: studyStageUnassessed, Momentum: studyUnknown, Screening: liveCoherenceQuery, EvidenceIDs: []string{},
		External: []StudyEvent{}, Name: family.name, Query: family.query, Curated: true, Source: "curated",
		PapersCount: len(papers), Limits: []LiveLimit{}}
	for _, paper := range papers {
		topic.PaperIDs = append(topic.PaperIDs, paper.ID)
	}
	topic.Branch = liveTopicBranch(papers, src.metadata)
	if src.emergence.Size > 0 {
		topic.Emergence = liveEmergenceOf(periods, topic.Series, src.emergence)
	}
	// Код и обсуждения опрашиваются для любой области: даже вне ИТ они дают дополнительные
	// свидетельства, а пустой ответ ничего не портит.
	if history {
		liveSequentialChannels(ctx, src, &topic, periods)
	} else {
		liveParallelChannels(ctx, src, &topic, periods)
	}
	liveAssess(&topic, periods, papers)
	topic.Comparison = liveCompare(periods, papers)
	if topic.Registry != nil {
		topic.Limits = append(topic.Limits, liveLimitTexts(topic.Registry.Limits)...)
	}
	if topic.Corpus != nil {
		topic.Limits = append(topic.Limits, liveLimitTexts(topic.Corpus.Limits)...)
	}
	// Пороги абсолютные: доля от самого себя набиралась бы всегда.
	topic.Market = liveMarketMaturity(topic, liveAbsoluteBar()).field()
	topic.Adoption = liveAdoptionOf(liveTitles(papers))
	topic.Confidence = new(liveConfidence(topic, liveAbsoluteBar()))
	stampEventPassports(topic.External)
	if src.metadata != nil {
		stampPaperPassports(papers, src.metadata)
	}
	return topic
}

// Ночной проход идёт подряд по полутораста направлениям: порядок запросов бережёт лимиты внешних API.
func liveSequentialChannels(ctx context.Context, src *liveSources, topic *LiveTopic, periods []LivePeriod) {
	src.step(liveStepGitHub, 0)
	liveGitHub(ctx, src, topic, periods, true)
	src.step(liveStepHN, 0)
	liveHN(ctx, src, topic, periods)
	liveChannels(ctx, src, topic, periods)
	topic.Registry = liveRegistry(ctx, src, liveTopicTerms(topic), periods)
	// У ночного снимка владельца нет, и канал загруженных документов остаётся пустым.
	topic.Corpus = liveCorpusSearch(ctx, src, src.owner, liveTopicTerms(topic), periods)
}

// Каналы опрашиваются одновременно, каждый в свою копию темы; поля сводятся после.
func liveParallelChannels(ctx context.Context, src *liveSources, topic *LiveTopic, periods []LivePeriod) {
	terms := liveTopicTerms(topic)
	fresh := func() LiveTopic {
		copied := *topic
		copied.Limits, copied.External = nil, nil
		return copied
	}
	github, hn, channels := fresh(), fresh(), fresh()
	var wg sync.WaitGroup
	wg.Go(func() {
		src.step(liveStepGitHub, 0)
		liveGitHub(ctx, src, &github, periods, false)
	})
	wg.Go(func() {
		src.step(liveStepHN, 0)
		liveHN(ctx, src, &hn, periods)
	})
	wg.Go(func() { liveChannels(ctx, src, &channels, periods) })
	wg.Go(func() { topic.Registry = liveRegistry(ctx, src, terms, periods) })
	wg.Go(func() { topic.Corpus = liveCorpusSearch(ctx, src, src.owner, terms, periods) })
	wg.Wait()
	topic.Repos, topic.Discussions = github.Repos, hn.Discussions
	topic.Patents, topic.PatentsCN, topic.Web = channels.Patents, channels.PatentsCN, channels.Web
	topic.External = slices.Concat(topic.External, github.External, hn.External)
	topic.Limits = slices.Concat(topic.Limits, github.Limits, hn.Limits, channels.Limits)
}

func liveGitHub(ctx context.Context, src *liveSources, topic *LiveTopic, periods []LivePeriod, history bool) {
	q := topic.Query.GitHub
	if history {
		prior := src.priorRepos[topic.ID+"\n"+q]
		repos := make([]int, 0, len(periods))
		for _, period := range periods {
			if n, ok := prior[period.Start]; ok && period.Complete {
				repos = append(repos, n)
				continue
			}
			last := liveDay(period.EndExclusive).AddDate(0, 0, -1).Format("2006-01-02")
			result, err := src.githubSearch(ctx, fmt.Sprintf("%s created:%s..%s", q, period.Start, last), "", 1)
			if err != nil {
				topic.Limits = append(topic.Limits, liveLimitFail(liveLimitGitHubOff, "github", err))
				repos = nil
				break
			}
			repos = append(repos, result.Total)
		}
		topic.Repos = repos
	}
	top, err := src.githubSearch(ctx, fmt.Sprintf("%s created:>=%s", q, periods[0].Start), "stars", liveTopRepos)
	if err != nil {
		topic.Limits = append(topic.Limits, liveLimitFail(liveLimitGitHubOff, "github", err))
		return
	}
	for _, item := range top.Items {
		community := "open_source"
		if item.Owner.Type == "Organization" {
			community = "organization"
		}
		if len(item.Created) >= 10 {
			owner, _, _ := strings.Cut(item.FullName, "/")
			topic.External = append(topic.External, StudyEvent{Topic: topic.ID, Kind: studyKindImplementation,
				Community: community, Date: item.Created[:10], Title: item.FullName, Org: owner, URL: item.URL, Stars: item.Stars})
		}
	}
}

func liveHN(ctx context.Context, src *liveSources, topic *LiveTopic, periods []LivePeriod) {
	q := topic.Query.HN
	discussions := make([]int, 0, len(periods))
	for _, period := range periods {
		result, err := src.hnSearch(ctx, q, liveDay(period.Start), liveDay(period.EndExclusive), 0)
		if err != nil {
			topic.Limits = append(topic.Limits, liveLimitFail(liveLimitHackerNewsOff, "hn", err))
			discussions = nil
			break
		}
		discussions = append(discussions, result.Hits)
	}
	topic.Discussions = discussions
	top, err := src.hnSearch(ctx, q, liveDay(periods[0].Start), time.Time{}, liveTopStories)
	if err != nil {
		topic.Limits = append(topic.Limits, liveLimitFail(liveLimitHackerNewsOff, "hn", err))
		return
	}
	for _, row := range top.Rows {
		link := row.URL
		if link == "" {
			link = "https://news.ycombinator.com/item?id=" + row.ID
		}
		if len(row.Created) >= 10 {
			topic.External = append(topic.External, StudyEvent{Topic: topic.ID, Kind: "discussion", Community: "discussion",
				Date: row.Created[:10], Title: row.Title, Org: "Hacker News", URL: link, Stars: row.Points})
		}
	}
}

var errLiveCollectorUnavailable = errors.New("live: collector unavailable")

func (s *TrendLiveService) searchWindow(ctx context.Context, src *liveSources, family liveFamily,
	periods []LivePeriod) (LiveTopic, []StudyPaper, error) {
	if s.collectorURL == "" {
		return LiveTopic{}, nil, errLiveCollectorUnavailable
	}
	src.step(liveStepArxiv, 0)
	runID, _ := ctx.Value(liveLLMRunKey{}).(string)
	result, err := s.collectorSearch(ctx, src.http,
		LiveWindowSearch{Query: family.name, Periods: periods, RunID: runID, Plan: family.plan})
	if err != nil {
		return LiveTopic{}, nil, err
	}
	if family.plan != nil && !liveSameQueryScope(family.plan.Scope, result.Scope) {
		return LiveTopic{}, nil, errLiveCollectorUnavailable
	}
	if err := ctx.Err(); err != nil {
		return LiveTopic{}, nil, fmt.Errorf("live: search canceled: %w", err)
	}
	if src.metadata == nil {
		src.metadata = map[string]LivePaperMetadata{}
	}
	src.scanned = result.Scanned
	maps.Copy(src.metadata, result.Metadata)
	if len(result.Terms) > 0 {
		lead := result.Terms[0]
		family.query = LiveQuery{Arxiv: result.Expression, GitHub: `"` + lead + `"`, HN: lead, Terms: result.Terms}
	}
	// Журналы добираются до сборки темы: после liveBuild счётчики периодов посчитаны и новые работы в них не попали бы.
	papers, scholarLimits := src.withScholar(ctx, result.Papers, result.Metadata, result.Terms, periods)
	topic := liveBuild(ctx, src, family, periods, papers, false)
	topic.Limits = append(topic.Limits, scholarLimits...)
	if err := ctx.Err(); err != nil {
		return LiveTopic{}, nil, fmt.Errorf("live: search canceled: %w", err)
	}
	topic.Limits = append(topic.Limits, result.Limits...)
	if liveApplyTemporalCoverage(&topic) {
		topic.Confidence = new(liveConfidence(topic, liveAbsoluteBar()))
	}
	return topic, papers, nil
}

// Любой отказ — errLiveCollectorUnavailable: у разового поиска есть запасной путь через API arXiv, у признаков нет.
func (s *TrendLiveService) collectorSearch(ctx context.Context, client *http.Client,
	request LiveWindowSearch,
) (LiveWindowResult, error) {
	var result LiveWindowResult
	if s.collectorURL == "" {
		return result, errLiveCollectorUnavailable
	}
	body, err := json.Marshal(request)
	if err != nil {
		return result, errLiveCollectorUnavailable
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(s.collectorURL, "/")+"/v1/search",
		bytes.NewReader(body))
	if err != nil {
		return result, errLiveCollectorUnavailable
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		slog.Warn("radar collector search", "error", err)
		return result, errLiveCollectorUnavailable
	}
	defer func() { _ = resp.Body.Close() }()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, liveMaxBody))
	if err != nil || resp.StatusCode != http.StatusOK {
		slog.Warn("radar collector search", "status", resp.StatusCode, "error", err)
		return result, errLiveCollectorUnavailable
	}
	if err := json.Unmarshal(raw, &result); err != nil {
		return result, errLiveCollectorUnavailable
	}
	return result, nil
}

func (s *TrendLiveService) topicLabel(ctx context.Context, papers []StudyPaper) string {
	if s.name == nil || len(papers) == 0 {
		return ""
	}
	titles := make([]string, 0, 12)
	for _, paper := range papers {
		if len(titles) == 12 {
			break
		}
		titles = append(titles, paper.Title)
	}
	label, err := s.name(ctx, titles)
	if err != nil {
		slog.Warn("radar topic naming unavailable", "error", err)
		return ""
	}
	return label
}
