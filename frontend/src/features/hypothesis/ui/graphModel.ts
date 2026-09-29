import type { GraphEdge, GraphNode } from "@/features/hypothesis/api";
const MAX_DOC_NODES = 160;
const MAX_SEARCH_DOC_NODES = 240;
type ViewStance = GraphEdge["class"] | "mixed";

export interface ViewNode {
  id: string;
  kind: GraphNode["kind"];
  label: string;
  verdict?: string;
  status?: string;
  kpiId?: string;
  degree: number;
  r: number;
  x: number;
  y: number;
}

export interface ViewLink {
  id: string;
  source: string;
  target: string;
  class: ViewStance;
  weight: number;
  chunks: number;
}

export interface GraphData {
  nodes: ViewNode[];
  links: ViewLink[];
  droppedDocs: number;
  totalDocs: number;
}

export interface GraphTransform {
  x: number;
  y: number;
  k: number;
}

function normalizeQuery(q: string): string {
  return q.trim().toLocaleLowerCase();
}

function chooseStance(counts: Record<GraphEdge["class"], number>): ViewStance {
  if (counts.supports > 0 && counts.contradicts > 0) return "mixed";
  if (counts.contradicts > 0) return "contradicts";
  if (counts.supports > 0) return "supports";
  return "context";
}

export function toViewData(
  nodes: GraphNode[],
  edges: GraphEdge[],
  kpiFilter: string,
  stanceFilter: string,
  query: string,
): GraphData {
  const byId = new Map(nodes.map((node) => [node.id, node]));
  const scopedHypIds = new Set(
    nodes
      .filter(
        (node) => node.kind === "hypothesis" && (kpiFilter === "all" || node.kpi_id === kpiFilter),
      )
      .map((node) => node.id),
  );
  const scopedEdges = edges.filter(
    (edge) =>
      scopedHypIds.has(edge.source) &&
      byId.has(edge.target) &&
      (stanceFilter === "all" || edge.class === stanceFilter),
  );
  const linkedDocs = new Set(scopedEdges.map((edge) => edge.target));
  const q = normalizeQuery(query);

  let hypIds = new Set(scopedHypIds);
  let docIds = new Set(linkedDocs);
  if (q) {
    const matchingHypIds = new Set(
      nodes
        .filter(
          (node) =>
            node.kind === "hypothesis" &&
            scopedHypIds.has(node.id) &&
            node.label.toLocaleLowerCase().includes(q),
        )
        .map((node) => node.id),
    );
    const matchingDocIds = new Set(
      nodes
        .filter(
          (node) =>
            node.kind === "document" &&
            linkedDocs.has(node.id) &&
            node.label.toLocaleLowerCase().includes(q),
        )
        .map((node) => node.id),
    );
    const nextHypIds = new Set<string>(matchingHypIds);
    const nextDocIds = new Set<string>(matchingDocIds);
    for (const edge of scopedEdges) {
      if (matchingHypIds.has(edge.source) || matchingDocIds.has(edge.target)) {
        nextHypIds.add(edge.source);
        nextDocIds.add(edge.target);
      }
    }
    hypIds = nextHypIds;
    docIds = nextDocIds;
  }

  const hypDegree = new Map<string, number>();
  const docDegree = new Map<string, number>();
  for (const edge of scopedEdges) {
    if (!hypIds.has(edge.source) || !docIds.has(edge.target)) continue;
    hypDegree.set(edge.source, (hypDegree.get(edge.source) ?? 0) + 1);
    docDegree.set(edge.target, (docDegree.get(edge.target) ?? 0) + 1);
  }

  const docs = nodes.filter((node) => node.kind === "document" && docIds.has(node.id));
  const docLimit = q ? MAX_SEARCH_DOC_NODES : MAX_DOC_NODES;
  const keptDocs =
    docs.length > docLimit
      ? new Set(
          docs
            .toSorted(
              (a, b) =>
                (docDegree.get(b.id) ?? b.degree ?? 0) - (docDegree.get(a.id) ?? a.degree ?? 0) ||
                a.label.localeCompare(b.label),
            )
            .slice(0, docLimit)
            .map((node) => node.id),
        )
      : null;

  const viewNodes = nodes
    .filter((node) =>
      node.kind === "hypothesis"
        ? hypIds.has(node.id)
        : docIds.has(node.id) && (keptDocs?.has(node.id) ?? true),
    )
    .map<ViewNode>((node) => {
      const degree =
        node.kind === "hypothesis" ? (hypDegree.get(node.id) ?? 0) : (docDegree.get(node.id) ?? 0);
      return {
        id: node.id,
        kind: node.kind,
        label: node.label,
        verdict: node.verdict,
        status: node.status,
        kpiId: node.kpi_id,
        degree,
        r:
          node.kind === "hypothesis"
            ? 8 + Math.min(8, Math.sqrt(degree) * 1.2)
            : 3.5 + Math.min(6, Math.sqrt(degree)),
        x: 0,
        y: 0,
      };
    });
  const viewIds = new Set(viewNodes.map((node) => node.id));
  const grouped = new Map<
    string,
    {
      source: string;
      target: string;
      chunks: Set<string>;
      counts: Record<GraphEdge["class"], number>;
    }
  >();
  for (const edge of scopedEdges) {
    if (!viewIds.has(edge.source) || !viewIds.has(edge.target)) continue;
    const key = `${edge.source}|${edge.target}`;
    const item = grouped.get(key) ?? {
      source: edge.source,
      target: edge.target,
      chunks: new Set<string>(),
      counts: { supports: 0, contradicts: 0, context: 0 },
    };
    item.counts[edge.class] += 1;
    item.chunks.add(edge.chunk_id ?? `${key}:${item.chunks.size}`);
    grouped.set(key, item);
  }
  const links = [...grouped.values()]
    .map<ViewLink>((item) => ({
      id: `${item.source}|${item.target}`,
      source: item.source,
      target: item.target,
      class: chooseStance(item.counts),
      weight: item.counts.supports + item.counts.contradicts + item.counts.context,
      chunks: item.chunks.size,
    }))
    .toSorted(
      (a, b) =>
        b.weight - a.weight || a.source.localeCompare(b.source) || a.target.localeCompare(b.target),
    );

  return {
    nodes: viewNodes,
    links,
    droppedDocs: keptDocs ? docs.length - keptDocs.size : 0,
    totalDocs: docs.length,
  };
}

export function buildNeighbors(id: string | null, links: ViewLink[]): Set<string> | null {
  if (id === null) return null;
  const set = new Set([id]);
  for (const link of links) {
    if (link.source === id) set.add(link.target);
    if (link.target === id) set.add(link.source);
  }
  return set;
}
