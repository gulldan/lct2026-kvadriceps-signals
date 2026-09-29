import { useState, type ReactNode } from "react";
import { cn } from "@/shared/lib/cn";

export interface BarDatum {
  label: string;
  value: number;
  displayValue: string;
}

export function BarChart({
  data,
  label,
  action,
}: {
  data: BarDatum[];
  label: string;
  action?: ReactNode;
}) {
  const [active, setActive] = useState<number | null>(null);
  const maximum = Math.max(1, ...data.map((item) => item.value));
  const selected = active === null ? null : data[active];
  return (
    <figure aria-label={label} className="min-w-0">
      <figcaption className="mb-3 flex min-h-5 flex-wrap items-center justify-between gap-2 text-sm">
        <span className="font-medium">{label}</span>
        <div className="flex flex-wrap items-center gap-3">
          {action}
          <span className="font-mono text-xs text-muted-foreground" aria-live="polite">
            {selected
              ? `${selected.label} · ${selected.displayValue}`
              : data.find((item) => item.value === maximum)?.displayValue}
          </span>
        </div>
      </figcaption>
      <ul className="flex h-36 items-end gap-1 border-b">
        {data.map((item, index) => (
          <li key={item.label} className="h-full min-w-0 flex-1">
            <button
              type="button"
              aria-label={`${item.label}: ${item.displayValue}`}
              onFocus={() => setActive(index)}
              onBlur={() => setActive(null)}
              onMouseEnter={() => setActive(index)}
              onMouseLeave={() => setActive(null)}
              className="flex h-full w-full items-end justify-center focus-visible:outline-2 focus-visible:outline-ring"
            >
              <span
                aria-hidden
                className={cn(
                  "w-full max-w-10 rounded-t-sm bg-chart-1",
                  active === index && "outline-2 outline-offset-2 outline-brand",
                )}
                style={{ height: `${(Math.max(0, item.value) / maximum) * 100}%` }}
              />
            </button>
          </li>
        ))}
      </ul>
      <div className="mt-2 flex justify-between text-xs text-muted-foreground" aria-hidden>
        <span>{data[0]?.label}</span>
        <span>{data.length > 1 ? data.at(-1)?.label : ""}</span>
      </div>
    </figure>
  );
}
