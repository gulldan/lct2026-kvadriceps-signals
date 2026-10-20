import {
  useEffect,
  useId,
  useLayoutEffect,
  useMemo,
  useRef,
  useState,
  type CSSProperties,
} from "react";
import { useTranslation } from "react-i18next";
import { ArrowLeft, Maximize2, Minus, Plus } from "lucide-react";
import { cn } from "@/shared/lib/cn";
import { formatMonth } from "@/shared/lib/format";
import { Button } from "@/shared/ui/Button";
import { InfoHint } from "@/shared/ui/InfoHint";
import { Notice } from "@/shared/ui/Notice";
import { ScrollArea } from "@/shared/ui/ScrollArea";
import { Tooltip, TooltipTrigger } from "@/shared/ui/Tooltip";
import type { MapSummary } from "../mapApi";
import type { MoneySummary } from "../moneyApi";
import {
  fitMapCamera,
  mapBounds,
  mapLabels,
  mapScale,
  projectMapPoint,
  publicationDelta,
  regionValue,
  REGION_POINTS,
  REGIONS,
  VIEW,
  WORLD,
  WORLD_CAMERA,
  type MapCamera,
  type MapRegion,
} from "../worldMap";
import { WorldMapCountry, WorldMapTooltip } from "./WorldMapCountry";
import { FundingSpike, WorldMapFunding } from "./WorldMapFunding";
import { CoinStack } from "./WorldMapInvestors";

function windowLabel(from?: string, to?: string) {
  const end = new Date(to ?? "");
  if (!from || !Number.isFinite(end.getTime())) return "";
  end.setUTCDate(end.getUTCDate() - 1);
  return `${formatMonth(from)} — ${formatMonth(end.toISOString())}`;
}

