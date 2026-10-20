import { formatNumber } from "@/shared/lib/format";
import { calendarRange } from "./liveMapMetrics";
import type {
  MoneyCell,
  MoneyEvent,
  MoneyFilter,
  MoneyKind,
  MoneyRegion,
  MoneySummary,
} from "./moneyApi";

export const MONEY_REGIONS = [
  "americas",
  "europe",
  "latam",
  "mena",
  "africa",
  "asia",
  "cis",
  "unknown",
] as const satisfies readonly MoneyRegion[];
export const MONEY_KINDS = [
  "round",
  "grant",
  "accelerator",
  "development",
  "research",
] as const satisfies readonly MoneyKind[];

export type MoneyQuadrant = "angel_window" | "consensus" | "money_first" | "quiet";

export interface MoneyPoint {
  rubric: string;
  /** log2 роста доли: 0 — как раньше, 1 — вдвое больше, −1 — вдвое меньше; обрезано до ±2. */
  science: number;
  money: number;
  events: number;
  quadrant: MoneyQuadrant;
}

// Меньше шести событий за полгода — это одна-две сделки: их рост ничего не говорит о направлении.
const MIN_EVENTS = 6;
// Двадцать работ за два полугодия — тот же порог шума, что и у карты радара.
const MIN_PAPERS = 20;

/** Ленты пишут «$13.9mn», «$101.9 million» и «SEK 500K»; в таблице сумма одного вида — «13,9 млн $», «$13.9M». */
export function moneyAmount(event: MoneyEvent) {
  return event.amount_value && event.currency
    ? formatNumber(event.amount_value, {
        style: "currency",
        currency: event.currency,
        notation: "compact",
        maximumFractionDigits: 1,
      })
    : event.amount;
}

export function moneyCells(summary: MoneySummary, filter: MoneyFilter) {
  return summary.cells.filter(
    (cell) =>
      (filter.region === "all" || cell.region === filter.region) &&
      (filter.kind === "all" || cell.kind === filter.kind),
  );
}

export function moneyTotals(cells: MoneyCell[]) {
  const kinds: Record<MoneyKind, number> = {
    round: 0,
    grant: 0,
    accelerator: 0,
    development: 0,
    research: 0,
  };
  let total = 0;
  let classified = 0;
  for (const cell of cells) {
    total += cell.count;
    kinds[cell.kind] += cell.count;
    if (cell.rubric) classified += cell.count;
  }
  return { total, classified, kinds };
}

export function moneyRubrics(cells: MoneyCell[]) {
  const counts = new Map<string, number>();
  for (const cell of cells)
    if (cell.rubric) counts.set(cell.rubric, (counts.get(cell.rubric) ?? 0) + cell.count);
  return [...counts]
    .map(([rubric, count]) => ({ rubric, count }))
    .toSorted((a, b) => b.count - a.count || a.rubric.localeCompare(b.rubric));
}

export function moneyRegions(cells: MoneyCell[]) {
  const counts = new Map<MoneyRegion, number>();
  for (const cell of cells) counts.set(cell.region, (counts.get(cell.region) ?? 0) + cell.count);
  return [...counts]
    .map(([region, count]) => ({ region, count }))
    .toSorted((a, b) => b.count - a.count || a.region.localeCompare(b.region));
}

/** Стадии раундов: у грантов и отборов своя «стадия», её в разбивку раундов не берём. */
export function moneyStages(cells: MoneyCell[]) {
  const counts: Partial<Record<MoneyCell["stage"], number>> = {};
  for (const cell of cells)
    if (cell.kind === "round") counts[cell.stage] = (counts[cell.stage] ?? 0) + cell.count;
  return counts;
}

/** Окна роста денег: три полных месяца против трёх предыдущих; текущий месяц неполный и в сравнение не входит. */
export function moneyWindows(months: string[]) {
  const complete = months.slice(0, -1);
  return { prior: complete.slice(-6, -3), recent: complete.slice(-3), current: months.at(-1) };
}

export function moneyMonths(cells: MoneyCell[], months: string[]) {
  const counts = new Map(months.map((month) => [month, 0]));
  for (const cell of cells) {
    const count = counts.get(cell.month);
    if (count !== undefined) counts.set(cell.month, count + cell.count);
  }
  return [...counts].map(([month, count]) => ({ month, count }));
}

/** «2026-04», «2026-06» → «апр.–июн. 2026 г.»; окно науки приходит строкой «2025-10–2026-03». */
export function monthRange(months: string | string[] | undefined, locale: string) {
  const list = typeof months === "string" ? months.split("–") : (months ?? []);
  const first = list[0];
  const last = list.at(-1);
  if (!first || !last) return null;
  const end = new Date(`${last}-01T00:00:00Z`);
  if (Number.isNaN(end.getTime())) return null;
  end.setUTCMonth(end.getUTCMonth() + 1);
  return calendarRange([{ start: `${first}-01`, end_exclusive: end.toISOString() }], locale);
}

const shift = (ratio: number) => Math.max(-2, Math.min(2, Math.log2(ratio)));

/**
 * Рост доли, а не числа: выгрузка набора раундов отстаёт на месяц, и общий объём последних месяцев
 * занижен для всех отраслей одинаково. Текущий неполный месяц в сравнение не входит.
 */
export function moneyQuadrant(summary: MoneySummary, cells: MoneyCell[]) {
  const windows = moneyWindows(summary.months);
  const prior = new Set(windows.prior);
  const recent = new Set(windows.recent);
  const byRubric = new Map<string, { prior: number; recent: number }>();
  let priorTotal = 0;
  let recentTotal = 0;
  for (const cell of cells) {
    if (!cell.rubric) continue;
    const entry = byRubric.get(cell.rubric) ?? { prior: 0, recent: 0 };
    if (prior.has(cell.month)) {
      entry.prior += cell.count;
      priorTotal += cell.count;
    } else if (recent.has(cell.month)) {
      entry.recent += cell.count;
      recentTotal += cell.count;
    }
    byRubric.set(cell.rubric, entry);
  }
  const science = new Map(summary.science.map((item) => [item.rubric, item]));
  const points: MoneyPoint[] = [];
  const unmeasured: string[] = [];
  for (const [rubric, money] of byRubric) {
    if (money.prior + money.recent < MIN_EVENTS || !priorTotal || !recentTotal) continue;
    const papers = science.get(rubric);
    if (!papers || papers.prior + papers.recent < MIN_PAPERS) {
      unmeasured.push(rubric);
      continue;
    }
    const moneyShift = shift(
      (money.recent + 0.5) / recentTotal / ((money.prior + 0.5) / priorTotal),
    );
    const scienceShift = shift(
      (papers.recent + 0.5) /
        papers.recent_population /
        ((papers.prior + 0.5) / papers.prior_population),
    );
    points.push({
      rubric,
      science: scienceShift,
      money: moneyShift,
      events: money.prior + money.recent,
      quadrant:
        scienceShift > 0
          ? moneyShift > 0
            ? "consensus"
            : "angel_window"
          : moneyShift > 0
            ? "money_first"
            : "quiet",
    });
  }
  return {
    points: points.toSorted((a, b) => b.events - a.events),
    unmeasured: unmeasured.toSorted(),
  };
}
