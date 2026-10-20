import { useState } from "react";
import { cn } from "@/shared/lib/cn";
import { formatNumber } from "@/shared/lib/format";
import { LegendItem } from "@/shared/ui/LegendItem";

export interface DonutDatum {
  key: string;
  label: string;
  value: number;
  color: string;
}

export function DonutChart({
  data,
  label,
  valueLabel,
}: {
  data: DonutDatum[];
  label: string;
  valueLabel: string;
}) {
  const [active, setActive] = useState<string>();
  const items = data.filter((item) => Number.isFinite(item.value) && item.value > 0);
  const total = items.reduce((sum, item) => sum + item.value, 0);
  const selected = items.find((item) => item.key === active);
  const percent = (value: number) => {
    const share = value / total;
    const belowPrecision = share > 0 && share < 0.001;
    return `${belowPrecision ? "<\u00a0" : ""}${formatNumber(belowPrecision ? 0.001 : share, {
      style: "percent",
      maximumFractionDigits: 1,
    })}`;
  };
  let offset = 0;
  return (
    <figure aria-label={label} className="grid min-w-0 items-center gap-6 sm:grid-cols-[14rem_1fr]">
      <div className="relative mx-auto w-56 max-w-full">
        <svg viewBox="0 0 200 200" aria-hidden className="block w-full">
          <circle cx="100" cy="100" r="78" fill="none" stroke="var(--border)" strokeWidth="30" />
          {items.map((item) => {
            const start = offset;
            const share = (item.value / total) * 100;
            offset += share;
            return (
              <circle
                key={item.key}
                cx="100"
                cy="100"
                r="78"
                fill="none"
                stroke={item.color}
                strokeWidth={active === item.key ? 36 : 30}
                pathLength="100"
                strokeDasharray={`${share} ${100 - share}`}
                strokeDashoffset={-start}
                transform="rotate(-90 100 100)"
                onPointerEnter={() => setActive(item.key)}
                onPointerLeave={() => setActive(undefined)}
                onPointerDown={() => setActive(item.key)}
              />
            );
          })}
        </svg>
        <div
          aria-hidden
          className="pointer-events-none absolute inset-0 flex flex-col items-center justify-center px-12 text-center"
        >
          <span className="font-mono text-2xl font-semibold tabular-nums">
            {formatNumber(selected?.value ?? total)}
          </span>
          <span className="mt-1 text-xs text-muted-foreground">
            {selected?.label ?? valueLabel}
          </span>
        </div>
      </div>
      <figcaption>
        <ul className="space-y-1">
          {items.map((item) => (
            <li key={item.key}>
              <button
                type="button"
                aria-label={`${item.label}: ${formatNumber(item.value)}, ${percent(item.value)}`}
                aria-pressed={active === item.key}
                onFocus={() => setActive(item.key)}
                onBlur={() => setActive(undefined)}
                onPointerEnter={() => setActive(item.key)}
                onPointerLeave={() => setActive(undefined)}
                onClick={() => setActive(item.key)}
                className={cn(
                  "flex min-h-11 w-full items-center justify-between gap-3 rounded-md px-2 py-2 text-left text-sm hover:bg-accent focus-visible:outline-2 focus-visible:outline-ring",
                  active === item.key && "bg-accent",
                )}
              >
                <LegendItem color={item.color} size="md" className="min-w-0">
                  {item.label}
                </LegendItem>
                <span className="flex shrink-0 gap-3 font-mono text-xs tabular-nums">
                  <span>{formatNumber(item.value)}</span>
                  <span className="w-14 text-right text-muted-foreground">
                    {percent(item.value)}
                  </span>
                </span>
              </button>
            </li>
          ))}
        </ul>
      </figcaption>
    </figure>
  );
}
