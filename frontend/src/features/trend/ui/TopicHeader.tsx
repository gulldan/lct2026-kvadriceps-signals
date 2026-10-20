import type { ReactNode } from "react";
import { useTranslation } from "react-i18next";
import { AiNote } from "@/shared/ui/AiNote";
import { Badge } from "@/shared/ui/Badge";
import { InfoHint } from "@/shared/ui/InfoHint";
import { Kicker } from "@/shared/ui/Kicker";
import { PageTitle } from "@/shared/ui/PageHeader";
import { SegmentBar } from "@/shared/ui/SegmentBar";
import type { LiveTopic } from "../liveApi";
import { stageTones } from "../stageTones";
import { topicBranch } from "../liveBranches";
import { DirectionCover } from "./DirectionCover";
import { GeneratedName } from "./GeneratedName";
import { LiveLimits } from "./LiveLimits";
import { liveTopicName } from "./LiveOverview";
import { signalScore } from "../signalScore";

export function TopicHeader({
  topic,
  kicker,
  actions,
  description,
  score: showScore = true,
}: {
  topic: LiveTopic;
  kicker?: ReactNode;
  actions?: ReactNode;
  score?: boolean;
  /** undefined — абзаца нет; null — описания ещё нет, и это сказано словами. */
  description?: string | null;
}) {
  const { t } = useTranslation("trend");
  const confidence = topic.confidence;
  const score = signalScore(topic);
  const percent = score?.percent ?? null;
  const branch = topicBranch(topic);
  // У технологии из запроса нет своего списка статей на сервере — обложку не получить.
  const cover = topic.source === "curated" || topic.source === "discovered";
  const about = description ? (
    <>
      <p className="text-justify text-base leading-relaxed hyphens-auto">{description}</p>
      <AiNote>{t("study.live.description.by")}</AiNote>
    </>
  ) : (
    description === null && (
      <p className="text-caption text-muted-foreground">{t("study.live.description.pending")}</p>
    )
  );
  return (
    <header className="space-y-2">
      {kicker && <Kicker>{kicker}</Kicker>}
      <div className="flex flex-wrap items-start justify-between gap-x-6 gap-y-3">
        <div className="min-w-0 flex-1 space-y-2">
          <PageTitle as="h2" variant="object">
            {liveTopicName(topic, t)}
          </PageTitle>
          <div className="flex flex-wrap items-center gap-2">
            {/* У технологии рядом стадия по шкале ТЗ: научная стадия ей противоречила бы. */}
            {topic.source !== "candidate" && (
              <Badge variant={stageTones[topic.stage].badge}>
                {t(`study.stage.${topic.stage}`)}
              </Badge>
            )}
            {/* Без подписи область науки читали как отрасль и видели противоречие с таблицей. */}
            {branch && (
              <span className="inline-flex items-center gap-1.5">
                <span className="text-caption text-muted-foreground">
                  {t("study.live.branchLabel")}
                </span>
                <Badge variant="outline">{t(`study.live.branches.${branch}`)}</Badge>
              </span>
            )}
            {topic.source === "discovered" && (
              <span className="inline-flex items-center gap-1">
                <Badge variant="outline">{t("study.live.discoveredBadge")}</Badge>
                <InfoHint label={t("study.live.discovered")}>
                  {t("study.live.discoveredNotice")}
                </InfoHint>
              </span>
            )}
          </div>
        </div>
        <div className="flex shrink-0 items-start gap-3">
          {showScore && percent !== null && score && (
            <div className="text-right">
              <p className="text-caption text-muted-foreground">
                {t(score.model ? "confidence.modelLabel" : "confidence.label")}
              </p>
              <p className="flex items-baseline justify-end gap-1">
                {score.gap ? (
                  <Badge variant="muted">{t("report.modelClass.insufficient_data")}</Badge>
                ) : (
                  <>
                    <span className="text-2xl leading-none font-semibold tabular-nums">
                      {percent}
                    </span>
                    <span className="text-xs text-muted-foreground">%</span>
                  </>
                )}
                <InfoHint label={t(score.model ? "confidence.modelLabel" : "confidence.label")}>
                  {score.gap
                    ? t("confidence.reference", { percent })
                    : score.model
                      ? t("confidence.modelHint", {
                          threshold: Math.round((score.threshold ?? 0) * 100),
                        })
                      : confidence &&
                        t("confidence.rulesHint", {
                          passed: confidence.passed,
                          total: confidence.total,
                        })}
                </InfoHint>
              </p>
              <p className="mt-1 text-micro text-muted-foreground">
                {score.gap
                  ? t(`report.dataGap.${score.gap}`)
                  : score.model
                    ? score.threshold !== undefined &&
                      t("confidence.modelThreshold", { percent: Math.round(score.threshold * 100) })
                    : confidence &&
                      t("confidence.count", { passed: confidence.passed, total: confidence.total })}
              </p>
              {!score.gap && (
                <SegmentBar
                  className="mt-1 justify-end"
                  value={Math.round((percent / 100) * 9)}
                  tone={score.passed ? "ok" : "ink"}
                  label={t("confidence.value", { percent })}
                />
              )}
            </div>
          )}
          {actions}
        </div>
      </div>
      {cover ? (
        <div className="flex flex-col gap-4 @4xl:flex-row @4xl:items-start @4xl:gap-6">
          <div className="min-w-0 flex-1 space-y-2">{about}</div>
          <DirectionCover topicId={topic.id} />
        </div>
      ) : (
        about
      )}
      <GeneratedName topic={topic} />
      {topic.source === "search" && (
        <p className="text-xs text-muted-foreground">{t("study.live.adhoc")}</p>
      )}
      <LiveLimits limits={topic.limits} />
    </header>
  );
}
