import { Button } from "@/shared/ui/Button";
import { badgeTones } from "@/shared/design-system/tones";
import { Link } from "react-router-dom";
import { useTranslation } from "react-i18next";
import {
  ArrowLeftRight,
  Check,
  FileText,
  GitBranch,
  Radar,
  Wand2,
  X,
  BookOpen,
} from "lucide-react";

import type { ApiEvidence, ApiHypothesis, ApiRevision } from "@/features/hypothesis/api";
import { betPassportBlocks, radarSignalHref } from "@/features/hypothesis/betPassport";
import { parseGeneration } from "@/features/hypothesis/provenance";
import { radarReportBlocks } from "@/features/hypothesis/radarProvenance";
import { RadarBetCheck } from "./RadarBetCheck";
import { ReportBlocks } from "@/shared/report/ReportBlocks";
import { Panel } from "@/shared/ui/Panel";
import { Notice } from "@/shared/ui/Notice";
import { SegmentBar } from "@/shared/ui/SegmentBar";
import { cn } from "@/shared/lib/cn";

const BRIDGE_SCALES = [
  {
    key: "maverick",
    labelKey: "provenance.scales.maverick",
    hintKey: "provenance.scales.maverickHint",
  },
  {
    key: "bridgingCentrality",
    labelKey: "provenance.scales.bridging",
    hintKey: "provenance.scales.bridgingHint",
  },
  {
    key: "affinity",
    labelKey: "provenance.scales.affinity",
    hintKey: "provenance.scales.affinityHint",
  },
] as const;

function GateRow({ passed, children }: { passed: boolean | undefined; children: React.ReactNode }) {
  const ok = passed !== false;
  return (
    <li className="flex items-start gap-2 text-sm">
      <span
        className={cn(
          "mt-0.5 flex size-4 shrink-0 items-center justify-center rounded-full",
          ok ? badgeTones.ok : badgeTones.risk,
        )}
        aria-hidden
      >
        {ok ? <Check className="size-3" /> : <X className="size-3" />}
      </span>
      <span className={ok ? "" : "text-risk"}>{children}</span>
    </li>
  );
}

