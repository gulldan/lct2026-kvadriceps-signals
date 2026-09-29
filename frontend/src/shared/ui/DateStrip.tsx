import { cn } from "@/shared/lib/cn";
import { formatDate, formatElapsed, formatMonth } from "@/shared/lib/format";

export interface DateStripItem {
  label: string;
  /** Дата дня («2025-08-01»), месяца («2026-08») или момента (ISO с временем). */
  date?: string;
  empty?: string;
  hint?: string;
}

/** Отсчёт — обычным шрифтом: моноширинный он переносился. */
export function DateStrip({
  items,
  columns = 4,
  className,
}: {
  items: DateStripItem[];
  columns?: 2 | 3 | 4;
  className?: string;
}) {
  return (
    <dl
      className={cn(
        "grid gap-x-6 gap-y-4 border-b pb-4",
        columns === 2
          ? "grid-cols-2"
          : columns === 3
            ? "grid-cols-1 sm:grid-cols-3"
            : "grid-cols-2 lg:grid-cols-4",
        className,
      )}
    >
      {items.map((item) => (
        <div key={item.label} className="row-span-3 grid min-w-0 grid-rows-subgrid gap-y-1">
          <dt className="text-sm leading-5 text-muted-foreground">{item.label}</dt>
          <dd className="text-xl leading-tight font-semibold text-balance">
            {item.date ? formatElapsed(item.date) : (item.empty ?? "—")}
          </dd>
          <dd className="text-xs text-muted-foreground">
            {item.date
              ? item.date.length <= 7
                ? formatMonth(item.date)
                : formatDate(item.date)
              : item.hint}
          </dd>
        </div>
      ))}
    </dl>
  );
}
