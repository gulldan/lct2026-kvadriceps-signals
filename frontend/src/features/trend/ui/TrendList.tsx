import { Link, useLocation, useSearchParams } from "react-router-dom";
import { useTranslation } from "react-i18next";
import { formatNumber } from "@/shared/lib/format";
import { Badge } from "@/shared/ui/Badge";
import { Button } from "@/shared/ui/Button";
import { StatStrip } from "@/shared/ui/Fields";
import { Section } from "@/shared/ui/Section";
import { Disclosure } from "@/shared/ui/Disclosure";
import { Pagination } from "@/shared/ui/Pagination";
import { Notice } from "@/shared/ui/Notice";
import { CandidateReasons } from "./CandidateReasons";
import { TopicSparkline } from "./TopicSparkline";
import { stageTones } from "../stageTones";
import { trendPath, type AnalysisJob, type TrendCandidate } from "../trendApi";
import { exploreAnalysis } from "../exploration";
import type { DescriptionJob } from "../descriptionApi";

const PAGE_SIZE = 6;
const NO_DESCRIPTIONS: Record<string, DescriptionJob> = {};

export function TrendList({
  job,
  descriptions = NO_DESCRIPTIONS,
}: {
  job: AnalysisJob;
  descriptions?: Record<string, DescriptionJob>;
}) {
  const { t } = useTranslation("trend");
  const location = useLocation();
  const [params, setParams] = useSearchParams();
  const returnTo = location.pathname + location.search;
  const result = job.result!;
  const ranked = result.ranking.ordered_ids.map((id) =>
    result.candidates.find((item) => item.id === id)!,
  );
  const remaining = exploreAnalysis(job).candidates.filter(
    (item) => !result.ranking.ordered_ids.includes(item.id),
  );
  const unrelated = result.candidates.filter((item) => !item.relevance.relevant);
  const basis = ranked.some((item) => item.assessment.basis === "observed_counts")
    ? "observed_counts"
    : ranked.some((item) => item.assessment.basis === "query_corpus_share")
      ? "query_corpus_share"
      : null;
  const pageCount = Math.max(1, Math.ceil(remaining.length / PAGE_SIZE));
  const requestedPage = Number(params.get("topicsPage"));
  const page = Number.isSafeInteger(requestedPage)
    ? Math.min(pageCount, Math.max(1, requestedPage))
    : 1;
  const shown = remaining.slice((page - 1) * PAGE_SIZE, page * PAGE_SIZE);
  return (
    <div className="space-y-8">
      {ranked.length > 0 && (
        <Section
          title={t(ranked.length === 15 ? "complete" : "partial", { count: ranked.length })}
          description={t("shortfall")}
        >
          {basis === "observed_counts" && <Notice>{t("provisionalRanking")}</Notice>}
          {basis === "query_corpus_share" && (
            <p className="max-w-prose text-sm text-muted-foreground">{t("queryCorpusRanking")}</p>
          )}
          <ol className="divide-y" aria-label={t("explore.ranked")}>
            {ranked.map((candidate, index) => (
              <li key={candidate.id} className="py-7 first:pt-4">
                <CandidateResult
                  job={job}
                  candidate={candidate}
                  returnTo={returnTo}
                  rank={index + 1}
                  description={descriptions[candidate.id]}
                />
              </li>
            ))}
          </ol>
        </Section>
      )}
      {remaining.length > 0 && (
        <Section
          title={t("explore.foundTopics", { count: remaining.length })}
          description={t("explore.foundHint")}
        >
          {ranked.length === 0 && (
            <p className="mb-4 max-w-prose text-sm text-muted-foreground">{t("zeroHint")}</p>
          )}
          <ul className="divide-y" aria-label={t("explore.topicList")}>
            {shown.map((candidate) => (
              <li key={candidate.id} className="py-7 first:pt-4">
                <CandidateResult
                  job={job}
                  candidate={candidate}
                  returnTo={returnTo}
                  description={descriptions[candidate.id]}
                />
              </li>
            ))}
          </ul>
          <Pagination
            page={page}
            pageCount={pageCount}
            onPage={(value) =>
              setParams((current) => {
                const next = new URLSearchParams(current);
                next.set("topicsPage", String(value));
                return next;
              })
            }
          />
        </Section>
      )}
      {!ranked.length && !remaining.length && (
        <Section title={t("explore.noMatches")} description={t("explore.noMatchesHint")}>
          <p className="text-sm text-muted-foreground">{t("zeroHint")}</p>
        </Section>
      )}
      {unrelated.length > 0 && (
        <Disclosure title={t("explore.otherTopics", { count: unrelated.length })}>
          <p className="mb-3 max-w-prose text-sm text-muted-foreground">{t("candidateHint")}</p>
          <ul className="divide-y">
            {unrelated.map((candidate) => (
              <li key={candidate.id} className="space-y-2 py-4">
                <h3 className="text-base font-medium wrap-anywhere">
                  <Button asChild variant="link" size="inline">
                    <Link
                      id={`topic-${candidate.id}`}
                      to={trendPath(job.id, candidate.id, returnTo)}
                    >
                      {candidate.title}
                    </Link>
                  </Button>
                </h3>
                <CandidateReasons candidate={candidate} />
              </li>
            ))}
          </ul>
        </Disclosure>
      )}
    </div>
  );
}

