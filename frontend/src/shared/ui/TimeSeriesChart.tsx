import { memo, useLayoutEffect, useRef, useState, type ReactNode } from "react";
import { dateFormatter, formatMonth } from "@/shared/lib/format";
import { cn } from "@/shared/lib/cn";
import { LegendItem } from "@/shared/ui/LegendItem";
import { ScrollArea } from "@/shared/ui/ScrollArea";

interface TimePoint {
  period: string;
  value: number | null;
  display: string;
}

export interface TimeSeries {
  id: string;
  label: string;
  points: TimePoint[];
}

const MONTH = /^\d{4}-\d{2}$/;
const COLORS = [
  "var(--color-chart-1)",
  "var(--color-chart-2)",
  "var(--color-chart-3)",
  "var(--color-chart-4)",
  "var(--color-chart-5)",
];
const MARKERS = ["bg-chart-1", "bg-chart-2", "bg-chart-3", "bg-chart-4", "bg-chart-5"];

/** Год только у первого деления и у января: четырнадцать «2026-01» не помещались в колонку. */
function tickLabel(period: string, first: boolean) {
  if (!MONTH.test(period)) return period;
  return dateFormatter({
    month: "short",
    year: first || period.endsWith("-01") ? "2-digit" : undefined,
    timeZone: "UTC",
  })
    .format(new Date(`${period}-01T00:00:00Z`))
    .replace(/ г\.$/, "")
    .replaceAll(".", "");
}

