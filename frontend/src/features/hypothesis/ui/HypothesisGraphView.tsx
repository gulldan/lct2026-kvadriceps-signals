import { useGraphViewport } from "./useGraphViewport";
import { statusTone, toneClasses } from "@/shared/design-system/tones";
import { THEME_EVENT } from "@/shared/theme";
import { Card } from "@/shared/ui/Card";
import {
  useCallback,
  useEffect,
  useMemo,
  useRef,
  useState,
  type CSSProperties,
  type PointerEvent as ReactPointerEvent,
} from "react";
import { useNavigate } from "react-router-dom";
import { useTranslation } from "react-i18next";
import { ExternalLink, Maximize2, Share2, ZoomIn, ZoomOut } from "lucide-react";

import {
  getHypothesis,
  getHypothesisGraph,
  type ApiEvidence,
  type HypothesisGraphData,
} from "@/features/hypothesis/api";
import { type ApiKPI, listKPIs } from "@/features/kpi";
import { DocPreviewSheet, useDocPreview } from "@/features/document";
import { Button } from "@/shared/ui/Button";
import { EmptyState } from "@/shared/ui/EmptyState";
import { ErrorState } from "@/shared/ui/ErrorState";
import { SearchField } from "@/shared/ui/SearchField";
import { FilterBar } from "@/shared/ui/FilterBar";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/shared/ui/Select";
import { Skeleton } from "@/shared/ui/Skeleton";
import { cn } from "@/shared/lib/cn";
import { formatNumber } from "@/shared/lib/format";

import { toViewData, buildNeighbors, type GraphTransform } from "./graphModel";
import { drawGraph } from "./graphCanvas";
const LABELS_TOP = 14;
type Phase = "loading" | "error" | "ready";
const VERDICT_KEYS = ["supported", "refuted", "mixed", "insufficient"] as const;

function verdictTone(verdict: string | undefined) {
  const labelKey: (typeof VERDICT_KEYS)[number] | "unverified" =
    VERDICT_KEYS.find((key) => key === verdict) ?? "unverified";
  return { ...toneClasses[statusTone(verdict)], labelKey };
}

function StatChip({ label, value }: { label: string; value: string }) {
  return (
    <span className="inline-flex items-baseline gap-1.5">
      <span className="font-mono text-sm font-semibold text-foreground tabular-nums">{value}</span>
      <span className="text-xs text-muted-foreground">{label}</span>
    </span>
  );
}

