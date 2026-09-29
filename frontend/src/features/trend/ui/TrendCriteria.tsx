import type { ReactNode } from "react";
import { Link, useLocation } from "react-router-dom";
import { useTranslation } from "react-i18next";
import { sourcePath } from "@/features/document";
import { formatNumber } from "@/shared/lib/format";
import { Badge } from "@/shared/ui/Badge";
import { Button } from "@/shared/ui/Button";
import { briefFields, MIN_BRIEF_FIELDS, type AnalysisJob, type TrendCandidate } from "../trendApi";

type Level = "ok" | "warn" | "unknown";

const REASONS = [
  "comparable_coverage_missing",
  "established_scientific_history",
  "no_recent_scientific_support",
  "scientific_date_uncertainty",
  "scientific_first_mention_unknown",
  "scientific_share_not_increasing",
  "scientific_count_not_increasing",
  "too_few_dated_scientific_works",
  "too_few_scientific_periods",
] as const;
type Reason = (typeof REASONS)[number];

interface Rule {
  key: "first" | "volume" | "activity" | "growth" | "recent" | "relevance" | "brief";
  value: ReactNode;
  basis: ReactNode;
  level: Level;
  reason?: Reason;
}

const verdict = (reason: Reason | undefined, known: boolean): Pick<Rule, "level" | "reason"> =>
  reason ? { level: "warn", reason } : { level: known ? "ok" : "unknown" };

export function TrendCriteria({ job, candidate }: { job: AnalysisJob; candidate: TrendCandidate }) {
  const { t } = useTranslation("trend");
  const location = useLocation();
  const reasons = new Set(candidate.assessment.reasons);
  const features = candidate.assessment.features;
  const emergence = job.method.parameters.emergence;
  const counts = candidate.assessment.basis === "observed_counts";
  const number = (value: number | null | undefined) =>
    typeof value === "number" ? formatNumber(value, { maximumFractionDigits: 3 }) : t("unknown");
  const param = (key: string) =>
    typeof emergence[key] === "number" ? formatNumber(emergence[key]) : "—";
  const failed = (...codes: Reason[]) => codes.find((code) => reasons.has(code));
  const first = candidate.measurements.earliest;
  const growthValue = counts ? features.log_count_ratio : features.log_rate_ratio;
  const recentValue = counts ? features.recent_found_works : features.recent_rate;
  const briefCount = briefFields(candidate);
  const uncertain = failed("scientific_first_mention_unknown", "scientific_date_uncertainty");
  const noCoverage = failed("comparable_coverage_missing");
  const rules: Rule[] = [
    {
      key: "first",
      value: first
        ? `${first.date.value} · ${t(`precision_${first.date.precision}`)}`
        : t("unknown"),
      basis: (
        <>
          {t("topic.basis.first", { count: param("max_first_age_quarters") })}
          {first && (
            <>
              {" "}
              <Button variant="link" size="inline" asChild>
                <Link
                  id="earliest-source"
                  to={sourcePath(first.document_id, first.snapshot_id, undefined, {
                    analysis: job.id,
                    returnTo: location.pathname + location.search + "#earliest-source",
                  })}
                >
                  {t("sources")}
                </Link>
              </Button>
            </>
          )}
        </>
      ),
      ...(uncertain
        ? { level: "unknown", reason: uncertain }
        : verdict(failed("established_scientific_history"), !!first)),
    },
    {
      key: "volume",
      value: number(features.dated_scientific_works),
      basis: t("topic.basis.volume", { count: param("min_scientific_works") }),
      ...verdict(
        failed("too_few_dated_scientific_works"),
        typeof features.dated_scientific_works === "number",
      ),
    },
    {
      key: "activity",
      value: number(features.active_scientific_quarters),
      basis: t("topic.basis.activity", { count: param("min_active_quarters") }),
      ...verdict(
        failed("too_few_scientific_periods"),
        typeof features.active_scientific_quarters === "number",
      ),
    },
    {
      key: "growth",
      value: number(growthValue),
      basis: t("topic.basis.growth", {
        recent: param("recent_quarters"),
        prior: param("prior_quarters"),
        smoothing: param("smoothing"),
      }),
      ...(noCoverage
        ? { level: "unknown", reason: noCoverage }
        : verdict(
            failed("scientific_share_not_increasing", "scientific_count_not_increasing"),
            typeof growthValue === "number",
          )),
    },
    {
      key: "recent",
      value: number(recentValue),
      basis: t("topic.basis.recent", { count: param("min_recent_works") }),
      ...verdict(failed("no_recent_scientific_support"), typeof recentValue === "number"),
    },
    {
      key: "relevance",
      value:
        candidate.relevance.semantic_similarity === null
          ? t("unknown")
          : formatNumber(candidate.relevance.semantic_similarity, { maximumFractionDigits: 2 }),
      basis: t("topic.basis.relevance", {
        overlap: formatNumber(candidate.relevance.lexical_overlap, { maximumFractionDigits: 2 }),
      }),
      level: candidate.relevance.relevant ? "ok" : "warn",
    },
    {
      key: "brief",
      value: `${briefCount} / 3`,
      basis: t("topic.basis.brief", { count: briefCount, min: MIN_BRIEF_FIELDS }),
      level: briefCount >= MIN_BRIEF_FIELDS ? "ok" : "warn",
    },
  ];
  const other = [...reasons].filter((code) => !REASONS.includes(code as Reason));
  return (
    <div className="space-y-3">
      <ul className="divide-y border-t">
        {rules.map((rule) => (
          <li
            key={rule.key}
            className="grid gap-x-4 gap-y-1 py-3 sm:grid-cols-[10rem_minmax(0,1fr)_auto] sm:items-start"
          >
            <span className="text-sm font-medium">{t(`topic.rules.${rule.key}`)}</span>
            <span className="min-w-0 text-sm leading-6">
              <span className="font-mono text-sm wrap-anywhere tabular-nums">{rule.value}</span>
              <span className="block text-xs leading-5 text-muted-foreground">
                {rule.basis}
                {rule.reason && ` ${t(rule.reason)}`}
              </span>
            </span>
            <Badge
              variant={rule.level === "unknown" ? "muted" : rule.level}
              className="justify-self-start sm:justify-self-end"
            >
              {t(`topic.level.${rule.level}`)}
            </Badge>
          </li>
        ))}
      </ul>
      {other.length > 0 && (
        <p className="text-xs text-muted-foreground">
          {t("topic.otherReasons", { reasons: other.join(", ") })}
        </p>
      )}
      <p className="text-xs text-muted-foreground">{t("topic.emergenceMap")}</p>
    </div>
  );
}