/** null — разрыв линии, а не ноль. */
export const TimeSeriesChart = memo(function TimeSeriesChart({
  series,
  label,
  maximum,
  log = false,
  compact = false,
  selected,
  onSelect,
  formatValue = String,
  note,
  periodLabel = formatMonth,
  captionBelow = false,
  plotClassName,
  className,
}: {
  series: TimeSeries[];
  label: string;
  maximum?: number;
  log?: boolean;
  compact?: boolean;
  selected?: string;
  onSelect?: (period: string) => void;
  formatValue?: (value: number) => string;
  note?: ReactNode;
  periodLabel?: (period: string) => string;
  captionBelow?: boolean;
  plotClassName?: string;
  className?: string;
}) {
  const [hover, setHover] = useState<string>();
  const [visibleTicks, setVisibleTicks] = useState<Set<string>>();
  const scroller = useRef<HTMLElement>(null);
  const points = series[0]?.points ?? [];
  const latestPeriod = points.at(-1)?.period;
  useLayoutEffect(() => {
    const element = scroller.current;
    if (!element || compact) return;
    const showLatest = () => {
      element.scrollLeft = element.scrollWidth - element.clientWidth;
    };
    showLatest();
    const resize = new ResizeObserver(showLatest);
    resize.observe(element);
    const ticks = new IntersectionObserver(
      (entries) => {
        setVisibleTicks((previous) => {
          const next = new Set(previous);
          for (const entry of entries) {
            const period = entry.target.getAttribute("aria-label");
            if (!period) continue;
            if (entry.intersectionRatio >= 0.99) next.add(period);
            else next.delete(period);
          }
          return next;
        });
      },
      { root: element, threshold: [0, 0.99] },
    );
    for (const button of element.querySelectorAll("button")) ticks.observe(button);
    return () => {
      resize.disconnect();
      ticks.disconnect();
    };
  }, [compact, latestPeriod, points.length]);
  const max = maximum ?? Math.max(1, ...series.flatMap((s) => s.points.map((p) => p.value ?? 0)));
  // log10(1+v): ноль остаётся нулём.
  const scaled = (value: number) => (log ? Math.log10(1 + Math.max(0, value)) : Math.max(0, value));
  const top = scaled(max);
  const axisTicks = log
    ? [0, 1, 10, 100, 1000, 10_000, 100_000].filter((value) => value <= max)
    : [0, max / 2, max];
  const width = 600;
  const height = 180;
  const x = (i: number) => 8 + (i / Math.max(1, points.length - 1)) * (width - 16);
  const y = (value: number) => height - 8 - (scaled(value) / Math.max(top, 1e-9)) * (height - 16);
  const active = hover ?? selected ?? points.at(-1)?.period;
  const index = points.findIndex((p) => p.period === active);
  // Иначе на узком экране подписи наезжали бы друг на друга и перехватывали нажатия соседей.
  const slot = points.every((point) => MONTH.test(point.period)) ? 40 : 72;
  const pointedPeriod = (clientX: number, element: SVGSVGElement) => {
    const rect = element.getBoundingClientRect();
    const position = ((clientX - rect.left) / rect.width) * width;
    const nearest = Math.max(
      0,
      Math.min(
        points.length - 1,
        Math.round(((position - 8) / (width - 16)) * (points.length - 1)),
      ),
    );
    return points[nearest]?.period;
  };
  const graph = (
    <svg
      viewBox={`0 0 ${width} ${height}`}
      preserveAspectRatio="none"
      role={compact ? "img" : undefined}
      aria-label={compact ? label : undefined}
      aria-hidden={!compact}
      className={cn("size-full overflow-visible", compact ? "h-10" : "cursor-crosshair")}
      onPointerMove={
        compact
          ? undefined
          : (event) => {
              setHover(pointedPeriod(event.clientX, event.currentTarget));
            }
      }
      onPointerLeave={() => setHover(undefined)}
      onPointerUp={
        compact || !onSelect
          ? undefined
          : (event) => {
              const period = pointedPeriod(event.clientX, event.currentTarget);
              if (period) onSelect(period);
            }
      }
    >
      {/* На логарифмической шкале линии на долях максимума не совпали бы с подписями. */}
      {!compact &&
        axisTicks.map((value) => (
          <line
            key={value}
            x1="0"
            x2={width}
            y1={y(value)}
            y2={y(value)}
            stroke="var(--color-border)"
            vectorEffect="non-scaling-stroke"
          />
        ))}
      {!compact && index >= 0 && (
        <line
          x1={x(index)}
          x2={x(index)}
          y1="0"
          y2={height}
          stroke="var(--color-border)"
          vectorEffect="non-scaling-stroke"
        />
      )}
      {series.map((s, n) => {
        let previous = false;
        const path = s.points
          .map((point, i) => {
            if (point.value === null) {
              previous = false;
              return "";
            }
            const command = previous ? "L" : "M";
            previous = true;
            return `${command}${x(i)},${y(point.value)}`;
          })
          .join(" ");
        return (
          <g key={s.id}>
            <path
              d={path}
              fill="none"
              stroke={COLORS[n % COLORS.length]}
              strokeWidth={compact ? 1.5 : 2}
              strokeLinejoin="round"
              strokeLinecap="round"
              vectorEffect="non-scaling-stroke"
            />
            {compact &&
              s.points.map(
                (point, i) =>
                  point.value !== null && (
                    <circle
                      key={point.period}
                      cx={x(i)}
                      cy={y(point.value)}
                      r={2.5}
                      fill={COLORS[n % COLORS.length]}
                    />
                  ),
              )}
          </g>
        );
      })}
    </svg>
  );
  if (compact) return <span className="block min-w-0">{graph}</span>;
  const values = series.map((s) => s.points[index]?.display ?? "—");
  const caption =
    note === undefined ? (
      <figcaption
        className={cn(
          "flex min-h-6 flex-wrap items-baseline justify-between gap-2 text-sm",
          captionBelow ? "mt-3" : "mb-5",
        )}
      >
        <span className="font-medium">{label}</span>
        <span className="font-mono text-xs text-muted-foreground" aria-live="polite">
          {formatMonth(active ?? "")} ·{" "}
          {series.map((s, n) => `${s.label}: ${values[n]}`).join(" · ")}
        </span>
      </figcaption>
    ) : (
      <figcaption className={cn("space-y-1.5", captionBelow ? "mt-3" : "mb-4")}>
        <div className="flex flex-wrap items-baseline justify-between gap-x-4 gap-y-0.5">
          <span className="text-sm font-medium tabular-nums" aria-live="polite">
            {periodLabel(active ?? "")}
          </span>
          <span className="text-xs text-muted-foreground">{note}</span>
        </div>
        {/* Цвет несёт только маркер: светлые оттенки рядов текстом не читаются. */}
        <ul className="flex flex-wrap gap-x-4 gap-y-1 text-sm text-secondary-foreground">
          {series.map((s, n) => (
            <li key={s.id}>
              <LegendItem marker={MARKERS[n % MARKERS.length]}>
                {s.label}{" "}
                <span className="font-medium text-foreground tabular-nums">{values[n]}</span>
              </LegendItem>
            </li>
          ))}
        </ul>
      </figcaption>
    );
  return (
    <figure aria-label={label} className={cn("flex min-w-0 flex-col", className)}>
      {!captionBelow && caption}
      <ScrollArea ref={scroller} aria-label={label} className="flex flex-1 flex-col">
        <div
          className="flex min-w-60 flex-1 flex-col pr-5"
          style={{ minWidth: 80 + (points.length - 1) * slot }}
        >
          <div className={cn("flex min-h-48 flex-1 gap-3 sm:min-h-56", plotClassName)}>
            {/* На логарифмической оси равные шаги подписи врали бы о приросте. */}
            <div
              className="relative sticky left-0 z-10 w-12 shrink-0 bg-card text-right text-xs text-muted-foreground tabular-nums"
              aria-hidden
            >
              {axisTicks.map((value) => (
                <span
                  key={value}
                  className="absolute right-0 -translate-y-1/2"
                  style={{ top: `${(y(value) / height) * 100}%` }}
                >
                  {formatValue(value)}
                </span>
              ))}
            </div>
            {/* Точки поверх полотна, иначе их сплющивает растянутый рисунок. */}
            <div className="relative min-w-0 flex-1">
              {graph}
              {series.map((s, n) => {
                const last = s.points.findLastIndex((point) => point.value !== null);
                const lone = (i: number) =>
                  (s.points[i - 1]?.value ?? null) === null &&
                  (s.points[i + 1]?.value ?? null) === null;
                return s.points.map((point, i) =>
                  point.value !== null && (i === index || i === last || lone(i)) ? (
                    <span
                      key={`${s.id}-${point.period}`}
                      aria-hidden
                      className={cn(
                        "pointer-events-none absolute -translate-1/2 rounded-full ring-2 ring-card",
                        i === index ? "size-3" : "size-2.5",
                        MARKERS[n % MARKERS.length],
                      )}
                      style={{
                        left: `${(x(i) / width) * 100}%`,
                        top: `${(y(point.value) / height) * 100}%`,
                      }}
                    />
                  ) : null,
                );
              })}
            </div>
          </div>
          <div className="relative mt-1 ml-15 h-control-sm">
            {points.map((point, i) => (
              <button
                key={point.period}
                type="button"
                aria-label={point.period}
                aria-pressed={selected === point.period}
                onClick={() => onSelect?.(point.period)}
                className={cn(
                  "absolute top-0 min-h-control-sm -translate-x-1/2 rounded-sm px-1 py-1 text-xs whitespace-nowrap tabular-nums transition-colors focus-visible:outline-2 focus-visible:outline-ring",
                  selected === point.period
                    ? "bg-brand-wash text-accent-foreground"
                    : "text-muted-foreground hover:text-foreground",
                )}
                style={{ left: `${(x(i) / width) * 100}%` }}
              >
                <span
                  className={
                    visibleTicks && !visibleTicks.has(point.period) ? "invisible" : undefined
                  }
                >
                  {tickLabel(point.period, i === 0)}
                </span>
              </button>
            ))}
          </div>
        </div>
      </ScrollArea>
      {captionBelow && caption}
    </figure>
  );
});
