import { useState, type PointerEvent } from "react";
import { useTranslation } from "react-i18next";

import { cn } from "@/shared/lib/cn";
import { LegendItem } from "@/shared/ui/LegendItem";

type DialTone = "muted" | "warn" | "ok";

export interface DialRay {
  key: string;
  label: string;
  value: string;
  level: string;
  tone: DialTone;
}

/** Длина луча — состояние правила, а не величина: у строк разные единицы и нет общей шкалы. */
const TONES = ["muted", "warn", "ok"] as const;
const STROKE: Record<DialTone, string> = {
  muted: "var(--color-muted-foreground)",
  warn: "var(--color-warn)",
  ok: "var(--color-ok)",
};
const MARKER: Record<DialTone, string> = {
  muted: "bg-muted-foreground",
  warn: "bg-warn",
  ok: "bg-ok",
};

const WIDTH = 440;
const HEIGHT = 312;
export const DIAL_ASPECT = HEIGHT / WIDTH;
const CX = 220;
const CY = 148;
const RINGS = [64, 84, 104];
const TICK = 14;
const LABEL = 120;
const TURN = 2 * Math.PI;

const polar = (angle: number, radius: number) =>
  `${(CX + radius * Math.cos(angle)).toFixed(1)},${(CY + radius * Math.sin(angle)).toFixed(1)}`;

const arc = (from: number, to: number, radius: number) =>
  `M${polar(from, radius)}A${radius},${radius} 0 ${to - from > Math.PI ? 1 : 0} 1 ${polar(to, radius)}`;

