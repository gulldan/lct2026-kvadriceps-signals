import type { LlmUsage } from "./api";
import type { BarDatum } from "@/shared/ui/BarChart";
import { formatDateShort, formatNumber } from "@/shared/lib/format";

/** API отдаёт только дни с данными (UTC); нули в выбранном окне дописываются здесь. */
export function usageDays(
  usage: Pick<LlmUsage, "from" | "to" | "by_day">,
  metric: "requests" | "tokens",
): BarDatum[] {
  const byDay = new Map(usage.by_day.map((row) => [row.day, row]));
  const end = Date.parse(`${usage.to.slice(0, 10)}T00:00:00Z`);
  const start = Date.parse(`${usage.from.slice(0, 10)}T00:00:00Z`);
  const days: BarDatum[] = [];
  for (let time = start; time <= end && days.length < 31; time += 86400000) {
    const day = new Date(time).toISOString().slice(0, 10);
    const row = byDay.get(day);
    const value = (metric === "requests" ? row?.requests : row?.total_tokens) ?? 0;
    days.push({
      label: formatDateShort(`${day}T12:00:00`),
      value,
      displayValue: formatNumber(value),
    });
  }
  return days;
}
