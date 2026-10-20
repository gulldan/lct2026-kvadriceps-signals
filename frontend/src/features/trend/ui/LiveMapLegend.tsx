import { useTranslation } from "react-i18next";
import { InfoHint } from "@/shared/ui/InfoHint";
import { LegendItem } from "@/shared/ui/LegendItem";
import type { LiveOverview, LiveTopic } from "../liveApi";
import { comparisonRanges, growthTones, publicationChange } from "../liveMapMetrics";

export function LiveMapLegend({
  topics,
  periods,
}: {
  topics: LiveTopic[];
  periods: LiveOverview["periods"];
}) {
  const { t, i18n } = useTranslation("trend");
  const ranges = comparisonRanges(topics[0], periods, i18n.language);
  const changes = topics.flatMap((topic) => {
    const change = publicationChange(topic, periods);
    return change ? [change] : [];
  });
  const fresh = changes.map((change) => change.fresh);
  const funded = topics.filter((topic) => (topic.money?.total ?? 0) > 0).length;
  return (
    <div className="mt-2 flex flex-wrap items-center gap-x-5 gap-y-2">
      {/* Не общий formatDate: в карточке предпросмотра шириной 320 px полные месяцы её рвут. */}
      <p className="w-full text-sm text-muted-foreground">
        {t("study.live.map.basis", {
          recent: ranges.recent,
          prior: ranges.prior.replace(/\.$/, ""),
        })}
      </p>
      <div className="flex flex-wrap items-center gap-1.5 text-xs">
        <span className="inline-flex items-end gap-0.5" aria-hidden>
          <span className="size-1.5 rounded-full bg-foreground" />
          <span className="size-3 rounded-full bg-foreground" />
        </span>
        <span>{t("study.live.map.size")}</span>
        {fresh.length > 0 && (
          <InfoHint label={t("study.live.map.groupsHelp")}>
            {t("study.live.map.groupsGuide", {
              min: Math.min(...fresh),
              max: Math.max(...fresh),
              period: ranges.recent,
            })}
          </InfoHint>
        )}
      </div>
      {funded > 0 && (
        <div className="flex items-center gap-1.5 text-xs" data-testid="trend-money-legend">
          <span aria-hidden className="size-3 rounded-full outline-2 outline-map-money-4" />
          <span>{t("study.live.map.money")}</span>
          <span className="font-mono text-muted-foreground tabular-nums">{funded}</span>
        </div>
      )}
      <div
        className="flex flex-wrap items-center gap-x-4 gap-y-2 text-xs"
        data-testid="trend-growth-legend"
      >
        <span className="font-semibold">{t("study.live.map.color")}</span>
        {(Object.keys(growthTones) as (keyof typeof growthTones)[]).map((band) => {
          const count = changes.filter((change) => change.band === band).length;
          return count > 0 ? (
            <LegendItem key={band} marker={growthTones[band].marker} size="md">
              {t(`study.live.map.bands.${band}`)}
              <span className="font-mono text-muted-foreground tabular-nums">{count}</span>
            </LegendItem>
          ) : null;
        })}
      </div>
    </div>
  );
}
