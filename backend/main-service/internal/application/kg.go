package application

import (
	"context"
	"sort"
	"strings"

	commonv1 "github.com/example/main-service/internal/platform/genproto/common/v1"
)

const (
	nodeMaterial  = "material"
	nodeProcess   = "process"
	nodeProperty  = "property"
	nodeKPI       = "kpi"
	nodeMechanism = "mechanism"
)

const (
	relProcessChanges  = "process_changes"  // состав → процесс
	relAffectsProperty = "affects_property" // процесс → свойство или микроструктура
	relSupportsKPI     = "supports_kpi"     // свойство → KPI
	relLeadsTo         = "leads_to"         // этап причинной цепочки → следующий этап
)

const kgMaxBridges = 12

// KGEdge несёт оба узла в себе, чтобы хранилище оставалось плоским списком рёбер только на добавление.
type KGEdge struct {
	FromType     string `json:"from_type"`
	FromName     string `json:"from_name"`
	Relation     string `json:"relation"`
	ToType       string `json:"to_type"`
	ToName       string `json:"to_name"`
	HypothesisID string `json:"hypothesis_id"`
	KPIID        string `json:"kpi_id"`
}

// GraphStore может быть nil: без Valkey фабрика работает, просто без графа.
type GraphStore interface {
	AddEdges(ctx context.Context, ownerID string, edges []KGEdge) error
	Edges(ctx context.Context, ownerID string) ([]KGEdge, error)
}

func deriveEdges(item genItem, kpiID, hypothesisID string) []KGEdge {
	material := compactText(firstNonEmpty(item.MaterialSystem, item.CompositionChange))
	process := compactText(item.ProcessChange)
	property := compactText(firstNonEmpty(item.TargetProperty, item.MicrostructureMechanism))
	mechanism := compactText(item.MicrostructureMechanism)

	edges := make([]KGEdge, 0, 8)
	add := func(ft, fn, rel, tt, tn string) {
		fn, tn = compactText(fn), compactText(tn)
		if fn == "" || tn == "" {
			return
		}
		edges = append(edges, KGEdge{
			FromType: ft, FromName: fn, Relation: rel, ToType: tt, ToName: tn,
			HypothesisID: hypothesisID, KPIID: kpiID,
		})
	}

	add(nodeMaterial, material, relProcessChanges, nodeProcess, process)
	// Механизм, отличный от свойства, встаёт между процессом и свойством как его микроструктурная причина.
	if mechanism != "" && mechanism != property {
		add(nodeProcess, process, relAffectsProperty, nodeMechanism, mechanism)
		add(nodeMechanism, mechanism, relAffectsProperty, nodeProperty, item.TargetProperty)
	} else {
		add(nodeProcess, process, relAffectsProperty, nodeProperty, property)
	}
	add(nodeProperty, property, relSupportsKPI, nodeKPI, kpiTargetName(kpiID))

	stages := make([]causalStep, 0, len(item.CausalChain))
	for _, st := range item.CausalChain {
		if compactText(st.Stage) != "" {
			stages = append(stages, st)
		}
	}
	for i := 0; i+1 < len(stages); i++ {
		add(causalStageType(stages[i].Stage), stages[i].Stage,
			relLeadsTo, causalStageType(stages[i+1].Stage), stages[i+1].Stage)
	}
	return edges
}

// kpiTargetName: названия KPI в genItem нет, поэтому узел держится на id; название подставит bridgeKPIName.
func kpiTargetName(kpiID string) string {
	if kpiID == "" {
		return ""
	}
	return "kpi:" + kpiID
}

func causalStageType(stage string) string {
	s := strings.ToLower(compactText(stage))
	switch {
	case strings.Contains(s, "состав"):
		return nodeMaterial
	case strings.Contains(s, "процесс") || strings.Contains(s, "режим"):
		return nodeProcess
	case strings.Contains(s, "микроструктур") || strings.Contains(s, "механизм"):
		return nodeMechanism
	case strings.Contains(s, "kpi") || strings.Contains(s, "показател"):
		return nodeKPI
	case strings.Contains(s, "свойств"):
		return nodeProperty
	default:
		return nodeProperty
	}
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if compactText(v) != "" {
			return v
		}
	}
	return ""
}

// BridgeCandidate — направление из графа: связь «процесс → свойство» одной гипотезы плюс «свойство → KPI»
// ДРУГОЙ. Черновик для интерфейса.
type BridgeCandidate struct {
	Statement      string   `json:"statement"`
	Process        string   `json:"process"`
	Property       string   `json:"property"`
	KPI            string   `json:"kpi"`
	FromHypothesis string   `json:"from_hypothesis"`
	ViaHypothesis  string   `json:"via_hypothesis"`
	Sources        []string `json:"sources"`
}

type kpiLink struct {
	kpiName string
	hypID   string
	kpiID   string
}

