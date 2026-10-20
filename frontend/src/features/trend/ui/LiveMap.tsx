import { useEffect, useId, useMemo, useRef, useState } from "react";
import { useTranslation } from "react-i18next";
import { Button } from "@/shared/ui/Button";
import { InfoHint } from "@/shared/ui/InfoHint";
import { Tooltip, TooltipContent, TooltipTrigger } from "@/shared/ui/Tooltip";
import { cn } from "@/shared/lib/cn";
import { interleaveBy } from "@/shared/lib/interleaveBy";
import type { LiveOverview, LiveTopic } from "../liveApi";
import { comparisonRanges, growthTones, publicationChange, symlogAxis } from "../liveMapMetrics";
import { previewSide } from "../previewPlacement";
import { LiveMapLegend } from "./LiveMapLegend";
import { LiveTopicPreview } from "./LiveTopicPreview";

const X_TICKS = [0, 3, 10, 30, 100, 300, 1000, 3000, 10000, 30000, 100000];
const LABEL_LEFT_FROM = 78;

// Запас на кириллицу: иначе на плотном полотне соседние имена сходятся и читаются как одно.
const LABEL_CH = 7.4;
const LABEL_MAX = 224;
const LABEL_GAP = 5;

type Box = { x0: number; x1: number; y0: number; y1: number };
type Side = "right" | "left" | "above" | "below";
const SIDE_CLASS: Record<Side, string> = {
  right: "-translate-y-1/2",
  left: "-translate-x-full -translate-y-1/2 text-right",
  above: "-translate-x-1/2 -translate-y-1/2 text-center",
  below: "-translate-x-1/2 -translate-y-1/2 text-center",
};
const anchorX = (side: Side, box: Box) =>
  side === "right" ? box.x0 : side === "left" ? box.x1 : (box.x0 + box.x1) / 2;
const LABEL_ROW = 16;
const LABEL_SHIFTS = [0, LABEL_ROW, -LABEL_ROW, 2 * LABEL_ROW, -2 * LABEL_ROW];

const overlaps = (a: Box, b: Box) => a.x0 < b.x1 && b.x0 < a.x1 && a.y0 < b.y1 && b.y0 < a.y1;
const MIN_D = 10;
const MAX_D = 34;
// Подписи чередуют области: самые крупные темы одной области не вытесняют остальные.
const LABELLED_MAX = 7;
const LABELLED_MIN = 3;
const LABELLED_PX = 140;

