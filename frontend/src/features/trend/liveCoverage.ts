import type { LiveOverview } from "./liveApi";

/** Два пропущенных шестичасовых цикла подряд: снимок уже не «сегодняшний». */
export const STALE_HOURS = 12;

interface LiveExaminedScope {
  completeMonths: number;
  from: string;
  to: string;
  current: { period: string; through: string } | null;
  works: number;
  categories: number;
  staleHours: number | null;
}

/** Возраст — по часам сборщика, а не браузера: один ответ читается одинаково на любом экране. */
export function liveExamined(
  overview: Pick<LiveOverview, "periods" | "categories" | "collection">,
): LiveExaminedScope {
  const complete = overview.periods.filter((period) => period.complete);
  const current = overview.periods.find((period) => !period.complete);
  const collection = overview.collection;
  let staleHours: number | null = null;
  if (collection?.checked_at && collection.last_success_at) {
    const age = Date.parse(collection.checked_at) - Date.parse(collection.last_success_at);
    staleHours = Number.isFinite(age) ? Math.max(0, age / 3_600_000) : null;
  }
  return {
    completeMonths: complete.length,
    from: complete[0]?.period ?? "",
    to: complete.at(-1)?.period ?? "",
    current: current ? { period: current.period, through: lastDay(current.end_exclusive) } : null,
    works: overview.periods.reduce((sum, period) => sum + period.population, 0),
    categories: overview.categories.split(" OR ").filter(Boolean).length,
    staleHours,
  };
}

export function liveFailureReason(code: string | undefined): "canceled" | "source" | "other" {
  return code === "canceled" || code === "source" ? code : "other";
}

/** У остановленного сборщика срок застывает — просроченное обещание было бы неправдой. */
export function liveRunOverdue(nextRunAt: string, now = Date.now()): boolean {
  const at = Date.parse(nextRunAt);
  return Number.isFinite(at) && at < now;
}

/** end_exclusive — первый день вне окна; человеку показывается последний день внутри. */
function lastDay(endExclusive: string): string {
  const day = new Date(endExclusive);
  day.setUTCDate(day.getUTCDate() - 1);
  return day.toISOString().slice(0, 10);
}
