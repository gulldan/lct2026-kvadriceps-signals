import { useTranslation } from "react-i18next";
import { formatNumber } from "@/shared/lib/format";
import { Card } from "@/shared/ui/Card";
import { TimeSeriesChart, type TimeSeries } from "@/shared/ui/TimeSeriesChart";
import type { TrendCandidate } from "../trendApi";
import { sharePoints } from "./TrendTimeline";

export function TopicSparkline({
  candidate,
  bare = false,
}: {
  candidate: TrendCandidate;
  bare?: boolean;
}) {
  const { t } = useTranslation("trend");
  const series = candidate.measurements.series;
  const known = series.some((period) => period.normalized_activity !== null);
  const share = (value: number | null) =>
    value === null
      ? t("unknown")
      : formatNumber(value, { style: "percent", maximumFractionDigits: 2 });
  const label = known ? t("activity") : t("explore.quarterPublications");
  const line: TimeSeries = known
    ? sharePoints(series, label, share)
    : {
        id: "count",
        label,
        points: series.map((period) => ({
          period: period.period,
          value: period.found.scientific_works,
          display: t("explore.papers", { count: period.found.scientific_works }),
        })),
      };
  const latest = line.points.findLast((point) => point.value !== null);
  const body = (
    <>
      <div className="flex flex-wrap items-baseline justify-between gap-x-3 gap-y-1 text-xs text-muted-foreground">
        <span>{label}</span>
        {latest && (
          <span className="font-mono tabular-nums">
            {latest.period} · {latest.display}
          </span>
        )}
      </div>
      <TimeSeriesChart compact label={label} series={[line]} />
      <div className="flex justify-between font-mono text-xs text-muted-foreground tabular-nums">
        <span>{series[0]?.period}</span>
        <span>{series.at(-1)?.period}</span>
      </div>
    </>
  );
  return bare ? (
    <div className="space-y-2">{body}</div>
  ) : (
    <Card className="space-y-2 p-4">{body}</Card>
  );
}
