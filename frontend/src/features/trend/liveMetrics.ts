import type { TFunction } from "i18next";
import type { LiveChannel, LiveTopic } from "./liveApi";
import type { TimeSeries } from "@/shared/ui/TimeSeriesChart";
import { formatNumber } from "@/shared/lib/format";

export function lastNewGroups(topic: LiveTopic, complete: number) {
  return topic.comparison?.fresh ?? topic.series[complete - 1]?.new_groups ?? 0;
}

/** Несобранный или неполный канал — неизвестность; измеренный ноль остаётся нулём. */
export function channelTotal(values: number[] | null, periods: number) {
  return values?.length === periods && periods > 0
    ? values.reduce((sum, value) => sum + value, 0)
    : null;
}

/** Несобранная история не обнуляет другой канал: null + n = n. Записи — от новых к старым, иначе
 *  второй канал прятался за страницами первого. */
export function mergeChannels(a?: LiveChannel | null, b?: LiveChannel | null): LiveChannel | null {
  if (!a || !b) return a ?? b ?? null;
  const length = Math.max(a.counts.length, b.counts.length);
  return {
    ...a,
    source: `${a.source} · ${b.source}`,
    observed_at: a.observed_at < b.observed_at ? a.observed_at : b.observed_at,
    counts: Array.from({ length }, (_, index) => {
      const x = a.counts[index] ?? null;
      const y = b.counts[index] ?? null;
      return x === null ? y : y === null ? x : x + y;
    }),
    total: a.total + b.total,
    partial: a.partial || b.partial,
    limits: [...a.limits, ...b.limits],
    records: [...(a.records ?? []), ...(b.records ?? [])].toSorted((p, q) =>
      q.date.localeCompare(p.date),
    ),
    coverage: [...(a.coverage ?? []), ...(b.coverage ?? [])],
  };
}

/** Слитые патенты приходят снаружи: объединение с сортировкой дороже самого ряда графика. */
export function publicationSeries(
  topic: LiveTopic,
  t: TFunction<"trend">,
  patents: LiveChannel | null,
): TimeSeries[] {
  return (
    [
      { id: "papers", values: topic.series.map((row) => row.count), partial: false },
      { id: "repos", values: topic.repos, partial: false },
      { id: "hn", values: topic.discussions, partial: false },
      { id: "patents", values: patents?.counts, partial: patents?.partial },
      { id: "web", values: topic.web?.counts, partial: topic.web?.partial },
    ] as const
  ).map(({ id, values, partial }) => {
    // Накопление: на месячных значениях редкие каналы читались как пустые. После несобранного
    // месяца сумма — нижняя оценка, «≥».
    let total = 0;
    let missed = false;
    return {
      id,
      label: t(`study.live.types.${id}`),
      points: topic.series.map((row, index) => {
        const value = values?.[index] ?? null;
        if (value === null) {
          missed = true;
          return { period: row.period, value: null, display: "—" };
        }
        total += value;
        return {
          period: row.period,
          value: total,
          display: `${partial || missed ? "≥" : ""}${formatNumber(total)}`,
        };
      }),
    };
  });
}
