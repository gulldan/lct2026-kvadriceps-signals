import { useState } from "react";
import { Link, useLocation } from "react-router-dom";
import { useTranslation } from "react-i18next";
import { ArrowUpRight, ExternalLink } from "lucide-react";
import { Button } from "@/shared/ui/Button";
import { sourcePath } from "@/features/document";
import { SourcePassport } from "./SourcePassport";
import type { LivePaperPassport } from "../liveApi";
import type { StudyTopic, TrendStudy } from "../studyApi";

type SourceStudy = Pick<TrendStudy, "papers">;
type SourceTopic = Pick<StudyTopic, "paper_ids" | "evidence_ids" | "reviewed">;

export function StudySources({
  study,
  topic,
  milestones = false,
  year,
  match,
  window = "2020–2025",
  passports,
  digests = false,
  onClearYear,
}: {
  study: SourceStudy;
  topic: SourceTopic;
  milestones?: boolean;
  year?: string;
  match?: (date: string) => boolean;
  window?: string;
  passports?: Record<string, LivePaperPassport>;
  digests?: boolean;
  onClearYear?: () => void;
}) {
  const { t, i18n } = useTranslation("trend");
  const location = useLocation();
  const [limit, setLimit] = useState(8);
  const ids = milestones && topic.evidence_ids.length ? topic.evidence_ids : topic.paper_ids;
  const papers = study.papers.filter(
    (paper) =>
      ids.includes(paper.id) && (match ? match(paper.date) : !year || paper.date.startsWith(year)),
  );
  const ordered = papers.toReversed();
  // Источник, открытый после «показать ещё», должен существовать, когда читатель вернётся.
  const returnIndex = ordered.findIndex(
    (paper) => location.hash === `#study-paper-${paper.id.replaceAll(".", "_")}`,
  );
  const shown = milestones
    ? papers.slice(0, 3)
    : ordered.slice(0, Math.max(limit, returnIndex + 1));
  return (
    <section
      className="min-w-0 space-y-3 border-t pt-4"
      aria-label={t(milestones ? "study.milestones" : "study.sources")}
    >
      <div className="flex flex-wrap items-baseline justify-between gap-2">
        <h3 className="text-sm font-medium">
          {t(milestones ? "study.milestones" : "study.sourceCount", {
            count: papers.length,
            year: year ?? window,
          })}
        </h3>
        {year && (
          <Button variant="link" size="inline" onClick={onClearYear}>
            {t("study.clearYear")}
          </Button>
        )}
      </div>
      {!shown.length && <p className="text-sm text-muted-foreground">{t("study.noPapers")}</p>}
      <ol className="divide-y">
        {shown.map((paper) => {
          const id = `study-paper-${paper.id.replaceAll(".", "_")}`;
          const passport = passports?.[paper.id];
          return (
            <li
              key={paper.id}
              className="grid min-w-0 grid-cols-[4.5rem_minmax(0,1fr)] items-start gap-3 py-3 first:pt-0 last:pb-0"
            >
              <time
                dateTime={paper.date}
                className="pt-1 text-xs text-muted-foreground tabular-nums"
              >
                {paper.date}
              </time>
              <div className="min-w-0">
                {!paper.document_id ? (
                  <Button asChild variant="link" size="inline" className="text-left">
                    <a id={id} href={paper.url} target="_blank" rel="noopener noreferrer">
                      {paper.title}
                      <ExternalLink aria-hidden />
                    </a>
                  </Button>
                ) : (
                  <Button asChild variant="link" size="inline" className="text-left">
                    <Link
                      id={id}
                      to={sourcePath(paper.document_id, paper.snapshot_id, undefined, {
                        returnTo: `${location.pathname}${location.search}#${id}`,
                      })}
                    >
                      {milestones &&
                      i18n.exists(`trend:study.events.${paper.id.replaceAll(".", "_")}.name`)
                        ? t(`study.events.${paper.id.replaceAll(".", "_")}.name`, {
                            defaultValue: paper.title,
                          })
                        : paper.title}
                      <ArrowUpRight aria-hidden />
                    </Link>
                  </Button>
                )}
                {passport && (
                  <SourcePassport
                    publisherType={passport.publisher_type}
                    genre={passport.genre}
                    sourceName={passport.source_name}
                    discoverySource={passport.discovery_source}
                    trust={passport.trust}
                    trustBasis={passport.trust_basis}
                    language={passport.language}
                    languageBasis={passport.language_basis}
                  />
                )}
                {digests && passport?.digest && (
                  <p className="mt-1 text-sm leading-6 text-muted-foreground">
                    {passport.digest}
                    <span className="block text-caption">
                      {t("report.digestBy", { model: passport.digest_model || "—" })}
                    </span>
                  </p>
                )}
                {milestones &&
                  i18n.exists(`trend:study.events.${paper.id.replaceAll(".", "_")}.body`) && (
                    <p className="mt-1 text-sm leading-6 text-muted-foreground">
                      {t(`study.events.${paper.id.replaceAll(".", "_")}.body`, {
                        defaultValue: paper.title,
                      })}
                    </p>
                  )}
              </div>
            </li>
          );
        })}
      </ol>
      {!milestones && papers.length > shown.length && (
        <Button variant="outline" size="sm" onClick={() => setLimit(shown.length + 12)}>
          {t("study.moreSources", { count: papers.length - shown.length })}
        </Button>
      )}
      {milestones && <p className="text-xs text-muted-foreground">{t("study.milestonesHint")}</p>}
    </section>
  );
}