export function WorldMapCanvas({
  summary,
  board,
  country,
  region,
  leading,
  container,
  countryName,
  regionName,
  onCountry,
  onRegion,
  onReset,
  failed,
  retry,
  investors,
}: {
  summary: MapSummary;
  investors?: { regions: Record<string, number>; countries: Record<string, number> };
  board?: MoneySummary;
  country: string | null;
  region: MapRegion | null;
  leading: Set<string>;
  container: HTMLElement | null;
  countryName: (code: string) => string;
  regionName: (value: MapRegion) => string;
  onCountry: (code: string) => void;
  onRegion: (value: MapRegion) => void;
  onReset: () => void;
  failed: boolean;
  retry: () => void;
}) {
  const { t } = useTranslation("trend");
  const viewport = useRef<HTMLElement>(null);
  const [size, setSize] = useState({ width: 960, height: 480 });
  const [manual, setManual] = useState<MapCamera | null>(null);
  const [dragging, setDragging] = useState(false);
  const drag = useRef<{ x: number; y: number; camera: MapCamera; moved: boolean } | null>(null);
  const patternId = useId();
  const helpId = useId();
  useLayoutEffect(() => {
    const element = viewport.current;
    if (!element) return;
    const initial = element.getBoundingClientRect();
    setSize({ width: initial.width, height: initial.height });
    let frame = 0;
    const observer = new ResizeObserver(([entry]) => {
      if (!entry || entry.contentRect.width <= 0) return;
      const { width, height } = entry.contentRect;
      cancelAnimationFrame(frame);
      frame = requestAnimationFrame(() => setSize({ width, height }));
    });
    observer.observe(element);
    return () => {
      observer.disconnect();
      cancelAnimationFrame(frame);
    };
  }, []);
  useEffect(() => setManual(null), [country, region]);
  const automatic = useMemo(() => {
    if (!country && !region) return WORLD_CAMERA;
    return fitMapCamera(
      mapBounds(
        WORLD.countries.filter((item) => (country ? item.c === country : item.r === region)),
      ),
      size.width,
      size.height,
    );
  }, [country, region, size]);
  const camera = manual ?? automatic;
  const scale = mapScale(size.width, size.height, camera.zoom);
  const origin = projectMapPoint(VIEW.x, VIEW.y, camera, size.width, size.height);
  const points = [
    ...new Map(
      WORLD.countries
        .filter((item) => item.c && summary.countries[item.c])
        .map((item) => [item.c, item]),
    ).values(),
  ]
    .filter((item) => !region || item.r === region)
    .map((item) => {
      const position = projectMapPoint(item.x, item.y, camera, size.width, size.height);
      return {
        id: item.c!,
        title: countryName(item.c!),
        x: position.x,
        y: position.y,
        value: summary.countries[item.c!]!.recent,
        prior: summary.countries[item.c!]!.prior,
        region: item.r,
      };
    });
  const values = REGIONS.map((id) => ({ id, value: regionValue(board, id) }));
  const maximum = Math.max(1, ...values.map((item) => item.value ?? 0));
  const positioned = values
    .filter((item) => !region || item.id === region)
    .map((item) => {
      const position = projectMapPoint(...REGION_POINTS[item.id], camera, size.width, size.height);
      return { id: item.id, value: item.value, x: position.x, y: position.y };
    })
    .filter(
      (item) =>
        item.x >= 48 && item.x <= size.width - 48 && item.y >= 40 && item.y <= size.height - 12,
    );
  // Единый линейный масштаб сохраняет соотношение высот даже на низком экране.
  const heightUnit = Math.min(
    112 / maximum,
    ...positioned
      .filter((item) => (item.value ?? 0) > 0)
      .map((item) => (item.y - 40) / item.value!),
  );
  const markers = board
    ? positioned.map((item) => ({
        id: item.id,
        value: item.value,
        x: item.x,
        y: item.y,
        height: (item.value ?? 0) * heightUnit,
      }))
    : [];
  const labels = mapLabels(
    points.map((point) => ({
      id: point.id,
      title: point.title,
      x: point.x,
      y: point.y,
      value: point.id === country ? Number.MAX_SAFE_INTEGER : point.value,
    })),
    size.width,
    size.height,
    markers.map((item) => ({
      left: item.x - 48,
      right: item.x + 48,
      top: item.y - item.height - 44,
      bottom: item.y + 8,
    })),
  );
  const zoomBy = (amount: number) =>
    setManual({ ...camera, zoom: Math.max(1, Math.min(8, camera.zoom + amount)) });
  const reset = () => {
    setManual(null);
    onReset();
  };
  const focused = Boolean(country || region);
  return (
    <>
      <div className="flex flex-wrap items-center justify-between gap-2">
        {focused && (
          <div className="flex items-center gap-2">
            <Button variant="ghost" size="icon-sm" aria-label={t("world.world")} onClick={reset}>
              <ArrowLeft aria-hidden />
            </Button>
            <h3 className="text-sm font-semibold">
              {country ? countryName(country) : regionName(region!)}
            </h3>
          </div>
        )}
        <div className="ml-auto flex items-center gap-1">
          <Button
            variant="ghost"
            size="icon-sm"
            aria-label={t("map.zoomOut")}
            disabled={camera.zoom <= 1}
            onClick={() => zoomBy(-0.5)}
          >
            <Minus aria-hidden />
          </Button>
          <Button
            variant="ghost"
            size="icon-sm"
            aria-label={t("map.zoomIn")}
            disabled={camera.zoom >= 8}
            onClick={() => zoomBy(0.5)}
          >
            <Plus aria-hidden />
          </Button>
          {(camera.zoom > 1 || manual) && (
            <Button variant="ghost" size="sm" onClick={reset}>
              {t("map.fit")}
            </Button>
          )}
          <Button
            variant="ghost"
            size="icon-sm"
            aria-label={t("map.fullscreen")}
            disabled={!document.fullscreenEnabled}
            onClick={() => {
              if (document.fullscreenElement) void document.exitFullscreen();
              else void container?.requestFullscreen?.().catch(() => {});
            }}
          >
            <Maximize2 aria-hidden />
          </Button>
        </div>
      </div>
      <ScrollArea
        ref={viewport}
        aria-label={t("world.aria")}
        aria-describedby={helpId}
        data-map-viewport
        data-map-zoom={camera.zoom}
        className={cn(
          "relative h-[clamp(16rem,40dvh,24rem)] w-full flex-none touch-none overflow-clip @sm/world:h-[clamp(20rem,52dvh,34rem)] @5xl/world:min-h-[28rem] @5xl/world:flex-1",
          dragging ? "cursor-grabbing" : "cursor-grab",
        )}
        onPointerDown={(event) => {
          drag.current = null;
          if (event.button !== 0 || (event.target as Element).closest("button")) return;
          drag.current = { x: event.clientX, y: event.clientY, camera, moved: false };
        }}
        onPointerMove={(event) => {
          const start = drag.current;
          if (!start || !event.buttons) return;
          const dx = event.clientX - start.x,
            dy = event.clientY - start.y;
          if (!start.moved && Math.hypot(dx, dy) < 5) return;
          start.moved = true;
          event.currentTarget.setPointerCapture(event.pointerId);
          setDragging(true);
          setManual({
            ...start.camera,
            x: start.camera.x - dx / scale,
            y: start.camera.y - dy / scale,
          });
        }}
        onPointerUp={() => setDragging(false)}
        onPointerCancel={() => {
          drag.current = null;
          setDragging(false);
        }}
        onClickCapture={(event) => {
          if (drag.current?.moved) {
            event.preventDefault();
            event.stopPropagation();
            drag.current = null;
          }
        }}
        onKeyDown={(event) => {
          if (event.target !== event.currentTarget) return;
          const delta = {
            ArrowLeft: [-40, 0],
            ArrowRight: [40, 0],
            ArrowUp: [0, -40],
            ArrowDown: [0, 40],
          }[event.key];
          if (delta) {
            event.preventDefault();
            setManual({
              ...camera,
              x: camera.x + delta[0]! / scale,
              y: camera.y + delta[1]! / scale,
            });
          } else if (event.key === "+" || event.key === "=") {
            event.preventDefault();
            zoomBy(0.5);
          } else if (event.key === "-") {
            event.preventDefault();
            zoomBy(-0.5);
          } else if (event.key === "Home") {
            event.preventDefault();
            reset();
          }
        }}
      >
        <span id={helpId} className="sr-only">
          {t("world.keyboard")}
        </span>
        <div
          data-map-canvas
          className={cn(
            "absolute top-0 left-0 origin-top-left motion-reduce:transition-none",
            !dragging && "transition-transform duration-200 ease-out",
          )}
          style={{
            width: VIEW.width,
            height: VIEW.height,
            transform: `translate(${origin.x}px, ${origin.y}px) scale(${scale})`,
          }}
        >
          <svg
            viewBox={`${VIEW.x} ${VIEW.y} ${VIEW.width} ${VIEW.height}`}
            className="block h-full w-full"
            style={
              {
                "--map-active-stroke": 2 / scale,
                "--map-border-stroke": 0.65 / scale,
              } as CSSProperties
            }
            aria-hidden
          >
            <defs>
              <pattern
                id={patternId}
                width="5"
                height="5"
                patternUnits="userSpaceOnUse"
                patternTransform="rotate(45)"
              >
                <rect width="5" height="5" className="fill-muted" />
                <line y2="5" className="stroke-border-strong" strokeWidth="0.5" />
              </pattern>
            </defs>
            {WORLD.countries.map((item) => (
              <WorldMapCountry
                key={item.n}
                item={item}
                title={item.c ? countryName(item.c) : item.n}
                counts={item.c ? summary.countries[item.c] : undefined}
                board={board}
                investors={item.c && investors ? (investors.countries[item.c] ?? 0) : undefined}
                container={container}
                emphasized={
                  (item.c !== null && country === item.c) ||
                  leading.has(item.c ?? "") ||
                  (item.c !== null && region === item.r)
                }
                muted={focused && (country ? country !== item.c : region !== item.r || !item.c)}
                patternId={patternId}
                onSelect={onCountry}
              />
            ))}
          </svg>
        </div>
        <svg aria-hidden className="pointer-events-none absolute inset-0 h-full w-full">
          {points
            .filter((point) => labels.has(point.id))
            .map((point) => (
              <line
                key={point.id}
                x1={point.x}
                y1={point.y}
                x2={labels.get(point.id)!.x}
                y2={labels.get(point.id)!.y}
                className="stroke-border-strong"
                strokeWidth="0.75"
              />
            ))}
        </svg>
        {points
          .filter((point) => labels.has(point.id))
          .map((point) => (
            <div
              key={point.id}
              className={cn(
                "absolute top-0 left-0 motion-reduce:transition-none",
                !dragging && "transition-transform duration-200 ease-out",
              )}
              style={{
                transform: `translate(${labels.get(point.id)!.x}px, ${labels.get(point.id)!.y}px)`,
              }}
            >
              <Tooltip delayDuration={200}>
                <TooltipTrigger asChild>
                  <Button
                    variant="ghost"
                    size="sm"
                    data-map-marker={point.id}
                    aria-label={point.title}
                    aria-pressed={country === point.id}
                    className="absolute max-w-none -translate-x-1/2 -translate-y-1/2 rounded-sm p-0 active:-translate-y-1/2"
                    onClick={() => onCountry(point.id)}
                  >
                    <span
                      aria-hidden
                      className="pointer-events-none max-w-36 rounded-sm bg-background px-1.5 py-0.5 text-xs leading-tight text-foreground"
                    >
                      <span className="block truncate font-medium">{point.title}</span>
                      <span className="block font-mono text-micro tabular-nums">
                        {publicationDelta(point.value, point.prior)}
                      </span>
                    </span>
                  </Button>
                </TooltipTrigger>
                <WorldMapTooltip
                  title={point.title}
                  counts={summary.countries[point.id]}
                  region={point.region}
                  board={board}
                  investors={investors ? (investors.countries[point.id] ?? 0) : undefined}
                  container={container}
                />
              </Tooltip>
            </div>
          ))}
        {board && (
          <WorldMapFunding
            markers={markers}
            board={board}
            container={container}
            region={region}
            regionName={regionName}
            investors={investors?.regions}
            onSelect={onRegion}
          />
        )}
      </ScrollArea>
      <div
        className="grid grid-cols-2 items-end gap-x-4 gap-y-3 border-t pt-3 text-xs text-muted-foreground @sm/world:flex @sm/world:justify-between"
        data-map-legend
      >
        <div className="max-w-full space-y-2">
          <div className="flex items-center gap-1.5">
            <span className="font-medium text-foreground">{t("world.growthTitle")}</span>
            <InfoHint container={container} label={t("world.growthHelp")}>
              {t("world.growthMethod", {
                period: windowLabel(summary.from, summary.mid),
                count: summary.topics,
              })}
            </InfoHint>
          </div>
          <div className="flex flex-wrap items-center gap-3">
            <div className="w-44">
              <div
                className="h-2 rounded-xs"
                style={{ background: "var(--map-growth-gradient)" }}
              />
              <div className="mt-1 flex justify-between font-mono tabular-nums">
                <span>≤ −50 %</span>
                <span>0</span>
                <span>≥ +100 %</span>
              </div>
            </div>
            <span className="flex items-center gap-1">
              <svg width="16" height="12" aria-hidden>
                <rect width="16" height="12" fill={`url(#${patternId})`} />
              </svg>
              {t("world.noComparison")}
            </span>
          </div>
          <p>{windowLabel(summary.mid, summary.to)}</p>
        </div>
        <div className="space-y-2">
          <div className="flex items-center gap-1.5">
            <span className="font-medium text-foreground">{t("world.moneyLayer")}</span>
            <InfoHint container={container} label={t("world.fundingHelp")}>
              {t("world.fundingMethod")}
            </InfoHint>
          </div>
          <div className="flex items-center gap-2">
            <FundingSpike height={16} />
            <span>{t("world.spikeLegend")}</span>
          </div>
          {investors && (
            <div className="flex items-center gap-2">
              <CoinStack coins={3} />
              <span>{t("world.investors.legend")}</span>
            </div>
          )}
          <p>
            {board?.months.length
              ? `${formatMonth(board.months[0]!)} — ${formatMonth(board.months.at(-1)!)}`
              : t("world.periodPending")}
          </p>
        </div>
      </div>
      {!board && (
        <Notice tone={failed ? "warn" : "muted"}>
          {t("world.moneyPending")}{" "}
          <Button variant="link" size="inline" onClick={retry}>
            {t("retry")}
          </Button>
        </Notice>
      )}
    </>
  );
}
