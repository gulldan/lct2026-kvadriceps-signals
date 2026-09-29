import { useTranslation } from "react-i18next";
import { briefFields, MIN_BRIEF_FIELDS, type TrendCandidate } from "../trendApi";

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

export function CandidateReasons({ candidate }: { candidate: TrendCandidate }) {
  const { t } = useTranslation("trend");
  const reasons = candidate.assessment.reasons.map((reason) =>
    t(REASONS.find((key) => key === reason) ?? "otherReason"),
  );
  if (!candidate.relevance.relevant) reasons.push(t("notRelevant"));
  if (briefFields(candidate) < MIN_BRIEF_FIELDS) reasons.push(t("incompleteBrief"));
  return (
    <ul className="space-y-1 text-sm text-muted-foreground">
      {[...new Set(reasons)].map((reason) => (
        <li key={reason}>{reason}</li>
      ))}
    </ul>
  );
}
