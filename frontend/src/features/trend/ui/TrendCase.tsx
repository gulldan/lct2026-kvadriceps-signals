import { Link, useLocation } from "react-router-dom";
import { useTranslation } from "react-i18next";
import { sourcePath } from "@/features/document";
import { formatDate, formatNumber } from "@/shared/lib/format";
import { Badge } from "@/shared/ui/Badge";
import { Button } from "@/shared/ui/Button";
import { originOf } from "../providerLabel";
import type { AnalysisJob, TrendCandidate } from "../trendApi";
import type { DescriptionFields } from "../descriptionApi";
import { TrendBrief } from "./TrendBrief";

const KIND_ORDER = ["company_material", "software_repository", "paper", "patent", "analysis"];

export function TrendCase({
  job,
  candidate,
  description,
}: {
  job: AnalysisJob;
  candidate: TrendCandidate;
  description?: DescriptionFields;
}) {
  const { t } = useTranslation("trend");
  const { t: documentT } = useTranslation("document");
  const location = useLocation();
  const result = job.result!;
  const rows = result.works
    .filter((work) => candidate.work_ids.includes(work.id))
    .flatMap((work) =>
      job.sources
        .filter((source) => work.document_ids.includes(source.document_id))
        .map((source) => ({ work, source })),
    )
    .toSorted(
      (a, b) =>
        KIND_ORDER.indexOf(a.source.source.source_kind) -
        KIND_ORDER.indexOf(b.source.source.source_kind),
    );
  const relationFor = (workID: string) =>
    result.relations.find(
      (link) => link.repository_id === workID && candidate.work_ids.includes(link.paper_id),
    );
  return (
    <div className="space-y-5">
      <TrendBrief job={job} candidate={candidate} description={description} fields={["example"]} />
      <div className="space-y-2">
        <h3 className="text-sm font-medium">{t("topic.leads")}</h3>
        {!rows.length && <p className="text-sm text-muted-foreground">{t("topic.noLeads")}</p>}
        <ul className="divide-y">
          {rows.map(({ work, source }) => {
            const meta = source.source;
            const relation = relationFor(work.id);
            const id = `case-${source.document_id}`;
            const proof = relation?.evidence[0];
            return (
              <li key={source.document_id} className="space-y-1 py-3 first:pt-0">
                <div className="flex min-w-0 flex-wrap items-center gap-2">
                  <Badge variant="outline">{documentT(`source.kind.${meta.source_kind}`)}</Badge>
                  <Button variant="link" size="inline" asChild>
                    <Link
                      id={id}
                      className="min-w-0 wrap-anywhere"
                      to={sourcePath(source.document_id, source.snapshot_id, undefined, {
                        analysis: job.id,
                        returnTo: location.pathname + location.search + `#${id}`,
                      })}
                    >
                      {source.title || t("sourceUntitled")}
                    </Link>
                  </Button>
                </div>
                <p className="text-xs wrap-anywhere text-muted-foreground">
                  {originOf(meta.url, meta.provider)}
                  {" · "}
                  {meta.publication?.value || t("unknown")}
                  {meta.github?.stars !== null && meta.github?.stars !== undefined && (
                    <>
                      {" · "}
                      {t("stars", {
                        count: formatNumber(meta.github.stars),
                        date: formatDate(meta.observed_at),
                      })}
                    </>
                  )}
                </p>
                {relation && proof && (
                  <p className="text-xs text-muted-foreground">
                    {t("topic.implements")}
                    {relation.official && ` · ${t("topic.official")}`}
                    {" · "}
                    <Button variant="link" size="inline" asChild>
                      <Link
                        to={sourcePath(proof.document_id, proof.snapshot_id, undefined, {
                          anchor: {
                            start: proof.char_start,
                            end: proof.char_end,
                            textHash: proof.text_sha256,
                          },
                          analysis: job.id,
                          returnTo: location.pathname + location.search + `#${id}`,
                        })}
                      >
                        {t("relationEvidence")}
                      </Link>
                    </Button>
                  </p>
                )}
              </li>
            );
          })}
        </ul>
      </div>
    </div>
  );
}