export function ProvenancePanel({
  hypothesis,
  revisions,
  onOpenDocument,
  onChanged,
}: {
  hypothesis: ApiHypothesis;
  revisions?: ApiRevision[];
  onOpenDocument: (documentId: string, filename?: string) => void;
  onChanged?: (updated: ApiHypothesis) => void;
}) {
  const { t } = useTranslation("hypothesisDetail");
  const gen = parseGeneration(hypothesis);

  if (gen.kind === "radar" && hypothesis.detail.radar) {
    const radar = hypothesis.detail.radar;
    return (
      <div className="space-y-5">
        <Panel
          title={t("provenance.passportTitle")}
          icon={GitBranch}
          action={
            <div className="flex flex-wrap justify-end gap-2">
              {radar.bet && <RadarBetCheck hypothesis={hypothesis} onChanged={onChanged} />}
              <Button asChild size="sm" variant="outline">
                <Link to={radarSignalHref(radar, hypothesis.kpi_id)}>
                  <Radar className="size-3.5" aria-hidden />
                  {t("provenance.openSignal")}
                </Link>
              </Button>
            </div>
          }
        >
          <ReportBlocks
            blocks={betPassportBlocks({ h: hypothesis, radar, revisions: revisions ?? [] })}
          />
        </Panel>
        <Panel title={t("provenance.radarDetails")} collapsible defaultOpen={false}>
          <ReportBlocks blocks={radarReportBlocks({ ...radar, bet: null })} />
        </Panel>
      </div>
    );
  }

  if (gen.kind === "fallback") {
    return (
      <Panel title={t("provenance.title")} icon={GitBranch}>
        <Notice>{t("provenance.fallback")}</Notice>
      </Panel>
    );
  }

  if (gen.kind === "auto_bridge") {
    const nameByDoc = new Map<string, string>();
    for (const e of hypothesis.evidence as ApiEvidence[]) {
      if (e.document_id && e.filename) nameByDoc.set(e.document_id, e.filename);
    }
    const scales = BRIDGE_SCALES.map((s) => ({
      key: s.key,
      labelKey: s.labelKey,
      hintKey: s.hintKey,
      value: gen.scores?.[s.key],
    })).filter(
      (s): s is (typeof BRIDGE_SCALES)[number] & { value: number } => typeof s.value === "number",
    );
    const conv = gen.gates?.convergence;
    const nov = gen.gates?.novelty;
    const hasGates = Boolean(gen.gates && (conv || gen.gates.grounding || nov));
    return (
      <Panel title={t("provenance.title")} icon={GitBranch}>
        <div className="space-y-3">
          <p className="text-sm text-muted-foreground">{t("provenance.bridgeIntro")}</p>
          {(gen.themeA || gen.themeB) && (
            <div className="flex flex-wrap items-center gap-2 rounded-lg border bg-brand-wash px-3 py-2.5 text-sm font-medium">
              <span className="min-w-0">{gen.themeA ?? t("provenance.themeA")}</span>
              <ArrowLeftRight className="size-4 shrink-0 text-brand" aria-hidden />
              <span className="min-w-0">{gen.themeB ?? t("provenance.themeB")}</span>
            </div>
          )}
          {gen.mediators.length > 0 && (
            <div>
              <p className="kicker text-muted-foreground">
                {t("provenance.mediators", { count: gen.mediators.length })}
              </p>
              <ul className="mt-1.5 space-y-1">
                {gen.mediators.map((docId) => (
                  <li key={docId}>
                    <Button
                      variant="link"
                      size="inline"
                      type="button"
                      onClick={() => onOpenDocument(docId, nameByDoc.get(docId))}
                      className="max-w-full text-left"
                    >
                      <FileText className="size-3.5 shrink-0" aria-hidden />
                      <span className="truncate">
                        {nameByDoc.get(docId) ?? t("provenance.openMediator")}
                      </span>
                    </Button>
                  </li>
                ))}
              </ul>
            </div>
          )}
          {(scales.length > 0 || typeof gen.scores?.convergence === "number") && (
            <div className="space-y-2">
              {scales.map((s) => (
                <div key={s.key} title={t(s.hintKey)}>
                  <div className="flex items-baseline justify-between gap-2 text-xs">
                    <span className="text-muted-foreground">{t(s.labelKey)}</span>
                    <span className="font-mono font-medium">{Math.round(s.value * 100)}</span>
                  </div>
                  <SegmentBar
                    className="mt-1"
                    value={Math.round(Math.max(0, Math.min(1, s.value)) * 9)}
                    label={t("provenance.scaleValue", {
                      label: t(s.labelKey),
                      value: Math.round(s.value * 100),
                    })}
                  />
                </div>
              ))}
              {typeof gen.scores?.convergence === "number" && (
                <p className="text-xs text-muted-foreground">
                  {t("provenance.convergenceLabel")}{" "}
                  <span className="font-mono font-medium text-foreground">
                    {gen.scores.convergence}
                  </span>{" "}
                  {t("provenance.convergencePaths", { count: gen.scores.convergence })}
                </p>
              )}
            </div>
          )}
          {hasGates && (
            <div>
              <p className="kicker text-muted-foreground">{t("provenance.gatesTitle")}</p>
              <ul className="mt-1.5 space-y-1.5">
                {conv && (
                  <GateRow passed={conv.passed}>
                    {t("provenance.gateConvergence", {
                      count: conv.actual ?? 0,
                      shown: conv.actual ?? "?",
                    })}
                    {typeof conv.required === "number" &&
                      ` ${t("provenance.gateRequired", { required: conv.required })}`}
                  </GateRow>
                )}
                {gen.gates?.grounding && (
                  <GateRow passed={gen.gates.grounding.passed}>
                    {t("provenance.gateGrounding")}
                  </GateRow>
                )}
                {nov && (
                  <GateRow passed={nov.passed}>
                    {t("provenance.gateNovelty")}
                    {typeof nov.topSim === "number" && (
                      <>
                        {t("provenance.noveltySimBefore")}{" "}
                        <span className="font-mono">{Math.round(nov.topSim * 100)} %</span>
                        {typeof nov.threshold === "number" &&
                          ` ${t("provenance.noveltyThreshold", {
                            value: Math.round(nov.threshold * 100),
                          })}`}
                      </>
                    )}
                    {nov.nearestDocId && (
                      <>
                        {" · "}
                        <Button
                          variant="link"
                          size="inline"
                          type="button"
                          onClick={() =>
                            onOpenDocument(nov.nearestDocId ?? "", nov.nearestFilename)
                          }
                          className=""
                        >
                          {nov.nearestFilename ?? t("provenance.openNearest")}
                        </Button>
                      </>
                    )}
                  </GateRow>
                )}
              </ul>
            </div>
          )}
          {gen.model && (
            <p className="font-mono text-xs text-muted-foreground">
              {t("provenance.wording", { model: gen.model })}
              {gen.promptVersion && ` · ${gen.promptVersion}`}
            </p>
          )}
        </div>
      </Panel>
    );
  }

  if (gen.kind === "auto_cluster") {
    return (
      <Panel title={t("provenance.title")} icon={GitBranch}>
        <p className="text-sm text-muted-foreground">
          {gen.clusterLabel
            ? t("provenance.clusterWithLabel", { label: gen.clusterLabel })
            : t("provenance.clusterNoLabel")}
        </p>
      </Panel>
    );
  }

  if (gen.kind === "on_demand") {
    return (
      <Panel title={t("provenance.title")} icon={Wand2}>
        <p className="text-sm text-muted-foreground">{t("provenance.onDemand")}</p>
        {gen.constraints && (
          <p className="mt-2 rounded-md bg-secondary px-3 py-2 text-sm">
            <span className="text-muted-foreground">{t("provenance.constraints")} </span>
            {gen.constraints}
          </p>
        )}
        {gen.externalWorks.length > 0 && (
          <div className="mt-3">
            <p className="kicker text-muted-foreground">
              {t("provenance.worldPractice")} · {gen.externalWorks.length}
            </p>
            <ul className="mt-1.5 space-y-1.5">
              {gen.externalWorks.map((w) => (
                <li key={w.title} className="text-sm leading-snug">
                  <BookOpen className="mr-1.5 inline size-3.5 text-brand" aria-hidden />
                  {w.doi ? (
                    <a
                      href={`https://doi.org/${w.doi}`}
                      target="_blank"
                      rel="noreferrer"
                      className="text-xs"
                    >
                      {w.title}
                    </a>
                  ) : (
                    w.title
                  )}
                  <span className="text-xs text-muted-foreground">
                    {w.year ? ` · ${w.year}` : ""}
                    {w.venue ? ` · ${w.venue}` : ""}
                  </span>
                </li>
              ))}
            </ul>
          </div>
        )}
        {gen.model && (
          <p className="font-mono text-xs text-muted-foreground">
            {t("provenance.model", { model: gen.model })}
            {gen.promptVersion && ` · ${gen.promptVersion}`}
          </p>
        )}
      </Panel>
    );
  }

  return null;
}
