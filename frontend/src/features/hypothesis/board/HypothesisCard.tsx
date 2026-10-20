import { Layers, Target } from "lucide-react";
import { useTranslation } from "react-i18next";

import type { ApiHypothesis } from "@/features/hypothesis/api";
import {
  displayHypothesisTitle,
  hasProjectedTheme,
  priorityScore,
} from "@/features/hypothesis/model";
import { evidenceStats, originMeta, verdictMeta } from "@/features/hypothesis/board/model";
import { SchemaCompleteness } from "@/features/hypothesis/ui/HypothesisInsights";
import { Badge } from "@/shared/ui/Badge";
import { CardButton, CardContent } from "@/shared/ui/Card";
import { SegmentBar } from "@/shared/ui/SegmentBar";
import { EvidenceTally } from "@/shared/ui/EvidenceTally";
import { cn } from "@/shared/lib/cn";

export function HypothesisCard({
  h,
  kpiTitle,
  clusterTitle,
  owner,
  onOpen,
}: {
  h: ApiHypothesis;
  kpiTitle?: string;
  /** null — тема известна, а подпись ещё едет: пустая строка держит место под неё. */
  clusterTitle?: string | null;
  owner?: string | null;
  onOpen: () => void;
}) {
  const { t } = useTranslation("hypothesis");
  const verdict = verdictMeta(h);
  const origin = originMeta(h);
  const evidence = evidenceStats(h);
  const title = displayHypothesisTitle(h);
  const rank = priorityScore(h);
  const hasCluster = !hasProjectedTheme(h) && clusterTitle !== undefined;
  return (
    <CardButton onClick={onOpen} className="flex flex-col">
      <CardContent className="flex flex-1 flex-col gap-3 p-4">
        <div className="flex items-start justify-between gap-3">
          <div className="flex min-w-0 flex-wrap items-center gap-1.5">
            {verdict ? (
              <Badge variant={verdict.tone}>{verdict.label}</Badge>
            ) : (
              <Badge variant="secondary">{t("verdicts.unverified")}</Badge>
            )}
          </div>
          {rank !== null && (
            <div className="shrink-0 text-right">
              <p className="mb-1 text-xs text-muted-foreground">{t("hreport.p.priority")}</p>
              <span className="font-mono text-base leading-none font-semibold tabular-nums">
                {rank}
              </span>
              <span className="font-mono text-xs text-muted-foreground">/100</span>
              <SegmentBar
                className="mt-1 justify-end"
                value={Math.round((rank / 100) * 9)}
                tone="brand"
                label={t("card.ratingAria", { rank })}
              />
            </div>
          )}
        </div>

        <div className="min-w-0">
          <h3 className="text-base leading-snug font-semibold wrap-anywhere">{title}</h3>
          <p className="mt-2 text-sm leading-relaxed wrap-anywhere text-muted-foreground">
            {h.statement}
          </p>
        </div>

        {h.assessment?.schema && <SchemaCompleteness schema={h.assessment.schema} />}

        <EvidenceTally
          className="mt-auto"
          supports={evidence.supports}
          contradicts={evidence.contradicts}
          mentions={evidence.context}
          verified={verdict !== null}
        />

        {(origin || owner) && (
          <div className="flex flex-wrap gap-x-3 gap-y-1 text-xs text-muted-foreground">
            {origin && <span>{origin.label}</span>}
            {owner && <span>{t("card.owner", { name: owner })}</span>}
          </div>
        )}

        {(kpiTitle || hasCluster) && (
          <div
            className={cn(
              "grid gap-1 border-t pt-2.5 text-xs text-muted-foreground",
              kpiTitle && hasCluster && "grid-rows-2",
            )}
          >
            {kpiTitle && (
              <span className="flex min-w-0 items-center gap-1.5">
                <Target className="size-3.5 shrink-0" aria-hidden />
                <span className="truncate">{kpiTitle}</span>
              </span>
            )}
            {hasCluster && (
              <span className="flex min-h-4 min-w-0 items-center gap-1.5">
                <Layers className="size-3.5 shrink-0" aria-hidden />
                <span className="truncate">{clusterTitle}</span>
              </span>
            )}
          </div>
        )}
      </CardContent>
    </CardButton>
  );
}
