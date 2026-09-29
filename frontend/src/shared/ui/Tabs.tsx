import { cn } from "@/shared/lib/cn";
import type { ComponentProps } from "react";

export interface TabItem {
  id: string;
  label: string;
  badge?: number | string;
}

export function tabKeyIndex(key: string, index: number, length: number): number | null {
  return key === "Home"
    ? 0
    : key === "End"
      ? length - 1
      : key === "ArrowRight"
        ? (index + 1) % length
        : key === "ArrowLeft"
          ? (index - 1 + length) % length
          : null;
}

export function tabUnderline(on: boolean) {
  return cn(
    "-mb-px shrink-0 border-b-2 transition-colors focus-visible:outline-offset-[-2px]",
    on
      ? "border-brand text-brand"
      : "border-transparent text-muted-foreground hover:border-border hover:text-foreground",
  );
}

export function Tabs({
  id,
  tabs,
  active,
  onChange,
  className,
}: {
  id: string;
  tabs: TabItem[];
  active: string;
  /** false — выбор запрещён проверкой или незавершённым сохранением. */
  onChange: (id: string) => void | boolean;
  className?: string;
}) {
  return (
    <div role="tablist" className={cn("flex gap-5 overflow-x-auto border-b", className)}>
      {tabs.map((t, index) => {
        const on = t.id === active;
        return (
          <button
            key={t.id}
            type="button"
            role="tab"
            id={`${id}-${t.id}`}
            aria-controls={`${id}-panel`}
            aria-selected={on}
            tabIndex={on ? 0 : -1}
            onClick={() => onChange(t.id)}
            onKeyDown={(event) => {
              const next = tabKeyIndex(event.key, index, tabs.length);
              if (next === null) return;
              event.preventDefault();
              const tab = tabs[next];
              if (!tab) return;
              if (onChange(tab.id) === false) return;
              document.getElementById(`${id}-${tab.id}`)?.focus();
            }}
            className={cn(
              "flex items-center gap-1.5 px-0.5 pt-1 pb-2.5 text-sm font-medium",
              tabUnderline(on),
            )}
          >
            {t.label}
            {t.badge !== undefined && t.badge !== "" && t.badge !== 0 && (
              <span
                className={cn(
                  "rounded-full px-1.5 text-xs tabular-nums",
                  on ? "bg-brand-wash text-accent-foreground" : "bg-muted text-muted-foreground",
                )}
              >
                {t.badge}
              </span>
            )}
          </button>
        );
      })}
    </div>
  );
}

export function TabPanel({
  tabsId,
  active,
  ...props
}: ComponentProps<"div"> & { tabsId: string; active: string }) {
  return (
    <div
      role="tabpanel"
      id={`${tabsId}-panel`}
      aria-labelledby={`${tabsId}-${active}`}
      tabIndex={0}
      {...props}
    />
  );
}
