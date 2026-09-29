import type { TFunction } from "i18next";
import type { TimeSeries } from "@/shared/ui/TimeSeriesChart";
import type { StudyTopic } from "./studyApi";

export type StudyMetric = "share" | "count" | "groups";

export function parseMetric(value: string | null): StudyMetric {
  return value === "count" || value === "groups" ? value : "share";
}

export function topicSeries(
  topic: { id: string; series: StudyTopic["series"] },
  name: string,
  metric: StudyMetric,
  t: TFunction<"trend">,
  format: (n: number) => string,
  fresh = false,
): TimeSeries {
  return {
    id: fresh ? `${topic.id}-new` : topic.id,
    label: fresh
      ? t("study.seriesNewGroups")
      : metric === "groups"
        ? t("study.seriesGroups", { name })
        : name,
    points: topic.series.map((row) => {
      const value =
        metric === "share"
          ? row.share === null
            ? null
            : row.share * 100
          : metric === "groups"
            ? fresh
              ? row.new_groups
              : row.groups
            : row.count;
      return {
        period: row.period,
        value,
        display:
          metric === "share"
            ? row.share === null
              ? "—"
              : `${format(row.share * 100)}% (${row.count}/${row.population})`
            : value === null
              ? "—"
              : String(value),
      };
    }),
  };
}
