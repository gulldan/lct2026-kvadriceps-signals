import { useLayoutEffect, useRef, type ReactNode } from "react";
import { cn } from "@/shared/lib/cn";
import { tabKeyIndex, tabUnderline } from "@/shared/ui/Tabs";

export interface MetricTabItem {
  id: string;
  label: string;
  value: ReactNode;
  hint?: string;
}

export function MetricTabs({
  id,
  items,
  active,
  onChange,
  className,
  "aria-labelledby": labelledBy,
}: {
  id: string;
  items: MetricTabItem[];
  active: string;
  onChange: (id: string) => void;
  className?: string;
  "aria-labelledby"?: string;
}) {
  const strip = useRef<HTMLDivElement>(null);
  // Смещение полосы возвращается после смены панели: короткая панель укорачивала страницу, и
  // браузер утягивал читателя вверх.
  const anchor = useRef<number | null>(null);
  const select = (next: string) => {
    anchor.current = strip.current?.getBoundingClientRect().top ?? null;
    onChange(next);
  };
  useLayoutEffect(() => {
    const held = anchor.current;
    anchor.current = null;
    const top = strip.current?.getBoundingClientRect().top;
    if (held === null || top === undefined || Math.abs(top - held) < 1) return;
    window.scrollBy({ top: top - held, behavior: "instant" });
  }, [active]);
  return (
    <div
      ref={strip}
      role="tablist"
      aria-labelledby={labelledBy}
      className={cn(
        "flex [scrollbar-width:none] gap-x-7 overflow-x-auto border-b max-sm:[mask-image:linear-gradient(to_right,var(--color-background)_85%,transparent)]",
        className,
      )}
    >
      {items.map((item, index) => {
        const on = item.id === active;
        return (
          <button
            key={item.id}
            type="button"
            role="tab"
            id={`${id}-${item.id}`}
            aria-controls={`${id}-panel`}
            aria-selected={on}
            tabIndex={on ? 0 : -1}
            onClick={() => select(item.id)}
            onKeyDown={(event) => {
              const next = tabKeyIndex(event.key, index, items.length);
              if (next === null) return;
              event.preventDefault();
              const tab = items[next];
              if (!tab) return;
              select(tab.id);
              // Без preventScroll браузер сам доводит кнопку до края и отменяет удержание полосы.
              document.getElementById(`${id}-${tab.id}`)?.focus({ preventScroll: true });
            }}
            className={cn(
              "flex grow basis-0 cursor-pointer flex-col items-start gap-1.5 pt-1 pb-3 text-left",
              tabUnderline(on),
            )}
          >
            <span className={cn("text-sm whitespace-nowrap", on && "font-medium")}>
              {item.label}
            </span>
            <span
              className={cn(
                "text-2xl leading-none font-semibold tabular-nums",
                !on && "text-foreground",
              )}
            >
              {item.value}
            </span>
            {item.hint && <span className="text-xs text-muted-foreground">{item.hint}</span>}
          </button>
        );
      })}
    </div>
  );
}