func findBridges(edges []KGEdge, kpiID, kpiLabel string) []BridgeCandidate {
	supportsByProp := indexKPILinks(edges)
	out := make([]BridgeCandidate, 0, kgMaxBridges)
	seen := make(map[string]struct{})
	for _, pe := range edges {
		if pe.Relation != relAffectsProperty || pe.ToType != nodeProperty {
			continue
		}
		process, property := compactText(pe.FromName), compactText(pe.ToName)
		if process == "" || property == "" {
			continue
		}
		links := supportsByProp[strings.ToLower(property)]
		out = appendBridges(out, seen, pe.HypothesisID, process, property, links, kpiID, kpiLabel)
		if len(out) >= kgMaxBridges {
			return out[:kgMaxBridges]
		}
	}
	return out
}

func indexKPILinks(edges []KGEdge) map[string][]kpiLink {
	byProp := make(map[string][]kpiLink)
	for _, e := range edges {
		if e.Relation != relSupportsKPI || compactText(e.FromName) == "" || compactText(e.ToName) == "" {
			continue
		}
		key := strings.ToLower(compactText(e.FromName))
		byProp[key] = append(byProp[key], kpiLink{kpiName: e.ToName, hypID: e.HypothesisID, kpiID: e.KPIID})
	}
	return byProp
}

func appendBridges(
	out []BridgeCandidate, seen map[string]struct{},
	fromHyp, process, property string, links []kpiLink, kpiID, kpiLabel string,
) []BridgeCandidate {
	for _, link := range links {
		if link.kpiID != kpiID || link.hypID == fromHyp {
			continue
		}
		kpiName := bridgeKPIName(link, kpiID, kpiLabel)
		dedup := strings.ToLower(process + "|" + property + "|" + kpiName)
		if _, dup := seen[dedup]; dup {
			continue
		}
		seen[dedup] = struct{}{}
		out = append(out, BridgeCandidate{
			Statement:      bridgeStatement(process, property, kpiName, fromHyp, link.hypID),
			Process:        process,
			Property:       property,
			KPI:            kpiName,
			FromHypothesis: fromHyp,
			ViaHypothesis:  link.hypID,
			Sources:        bridgeSources(fromHyp, link.hypID),
		})
		if len(out) >= kgMaxBridges {
			return out
		}
	}
	return out
}

func bridgeKPIName(link kpiLink, kpiID, kpiLabel string) string {
	if kpiLabel != "" && link.kpiID == kpiID {
		return kpiLabel
	}
	return strings.TrimPrefix(link.kpiName, "kpi:")
}

func bridgeStatement(process, property, kpi, fromHyp, viaHyp string) string {
	var b strings.Builder
	b.WriteString("Применить процесс «")
	b.WriteString(process)
	b.WriteString("»")
	if fromHyp != "" {
		b.WriteString(" (из работы ")
		b.WriteString(shortID(fromHyp))
		b.WriteByte(')')
	}
	b.WriteString(" для улучшения свойства «")
	b.WriteString(property)
	b.WriteString("», влияющего на KPI «")
	b.WriteString(kpi)
	b.WriteString("»")
	if viaHyp != "" {
		b.WriteString(" (из работы ")
		b.WriteString(shortID(viaHyp))
		b.WriteByte(')')
	}
	b.WriteByte('.')
	return b.String()
}

func bridgeSources(ids ...string) []string {
	set := make(map[string]struct{}, len(ids))
	for _, id := range ids {
		if id != "" {
			set[id] = struct{}{}
		}
	}
	out := make([]string, 0, len(set))
	for id := range set {
		out = append(out, id)
	}
	sort.Strings(out)
	return out
}

func shortID(id string) string {
	if i := strings.IndexByte(id, '-'); i > 0 {
		return id[:i]
	}
	if len(id) > 8 {
		return id[:8]
	}
	return id
}

// GraphHypotheses проверяет доступ к цели и без графа; недоступный граф даёт пустой список, а не ошибку.
func (s *HypothesisService) GraphHypotheses(ctx context.Context, ownerID, kpiID string) ([]BridgeCandidate, error) {
	empty := []BridgeCandidate{}
	kpi, err := s.GetKPI(ctx, ownerID, kpiID)
	if err != nil {
		return nil, err
	}
	if s.graph == nil {
		return empty, nil
	}
	// Ошибка чтения графа намеренно сводится к «мостов нет»: клик не должен падать из-за Valkey.
	edges, _ := s.graph.Edges(ctx, ownerID)
	if len(edges) == 0 {
		return empty, nil
	}
	if bridges := findBridges(edges, kpiID, kpiNodeLabel(kpi)); len(bridges) > 0 {
		return bridges, nil
	}
	return empty, nil
}

func kpiNodeLabel(kpi *commonv1.KPI) string {
	title := compactText(kpi.GetTitle())
	if m := compactText(kpi.GetMetric()); m != "" && title != "" {
		return title + " (" + m + ")"
	}
	return title
}
