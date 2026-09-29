import type { ReactNode } from "react";
import { Label } from "@/shared/ui/Label";
import { Switch } from "@/shared/ui/CSwitch";
import { cn } from "@/shared/lib/cn";

export function ToggleRow({
  id,
  label,
  hint,
  badge,
  checked,
  onCheckedChange,
}: {
  id: string;
  label: string;
  hint?: string;
  badge?: ReactNode;
  checked: boolean;
  onCheckedChange: (checked: boolean) => void;
}) {
  return (
    <div className="flex items-center justify-between gap-4 py-3">
      <div className="min-w-0 space-y-1">
        <span className="flex min-h-6 items-center gap-2">
          <Label htmlFor={id}>{label}</Label>
          {badge}
        </span>
        {hint && (
          <p id={`${id}-hint`} className="text-xs text-muted-foreground">
            {hint}
          </p>
        )}
      </div>
      <Switch
        id={id}
        aria-describedby={hint ? `${id}-hint` : undefined}
        checked={checked}
        onCheckedChange={onCheckedChange}
      />
    </div>
  );
}

export function FieldRow({
  id,
  label,
  hint,
  badge,
  unit,
  children,
  layout = "stack",
}: {
  id: string;
  label: string;
  hint?: string;
  badge?: ReactNode;
  unit?: string;
  children: ReactNode;
  /** compact прячет только видимую строку: доступная подпись и DOM поля остаются. */
  layout?: "stack" | "row" | "compact";
}) {
  const help = hint && (
    <p id={`${id}-hint`} className="text-xs leading-relaxed text-muted-foreground">
      {hint}
    </p>
  );
  return (
    <div
      className={cn(
        "grid min-w-0 content-start gap-2",
        layout === "row" &&
          "py-4 sm:grid-cols-[minmax(0,1fr)_minmax(0,24rem)] sm:items-center sm:gap-x-8",
      )}
    >
      <div className={cn("min-w-0 space-y-1", layout === "compact" && "sr-only")}>
        <span className="flex min-h-6 items-center gap-2">
          <Label htmlFor={id}>{label}</Label>
          {badge}
        </span>
        {layout === "row" && help}
      </div>
      <div className={cn("flex min-w-0 items-center gap-2", layout === "row" && "sm:justify-end")}>
        {children}
        {unit && <span className="shrink-0 text-xs text-muted-foreground">{unit}</span>}
      </div>
      {layout !== "row" && help}
    </div>
  );
}

export function StatStrip({
  items,
  columns = 3,
}: {
  items: { label: string; value: string; hint?: string }[];
  columns?: 2 | 3 | 4 | 6;
}) {
  return (
    <dl
      className={cn(
        "grid gap-4 border-b pb-4",
        columns === 2
          ? "grid-cols-2"
          : columns === 4
            ? "grid-cols-2 sm:grid-cols-4"
            : columns === 6
              ? "grid-cols-2 sm:grid-cols-3 xl:grid-cols-6"
              : "grid-cols-1 sm:grid-cols-3",
      )}
    >
      {items.map((it) => (
        <div key={it.label} className="min-w-0">
          <dt className="text-xs text-muted-foreground">{it.label}</dt>
          <dd className="mt-1 text-sm font-medium break-words tabular-nums" title={it.value}>
            <span>{it.value}</span>
            {it.hint && (
              <span className="mt-0.5 block text-xs font-normal text-muted-foreground">
                {it.hint}
              </span>
            )}
          </dd>
        </div>
      ))}
    </dl>
  );
}