export function LiveMap({
  topics,
  allTopics,
  periods,
  observedAt,
  name,
  format,
  onOpen,
}: {
  topics: LiveTopic[];
  allTopics: LiveTopic[];
  periods: LiveOverview["periods"];
  observedAt: string;
  name: (topic: LiveTopic) => string;
  format: (n: number, digits?: number) => string;
  onOpen: (id: string) => void;
}) {
  const { t, i18n } = useTranslation("trend");
  const [hovered, setHovered] = useState<string | null>(null);
  const [focusedId, setFocusedId] = useState<string | null>(null);
  const pointRefs = useRef(new Map<string, HTMLButtonElement>());
  const keyboardHintId = useId();
  const [previewPosition, setPreviewPosition] = useState<"top" | "right" | "bottom" | "left">(
    "right",
  );
  const plot = useRef<HTMLFieldSetElement>(null);
  const [size, setSize] = useState({ width: 1000, height: 544 });
  useEffect(() => {
    const element = plot.current;
    if (!element) return;
    const observer = new ResizeObserver(([entry]) => {
      if (entry) setSize({ width: entry.contentRect.width, height: entry.contentRect.height });
    });
    observer.observe(element);
    return () => observer.disconnect();
  }, []);
  // Без мемо каждое движение мыши заново считало оси, размеры кругов и места всех подписей.
  const layout = useMemo(() => {
    const allPoints = allTopics.flatMap((topic) => {
      const change = publicationChange(topic, periods);
      return change ? [{ topic, ...change }] : [];
    });
    const shown = new Set(topics.map((topic) => topic.id));
    const visible = allPoints.filter((point) => shown.has(point.topic.id));
    if (visible.length === 0) return null;
    // Размер круга — по всему снимку, иначе смена фильтра меняла бы его у того же направления.
    const logs = visible.map((point) => Math.log10(point.current + 1));
    const xPad = Math.max(
      0.12,
      ((Math.max(...logs) - Math.min(...logs)) * 28) / Math.max(1, size.width - 56),
    );
    const xMin = Math.min(...logs) - xPad;
    const xMax = Math.max(...logs) + xPad;
    const axis = symlogAxis(visible.map((point) => point.added));
    const freshMin = Math.min(...allPoints.map((point) => point.fresh));
    const freshMax = Math.max(...allPoints.map((point) => point.fresh));
    const x = (papers: number) => ((Math.log10(papers + 1) - xMin) / (xMax - xMin)) * 100;
    const y = (added: number) => (1 - axis.at(added)) * 100;
    const points = visible.map((point, index) => {
      const d =
        MIN_D +
        (MAX_D - MIN_D) * Math.sqrt((point.fresh - freshMin) / Math.max(1, freshMax - freshMin));
      return {
        topic: point.topic,
        band: point.band,
        fresh: point.fresh,
        index,
        label: name(point.topic),
        d,
        hit: Math.max(28, d + 6),
        left: x(point.current),
        top: y(point.added),
      };
    });
    const labelled = new Map<
      string,
      { side: Side; box: Box; leader: [number, number, number, number] | null }
    >();
    const circle = (point: (typeof points)[number]): Box => {
      const r = point.d / 2;
      const cx = (point.left * size.width) / 100;
      const cy = (point.top * size.height) / 100;
      return { x0: cx - r, x1: cx + r, y0: cy - r, y1: cy + r };
    };
    const circles = points.map(circle);
    // Два направления могут делить русский лейбл: подпись получает только крупнейший круг.
    const printed = new Set<string>();
    const ranked = points
      .toSorted((a, b) => b.fresh - a.fresh)
      .filter((point) => {
        if (printed.has(point.label)) return false;
        printed.add(point.label);
        return true;
      });
    const named = interleaveBy(ranked, (point) => point.topic.branch ?? "").slice(
      0,
      Math.min(LABELLED_MAX, Math.max(LABELLED_MIN, Math.floor(size.width / LABELLED_PX))),
    );
    // Первыми — подписи с меньшим выбором мест: иначе сосед занимал единственное место в углу.
    const reserved = named.map(circle);
    const options = named.map((point, index) => {
      const cx = (point.left * size.width) / 100;
      const cy = (point.top * size.height) / 100;
      const w = Math.min(point.label.length * LABEL_CH, LABEL_MAX);
      const gap = point.d / 2 + LABEL_GAP;
      const candidates = LABEL_SHIFTS.flatMap((shift) => {
        const out = Math.abs(shift);
        const sides: [Side, Box][] = [
          ["right", { x0: cx + gap, x1: cx + gap + w, y0: cy - 7 + shift, y1: cy + 7 + shift }],
          ["left", { x0: cx - gap - w, x1: cx - gap, y0: cy - 7 + shift, y1: cy + 7 + shift }],
          [
            "above",
            { x0: cx - w / 2, x1: cx + w / 2, y0: cy - gap - out - 14, y1: cy - gap - out },
          ],
          [
            "below",
            { x0: cx - w / 2, x1: cx + w / 2, y0: cy + gap + out, y1: cy + gap + out + 14 },
          ],
        ];
        if (point.left > LABEL_LEFT_FROM) sides.reverse();
        return sides.map(([side, box]) => ({ side, box, out }));
      }).filter(
        ({ box }) =>
          box.x0 >= 0 &&
          box.x1 <= size.width &&
          box.y0 >= 0 &&
          box.y1 <= size.height &&
          !reserved.some((other, at) => at !== index && overlaps(box, other)),
      );
      return { point, cx, cy, candidates };
    });
    const placed: Box[] = [];
    const covered = (box: Box) => circles.filter((other) => overlaps(box, other)).length;
    for (const { point, cx, cy, candidates } of options.toSorted(
      (a, b) => a.candidates.length - b.candidates.length,
    )) {
      // Сортировка устойчива: на равном расстоянии решает порядок сторон.
      const fit = candidates
        .filter(({ box }) => !placed.some((other) => overlaps(box, other)))
        .toSorted((a, b) => a.out - b.out || covered(a.box) - covered(b.box))[0];
      if (!fit) continue;
      placed.push(fit.box);
      const { box } = fit;
      const px = Math.min(Math.max(cx, box.x0), box.x1);
      const py = Math.min(Math.max(cy, box.y0), box.y1);
      const length = Math.hypot(px - cx, py - cy) || 1;
      const r = point.d / 2;
      labelled.set(point.topic.id, {
        side: fit.side,
        box,
        leader: fit.out
          ? [cx + ((px - cx) * r) / length, cy + ((py - cy) * r) / length, px, py]
          : null,
      });
    }
    return {
      points,
      labelled,
      ranges: comparisonRanges(allTopics[0], periods, i18n.language),
      xTicks: X_TICKS.map((value) => ({ value, left: x(value) })).filter(
        (tick) => tick.left >= 0 && tick.left <= 100,
      ),
      yTicks: axis.ticks.map((value) => ({ value, top: y(value) })),
    };
  }, [allTopics, topics, periods, size, name, i18n.language]);
  const navigation = useMemo(
    () =>
      layout?.points
        .toSorted(
          (a, b) => a.left - b.left || a.top - b.top || a.topic.id.localeCompare(b.topic.id),
        )
        .map((point) => point.topic.id) ?? [],
    [layout],
  );
  const entryId = focusedId && navigation.includes(focusedId) ? focusedId : navigation[0];
  useEffect(() => {
    // При обновлении карты возвращаем потерянный фокус, но не забираем его у фильтров.
    if (focusedId && entryId !== focusedId && document.activeElement === document.body) {
      pointRefs.current.get(entryId ?? "")?.focus();
    }
  }, [entryId, focusedId]);
  if (!layout) return null;
  const { points, labelled, ranges, xTicks, yTicks } = layout;
  return (
    <figure className="min-w-0" aria-label={t("study.live.map.aria")}>
      <figcaption className="mb-2 ml-12 flex flex-wrap items-center gap-x-1 gap-y-1">
        <p className="text-sm font-bold text-foreground" data-testid="trend-axis-y">
          {t("study.live.map.axisY")}
        </p>
        <InfoHint label={t("study.live.map.help")}>{t("study.live.map.guide")}</InfoHint>
        <p className="min-w-0 grow basis-64 text-sm text-muted-foreground">
          {t("study.live.map.keyboardHint")}
        </p>
        <p id={keyboardHintId} className="sr-only">
          {t("study.live.map.keyboardInstructions")}
        </p>
      </figcaption>
      <div className="relative mr-2 mb-7 ml-12">
        <fieldset
          ref={plot}
          aria-label={t("study.live.map.aria")}
          aria-describedby={keyboardHintId}
          className="relative h-[clamp(12rem,calc(100dvh-35rem),29rem)] min-w-0 overflow-hidden border-b border-l border-input"
        >
          {yTicks.map((tick) => (
            <div
              key={tick.value}
              aria-hidden
              className={cn(
                "pointer-events-none absolute inset-x-0 border-t",
                tick.value === 0 ? "border-input" : "border-border",
              )}
              style={{ top: `${tick.top}%` }}
            />
          ))}
          {xTicks.map((tick) => (
            <div
              key={tick.value}
              aria-hidden
              className="pointer-events-none absolute inset-y-0 border-l border-border"
              style={{ left: `${tick.left}%` }}
            />
          ))}
          <svg aria-hidden className="pointer-events-none absolute inset-0 size-full">
            {[...labelled].map(
              ([id, { leader }]) =>
                leader && (
                  <line
                    key={id}
                    x1={leader[0]}
                    y1={leader[1]}
                    x2={leader[2]}
                    y2={leader[3]}
                    className="stroke-muted-foreground"
                    strokeWidth={1}
                  />
                ),
            )}
          </svg>
          {points.map((point) => {
            const place = labelled.get(point.topic.id);
            if (!place) return null;
            return (
              <span
                key={point.topic.id}
                aria-hidden
                className={cn(
                  "pointer-events-none absolute z-10 max-w-56 truncate bg-background text-xs leading-none font-medium text-foreground",
                  SIDE_CLASS[place.side],
                )}
                style={{
                  left: anchorX(place.side, place.box),
                  top: (place.box.y0 + place.box.y1) / 2,
                }}
              >
                {point.label}
              </span>
            );
          })}
          {points.map((point) => (
            <Tooltip
              key={point.topic.id}
              delayDuration={200}
              open={hovered === point.topic.id}
              onOpenChange={(open) => {
                // Прокрутка от предыдущего фокуса может прийти уже после возврата на точку.
                if (!open && document.activeElement === pointRefs.current.get(point.topic.id))
                  return;
                if (open && plot.current) {
                  // Сторону выбирают экранные координаты, а в раскладке лежат доли ширины и высоты.
                  const rect = plot.current.getBoundingClientRect();
                  const positions = points.map((p) => ({
                    x: rect.left + (p.left * rect.width) / 100,
                    y: rect.top + (p.top * rect.height) / 100,
                    radius: p.hit / 2,
                  }));
                  setPreviewPosition(
                    previewSide(positions[point.index]!, positions, {
                      width: window.innerWidth,
                      height: window.innerHeight,
                    }),
                  );
                }
                setHovered((current) =>
                  open ? point.topic.id : current === point.topic.id ? null : current,
                );
              }}
            >
              <TooltipTrigger asChild>
                <Button
                  ref={(element) => {
                    if (element) pointRefs.current.set(point.topic.id, element);
                    else pointRefs.current.delete(point.topic.id);
                  }}
                  data-live-topic={point.topic.id}
                  variant="ghost"
                  size="content"
                  aria-label={point.label}
                  tabIndex={point.topic.id === entryId ? 0 : -1}
                  onFocus={() => setFocusedId(point.topic.id)}
                  onKeyDown={(event) => {
                    if (event.key === "Tab" || event.key === "Escape") setHovered(null);
                    if (event.altKey || event.ctrlKey || event.metaKey) return;
                    const index = navigation.indexOf(point.topic.id);
                    const destinations: Record<string, number> = {
                      ArrowLeft: (index + navigation.length - 1) % navigation.length,
                      ArrowRight: (index + 1) % navigation.length,
                      Home: 0,
                      End: navigation.length - 1,
                    };
                    const nextIndex = destinations[event.key];
                    if (nextIndex === undefined) return;
                    event.preventDefault();
                    const next = navigation[nextIndex];
                    if (next) pointRefs.current.get(next)?.focus();
                  }}
                  onClick={() => {
                    setFocusedId(point.topic.id);
                    setHovered(null);
                    onOpen(point.topic.id);
                  }}
                  className={cn(
                    "absolute -translate-x-1/2 -translate-y-1/2 justify-center rounded-full p-0 active:-translate-y-1/2",
                    hovered === point.topic.id && "z-20",
                  )}
                  style={{
                    left: `${point.left}%`,
                    top: `${point.top}%`,
                    width: point.hit,
                    height: point.hit,
                  }}
                >
                  <span
                    aria-hidden
                    data-growth={point.band}
                    data-money={(point.topic.money?.total ?? 0) > 0 || undefined}
                    className={cn(
                      "pointer-events-none shrink-0 rounded-full border-2 border-card opacity-80",
                      growthTones[point.band].marker,
                      // Обводка — вторая кодировка поверх цвета роста: инвесторы уже вкладываются в тему.
                      (point.topic.money?.total ?? 0) > 0 && "outline-2 outline-map-money-4",
                    )}
                    style={{ width: point.d, height: point.d }}
                  />
                </Button>
              </TooltipTrigger>
              {hovered === point.topic.id && (
                <TooltipContent
                  variant="card"
                  side={previewPosition}
                  sideOffset={12}
                  collisionPadding={16}
                  onEscapeKeyDown={() => setHovered(null)}
                  onPointerDownOutside={() => setHovered(null)}
                  // На телефоне подсказка выше свободного места и листается — нужна клавиатура.
                  tabIndex={0}
                  className="max-h-(--radix-tooltip-content-available-height) w-80 max-w-[calc(100vw-2rem)] overflow-y-auto"
                >
                  <LiveTopicPreview
                    topic={point.topic}
                    periods={periods}
                    observedAt={observedAt}
                    name={point.label}
                    format={format}
                  />
                </TooltipContent>
              )}
            </Tooltip>
          ))}
        </fieldset>
        {xTicks.map((tick) => (
          <span
            key={tick.value}
            aria-hidden
            className="pointer-events-none absolute top-full mt-1 -translate-x-1/2 font-mono text-xs font-medium text-foreground tabular-nums"
            style={{ left: `${tick.left}%` }}
          >
            {tick.value}
          </span>
        ))}
        {yTicks.map((tick) => (
          <span
            key={tick.value}
            aria-hidden
            className="pointer-events-none absolute -left-12 w-11 -translate-y-1/2 text-right font-mono text-xs font-medium text-foreground tabular-nums"
            style={{ top: `${tick.top}%` }}
          >
            {tick.value > 0 ? "+" : ""}
            {format(tick.value, 0)}
          </span>
        ))}
      </div>
      <p className="text-right text-sm font-bold text-foreground" data-testid="trend-axis-x">
        {t("study.live.map.axisX", { period: ranges.recent })}
      </p>
      <LiveMapLegend topics={topics} periods={periods} />
    </figure>
  );
}