function CandidateResult({
  job,
  candidate,
  returnTo,
  rank,
  description,
}: {
  job: AnalysisJob;
  candidate: TrendCandidate;
  returnTo: string;
  rank?: number;
  description?: DescriptionJob;
}) {
  const { t } = useTranslation("trend");
  const label = description?.result?.label;
  const summary = description?.result?.summary;
  const fields = description?.result?.fields ?? candidate.brief;
  const features = candidate.assessment.features;
  const counts = candidate.assessment.basis === "observed_counts";
  const number = (value: number | null | undefined) =>
    typeof value === "number" ? formatNumber(value, { maximumFractionDigits: 3 }) : t("unknown");
  const first = candidate.measurements.earliest;
  const ranked = rank !== undefined;
  return (
    <article className="grid gap-5 xl:grid-cols-[minmax(0,1fr)_20rem]">
      <div className="min-w-0 space-y-3">
        <div className="flex min-w-0 flex-wrap items-center gap-x-3 gap-y-1">
          <h3 className="font-display text-xl leading-snug wrap-anywhere">
            {ranked && (
              <span className="mr-3 text-base text-muted-foreground tabular-nums">{rank}.</span>
            )}
            <Link
              id={`topic-${candidate.id}`}
              className="hover:underline focus-visible:outline-2 focus-visible:outline-ring"
              to={trendPath(job.id, candidate.id, returnTo)}
            >
              {label || candidate.title}
            </Link>
          </h3>
          <Badge variant={ranked ? stageTones.emerging_signal.badge : stageTones.unassessed.badge}>
            {t(ranked ? "topic.stage.emerging" : "topic.stage.candidate")}
          </Badge>
        </div>
        {label && (
          <p className="text-sm wrap-anywhere text-muted-foreground">
            {t("sourceTitle", { title: candidate.title })}
          </p>
        )}
        {summary && <p className="max-w-prose text-sm leading-relaxed">{summary}</p>}
        <dl className="max-w-prose space-y-1 text-sm">
          {(["problem", "advantage", "example"] as const).map((field) => (
            <div key={field} className="flex gap-2">
              <dt className="shrink-0 font-medium">{t(`topic.short.${field}`)}</dt>
              <dd className="line-clamp-2 min-w-0 wrap-anywhere text-muted-foreground">
                {fields[field]?.text ?? t("missingClaim")}
              </dd>
            </div>
          ))}
        </dl>
      </div>
      <div className="space-y-3">
        <TopicSparkline candidate={candidate} bare />
        <StatStrip
          columns={3}
          items={[
            { label: t("topic.facts.first"), value: first ? first.date.value : t("unknown") },
            { label: t("topic.facts.works"), value: number(features.dated_scientific_works) },
            {
              label: t("topic.facts.growth"),
              value: number(counts ? features.log_count_ratio : features.log_rate_ratio),
            },
          ]}
        />
        <Button variant="outline" size="sm" asChild>
          <Link to={trendPath(job.id, candidate.id, returnTo)}>{t("detail")}</Link>
        </Button>
      </div>
    </article>
  );
}
