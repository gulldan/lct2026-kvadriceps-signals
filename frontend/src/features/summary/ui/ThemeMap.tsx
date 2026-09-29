import { useEffect, useId, useRef, useState } from "react";
import { Link } from "react-router-dom";
import { useTranslation } from "react-i18next";
import { RotateCcw } from "lucide-react";
import {
  nearbyThemes,
  THEME_REGIONS,
  themeRegion,
  type ThemePoint,
  type ThemeRegion,
  type ThemeSelection,
} from "../themeMap";
import { formatNumber } from "@/shared/lib/format";
import { cn } from "@/shared/lib/cn";
import { chartTones } from "@/shared/design-system/tones";
import { Button } from "@/shared/ui/Button";
import { Tooltip, TooltipContent, TooltipTrigger } from "@/shared/ui/Tooltip";

const REGION_TONES: Record<ThemeRegion, { circle: string; field: string; dot: string }> = {
  earlierDistinct: chartTones[2],
  recentDistinct: chartTones[1],
  earlierSimilar: chartTones[4],
  recentSimilar: chartTones[3],
};
const TICKS = [0, 25, 50, 75, 100];

export function ThemeMap({
  points,
  maxDocuments,
  selection,
  onSelect,
}: {
  points: ThemePoint[];
  maxDocuments: number;
  selection: ThemeSelection | null;
  onSelect: (selection: ThemeSelection | null) => void;
}) {
  const { t } = useTranslation("summary");
  const mapRef = useRef<HTMLFieldSetElement>(null);
  const pointRefs = useRef(new Map<string, HTMLButtonElement>());
  const keyboardHintId = useId();
  const [focusedId, setFocusedId] = useState<string | null>(null);
  const [hovered, setHovered] = useState<string | null>(null);
  const entryId = points.find((point) => point.id === focusedId)?.id ?? points[0]?.id;
  const selected = points.find((point) => selection?.kind === "point" && point.id === selection.id);
  const active = points.find((point) => point.id === (hovered ?? selected?.id));
  const regions = THEME_REGIONS.map((id) => ({
    kind: "region" as const,
    id,
    points: points.filter((point) => themeRegion(point) === id),
  }));
  const selectedIds = new Set(selection?.points.map((point) => point.id));

  useEffect(() => {
    // Если обновление удалило точку под фокусом, сохраняем вход в карту, не забирая фокус у поиска.
    if (focusedId && entryId !== focusedId && document.activeElement === document.body) {
      pointRefs.current.get(entryId ?? "")?.focus();
    }
  }, [entryId, focusedId]);

  const regionLabels = regions.map((region) => (
    <Button
      key={region.id}
      data-map-region={region.id}
      variant={selection?.kind === "region" && selection.id === region.id ? "secondary" : "ghost"}
      size="content"
      disabled={region.points.length === 0}
      aria-pressed={selection?.kind === "region" && selection.id === region.id}
      aria-controls="theme-list"
      aria-label={t("map.openRegion", {
        label: t(`map.regions.${region.id}.label`),
        count: region.points.length,
      })}
      onClick={() => {
        setHovered(null);
        onSelect(region);
      }}
      className="min-h-control-lg min-w-0 gap-2 px-1 py-2 text-left sm:px-3"
    >
      <span
        aria-hidden
        className={cn("size-2 shrink-0 rounded-full", REGION_TONES[region.id].dot)}
      />
      <span className="flex-1">{t(`map.regions.${region.id}.label`)}</span>
      <span className="font-mono tabular-nums">{formatNumber(region.points.length)}</span>
    </Button>
  ));

  return (
    <figure className="min-w-0" aria-label={t("map.aria")}>
      <div className="mb-3 flex flex-wrap items-center gap-x-4 gap-y-2">
        <Button
          variant="link"
          aria-controls="theme-list"
          onClick={() => {
            setHovered(null);
            document.getElementById("theme-list")?.focus();
          }}
        >
          {t("map.skipToList")}
        </Button>
        <p id={keyboardHintId} className="min-w-64 flex-1 text-sm text-muted-foreground">
          {t("map.keyboardHint")}
        </p>
      </div>
      <div className="mr-4 mb-3 ml-10 grid grid-cols-2 gap-2">{regionLabels.slice(0, 2)}</div>
      <div className="relative mr-4 ml-10">
        <fieldset
          ref={mapRef}
          tabIndex={-1}
          aria-label={t("map.aria")}
          aria-describedby={keyboardHintId}
          className="relative h-80 min-w-0 focus-visible:outline-2 focus-visible:outline-offset-4 focus-visible:outline-ring sm:h-96"
        >
          {regions.map((region, index) => (
            <div
              key={region.id}
              aria-hidden
              className={cn(
                "pointer-events-none absolute h-1/2 w-1/2",
                REGION_TONES[region.id].field,
              )}
              style={{ left: `${(index % 2) * 50}%`, top: `${Math.floor(index / 2) * 50}%` }}
            />
          ))}
          {TICKS.map((value) => (
            <div key={value} aria-hidden className="pointer-events-none">
              <div
                className={cn(
                  "absolute inset-x-0 border-t border-border",
                  value === 50 && "border-dashed border-border",
                )}
                style={{ top: `${value}%` }}
              />
              <div
                className={cn(
                  "absolute inset-y-0 border-l border-border",
                  value === 50 && "border-dashed border-border",
                )}
                style={{ left: `${value}%` }}
              />
            </div>
          ))}
          {points.map((point, index) => {
            const diameter = 2 * (4 + 9 * Math.sqrt(Math.max(0, point.documents) / maxDocuments));
            return (
              <Tooltip
                key={point.id}
                delayDuration={150}
                open={hovered === point.id}
                onOpenChange={(open) => {
                  setHovered((current) =>
                    open ? point.id : current === point.id ? null : current,
                  );
                }}
              >
                <TooltipTrigger asChild>
                  <Button
                    ref={(element) => {
                      if (element) pointRefs.current.set(point.id, element);
                      else pointRefs.current.delete(point.id);
                    }}
                    data-theme-point={point.id}
                    data-theme-region={themeRegion(point)}
                    variant="ghost"
                    size="content"
                    aria-label={t("map.openTheme", { label: point.label })}
                    aria-pressed={selection?.kind === "point" && selection.id === point.id}
                    aria-controls="theme-list"
                    tabIndex={point.id === entryId ? 0 : -1}
                    onFocus={() => setFocusedId(point.id)}
                    onKeyDown={(event) => {
                      const destinations: Record<string, number> = {
                        ArrowLeft: (index + points.length - 1) % points.length,
                        ArrowRight: (index + 1) % points.length,
                        Home: 0,
                        End: points.length - 1,
                      };
                      const nextIndex = destinations[event.key];
                      if (nextIndex === undefined) return;
                      event.preventDefault();
                      const next = points[nextIndex];
                      if (next) pointRefs.current.get(next.id)?.focus();
                    }}
                    onClick={(event) => {
                      // Tooltip закрывается по щелчку, а после касания подсказка нужна открытой.
                      event.preventDefault();
                      setFocusedId(point.id);
                      setHovered(point.id);
                      const bounds = mapRef.current?.getBoundingClientRect();
                      if (bounds) {
                        onSelect({
                          kind: "point",
                          id: point.id,
                          points: nearbyThemes(points, point, bounds.width, bounds.height),
                        });
                      }
                    }}
                    className={cn(
                      "absolute -translate-x-1/2 -translate-y-1/2 justify-center rounded-full p-0 active:-translate-y-1/2",
                      active?.id === point.id && "z-10",
                    )}
                    style={{
                      left: `${point.freshness * 100}%`,
                      top: `${(1 - point.novelty) * 100}%`,
                      width: Math.max(24, diameter),
                      height: Math.max(24, diameter),
                    }}
                  >
                    <span
                      aria-hidden
                      className={cn(
                        "pointer-events-none shrink-0 rounded-full border transition-opacity",
                        REGION_TONES[themeRegion(point)].circle,
                        selection &&
                          !selectedIds.has(point.id) &&
                          active?.id !== point.id &&
                          "opacity-25",
                        active?.id === point.id && "outline-2 outline-offset-2 outline-foreground",
                      )}
                      style={{ width: diameter, height: diameter }}
                    />
                  </Button>
                </TooltipTrigger>
                {hovered === point.id && (
                  <TooltipContent
                    variant="card"
                    sideOffset={10}
                    collisionPadding={16}
                    className="max-w-[min(20rem,calc(100vw-2rem))] text-left leading-relaxed wrap-anywhere"
                  >
                    <p className="font-medium">{point.label}</p>
                    <p className="mt-1 tabular-nums">
                      {t("map.selected", {
                        documents: formatNumber(point.documents),
                        freshness: formatNumber(point.freshness * 100, {
                          maximumFractionDigits: 1,
                        }),
                        novelty: formatNumber(point.novelty * 100, { maximumFractionDigits: 1 }),
                      })}
                    </p>
                  </TooltipContent>
                )}
              </Tooltip>
            );
          })}
        </fieldset>
        {TICKS.map((value) => (
          <span
            key={value}
            className="pointer-events-none absolute -left-7 w-6 -translate-y-1/2 text-right text-xs text-muted-foreground tabular-nums"
            style={{ top: `${100 - value}%` }}
            aria-hidden
          >
            {formatNumber(value)}
          </span>
        ))}
        {/* Подпись повёрнута вдоль шкалы, поэтому «→» из словаря смотрит вверх. */}
        <span
          aria-hidden
          className="pointer-events-none absolute top-1/2 -left-8.5 -translate-x-1/2 -translate-y-1/2 -rotate-90 text-xs leading-none whitespace-nowrap text-muted-foreground"
        >
          {t("map.axisY")}
        </span>
      </div>
      <div
        className="relative mt-4 mr-4 ml-10 h-4 text-xs text-muted-foreground tabular-nums"
        aria-hidden
      >
        {TICKS.map((value) => (
          <span key={value} className="absolute -translate-x-1/2" style={{ left: `${value}%` }}>
            {formatNumber(value)}
          </span>
        ))}
      </div>
      <div className="mt-3 mr-4 ml-10 grid grid-cols-2 gap-2">{regionLabels.slice(2)}</div>
      <div className="mt-2 flex flex-wrap justify-between gap-x-4 gap-y-1 text-xs text-muted-foreground">
        <p>{t("map.sizeLegend")}</p>
        <p className="ml-auto">{t("map.axisX")}</p>
      </div>
      <figcaption className="mt-4 flex min-h-12 items-start gap-2 text-sm text-muted-foreground">
        <span className="min-w-0 flex-1" aria-live="polite">
          {selected ? (
            <Button asChild variant="link" size="inline" className="text-left wrap-anywhere">
              <Link to={`/clusters/${selected.id}`}>{selected.label}</Link>
            </Button>
          ) : selection?.kind === "region" ? (
            t(`map.regions.${selection.id}.description`)
          ) : (
            t("map.description")
          )}
        </span>
        {selection && (
          <Button
            variant="ghost"
            size="sm"
            aria-label={t("map.reset")}
            title={t("map.reset")}
            className="-mt-1.5"
            onClick={() => {
              setHovered(null);
              onSelect(null);
              mapRef.current?.focus();
            }}
          >
            <RotateCcw className="size-4" aria-hidden />
            <span className="hidden sm:inline">{t("map.reset")}</span>
          </Button>
        )}
      </figcaption>
    </figure>
  );
}
