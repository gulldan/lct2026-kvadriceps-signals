import { useId, type ReactNode } from "react";

import { cn } from "@/shared/lib/cn";
import { useFilterBarSize } from "@/shared/ui/FilterBar";

type SegmentedOption<T extends string> = {
  value: T;
  label: ReactNode;
  disabled?: boolean;
};

export function Segmented<T extends string>({
  options,
  value,
  onChange,
  size: requested = "md",
  className,
  id,
  "aria-label": ariaLabel,
}: {
  options: readonly SegmentedOption<T>[];
  value: T;
  onChange: (value: T) => void;
  size?: "sm" | "md";
  className?: string;
  id?: string;
  "aria-label"?: string;
}) {
  const name = useId();
  const bar = useFilterBarSize();
  const size = bar ? (bar === "sm" ? "sm" : "md") : requested;
  return (
    <fieldset
      data-slot="segmented"
      data-size={size}
      id={id}
      aria-label={ariaLabel}
      className={cn(
        "inline-flex w-fit max-w-full min-w-0 rounded-lg border p-0.5",
        size === "md" ? "min-h-control" : "min-h-control-sm",
        className,
      )}
    >
      {options.map((o) => {
        const active = o.value === value;
        return (
          <label key={o.value} className="relative inline-flex min-w-0 cursor-pointer">
            <input
              type="radio"
              name={name}
              value={o.value}
              disabled={o.disabled}
              checked={active}
              onChange={() => onChange(o.value)}
              className="peer sr-only"
            />
            <span
              className={cn(
                "inline-flex min-w-0 items-center justify-center gap-1.5 rounded-md py-1 font-medium wrap-anywhere transition-colors peer-focus-visible:ring-2 peer-focus-visible:ring-ring peer-focus-visible:ring-offset-2 peer-focus-visible:ring-offset-background peer-disabled:cursor-not-allowed peer-disabled:opacity-50",
                size === "md"
                  ? "min-h-[calc(var(--control-md)-0.25rem-2px)] px-3.5 text-sm"
                  : "min-h-[calc(var(--control-sm)-0.25rem-2px)] px-2.5 text-xs",
                active
                  ? "bg-primary text-primary-foreground"
                  : "text-muted-foreground hover:text-foreground",
              )}
            >
              {o.label}
            </span>
          </label>
        );
      })}
    </fieldset>
  );
}
