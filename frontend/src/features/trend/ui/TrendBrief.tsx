import { Link, useLocation } from "react-router-dom";
import { useTranslation } from "react-i18next";
import { sourcePath } from "@/features/document";
import { cn } from "@/shared/lib/cn";
import { Badge } from "@/shared/ui/Badge";
import { Button } from "@/shared/ui/Button";
import { providerLabel } from "../providerLabel";
import type { AnalysisJob, TrendCandidate } from "../trendApi";
import type { DescriptionFields } from "../descriptionApi";

const ALL_FIELDS: (keyof DescriptionFields)[] = ["problem", "advantage", "example"];

export function TrendBrief({
  job,
  candidate,
  description,
  summary,
  fields = ALL_FIELDS,
}: {
  job: AnalysisJob;
  candidate: TrendCandidate;
  description?: DescriptionFields;
  summary?: string;
  fields?: (keyof DescriptionFields)[];
}) {
  const { t } = useTranslation("trend");
  const location = useLocation();
  return (
    <dl className="space-y-5">
      {summary && (
        <div>
          <dt className="mb-1 text-sm font-medium">{t("summaryLabel")}</dt>
          <dd className="max-w-prose text-sm leading-relaxed">{summary}</dd>
        </div>
      )}
      {fields.map((field) => {
        const claim = (description ?? candidate.brief)[field];
        const evidence = candidate.evidence.filter((item) => claim?.evidence_ids.includes(item.id));
        return (
          <div key={field}>
            <dt className="mb-1 text-sm font-medium">{t(field)}</dt>
            <dd className="space-y-2">
              <p
                className={cn(
                  "max-w-prose text-sm leading-relaxed wrap-anywhere whitespace-pre-line",
                  claim && "border-l-2 border-brand-border pl-3",
                )}
              >
                {claim?.text ?? t("missingClaim")}
              </p>
              {evidence.map((item, index) => {
                const id = `evidence-${candidate.id}-${field}-${index}`;
                const source = job.sources.find((ref) => ref.document_id === item.document_id);
                return (
                  <p
                    key={item.id}
                    className="flex min-w-0 flex-wrap items-center gap-x-2 gap-y-1 text-xs text-muted-foreground"
                  >
                    <Badge variant="outline">
                      {t(item.claim_type === "company_claim" ? "companyClaim" : "researchClaim")}
                    </Badge>
                    <Button variant="link" size="inline" asChild>
                      <Link
                        id={id}
                        className="min-w-0 wrap-anywhere"
                        to={sourcePath(item.document_id, item.snapshot_id, undefined, {
                          anchor: {
                            start: item.start,
                            end: item.end,
                            textHash: item.text_sha256,
                          },
                          analysis: job.id,
                          returnTo: location.pathname + location.search + `#${id}`,
                        })}
                      >
                        {source
                          ? `${providerLabel(source.source.provider)} · ${source.title || t("sourceUntitled")}`
                          : t("sourceLink")}
                      </Link>
                    </Button>
                    {source?.source.publication && <span>· {source.source.publication.value}</span>}
                  </p>
                );
              })}
            </dd>
          </div>
        );
      })}
    </dl>
  );
}
