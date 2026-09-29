import { Link, useLocation } from "react-router-dom";
import { useTranslation } from "react-i18next";
import { sourcePath } from "@/features/document";
import { Button } from "@/shared/ui/Button";
import { Section } from "@/shared/ui/Section";
import { Notice } from "@/shared/ui/Notice";
import { useAnalysis } from "../useAnalysis";
import type { AnalysisJob } from "../trendApi";

function ImplementationLinks({ job, workIDs }: { job: AnalysisJob; workIDs: string[] }) {
  const { t } = useTranslation("trend");
  const location = useLocation();
  const result = job.result!;
  const links = result.relations.filter(
    (link) => workIDs.includes(link.paper_id) || workIDs.includes(link.repository_id),
  );
  const sourceFor = (workID: string) => {
    const work = result.works.find((item) => item.id === workID);
    return job.sources.find((source) => work?.document_ids.includes(source.document_id));
  };
  return (
    <div className="space-y-4">
      <p className="max-w-prose text-sm text-muted-foreground">{t("relationHint")}</p>
      {!links.length && <p className="text-sm">{t("noRelations")}</p>}
      <ul className="divide-y">
        {links.map((link) => (
          <li key={link.id} className="space-y-3 py-4">
            <div className="flex flex-col items-start gap-2">
              {[link.paper_id, link.repository_id].map((workID) => {
                const source = sourceFor(workID);
                if (!source) return null;
                const id = `relation-${link.id}-${source.document_id}`;
                return (
                  <Button key={workID} variant="link" size="inline" asChild>
                    <Link
                      id={id}
                      to={sourcePath(source.document_id, source.snapshot_id, undefined, {
                        analysis: job.id,
                        returnTo: location.pathname.startsWith("/trends")
                          ? location.pathname + location.search + `#${id}`
                          : undefined,
                      })}
                    >
                      {source.source.provider === "github"
                        ? "GitHub"
                        : source.source.provider === "arxiv"
                          ? "arXiv"
                          : source.source.provider}
                      {" · "}
                      {source.title || t("sourceUntitled")}
                    </Link>
                  </Button>
                );
              })}
            </div>
            {link.evidence.slice(0, 1).map((proof) => (
              <div key={`${proof.document_id}:${proof.char_start}`}>
                <p className="mb-1 text-sm wrap-anywhere">{proof.quote}</p>
                <Button variant="link" size="inline" asChild>
                  <Link
                    to={sourcePath(proof.document_id, proof.snapshot_id, undefined, {
                      anchor: {
                        start: proof.char_start,
                        end: proof.char_end,
                        textHash: proof.text_sha256,
                      },
                      analysis: job.id,
                      returnTo: location.pathname.startsWith("/trends")
                        ? location.pathname + location.search
                        : undefined,
                    })}
                  >
                    {t("relationEvidence")}
                  </Link>
                </Button>
              </div>
            ))}
          </li>
        ))}
      </ul>
    </div>
  );
}

export function SourceAnalysisRelations({
  analysis,
  documentID,
}: {
  analysis: string;
  documentID: string;
}) {
  const { t } = useTranslation("trend");
  const { job, error } = useAnalysis(analysis);
  if (!analysis) return null;
  if (error) return <Notice>{t("unavailable")}</Notice>;
  if (!job?.result) return null;
  const works = job.result.works
    .filter((work) => work.document_ids.includes(documentID))
    .map((work) => work.id);
  return (
    <Section title={t("relations")}>
      <ImplementationLinks job={job} workIDs={works} />
    </Section>
  );
}
