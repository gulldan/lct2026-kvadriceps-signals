/* oxlint-disable jsx-a11y/prefer-tag-over-role -- Interactive SVG nodes need roles; HTML buttons are not valid SVG children. */
import { useEffect, useMemo, useRef, useState } from "react";
import { useTranslation } from "react-i18next";
import { Focus, Maximize2, Minus, Plus } from "lucide-react";
import { Button } from "@/shared/ui/Button";
import { topicLabel, visibleWorks, type MapNode, type MapTopic } from "../mapData";

const yearColors = [
  "var(--category-1)",
  "var(--category-2)",
  "var(--category-3)",
  "var(--category-4)",
  "var(--category-5)",
];
export const publicationColor = (year: number | null, years: number[]) =>
  year === null
    ? "var(--category-unassigned)"
    : yearColors[Math.max(0, years.indexOf(year)) % yearColors.length]!;

function bounds(nodes: MapNode[], aspect: number) {
  const left = (nodes.length ? Math.min(...nodes.map((n) => n.x - n.radius)) : -30) - 60;
  const top = (nodes.length ? Math.min(...nodes.map((n) => n.y - n.radius)) : -30) - 50;
  const right = (nodes.length ? Math.max(...nodes.map((n) => n.x + n.radius)) : 30) + 60;
  const bottom = (nodes.length ? Math.max(...nodes.map((n) => n.y + n.radius)) : 30) + 50;
  const w = Math.max(right - left, (bottom - top) * aspect);
  const h = w / aspect;
  return { x: (left + right - w) / 2, y: (top + bottom - h) / 2, w, h };
}