export function HypothesisGraphView() {
  const { t } = useTranslation("hypothesis");
  const navigate = useNavigate();
  const { preview, openCitation, close: closePreview, retry: retryPreview } = useDocPreview();

  const [phase, setPhase] = useState<Phase>("loading");
  const [raw, setRaw] = useState<HypothesisGraphData>({ nodes: [], edges: [] });
  const [kpis, setKpis] = useState<ApiKPI[]>([]);
  const [kpiFilter, setKpiFilter] = useState("all");
  const [stanceFilter, setStanceFilter] = useState("all");
  const [query, setQuery] = useState("");
  const [positions, setPositions] = useState(new Map<string, { x: number; y: number }>());
  const [hoveredId, setHoveredId] = useState<string | null>(null);
  const [hoverPoint, setHoverPoint] = useState<{ x: number; y: number } | null>(null);
  const [selectedId, setSelectedId] = useState<string | null>(null);
  const [evidence, setEvidence] = useState<ApiEvidence[]>([]);

  const dragRef = useRef<{
    x: number;
    y: number;
    nodeId: string | null;
    transform: GraphTransform;
    moved: boolean;
  } | null>(null);
  const requestRef = useRef(0);
  const lastHoverRef = useRef<string | null>(null);

  const load = useCallback(async () => {
    setPhase("loading");
    try {
      const [graph, kpiList] = await Promise.all([getHypothesisGraph(), listKPIs()]);
      setRaw(graph);
      setKpis(kpiList);
      setPhase("ready");
    } catch {
      setPhase("error");
    }
  }, []);

  useEffect(() => {
    void load();
  }, [load]);

  const data = useMemo(
    () => toViewData(raw.nodes, raw.edges, kpiFilter, stanceFilter, query),
    [raw, kpiFilter, stanceFilter, query],
  );

  const nodes = useMemo(
    () =>
      data.nodes.map((node) => {
        const pos = positions.get(node.id);
        return pos ? { ...node, x: pos.x, y: pos.y } : node;
      }),
    [data.nodes, positions],
  );
  const { canvasRef, interacted, viewport, transform, setTransform, fitToGraph, hitTest, zoomBy } =
    useGraphViewport(phase, nodes, positions.size);
  const nodeById = useMemo(() => new Map(nodes.map((node) => [node.id, node])), [nodes]);
  const topHypotheses = useMemo(
    () =>
      nodes
        .filter((node) => node.kind === "hypothesis" && node.degree > 0)
        .toSorted((a, b) => b.degree - a.degree || a.label.localeCompare(b.label))
        .slice(0, 8),
    [nodes],
  );
  const indexHypotheses = useMemo(
    () =>
      nodes
        .filter((node) => node.kind === "hypothesis")
        .toSorted((a, b) => b.degree - a.degree || a.label.localeCompare(b.label)),
    [nodes],
  );
  const labelIds = useMemo(
    () => new Set(topHypotheses.slice(0, LABELS_TOP).map((node) => node.id)),
    [topHypotheses],
  );
  const activeId = hoveredId ?? selectedId;
  const neighbors = useMemo(() => buildNeighbors(activeId, data.links), [activeId, data.links]);
  const focusId = selectedId ?? topHypotheses[0]?.id ?? null;
  const focusNode = focusId ? (nodeById.get(focusId) ?? null) : null;
  const docsByHyp = useMemo(() => {
    const map = new Map<string, number>();
    for (const link of data.links) map.set(link.source, (map.get(link.source) ?? 0) + 1);
    return map;
  }, [data.links]);
  const focusKind = focusNode?.kind;
  useEffect(() => {
    if (!focusId || focusKind !== "hypothesis") {
      setEvidence([]);
      return;
    }
    let alive = true;
    getHypothesis(focusId)
      .then((h) => alive && setEvidence(h.evidence ?? []))
      .catch(() => alive && setEvidence([]));
    return () => {
      alive = false;
    };
  }, [focusId, focusKind]);
  const evidenceByNode = useMemo(() => {
    const map = new Map<string, ApiEvidence[]>();
    for (const ev of evidence) {
      const key = ev.document_id ?? `file:${ev.filename}`;
      const list = map.get(key);
      if (list) list.push(ev);
      else map.set(key, [ev]);
    }
    return map;
  }, [evidence]);

  useEffect(() => {
    if (phase !== "ready" || data.nodes.length === 0) {
      setPositions(new Map());
      return;
    }
    setPositions(new Map());
    const requestId = requestRef.current + 1;
    requestRef.current = requestId;
    const worker = new Worker(new URL("./graphLayout.worker.ts", import.meta.url), {
      type: "module",
    });
    worker.addEventListener(
      "message",
      (
        event: MessageEvent<{ requestId: number; nodes: { id: string; x: number; y: number }[] }>,
      ) => {
        if (event.data.requestId !== requestId) return;
        interacted.current = false;
        setPositions(new Map(event.data.nodes.map((node) => [node.id, { x: node.x, y: node.y }])));
      },
    );
    worker.postMessage(
      {
        requestId,
        nodes: data.nodes.map((node) => ({
          id: node.id,
          kind: node.kind,
          degree: node.degree,
          r: node.r,
        })),
        links: data.links.map((link) => ({
          source: link.source,
          target: link.target,
          weight: link.weight,
        })),
      },
      [],
    );
    return () => worker.terminate();
  }, [data.links, data.nodes, phase, interacted]);

  useEffect(() => {
    if (!selectedId && positions.size > 0 && topHypotheses[0]) {
      setSelectedId(topHypotheses[0].id);
    }
  }, [positions.size, selectedId, topHypotheses]);

  useEffect(() => {
    if (selectedId && !nodeById.has(selectedId)) setSelectedId(null);
  }, [nodeById, selectedId]);

  const [themeTick, setThemeTick] = useState(0);
  useEffect(() => {
    const redraw = () => setThemeTick((tick) => tick + 1);
    window.addEventListener(THEME_EVENT, redraw);
    return () => window.removeEventListener(THEME_EVENT, redraw);
  }, []);

  useEffect(() => {
    if (phase !== "ready") return;
    const canvas = canvasRef.current;
    if (!canvas) return;
    drawGraph({
      canvas,
      nodes,
      links: data.links,
      transform,
      activeId,
      selectedId,
      labelIds,
      neighbors,
    });
  }, [
    canvasRef,
    activeId,
    data.links,
    labelIds,
    neighbors,
    nodes,
    phase,
    selectedId,
    themeTick,
    transform,
    viewport,
  ]);

  const localPoint = useCallback((e: ReactPointerEvent<HTMLCanvasElement>) => {
    const rect = e.currentTarget.getBoundingClientRect();
    return { x: e.clientX - rect.left, y: e.clientY - rect.top };
  }, []);

  const centerNode = useCallback(
    (id: string) => {
      const node = nodeById.get(id);
      if (!node) return;
      interacted.current = true;
      setSelectedId(id);
      setTransform((prev) => {
        const k = Math.max(prev.k, 1.22);
        return { k, x: -node.x * k, y: -node.y * k };
      });
    },
    [nodeById, interacted, setTransform],
  );

  const openNode = useCallback(
    (id: string) => {
      const node = nodeById.get(id);
      if (!node) return;
      if (node.kind === "hypothesis") {
        navigate(`/hypotheses/${node.id}`);
      } else if (!node.id.startsWith("file:")) {
        openCitation({ documentId: node.id, filename: node.label });
      }
    },
    [navigate, nodeById, openCitation],
  );

  const openRelated = useCallback(
    (id: string) => {
      const node = nodeById.get(id);
      if (!node) return;
      if (node.kind === "hypothesis") {
        navigate(`/hypotheses/${node.id}`);
        return;
      }
      const ev = (evidenceByNode.get(node.id) ?? [])[0];
      if (!ev && node.id.startsWith("file:")) return;
      openCitation({
        documentId: ev?.document_id ?? node.id,
        chunkId: ev?.chunk_id,
        filename: node.label,
        snippet: ev?.snippet,
        page: ev?.page_start,
      });
    },
    [evidenceByNode, navigate, nodeById, openCitation],
  );

  const onPointerDown = useCallback(
    (e: ReactPointerEvent<HTMLCanvasElement>) => {
      const p = localPoint(e);
      const node = hitTest(p.x, p.y);
      e.currentTarget.setPointerCapture(e.pointerId);
      dragRef.current = { x: p.x, y: p.y, nodeId: node?.id ?? null, transform, moved: false };
      lastHoverRef.current = node?.id ?? null;
      if (node) {
        setHoveredId(node.id);
        setHoverPoint(p);
      } else {
        interacted.current = true;
        setHoveredId(null);
        setHoverPoint(null);
      }
    },
    [hitTest, localPoint, transform, interacted],
  );

  const onPointerMove = useCallback(
    (e: ReactPointerEvent<HTMLCanvasElement>) => {
      const p = localPoint(e);
      const drag = dragRef.current;
      if (drag && drag.nodeId === null) {
        const dx = p.x - drag.x;
        const dy = p.y - drag.y;
        if (Math.hypot(dx, dy) > 2) drag.moved = true;
        setTransform({ ...drag.transform, x: drag.transform.x + dx, y: drag.transform.y + dy });
        return;
      }
      if (drag && drag.nodeId !== null && Math.hypot(p.x - drag.x, p.y - drag.y) > 5) {
        drag.moved = true;
      }
      const node = hitTest(p.x, p.y);
      const id = node?.id ?? null;
      if (id !== lastHoverRef.current) {
        lastHoverRef.current = id;
        setHoveredId(id);
        setHoverPoint(node ? p : null);
      }
    },
    [hitTest, localPoint, setTransform],
  );

  const onPointerUp = useCallback(
    (e: ReactPointerEvent<HTMLCanvasElement>) => {
      const drag = dragRef.current;
      dragRef.current = null;
      if (!drag?.nodeId || drag.moved) return;
      if (nodeById.get(drag.nodeId)?.kind === "document") {
        openRelated(drag.nodeId);
        return;
      }
      if (selectedId === drag.nodeId) {
        openNode(drag.nodeId);
        return;
      }
      setSelectedId(drag.nodeId);
      lastHoverRef.current = drag.nodeId;
      setHoveredId(drag.nodeId);
      setHoverPoint(localPoint(e));
    },
    [localPoint, nodeById, openNode, openRelated, selectedId],
  );

  const onPointerLeave = useCallback(() => {
    if (dragRef.current) return;
    lastHoverRef.current = null;
    setHoveredId(null);
    setHoverPoint(null);
  }, []);

  if (phase === "loading") return <Skeleton className="mt-6 h-[680px] w-full rounded-xl" />;
  if (phase === "error") {
    return <ErrorState message={t("graph.loadError")} onRetry={() => void load()} />;
  }
  if (!raw.nodes.some((node) => node.kind === "hypothesis")) {
    return (
      <EmptyState icon={Share2} title={t("graph.emptyTitle")} description={t("graph.emptyText")} />
    );
  }

  const hypCount = data.nodes.filter((node) => node.kind === "hypothesis").length;
  const docCount = data.nodes.length - hypCount;
  const tooltipStyle = hoverPoint
    ? ({
        left: Math.min(Math.max(12, hoverPoint.x + 14), Math.max(12, viewport.w - 260)),
        top: Math.min(Math.max(12, hoverPoint.y + 14), Math.max(12, viewport.h - 96)),
      } satisfies CSSProperties)
    : undefined;

  return (
    <div className="mt-4 flex flex-col gap-3">
      <div className="flex flex-wrap items-center gap-x-5 gap-y-1.5 px-0.5">
        <StatChip value={formatNumber(hypCount)} label={t("graph.stats.hypotheses")} />
        <StatChip value={formatNumber(docCount)} label={t("graph.stats.documents")} />
        <StatChip value={formatNumber(data.links.length)} label={t("graph.stats.links")} />
        {data.droppedDocs > 0 && (
          <span className="text-xs text-muted-foreground">
            {t("graph.stats.hiddenDocs", { count: data.droppedDocs })}
          </span>
        )}
      </div>

      <Card asChild>
        <FilterBar className="gap-3 p-3">
          <SearchField
            value={query}
            onChange={setQuery}
            placeholder={t("graph.searchPlaceholder")}
            ariaLabel={t("graph.searchAria")}
            className="w-full min-w-0 sm:min-w-56 sm:flex-1"
          />
          {kpis.length > 0 && (
            <Select value={kpiFilter} onValueChange={setKpiFilter}>
              <SelectTrigger
                className="w-full sm:w-auto sm:min-w-52"
                aria-label={t("graph.goalFilterAria")}
              >
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                <SelectItem value="all">{t("board.allGoals")}</SelectItem>
                {kpis.map((kpi) => (
                  <SelectItem key={kpi.id} value={kpi.id}>
                    {kpi.title}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
          )}
          <Select value={stanceFilter} onValueChange={setStanceFilter}>
            <SelectTrigger
              className="w-full sm:w-auto sm:min-w-40"
              aria-label={t("graph.stanceFilterAria")}
            >
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              <SelectItem value="all">{t("graph.stanceAll")}</SelectItem>
              <SelectItem value="supports">{t("graph.stances.supports")}</SelectItem>
              <SelectItem value="contradicts">{t("graph.stances.contradicts")}</SelectItem>
              <SelectItem value="context">{t("graph.stances.context")}</SelectItem>
            </SelectContent>
          </Select>
          <div className="flex items-center gap-2">
            <Button
              type="button"
              variant="outline"
              size="icon"
              onClick={() => zoomBy(1 / 1.18)}
              title={t("graph.zoomOut")}
              aria-label={t("graph.zoomOut")}
            >
              <ZoomOut className="size-4" aria-hidden />
            </Button>
            <Button
              type="button"
              variant="outline"
              size="icon"
              onClick={() => {
                interacted.current = false;
                fitToGraph(true);
              }}
              title={t("graph.fit")}
              aria-label={t("graph.fit")}
            >
              <Maximize2 className="size-4" aria-hidden />
            </Button>
            <Button
              type="button"
              variant="outline"
              size="icon"
              onClick={() => zoomBy(1.18)}
              title={t("graph.zoomIn")}
              aria-label={t("graph.zoomIn")}
            >
              <ZoomIn className="size-4" aria-hidden />
            </Button>
          </div>
        </FilterBar>
      </Card>

      <div className="grid gap-4 xl:h-[calc(100vh-19rem)] xl:min-h-[560px] xl:grid-cols-[minmax(320px,420px)_minmax(0,1fr)]">
        <Card asChild className="flex max-h-[46vh] flex-col overflow-hidden xl:max-h-none">
          <aside>
            <div className="border-b px-4 py-3">
              <div className="flex items-center justify-between gap-2">
                <h3 className="text-sm font-semibold">{t("graph.quickNav")}</h3>
                <span className="rounded-md bg-secondary px-1.5 py-0.5 font-mono text-xs text-muted-foreground tabular-nums">
                  {formatNumber(indexHypotheses.length)}
                </span>
              </div>
              <p className="mt-1 text-xs leading-snug text-muted-foreground">
                {t("graph.quickNavHint")}
              </p>
            </div>
            {indexHypotheses.length === 0 ? (
              <p className="px-4 py-3 text-xs text-muted-foreground">{t("graph.noRelated")}</p>
            ) : (
              <div className="flex-1 space-y-0.5 overflow-y-auto p-2">
                {indexHypotheses.map((node) => {
                  const tone = verdictTone(node.verdict);
                  const active = selectedId === node.id;
                  const linked = docsByHyp.get(node.id) ?? 0;
                  return (
                    <div
                      key={node.id}
                      className={cn(
                        "group flex items-start rounded-lg pr-1 transition-colors hover:bg-secondary",
                        active && "bg-brand-wash hover:bg-brand-wash",
                      )}
                    >
                      <Button
                        variant="ghost"
                        size="content"
                        type="button"
                        aria-label={node.label}
                        aria-current={active}
                        className="min-w-0 flex-1 items-start gap-2.5 px-2.5 py-2 text-left"
                        onClick={() => centerNode(node.id)}
                      >
                        <span
                          className={cn(
                            "mt-[3px] size-2.5 shrink-0 rounded-full ring-2 ring-card",
                            tone.marker,
                          )}
                          aria-hidden
                        />
                        <span className="min-w-0 flex-1">
                          <span className="line-clamp-3 text-[13px] leading-snug font-medium">
                            {node.label}
                          </span>
                          <span className="mt-0.5 block text-caption leading-tight">
                            <span className={tone.text}>{t(`verdicts.${tone.labelKey}`)}</span>
                            <span className="text-muted-foreground">
                              {" · "}
                              {linked > 0
                                ? t("graph.sourceCount", { count: linked })
                                : t("graph.noSources")}
                            </span>
                          </span>
                        </span>
                      </Button>
                      <Button
                        variant="ghost"
                        size="icon-sm"
                        type="button"
                        title={t("graph.openHypothesis")}
                        aria-label={t("graph.openHypothesis")}
                        className={cn(
                          "mt-1.5 shrink-0 transition-opacity group-hover:opacity-100 focus-visible:opacity-100 xl:opacity-0",
                          active && "opacity-100",
                        )}
                        onClick={() => openNode(node.id)}
                      >
                        <ExternalLink className="size-3.5" aria-hidden />
                      </Button>
                    </div>
                  );
                })}
              </div>
            )}
          </aside>
        </Card>

        <Card className="relative h-[64vh] overflow-hidden xl:h-auto">
          <canvas
            ref={canvasRef}
            className={cn(
              "block size-full touch-none select-none",
              hoveredId ? "cursor-pointer" : "cursor-grab",
            )}
            aria-label={t("graph.aria")}
            onPointerDown={onPointerDown}
            onPointerMove={onPointerMove}
            onPointerUp={onPointerUp}
            onPointerCancel={onPointerUp}
            onPointerLeave={onPointerLeave}
          />
          {positions.size === 0 && (
            <div className="pointer-events-none absolute inset-0 grid place-items-center bg-card">
              <Skeleton className="h-20 w-64 rounded-xl" />
            </div>
          )}
          <Card className="pointer-events-none absolute bottom-2 left-2 flex flex-wrap items-center gap-x-3 gap-y-1 px-2.5 py-1.5 text-caption text-muted-foreground">
            <span className="inline-flex items-center gap-1.5">
              <span className="size-2 rounded-full" style={{ background: "var(--ok)" }} />
              {t("graph.nodeLegend.supported")}
            </span>
            <span className="inline-flex items-center gap-1.5">
              <span className="size-2 rounded-full" style={{ background: "var(--risk)" }} />
              {t("graph.nodeLegend.refuted")}
            </span>
            <span className="inline-flex items-center gap-1.5">
              <span className="size-2 rounded-full" style={{ background: "var(--warn)" }} />
              {t("graph.nodeLegend.mixed")}
            </span>
            <span className="inline-flex items-center gap-1.5">
              <span className="size-2 rounded-full" style={{ background: "var(--brand)" }} />
              {t("graph.nodeLegend.unverified")}
            </span>
            <span className="inline-flex items-center gap-1.5">
              <span
                className="size-2 rounded-full"
                style={{ background: "var(--muted-foreground)" }}
              />
              {t("graph.nodeLegend.document")}
            </span>
          </Card>
          {hoveredId && hoverPoint && (
            <div
              className="pointer-events-none absolute max-w-64 rounded-lg border bg-popover px-3 py-2 text-xs shadow-lg"
              style={tooltipStyle}
            >
              <div className="mb-1 flex items-center gap-2">
                <span
                  className="size-2 rounded-full"
                  style={{
                    background:
                      nodeById.get(hoveredId)?.kind === "document"
                        ? "var(--muted-foreground)"
                        : "var(--brand)",
                  }}
                />
                <span className="font-medium text-foreground">
                  {nodeById.get(hoveredId)?.kind === "document"
                    ? t("graph.document")
                    : t("graph.hypothesis")}
                </span>
              </div>
              <p className="line-clamp-2 text-muted-foreground">{nodeById.get(hoveredId)?.label}</p>
            </div>
          )}
        </Card>
      </div>

      <DocPreviewSheet
        doc={preview ? (preview.full ?? preview.doc) : null}
        fragmentId={preview?.fragmentId}
        loading={preview ? !preview.full && !preview.failed : false}
        error={preview?.failed ?? false}
        onRetry={retryPreview}
        onClose={closePreview}
      />
    </div>
  );
}
