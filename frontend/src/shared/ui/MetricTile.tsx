import type { ComponentType, ReactNode } from "react";

import { cn } from "@/shared/lib/cn";
import { Card } from "@/shared/ui/Card";

export function MetricTile({
  label,
  value,
  sub,
  icon: Icon,
  hint,
  className,
  variant = "card",
}: {
  label: string;
  value: ReactNode;
  sub?: ReactNode;
  icon?: ComponentType<{ className?: string }>;
  hint?: ReactNode;
  className?: string;
  variant?: "card" | "plain";
}) {
  const Surface = variant === "card" ? Card : "div";
  return (
    <Surface
      className={cn(
        "row-span-3 grid min-w-0 grid-rows-subgrid gap-y-2",
        variant === "card" ? "p-4" : "py-3 pr-4",
        className,
      )}
    >
      <div className="flex min-h-5 items-start justify-between gap-2">
        <span className="inline-flex min-w-0 items-center gap-1 text-sm text-muted-foreground">
          <span className="min-w-0 wrap-anywhere hyphens-auto">{label}</span>
          {hint}
        </span>
        {Icon && <Icon className="size-4 shrink-0 text-muted-foreground" aria-hidden />}
      </div>
      <p className="font-mono text-2xl leading-none font-semibold tabular-nums">{value}</p>
      <div className="text-xs text-muted-foreground">{sub}</div>
    </Surface>
  );
}