export function SignalDial({
  label,
  caption,
  centre,
  rings,
  items,
  className,
  active,
  onActive,
  target,
}: {
  label: string;
  caption: string;
  centre: string;
  rings: readonly [string, string, string];
  items: DialRay[];
  className?: string;
  active?: string;
  onActive?: (key?: string) => void;
  target?: (key: string) => string;
}) {
  const { t } = useTranslation();
  const [own, setOwn] = useState<string>();
  const current = onActive ? active : own;
  const point = onActive ?? setOwn;
  if (!items.length) return null;
  const step = TURN / items.length;
  const gap = Math.min(step * 0.16, 0.07);
  const angle = (index: number) => -Math.PI / 2 + index * step;
  const passed = items.filter((item) => item.tone === "ok").length;
  const shown = items.find((item) => item.key === current);
  const total = t("ui.ofTotal", { value: passed, total: items.length });
  // Ближайший по углу луч, а не попадание в засечку: у тонкой дуги прицельная зона меньше пальца.
  const pointed = (event: PointerEvent<HTMLDivElement>) => {
    const box = event.currentTarget.getBoundingClientRect();
    const x = ((event.clientX - box.left) / box.width) * WIDTH - CX;
    const y = ((event.clientY - box.top) / box.height) * HEIGHT - CY;
    const turned = (Math.atan2(y, x) + Math.PI / 2 + TURN) % TURN;
    point(items[Math.round(turned / step) % items.length]?.key);
  };
  const jump = (key: string) => {
    const row = target && document.getElementById(target(key));
    if (!row) return;
    row.scrollIntoView({
      block: "center",
      behavior: matchMedia("(prefers-reduced-motion: reduce)").matches ? "auto" : "smooth",
    });
    // Следующий Tab продолжает со строки, а не с кольца.
    row.focus({ preventScroll: true });
    // Курсор к концу прокрутки уже ушёл с луча, и подсветка наведения гасла раньше времени.
    row.dataset.flash = "";
    setTimeout(() => delete row.dataset.flash, 1600);
  };
  return (
    <figure className={cn("@container flex min-w-0 flex-col", className)}>
      {/* Подписи набраны поверх рисунка, а не в SVG: текст SVG растягивался вместе с фигурой. */}
      <div
        className="relative mx-auto w-full max-w-md"
        // Касание пальцем приходит без движения: луч выбирается при нажатии.
        onPointerDown={pointed}
        onPointerMove={pointed}
        onPointerLeave={() => point(undefined)}
      >
        <svg
          viewBox={`0 0 ${WIDTH} ${HEIGHT}`}
          // oxlint-disable-next-line jsx-a11y/prefer-tag-over-role -- рисунок собран дугами, тега <img> у него нет
          role="img"
          aria-label={`${label}: ${rings[2]} — ${total}`}
          className="block w-full"
        >
          {items.map((item, index) => {
            const mid = angle(index);
            const filled = TONES.indexOf(item.tone) + 1;
            return (
              <g key={item.key}>
                {current === item.key && (
                  <path
                    d={arc(mid - step / 2, mid + step / 2, (RINGS[0]! + RINGS[2]!) / 2)}
                    fill="none"
                    stroke="var(--color-secondary)"
                    strokeWidth={RINGS[2]! - RINGS[0]! + TICK + 10}
                  />
                )}
                {RINGS.map((radius, ring) => (
                  <path
                    key={radius}
                    d={arc(mid - step / 2 + gap, mid + step / 2 - gap, radius)}
                    fill="none"
                    strokeWidth={TICK}
                    stroke={ring < filled ? STROKE[item.tone] : "var(--color-border)"}
                  />
                ))}
              </g>
            );
          })}
          {/* Круг поверх засечек, иначе щелчок до него не доходит. */}
          {target && (
            <circle
              cx={CX}
              cy={CY}
              r={LABEL}
              fill="transparent"
              className="cursor-pointer"
              onClick={() => current && jump(current)}
            />
          )}
        </svg>
        {items.map((item, index) => {
          const mid = angle(index);
          const cos = Math.cos(mid);
          const sin = Math.sin(mid);
          const above = Math.abs(cos) < 0.3;
          // Сверху и снизу соседние подписи сходятся вплотную: каждая вторая уходит на своё кольцо.
          const radius = LABEL + (above && index % 2 ? 18 : 0);
          const look = cn(
            "absolute rounded-md px-1 text-xs whitespace-nowrap @sm:text-sm",
            current === item.key
              ? "bg-accent font-semibold text-accent-foreground"
              : "text-secondary-foreground",
          );
          const style = {
            left: `${((CX + radius * cos) / WIDTH) * 100}%`,
            top: `${((CY + radius * sin) / HEIGHT) * 100}%`,
            transform: above
              ? `translate(-50%, ${sin < 0 ? "-100%" : "0"})`
              : `translate(${cos > 0 ? "0" : "-100%"}, -50%)`,
          };
          if (!target)
            return (
              <span
                key={item.key}
                aria-hidden
                className={cn(look, "pointer-events-none")}
                style={style}
              >
                {item.label}
              </span>
            );
          const claim = (event: PointerEvent) => {
            event.stopPropagation();
            point(item.key);
          };
          return (
            <button
              key={item.key}
              type="button"
              aria-controls={target(item.key)}
              aria-describedby={target(item.key)}
              className={look}
              style={style}
              // Длинное слово у косого луча заходит в сектор соседа: своя подпись важнее.
              onPointerDown={claim}
              onPointerMove={claim}
              onClick={(event) => {
                event.stopPropagation();
                jump(item.key);
              }}
              onFocus={() => point(item.key)}
              onBlur={() => point(undefined)}
            >
              {item.label}
            </button>
          );
        })}
        <span
          aria-hidden
          className="pointer-events-none absolute flex -translate-1/2 flex-col items-center"
          style={{ left: `${(CX / WIDTH) * 100}%`, top: `${(CY / HEIGHT) * 100}%` }}
        >
          <span className="text-3xl leading-none font-semibold tabular-nums @sm:text-4xl">
            {passed}/{items.length}
          </span>
          <span className="mt-1 text-xs text-secondary-foreground">{centre}</span>
        </span>
      </div>
      {/* Высота постоянна: появляясь и переносясь, строка сдвигала бы всё ниже. */}
      <p
        className="mt-1 h-5 truncate text-center text-sm text-foreground tabular-nums"
        aria-live="polite"
      >
        {shown && `${shown.label} · ${shown.value} · ${shown.level}`}
      </p>
      <figcaption className="pt-3">
        <div className="flex flex-wrap justify-center gap-x-4 gap-y-1 text-sm text-secondary-foreground">
          {TONES.map((tone, ring) => (
            <LegendItem key={tone} marker={MARKER[tone]} size="sm">
              {rings[ring]}
            </LegendItem>
          ))}
        </div>
        <p className="mt-2 text-center text-xs text-muted-foreground">{caption}</p>
      </figcaption>
    </figure>
  );
}
