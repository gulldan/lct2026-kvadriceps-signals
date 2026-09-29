import { useTranslation } from "react-i18next";
import { BarChart } from "@/shared/ui/BarChart";
import { Section } from "@/shared/ui/Section";
import { MetricTile } from "@/shared/ui/MetricTile";
import { Disclosure } from "@/shared/ui/Disclosure";
import { formatNumber } from "@/shared/lib/format";
import type { AnalysisJob } from "../trendApi";
import { exploreAnalysis } from "../exploration";

export function TrendOverview({ job }: { job: AnalysisJob }) {
  const { t } = useTranslation("trend");
  const result = job.result!;
  const found = exploreAnalysis(job);
  const data = found.years.map(({ year, count }) => ({
    label: String(year),
    value: count,
    displayValue: t("explore.papers", { count }),
  }));
  return (
    <Section title={t("explore.title")}>
      <div className="grid grid-cols-2 gap-x-6 gap-y-3 border-b pb-4 lg:grid-cols-4">
        <MetricTile
          variant="plain"
          label={t("explore.examined")}
          value={formatNumber(result.works.length)}
          sub={t("explore.versions", { count: job.sources.length })}
        />
        <MetricTile
          variant="plain"
          label={t("explore.topics")}
          value={formatNumber(found.candidates.length)}
          sub={t("explore.matching")}
        />
        <MetricTile
          variant="plain"
          label={t("explore.works")}
          value={formatNumber(found.works.length)}
          sub={t("explore.deduplicated")}
        />
        <MetricTile
          variant="plain"
          label={t("explore.ranked")}
          value={formatNumber(result.ranking.ordered_ids.length)}
          sub={t("explore.rankHint")}
        />
      </div>
      <div className="mt-6 space-y-3">
        {data.some((item) => item.value > 0) ? (
          <BarChart label={t("explore.publications")} data={data} />
        ) : (
          <p className="text-sm text-muted-foreground">{t("explore.noDates")}</p>
        )}
        <p className="max-w-prose text-sm leading-relaxed text-muted-foreground">
          {t("explore.chartHint")}
        </p>
        {(found.undated > 0 || found.outside > 0) && (
          <p className="text-xs text-muted-foreground">
            {t("explore.omitted", { undated: found.undated, outside: found.outside })}
          </p>
        )}
      </div>
      <Disclosure className="mt-5" title={t("explore.how")}>
        <ol className="max-w-prose list-decimal space-y-3 pl-5 text-sm leading-relaxed">
          <li>
            {t("explore.stepCorpus", { sources: job.sources.length, works: result.works.length })}
          </li>
          <li>
            {t("explore.stepSearch", {
              query: result.search.applied_query,
              topics: found.candidates.length,
            })}
          </li>
          <li>{t("explore.stepEvidence")}</li>
          <li>{t("explore.stepRank", { count: result.ranking.ordered_ids.length })}</li>
        </ol>
      </Disclosure>
    </Section>
  );
}
