import { useTranslation } from "react-i18next";
import { Badge } from "@/shared/ui/Badge";
import { cn } from "@/shared/lib/cn";
import { formatDate } from "@/shared/lib/format";
import type { LiveOverview, LiveTopic } from "../liveApi";
import { channelTotal } from "../liveMetrics";
import { comparisonRanges, growthTones, publicationChange } from "../liveMapMetrics";
import { stageTones } from "../stageTones";
import { LivePreviewHistory } from "./LivePreviewHistory";
import { moneyTitle } from "../liveTopicMoney";

/** Итоги и истории считаются по одному окну наблюдения, включая текущий период. */
export function LiveTopicPreview({
  topic,
  periods,
  observedAt,
  name,
  format,
}: {
  topic: LiveTopic;
  periods: LiveOverview["periods"];
  observedAt: string;
  name: string;
  format: (n: number, digits?: number) => string;
}) {
  const { t, i18n } = useTranslation("trend");
  const count = (value: number | null) => (value === null ? "—" : format(value, 0));
  const last = periods.at(-1);
  const ranges = comparisonRanges(topic, periods, i18n.language);
  const change = publicationChange(topic, periods);
  return (
    <div className="space-y-2 text-left leading-normal" data-testid="trend-preview">
      <div className="space-y-1.5">
        <p className="text-sm font-semibold wrap-anywhere">{name}</p>
        <Badge variant={stageTones[topic.stage].badge}>
          {t(`study.live.table.stages.${topic.stage}`)}
        </Badge>
        {topic.description && (
          <p className="line-clamp-3 text-xs text-muted-foreground">{topic.description}</p>
        )}
      </div>
      {change && (
        <div className="space-y-1 border-y py-2" data-testid="trend-preview-change">
          <p className="flex items-center gap-1.5 text-xs font-semibold">
            <span
              aria-hidden
              className={cn("size-2 rounded-full", growthTones[change.band].marker)}
            />
            {change.added === 0
              ? t("study.live.preview.unchanged")
              : t(change.added > 0 ? "study.live.preview.more" : "study.live.preview.fewer", {
                  count: Math.abs(change.added),
                  amount: format(Math.abs(change.added), 0),
                })}
            <span className="ml-auto font-mono font-normal tabular-nums">
              {format(change.prior, 0)} → {format(change.current, 0)}
            </span>
          </p>
          <p className="text-caption text-muted-foreground">
            {t("study.live.preview.comparison", {
              prior: ranges.prior,
              recent: ranges.recent,
            })}
          </p>
        </div>
      )}
      <LivePreviewHistory
        periods={periods}
        observedAt={observedAt}
        channels={[
          {
            id: "papers",
            total: topic.papers_count,
            values: periods.map(
              ({ period }) => topic.series.find((row) => row.period === period)?.count ?? null,
            ),
          },
          {
            id: "repos",
            total: channelTotal(topic.repos, topic.series.length),
            values: periods.map(
              ({ period }) =>
                topic.repos?.[topic.series.findIndex((row) => row.period === period)] ?? null,
            ),
          },
          {
            id: "hn",
            total: channelTotal(topic.discussions, topic.series.length),
            values: periods.map(
              ({ period }) =>
                topic.discussions?.[topic.series.findIndex((row) => row.period === period)] ?? null,
            ),
          },
        ]}
        format={count}
      />
      <dl className="flex justify-between gap-3 border-t pt-2 text-xs">
        {(["patents", "web"] as const).map((key) => (
          <div key={key} className="flex items-baseline gap-1.5">
            <dt className={key === "patents" ? "text-chart-4" : "text-chart-5"}>
              {t(`study.live.types.${key}`)}
            </dt>
            <dd className="font-mono tabular-nums">
              {topic[key]
                ? `${topic[key]!.partial && topic[key]!.total > 0 ? "≥" : ""}${format(topic[key]!.total, 0)}`
                : "—"}
            </dd>
          </div>
        ))}
      </dl>
      {(topic.money?.total ?? 0) > 0 && (
        <p className="border-t pt-2 text-xs" data-testid="trend-preview-money">
          <span className="font-semibold">{t("study.live.preview.money")}</span>{" "}
          {moneyTitle(topic.money!, t)}
        </p>
      )}
      <div className="space-y-1 border-t pt-2">
        <dl className="flex flex-wrap justify-between gap-x-4 gap-y-1 text-xs">
          <div className="flex items-baseline gap-1.5">
            <dt className="text-muted-foreground">{t("study.live.preview.groups")}</dt>
            <dd className="font-mono tabular-nums">{count(topic.groups)}</dd>
          </div>
          {change && (
            <div className="flex items-baseline gap-1.5">
              <dt className="text-muted-foreground">{t("study.live.preview.newGroups")}</dt>
              <dd className="font-mono tabular-nums">{count(change.fresh)}</dd>
            </div>
          )}
        </dl>
        {change && !topic.comparison && (
          <p className="text-right text-caption text-muted-foreground">{ranges.recent}</p>
        )}
        {last && !last.complete && (
          <p className="text-caption text-muted-foreground">
            {t("study.live.preview.current", {
              from: formatDate(last.start),
              to: formatDate(observedAt),
            })}
          </p>
        )}
      </div>
    </div>
  );
}
