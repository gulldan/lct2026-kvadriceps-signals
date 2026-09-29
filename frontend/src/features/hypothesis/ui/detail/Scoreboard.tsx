import { CardButton, Card, CardContent } from "@/shared/ui/Card";
import { badgeTones } from "@/shared/design-system/tones";
import { useEffect, useRef, useState } from "react";
import type { ReactNode } from "react";
import { useTranslation } from "react-i18next";
import {
  ChevronRight,
  Cpu,
  Gauge,
  Lightbulb,
  Milestone,
  Sparkles,
  TrendingUp,
  Trophy,
  ShieldCheck,
} from "lucide-react";
import type { LucideIcon } from "lucide-react";

import type { ApiHypothesis, Verdict } from "@/features/hypothesis/api";
import { pct, trlMeta } from "@/features/hypothesis/model";
import { radarStageCode, stageReason, tzStageLabel } from "@/features/hypothesis/betPassport";
import { ConfidenceSplit, RankingExplainer } from "@/features/hypothesis/ui/HypothesisInsights";
import { ConsensusMeter } from "@/features/hypothesis/ui/detail/ConsensusMeter";
import { ItcSummary } from "@/features/hypothesis/ui/Itc";
import { formatItcIndex, itcIndex } from "@/features/hypothesis/itcProfile";
import { cn } from "@/shared/lib/cn";
import { formatDateShort } from "@/shared/lib/format";
import { GLOSSARY } from "@/shared/glossary";
import { AiNote } from "@/shared/ui/AiNote";
import { Badge } from "@/shared/ui/Badge";
import { Button } from "@/shared/ui/Button";
import { InfoHint } from "@/shared/ui/InfoHint";
import { Sheet, SheetContent, SheetDescription, SheetHeader, SheetTitle } from "@/shared/ui/Sheet";
import { ScrollArea } from "@/shared/ui/ScrollArea";

const VERDICT_META: Record<Verdict, { labelKey: `verdicts.${Verdict}`; className: string }> = {
  supported: { labelKey: "verdicts.supported", className: badgeTones.ok },
  refuted: { labelKey: "verdicts.refuted", className: badgeTones.risk },
  mixed: { labelKey: "verdicts.mixed", className: badgeTones.warn },
  insufficient: {
    labelKey: "verdicts.insufficient",
    className: badgeTones.secondary,
  },
};

const LEARNING_FACETS = [
  { key: "value", labelKey: "learning.value", hintKey: "learning.valueHint" },
  { key: "uncertainty", labelKey: "learning.uncertainty", hintKey: "learning.uncertaintyHint" },
  {
    key: "evidence_quality",
    labelKey: "learning.evidenceGap",
    hintKey: "learning.evidenceGapHint",
  },
] as const;

type TileKey =
  | "priority"
  | "verification"
  | "novelty"
  | "freshness"
  | "trl"
  | "tz"
  | "itc"
  | "learning";

interface Tile {
  key: TileKey;
  label: string;
  icon: LucideIcon;
  hint?: string;
  value: ReactNode;
  chip: ReactNode;
  visual?: ReactNode;
  detail?: ReactNode;
  accent?: boolean;
}

function TileNumber({ text, unit }: { text: string | number; unit?: string }) {
  return (
    <span className="flex items-baseline gap-1">
      <span className="text-2xl leading-none font-semibold tabular-nums">{text}</span>
      {unit && <span className="text-xs text-muted-foreground">{unit}</span>}
    </span>
  );
}

function TileBar({ value }: { value: number }) {
  return (
    <span className="h-1 w-full overflow-hidden rounded-full bg-muted" aria-hidden>
      <span
        className="block h-full rounded-full bg-brand"
        style={{ width: `${Math.max(0, Math.min(100, value))}%` }}
      />
    </span>
  );
}

function TileDots({ value, max }: { value: number | null; max: number }) {
  return (
    <span className="flex w-full gap-0.5" aria-hidden>
      {Array.from({ length: max }, (_, i) => i + 1).map((level) => (
        <span
          key={level}
          className={cn(
            "h-1 flex-1 rounded-full",
            value !== null && level <= value ? "bg-brand" : "bg-muted",
          )}
        />
      ))}
    </span>
  );
}

