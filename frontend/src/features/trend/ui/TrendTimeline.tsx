import { useTranslation } from "react-i18next";
import { formatNumber } from "@/shared/lib/format";
import { Disclosure } from "@/shared/ui/Disclosure";
import { BarChart } from "@/shared/ui/BarChart";
import { TimeSeriesChart, type TimeSeries } from "@/shared/ui/TimeSeriesChart";
import { Table, TableHeader, TableBody, TableRow, TableHead, TableCell } from "@/shared/ui/Table";
import type { TrendCandidate } from "../trendApi";

/** Доля в процентных единицах: ось графика начинается от единицы, а не от доли. */
export function sharePoints(
  series: TrendCandidate["measurements"]["series"],
  label: string,
  display: (value: number | null) => string,
): TimeSeries {
  return {
    id: "share",
    label,
    points: series.map((period) => ({
      period: period.period,
      value: period.normalized_activity === null ? null : period.normalized_activity * 100,
      display: display(period.normalized_activity),
    })),
  };
}

export function TrendTimeline({ candidate }: { candidate: TrendCandidate }) {
  const { t } = useTranslation("trend");
  const series = candidate.measurements.series;
  const known = series.some((period) => period.normalized_activity !== null);
  const number = (value: number | null) => (value === null ? t("unknown") : formatNumber(value));
  const share = (value: number | null) =>
    value === null
      ? t("unknown")
      : formatNumber(value, { style: "percent", maximumFractionDigits: 2 });
  return (
    <div className="space-y-4">
      <p className="max-w-prose text-sm text-muted-foreground">{t("timelineHint")}</p>
      {known ? (
        <TimeSeriesChart
          label={t("activity")}
          series={[sharePoints(series, t("activity"), share)]}
          formatValue={(value) => `${formatNumber(value, { maximumFractionDigits: 2 })} %`}
        />
      ) : (
        <div className="space-y-3">
          {series.some((period) => period.found.scientific_works > 0) && (
            <BarChart
              label={t("explore.quarterPublications")}
              data={series.map((period) => ({
                label: period.period + (period.complete_period ? "" : ` · ${t("partialPeriod")}`),
                value: period.found.scientific_works,
                displayValue: t("explore.papers", { count: period.found.scientific_works }),
              }))}
            />
          )}
          <p className="max-w-prose text-sm text-muted-foreground">{t("noChart")}</p>
        </div>
      )}
      <Disclosure title={t("table")}>
        <Table aria-label={t("table")}>
          <TableHeader>
            <TableRow>
              {(
                [
                  "quarter",
                  "scientific",
                  "repositories",
                  "releases",
                  "patents",
                  "companies",
                  "analyses",
                  "exposure",
                  "activity",
                  "coverage",
                ] as const
              ).map((key) => (
                <TableHead key={key} scope="col">
                  {t(key)}
                </TableHead>
              ))}
            </TableRow>
          </TableHeader>
          <TableBody>
            {series.map((period) => (
              <TableRow key={period.period}>
                <TableHead scope="row" className="whitespace-nowrap">
                  {period.period}
                  {!period.complete_period && (
                    <span className="block text-xs text-warn">{t("partialPeriod")}</span>
                  )}
                </TableHead>
                {(
                  [
                    "scientific_works",
                    "independent_repositories",
                    "published_releases",
                    "patent_publications",
                    "company_materials",
                    "analyses",
                  ] as const
                ).map((unit) => (
                  <TableCell key={unit} className="tabular-nums">
                    {number(period.found[unit])}
                  </TableCell>
                ))}
                <TableCell>{number(period.comparable_exposure)}</TableCell>
                <TableCell>{share(period.normalized_activity)}</TableCell>
                <TableCell>
                  {t(
                    period.comparable_exposure === null ? "insufficientCoverage" : "knownCoverage",
                  )}
                </TableCell>
              </TableRow>
            ))}
          </TableBody>
        </Table>
      </Disclosure>
    </div>
  );
}
