import { dateFormatter } from "@/shared/lib/format";
import type { LiveOverview, LiveTopic } from "./liveApi";

export const growthTones = {
  declining: { marker: "bg-chart-5", text: "text-chart-5" },
  moderate: { marker: "bg-chart-1", text: "text-chart-1" },
  strong: { marker: "bg-chart-2", text: "text-chart-2" },
  rapid: { marker: "bg-chart-3", text: "text-chart-3" },
  surging: { marker: "bg-chart-4", text: "text-chart-4" },
  new: { marker: "bg-foreground", text: "text-foreground" },
} as const;

export function growthBand(prior: number, current: number): keyof typeof growthTones {
  if (current < prior) return "declining";
  if (prior === 0 && current > 0) return "new";
  if (current <= prior * 1.5) return "moderate";
  if (current <= prior * 2) return "strong";
  if (current <= prior * 4) return "rapid";
  return "surging";
}

export function publicationChange(topic: LiveTopic, periods: LiveOverview["periods"]) {
  if (topic.comparison) {
    const { prior, current, fresh } = topic.comparison;
    return { prior, current, added: current - prior, fresh, band: growthBand(prior, current) };
  }
  // Старые сохранённые ответы с кварталами остаются читаемыми.
  const window = periods.filter((period) => period.complete).slice(-4);
  if (window.length !== 4) return null;
  const rows = window.map(({ period }) => topic.series.find((row) => row.period === period));
  if (rows.some((row) => !row)) return null;
  const prior = rows[0]!.count + rows[1]!.count;
  const current = rows[2]!.count + rows[3]!.count;
  return {
    prior,
    current,
    added: current - prior,
    fresh: rows[3]!.new_groups,
    band: growthBand(prior, current),
  };
}

export function comparisonRanges(
  topic: LiveTopic | undefined,
  periods: LiveOverview["periods"],
  locale: string,
) {
  if (topic?.comparison) {
    const c = topic.comparison;
    const dates = dateFormatter(
      { day: "numeric", month: "short", year: "numeric", timeZone: "UTC" },
      locale,
    );
    const range = (start: string, end: string) =>
      dates.formatRange(new Date(start), new Date(new Date(end).getTime() - 1));
    return {
      prior: range(c.prior_start, c.recent_start),
      recent: range(c.recent_start, c.end_exclusive),
    };
  }
  const complete = periods.filter((p) => p.complete);
  return {
    prior: calendarRange(complete.slice(-4, -2), locale),
    recent: calendarRange(complete.slice(-2), locale),
  };
}

export function calendarRange(
  periods: Pick<LiveOverview["periods"][number], "start" | "end_exclusive">[],
  locale: string,
) {
  const first = periods[0];
  const last = periods.at(-1);
  if (!first || !last) return "—";
  const start = new Date(first.start);
  const end = new Date(new Date(last.end_exclusive).getTime() - 1);
  if (start.getUTCFullYear() === end.getUTCFullYear()) {
    const months = dateFormatter({ month: "short", timeZone: "UTC" }, locale).formatRange(
      start,
      end,
    );
    const year = dateFormatter({ year: "numeric", timeZone: "UTC" }, locale).format(end);
    return new Intl.Locale(locale).language === "zh" ? `${year}${months}` : `${months} ${year}`;
  }
  return dateFormatter({ month: "short", year: "numeric", timeZone: "UTC" }, locale).formatRange(
    start,
    end,
  );
}

const SYMLOG_STEPS = [1, 3, 10, 30, 100, 300, 1000, 3000, 10000, 30000];
const symlog = (value: number) => Math.sign(value) * Math.log10(1 + Math.abs(value));

/** Логарифм модуля со знаком: на линейной шкале сотня направлений около нуля слипалась в полосу. */
export function symlogAxis(values: number[]) {
  const bottom = symlog(Math.min(0, ...values));
  const top = symlog(Math.max(0, ...values));
  const pad = Math.max(top - bottom, 1) * 0.06;
  const min = bottom - pad;
  const max = top + pad;
  // Запас по краям нужен кругам, а не обещанию прироста, которого ни у кого нет.
  const lowest = Math.min(0, ...values);
  const highest = Math.max(0, ...values);
  return {
    at: (value: number) => (symlog(value) - min) / (max - min),
    ticks: [...SYMLOG_STEPS.map((step) => -step).toReversed(), 0, ...SYMLOG_STEPS].filter(
      (value) => value >= lowest && value <= highest,
    ),
  };
}