export function Scoreboard({
  hypothesis,
  title,
  refining,
  onRefine,
}: {
  hypothesis: ApiHypothesis;
  title: string;
  refining: boolean;
  onRefine: () => void;
}) {
  const { t } = useTranslation("hypothesisDetail");
  const [sheetKey, setSheetKey] = useState<TileKey | null>(null);
  const [condensed, setCondensed] = useState(false);
  const [box, setBox] = useState<{ left: number; width: number } | null>(null);
  const cardRef = useRef<HTMLDivElement>(null);
  const sectionRefs = useRef<Partial<Record<TileKey, HTMLElement | null>>>({});

  useEffect(() => {
    const el = cardRef.current;
    if (!el) {
      return;
    }
    const io = new IntersectionObserver(([entry]) => setCondensed(!entry?.isIntersecting), {
      threshold: 0,
    });
    io.observe(el);
    const measure = () => {
      const r = el.getBoundingClientRect();
      setBox({ left: r.left, width: r.width });
    };
    measure();
    const ro = new ResizeObserver(measure);
    ro.observe(el);
    window.addEventListener("resize", measure);
    return () => {
      io.disconnect();
      ro.disconnect();
      window.removeEventListener("resize", measure);
    };
  }, []);

  useEffect(() => {
    if (sheetKey === null) {
      return;
    }
    const id = window.setTimeout(() => {
      sectionRefs.current[sheetKey]?.scrollIntoView({ block: "start" });
    }, 60);
    return () => window.clearTimeout(id);
  }, [sheetKey]);

  const a = hypothesis.assessment;
  const check = a.check;
  const scores = a.scores;
  const ranking = a.ranking;
  const learning = a.learning_priority;
  const itc = a.itc;
  const trl = trlMeta(hypothesis.trl);
  const verdict = check?.verdict
    ? (VERDICT_META[check.verdict] ?? VERDICT_META.insufficient)
    : null;

  const evFor = hypothesis.evidence.filter((e) => e.stance === "supports").length;
  const evAgainst = hypothesis.evidence.filter((e) => e.stance === "contradicts").length;
  const forCount = check?.supporting?.length ?? evFor;
  const againstCount = check?.contradicting?.length ?? evAgainst;
  const basis = check
    ? [
        forCount + againstCount > 0
          ? t("verification.checkFragments", { count: forCount + againstCount })
          : null,
        check.checked_at
          ? t("verification.checkedAt", { date: formatDateShort(check.checked_at) })
          : null,
      ]
        .filter(Boolean)
        .join(" · ")
    : "";

  const noveltyRationale = (a.novelty?.rationale ?? "").trim();
  const freshnessRationale = (a.topic_freshness?.rationale ?? "").trim();

  const tiles: Tile[] = [];

  if (ranking && (ranking.factors?.length ?? 0) > 0) {
    const score = Math.round((ranking.score ?? 0) * 100);
    tiles.push({
      key: "priority",
      label: t("ranking.priority"),
      icon: Trophy,
      hint: GLOSSARY.ranking,
      value: <TileNumber text={score} unit="/100" />,
      chip: <span className="font-medium tabular-nums">{score}</span>,
      visual: <TileBar value={score} />,
      detail: <RankingExplainer ranking={ranking} />,
      accent: true,
    });
  }

  const verdictBadge = verdict ? (
    <Badge className={cn("border-transparent", verdict.className)}>{t(verdict.labelKey)}</Badge>
  ) : null;
  tiles.push({
    key: "verification",
    label: t("panels.verification"),
    icon: ShieldCheck,
    hint: GLOSSARY.verify,
    value: verdictBadge ?? (
      <span className="text-sm leading-6 font-medium text-muted-foreground">
        {t("scoreboard.queued")}
      </span>
    ),
    chip: verdictBadge ?? <span className="text-muted-foreground">{t("scoreboard.queued")}</span>,
    visual:
      forCount + againstCount > 0 ? (
        <span className="flex items-center gap-1.5 text-caption text-muted-foreground tabular-nums">
          <span className="size-1.5 shrink-0 rounded-full bg-ok" aria-hidden />
          {t("scoreboard.forAgainst", { for: forCount, against: againstCount })}
        </span>
      ) : undefined,
    detail: (
      <div className="space-y-3">
        {verdict ? (
          <>
            {scores && <ConfidenceSplit scores={scores} />}
            <ConsensusMeter forCount={forCount} againstCount={againstCount} />
            {check?.rationale && (
              <p className="text-xs leading-relaxed text-muted-foreground">{check.rationale}</p>
            )}
            {basis && (
              <p className="text-caption text-muted-foreground">
                {t("verification.basedOn", { basis })}
              </p>
            )}
            <AiNote>{t("verification.aiNote")}</AiNote>
          </>
        ) : scores ? (
          <ConfidenceSplit scores={scores} />
        ) : (
          <p className="text-xs text-muted-foreground">{t("verification.queued")}</p>
        )}
        <div className="flex flex-wrap items-center justify-between gap-2 border-t pt-3">
          <p className="text-xs text-muted-foreground">
            {verdict ? t("verification.doneNote") : t("verification.pendingNote")}
          </p>
          <Button size="sm" variant="outline" disabled={refining} onClick={onRefine}>
            <Sparkles className="size-3.5" aria-hidden />
            {refining ? t("actions.refining") : t("actions.refine")}
          </Button>
        </div>
      </div>
    ),
  });

  if (typeof a.novelty?.score === "number" || noveltyRationale !== "") {
    tiles.push({
      key: "novelty",
      label: t("novelty.title"),
      icon: Sparkles,
      value: <TileNumber text={pct(a.novelty?.score)} />,
      chip: <span className="font-medium tabular-nums">{pct(a.novelty?.score)}</span>,
      visual:
        typeof a.novelty?.score === "number" ? (
          <TileBar value={Math.round(a.novelty.score * 100)} />
        ) : undefined,
      detail:
        noveltyRationale !== "" ? (
          <p className="text-sm leading-relaxed text-muted-foreground">{noveltyRationale}</p>
        ) : undefined,
    });
  }

  if (typeof a.topic_freshness?.score === "number" || freshnessRationale !== "") {
    tiles.push({
      key: "freshness",
      label: t("freshness.title"),
      icon: TrendingUp,
      value: <TileNumber text={pct(a.topic_freshness?.score)} />,
      chip: <span className="font-medium tabular-nums">{pct(a.topic_freshness?.score)}</span>,
      visual:
        typeof a.topic_freshness?.score === "number" ? (
          <TileBar value={Math.round(a.topic_freshness.score * 100)} />
        ) : undefined,
      detail:
        freshnessRationale !== "" ? (
          <p className="text-sm leading-relaxed text-muted-foreground">{freshnessRationale}</p>
        ) : undefined,
    });
  }

  tiles.push({
    key: "trl",
    label: t("scoreboard.trl"),
    icon: Gauge,
    hint: GLOSSARY.trl,
    value: <TileNumber text={hypothesis.trl ?? "—"} unit={t("scoreboard.outOf", { max: 9 })} />,
    chip: (
      <span className="font-medium tabular-nums">
        {t("scoreboard.trlChip", { level: hypothesis.trl ?? "—" })}
      </span>
    ),
    visual: <TileDots value={hypothesis.trl} max={9} />,
    detail: (
      <div className="space-y-2">
        <p className="text-sm font-medium">{trl.label}</p>
        <p className="text-xs text-muted-foreground">{a.trl?.name ?? t("trl.scaleName")}</p>
        {(a.trl?.rationale ?? "") !== "" && (
          <p className="text-xs leading-relaxed text-muted-foreground">{a.trl?.rationale}</p>
        )}
        {a.trl?.levels && a.trl.levels.length > 0 && (
          <ul className="space-y-0.5 text-xs">
            {a.trl.levels.map((l) => (
              <li key={l.level} className={l.met ? "text-ok" : "text-muted-foreground"}>
                {l.met ? "✓" : "—"} {t("trl.level", { level: l.level })}
                {l.note ? `: ${l.note}` : ""}
              </li>
            ))}
          </ul>
        )}
        <p className="text-xs text-muted-foreground">
          {hypothesis.trl === null ? t("trl.autoPendingNote") : t("trl.autoDoneNote")}
        </p>
      </div>
    ),
  });

  const radar = hypothesis.detail.radar;
  if (radar) {
    const stage = radarStageCode(radar);
    tiles.push({
      key: "tz",
      label: t("scoreboard.tz"),
      icon: Milestone,
      value: <TileNumber text={stage || "—"} unit={t("scoreboard.outOf", { max: 4 })} />,
      chip: (
        <span className="font-medium tabular-nums">
          {t("scoreboard.tzChip", { level: stage || "—" })}
        </span>
      ),
      visual: <TileDots value={stage || null} max={4} />,
      detail: (
        <div className="space-y-2">
          <ul className="space-y-0.5 text-xs">
            {[1, 2, 3, 4].map((code) => (
              <li
                key={code}
                className={code === stage ? "font-medium text-foreground" : "text-muted-foreground"}
              >
                {tzStageLabel(code)}
              </li>
            ))}
          </ul>
          {radar.tz?.stage_basis && (
            <p className="text-xs leading-relaxed text-muted-foreground">
              {stageReason(radar.tz.stage_basis)}
            </p>
          )}
          <p className="text-xs text-muted-foreground">{t("scoreboard.tzNote")}</p>
        </div>
      ),
    });
  }

  if (itc) {
    tiles.push({
      key: "itc",
      label: t("scoreboard.itc"),
      icon: Cpu,
      hint: GLOSSARY.itc,
      value: <TileNumber text={formatItcIndex(itc)} unit={t("itc.outOf100")} />,
      chip: <span className="font-medium tabular-nums">{formatItcIndex(itc)}/100</span>,
      visual: <TileDots value={(itcIndex(itc) ?? 0) / 10} max={10} />,
      detail: <ItcSummary itc={itc} bare />,
    });
  }

  if (typeof learning?.score === "number") {
    const score = Math.round(learning.score * 100);
    const gap =
      typeof learning.evidence_quality === "number" ? 1 - learning.evidence_quality : undefined;
    const rows = LEARNING_FACETS.map((f) => ({
      key: f.key,
      labelKey: f.labelKey,
      hintKey: f.hintKey,
      value: f.key === "evidence_quality" ? gap : (learning[f.key] as number | undefined),
    })).filter((f) => typeof f.value === "number");
    tiles.push({
      key: "learning",
      label: t("learning.title"),
      icon: Lightbulb,
      value: <TileNumber text={score} unit="/100" />,
      chip: <span className="font-medium tabular-nums">{score}</span>,
      visual: <TileBar value={score} />,
      detail: (
        <div className="space-y-3">
          <p className="text-xs leading-relaxed text-muted-foreground">{t("learning.titleHint")}</p>
          {rows.length > 0 && (
            <div className="space-y-1.5">
              {rows.map((r) => (
                <div key={r.key} className="flex items-baseline justify-between gap-2 text-xs">
                  <span className="inline-flex items-center gap-1 text-muted-foreground">
                    {t(r.labelKey)}
                    <InfoHint label={t(r.labelKey)}>{t(r.hintKey)}</InfoHint>
                  </span>
                  <span className="font-medium text-foreground tabular-nums">{pct(r.value)}</span>
                </div>
              ))}
            </div>
          )}
        </div>
      ),
    });
  }

  if (tiles.length === 0) {
    return null;
  }

  return (
    <>
      <Card ref={cardRef}>
        <CardContent className="space-y-3 p-5">
          <div className="flex flex-wrap items-center justify-between gap-2">
            <h3 className="flex items-center gap-2">
              <Gauge className="size-4 text-muted-foreground" aria-hidden />
              <span className="kicker text-foreground">{t("scoreboard.title")}</span>
            </h3>
            <span className="text-xs text-muted-foreground">{t("scoreboard.hint")}</span>
          </div>
          <div className="grid grid-cols-2 gap-2 md:grid-cols-3 xl:[grid-template-columns:repeat(auto-fit,minmax(9.5rem,1fr))]">
            {tiles.map((tile) => (
              <CardButton
                key={tile.key}
                type="button"
                onClick={() => setSheetKey(tile.key)}
                className={cn(
                  "flex flex-col items-start justify-between gap-1.5 p-3 text-left",
                  "hover:border-brand-border hover:bg-muted",
                  tile.accent && "border-brand-border bg-brand-wash",
                )}
              >
                <span className="flex w-full items-start justify-between gap-1">
                  <span className="text-caption leading-tight font-medium tracking-wide text-muted-foreground uppercase">
                    {tile.label}
                  </span>
                  <ChevronRight className="size-3.5 shrink-0 text-muted-foreground" aria-hidden />
                </span>
                <span className="flex min-h-6 items-center">{tile.value}</span>
                {tile.visual}
              </CardButton>
            ))}
          </div>
        </CardContent>
      </Card>

      {condensed && box && (
        <div className="fixed top-0 z-40" style={{ left: box.left, width: box.width }}>
          <div className="flex h-12 items-center gap-2 rounded-b-xl border border-t-0 bg-card pr-2 pl-3 shadow-md backdrop-blur supports-[backdrop-filter]:bg-card">
            <Button
              variant="link"
              size="inline"
              type="button"
              onClick={() => window.scrollTo({ top: 0, behavior: "smooth" })}
              className="hidden min-w-0 flex-1 truncate text-left md:block"
            >
              {title}
            </Button>
            <div className="flex min-w-0 flex-1 items-center gap-0.5 overflow-x-auto md:flex-none">
              {tiles.map((tile) => (
                <Button
                  variant="ghost"
                  size="sm"
                  key={tile.key}
                  type="button"
                  title={tile.label}
                  onClick={() => setSheetKey(tile.key)}
                  className="shrink-0 gap-1 px-1.5"
                >
                  <tile.icon className="size-3.5 text-muted-foreground" aria-hidden />
                  {tile.chip}
                </Button>
              ))}
            </div>
          </div>
        </div>
      )}

      <Sheet open={sheetKey !== null} onOpenChange={(open) => !open && setSheetKey(null)}>
        <SheetContent
          side="right"
          className="flex w-full flex-col gap-0 overflow-hidden p-0 sm:w-[30rem] sm:max-w-[30rem]"
        >
          <SheetHeader className="sticky top-0 z-(--layer-sticky) border-b bg-background py-4 pr-14 pl-5 backdrop-blur">
            <SheetTitle className="flex items-center gap-2 text-base">
              <Gauge className="size-4 text-muted-foreground" aria-hidden />
              {t("scoreboard.details")}
            </SheetTitle>
            <SheetDescription className="sr-only">{t("scoreboard.hint")}</SheetDescription>
          </SheetHeader>
          <ScrollArea aria-label={t("scoreboard.details")} className="flex-1 divide-y">
            {tiles.map((tile) => (
              <section
                key={tile.key}
                ref={(el) => {
                  sectionRefs.current[tile.key] = el;
                }}
                className={cn(
                  "scroll-mt-16 space-y-3 px-5 py-4",
                  sheetKey === tile.key && "bg-brand-wash",
                )}
              >
                <div className="flex items-center justify-between gap-2">
                  <h4 className="flex items-center gap-2">
                    <tile.icon className="size-4 text-muted-foreground" aria-hidden />
                    <span className="kicker text-foreground">{tile.label}</span>
                    {tile.hint && (
                      <InfoHint label={t("panels.whatIs", { title: tile.label })}>
                        {tile.hint}
                      </InfoHint>
                    )}
                  </h4>
                  <span className="flex items-center gap-1 text-sm">{tile.chip}</span>
                </div>
                {tile.detail ?? (
                  <p className="text-xs text-muted-foreground">{t("scoreboard.noDetail")}</p>
                )}
              </section>
            ))}
          </ScrollArea>
        </SheetContent>
      </Sheet>
    </>
  );
}