export function TechnologyNetwork({
  topics,
  layout,
  years,
  year,
  selectedTopic,
  selectedWork,
  onSelect,
}: {
  topics: MapTopic[];
  layout: MapNode[];
  years: number[];
  year: number | null;
  selectedTopic: string;
  selectedWork: string;
  onSelect: (topic: string, work?: string) => void;
}) {
  const { t } = useTranslation("trend");
  const svg = useRef<SVGSVGElement>(null);
  const container = useRef<HTMLElement>(null);
  const drag = useRef<{ x: number; y: number; originX: number; originY: number } | null>(null);
  const moved = useRef(false);
  const [size, setSize] = useState({ w: 800, h: 480 });
  const [hovered, setHovered] = useState("");
  const base = useMemo(() => bounds(layout, size.w / size.h), [layout, size]);
  const [view, setView] = useState(base);
  useEffect(() => setView(base), [base]);
  useEffect(() => {
    const element = svg.current;
    if (!element) return;
    const observer = new ResizeObserver(() => {
      const { width, height } = element.getBoundingClientRect();
      if (width && height) setSize({ w: width, h: height });
    });
    observer.observe(element);
    return () => observer.disconnect();
  }, []);
  const topicByID = new Map(topics.map((topic) => [topic.candidate.id, topic]));
  const workByID = new Map(
    topics.flatMap((topic) => topic.works.map((work) => [work.id, work] as const)),
  );
  const activeWorks = new Set(
    topics.flatMap((topic) => visibleWorks(topic, year).map((work) => work.id)),
  );
  const nodes = layout.filter((node) => {
    const topic = topicByID.get(node.topicID);
    if (!topic) return false;
    return node.workID ? activeWorks.has(node.workID) : visibleWorks(topic, year).length > 0;
  });
  const nodeByID = new Map(nodes.map((node) => [node.id, node]));
  const relatedWorks = new Set(topicByID.get(selectedTopic)?.works.map((work) => work.id));
  const emphasized = (node: MapNode) =>
    node.workID ? relatedWorks.has(node.workID) : node.topicID === selectedTopic;
  const label = (node: MapNode) =>
    node.workID ? workByID.get(node.workID)!.title : topicByID.get(node.topicID)!.candidate.title;
  const unit = view.w / size.w;
  const nodeRadius = (node: MapNode) =>
    node.workID
      ? node.radius
      : 8 + Math.sqrt(visibleWorks(topicByID.get(node.topicID)!, year).length) * 1.5;
  // Сначала геометрия, потом порядок отрисовки: чужая зона касания не отнимает видимую точку.
  const pick = (clientX: number, clientY: number) => {
    const matrix = svg.current?.getScreenCTM();
    if (!matrix) return;
    const point = new DOMPoint(clientX, clientY).matrixTransform(matrix.inverse());
    let closest: MapNode | undefined;
    let closestDistance = Infinity;
    let insideGlyph = false;
    for (const node of nodes) {
      const distance = Math.hypot(point.x - node.x, point.y - node.y);
      const radius = nodeRadius(node);
      if (distance > Math.max(radius, 12 * unit)) continue;
      const inside = distance <= radius;
      if (
        (inside && !insideGlyph) ||
        (inside === insideGlyph &&
          (distance < closestDistance || (distance === closestDistance && !node.workID)))
      ) {
        closest = node;
        closestDistance = distance;
        insideGlyph = inside;
      }
    }
    return closest;
  };
  const reveal = (node: MapNode) =>
    setView((current) => {
      const margin = Math.min(nodeRadius(node) + 20 * unit, Math.min(current.w, current.h) / 3);
      return {
        ...current,
        x:
          node.x < current.x + margin
            ? node.x - margin
            : node.x > current.x + current.w - margin
              ? node.x + margin - current.w
              : current.x,
        y:
          node.y < current.y + margin
            ? node.y - margin
            : node.y > current.y + current.h - margin
              ? node.y + margin - current.h
              : current.y,
      };
    });
  const selectNode = (node: MapNode) =>
    onSelect(
      node.workID && relatedWorks.has(node.workID) ? selectedTopic : node.topicID,
      node.workID,
    );
  const zoom = (factor: number) =>
    setView((current) => {
      const w = Math.max(base.w / 6, Math.min(base.w * 1.5, current.w * factor));
      const h = w / (size.w / size.h);
      return { x: current.x + (current.w - w) / 2, y: current.y + (current.h - h) / 2, w, h };
    });
  const labeled = new Set(
    topics
      .toSorted((a, b) => b.works.length - a.works.length)
      .slice(0, size.w < 500 ? 4 : 9)
      .map((topic) => topic.candidate.id),
  );
  const focusID =
    selectedWork && nodeByID.has("work:" + selectedWork)
      ? "work:" + selectedWork
      : nodeByID.has("topic:" + selectedTopic)
        ? "topic:" + selectedTopic
        : nodes[0]?.id;
  const labels = new Map<
    string,
    { x: number; y: number; lines: string[]; width: number; height: number }
  >();
  const priority = (node: MapNode) =>
    node.id === hovered
      ? 3
      : node.id === focusID
        ? 2
        : node.topicID === selectedTopic && !node.workID
          ? 1
          : 0;
  for (const node of nodes.toSorted((a, b) => priority(b) - priority(a))) {
    if (node.x < view.x || node.x > view.x + view.w || node.y < view.y || node.y > view.y + view.h)
      continue;
    if (node.id !== hovered && node.id !== focusID && (node.workID || !labeled.has(node.topicID)))
      continue;
    const lines =
      topicLabel(label(node))
        .match(/.{1,24}(?:\s|$)|.{1,24}/g)
        ?.slice(0, 2)
        .map((line) => line.trim()) ?? [];
    const width =
      Math.max(
        ...lines.map((line) =>
          Array.from(line).reduce((sum, char) => sum + (char.codePointAt(0)! > 0x24f ? 12 : 7), 0),
        ),
        1,
      ) * unit;
    const height = lines.length * 15 * unit;
    const x = Math.max(
      view.x + width / 2 + 8 * unit,
      Math.min(view.x + view.w - width / 2 - 8 * unit, node.x),
    );
    for (const offset of [node.y + node.radius + 16 * unit, node.y - node.radius - height]) {
      const y = Math.max(view.y + 16 * unit, Math.min(view.y + view.h - height - 8 * unit, offset));
      const overlaps = [...labels.values()].some(
        (other) =>
          Math.abs(other.x - x) < (other.width + width) / 2 + 8 * unit &&
          y < other.y + other.height + 4 * unit &&
          y + height + 4 * unit > other.y,
      );
      if (overlaps) continue;
      labels.set(node.id, { x, y, lines, width, height });
      break;
    }
  }
  return (
    <figure
      ref={container}
      className="min-w-0 bg-card [&:fullscreen]:flex [&:fullscreen]:flex-col [&:fullscreen]:p-4 [&:fullscreen>svg]:min-h-0 [&:fullscreen>svg]:flex-1"
      aria-label={t("map.network")}
    >
      <div className="flex flex-wrap items-center justify-between gap-2 border-b px-3 py-2">
        <p className="text-sm font-medium">{t("map.network")}</p>
        <div className="flex items-center gap-1">
          <Button
            variant="ghost"
            size="icon-sm"
            aria-label={t("map.zoomOut")}
            title={t("map.zoomOut")}
            onClick={() => zoom(1.3)}
          >
            <Minus aria-hidden />
          </Button>
          <Button
            variant="ghost"
            size="icon-sm"
            aria-label={t("map.zoomIn")}
            title={t("map.zoomIn")}
            onClick={() => zoom(1 / 1.3)}
          >
            <Plus aria-hidden />
          </Button>
          <Button
            variant="ghost"
            size="icon-sm"
            aria-label={t("map.focus")}
            title={t("map.focus")}
            onClick={() => setView(bounds(nodes.filter(emphasized), size.w / size.h))}
          >
            <Focus aria-hidden />
          </Button>
          <Button variant="ghost" size="sm" onClick={() => setView(base)}>
            {t("map.fit")}
          </Button>
          <Button
            variant="ghost"
            size="icon-sm"
            aria-label={t("map.fullscreen")}
            title={t("map.fullscreen")}
            disabled={!document.fullscreenEnabled}
            onClick={() => {
              if (document.fullscreenElement) void document.exitFullscreen();
              else void container.current?.requestFullscreen?.().catch(() => {});
            }}
          >
            <Maximize2 aria-hidden />
          </Button>
        </div>
      </div>
      {/* oxlint-disable jsx-a11y/click-events-have-key-events, jsx-a11y/no-noninteractive-element-interactions -- The canvas delegates geometric picking; each SVG node owns its keyboard and assistive-click handlers. */}
      <svg
        ref={svg}
        role="group"
        aria-label={t("map.graphKeyboard")}
        viewBox={`${view.x} ${view.y} ${view.w} ${view.h}`}
        className={`block h-[390px] w-full touch-none select-none md:h-[490px] ${hovered ? "cursor-pointer" : "cursor-grab"}`}
        onPointerDown={(event) => {
          moved.current = false;
          drag.current = { x: event.clientX, y: event.clientY, originX: view.x, originY: view.y };
          event.currentTarget.setPointerCapture(event.pointerId);
        }}
        onPointerMove={(event) => {
          if (!drag.current) {
            setHovered(pick(event.clientX, event.clientY)?.id ?? "");
            return;
          }
          const start = drag.current;
          if (Math.hypot(event.clientX - start.x, event.clientY - start.y) > 3)
            moved.current = true;
          if (!moved.current) return;
          setView((current) => ({
            ...current,
            x: start.originX - (event.clientX - start.x) * unit,
            y: start.originY - (event.clientY - start.y) * unit,
          }));
        }}
        onClick={(event) => {
          if (!moved.current) {
            const node = pick(event.clientX, event.clientY);
            if (node) {
              const element = [
                ...event.currentTarget.querySelectorAll<SVGGElement>("[data-node-id]"),
              ].find((item) => item.dataset.nodeId === node.id);
              element?.focus({ preventScroll: true });
              selectNode(node);
            }
          }
        }}
        onPointerUp={() => {
          drag.current = null;
        }}
        onPointerCancel={() => {
          drag.current = null;
        }}
        onPointerLeave={() => setHovered("")}
      >
        <g aria-hidden="true" fill="none">
          {topics.flatMap((topic) => {
            const parent = nodeByID.get("topic:" + topic.candidate.id);
            if (!parent) return [];
            return visibleWorks(topic, year).map((work) => {
              const child = nodeByID.get("work:" + work.id);
              return child ? (
                <line
                  key={topic.candidate.id + work.id}
                  x1={parent.x}
                  y1={parent.y}
                  x2={child.x}
                  y2={child.y}
                  stroke={
                    topic.candidate.id === selectedTopic ? "var(--brand-border)" : "var(--border)"
                  }
                  strokeWidth={unit * (topic.candidate.id === selectedTopic ? 1.3 : 0.65)}
                />
              ) : null;
            });
          })}
        </g>
        {nodes.map((node, index) => {
          const topic = topicByID.get(node.topicID)!;
          const work = node.workID ? workByID.get(node.workID)! : null;
          const contents = visibleWorks(topic, year);
          const dates = contents.flatMap((item) => (item.year === null ? [] : [item.year]));
          const color = publicationColor(
            work ? work.year : dates.length ? Math.max(...dates) : null,
            years,
          );
          const selected = work ? selectedWork === work.id : selectedTopic === node.topicID;
          const radius = nodeRadius(node);
          return (
            <g
              key={node.id}
              role="button"
              tabIndex={node.id === focusID ? 0 : -1}
              aria-label={
                work
                  ? t("map.workNode", { title: work.title })
                  : t("map.topicNode", { title: topic.candidate.title, count: contents.length })
              }
              aria-pressed={selected}
              id={work ? undefined : `topic-${node.topicID}`}
              data-node-id={node.id}
              data-node-kind={work ? "work" : "topic"}
              className="pointer-events-none outline-none focus-visible:[&>circle]:stroke-foreground"
              onFocus={() => {
                setHovered(node.id);
                reveal(node);
              }}
              onBlur={() => setHovered("")}
              onClick={(event) => {
                event.stopPropagation();
                selectNode(node);
              }}
              onKeyDown={(event) => {
                if (event.key === "Enter" || event.key === " ") {
                  event.preventDefault();
                  selectNode(node);
                }
                const next =
                  event.key === "ArrowRight" || event.key === "ArrowDown"
                    ? (index + 1) % nodes.length
                    : event.key === "ArrowLeft" || event.key === "ArrowUp"
                      ? (index + nodes.length - 1) % nodes.length
                      : null;
                if (next !== null) {
                  event.preventDefault();
                  svg.current?.querySelectorAll<SVGGElement>("[data-node-id]")[next]?.focus();
                }
              }}
            >
              <title>{label(node)}</title>
              <circle
                cx={node.x}
                cy={node.y}
                r={Math.max(radius + 3, 12 * unit)}
                fill="transparent"
                stroke={selected ? "var(--foreground)" : "transparent"}
                strokeWidth={unit * 1.5}
              />
              {work && work.kind !== "paper" ? (
                <rect
                  x={node.x - radius}
                  y={node.y - radius}
                  width={radius * 2}
                  height={radius * 2}
                  fill={color}
                />
              ) : (
                <circle cx={node.x} cy={node.y} r={radius} fill={color} />
              )}
              {!work && radius / unit > 9 && (
                <text
                  x={node.x}
                  y={node.y}
                  textAnchor="middle"
                  dominantBaseline="central"
                  fill="var(--card)"
                  fontSize={11 * unit}
                  className="font-mono"
                  aria-hidden="true"
                >
                  {contents.length}
                </text>
              )}
            </g>
          );
        })}
        <g aria-hidden="true" pointerEvents="none">
          {[...labels].map(([id, item]) => (
            <text
              key={id}
              data-node-label={id}
              x={item.x}
              y={item.y}
              textAnchor="middle"
              fill="var(--foreground)"
              stroke="var(--card)"
              strokeWidth={3 * unit}
              paintOrder="stroke"
              fontSize={12 * unit}
              fontWeight={id === focusID ? 600 : 400}
            >
              {item.lines.map((line, index) => (
                <tspan key={line} x={item.x} dy={index ? 15 * unit : 0}>
                  {line}
                </tspan>
              ))}
            </text>
          ))}
        </g>
      </svg>
      {/* oxlint-enable jsx-a11y/click-events-have-key-events, jsx-a11y/no-noninteractive-element-interactions */}
      <figcaption
        className="min-h-10 border-t px-3 py-2 text-xs leading-relaxed text-muted-foreground"
        aria-live="polite"
      >
        {hovered && nodeByID.has(hovered) ? label(nodeByID.get(hovered)!) : t("map.networkHint")}
      </figcaption>
    </figure>
  );
}
