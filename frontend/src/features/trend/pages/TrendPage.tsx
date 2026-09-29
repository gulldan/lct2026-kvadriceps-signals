import { Link, useParams, useSearchParams, useLocation } from "react-router-dom";
import { useTranslation } from "react-i18next";
import { ArrowLeft } from "lucide-react";
import { formatDate, formatNumber } from "@/shared/lib/format";
import { PageLayout } from "@/shared/ui/PageLayout";
import { PageHeader, PageToolbar, BackButton } from "@/shared/ui/PageHeader";
import { Badge } from "@/shared/ui/Badge";
import { Button } from "@/shared/ui/Button";
import { Disclosure } from "@/shared/ui/Disclosure";
import { StatStrip } from "@/shared/ui/Fields";
import { Notice } from "@/shared/ui/Notice";
import { Section } from "@/shared/ui/Section";
import { useAnalysis } from "../useAnalysis";
import { useReturnFocus } from "../useReturnFocus";
import { useDescription } from "../useDescription";
import { analysisPath, type AnalysisJob, type TrendCandidate } from "../trendApi";
import { providerLabel } from "../providerLabel";
import { stageTones } from "../stageTones";
import { AnalysisState } from "../ui/AnalysisState";
import { TrendBrief } from "../ui/TrendBrief";
import { TrendCase } from "../ui/TrendCase";
import { TrendCriteria } from "../ui/TrendCriteria";
import { TrendTimeline } from "../ui/TrendTimeline";
import { TrendLadder } from "../ui/TrendLadder";
import { TopicSparkline } from "../ui/TopicSparkline";
import { TrendSources } from "../ui/TrendSources";
import { DirectionMetrics } from "../ui/DirectionMetrics";

function TopicFacts({ job, candidate }: { job: AnalysisJob; candidate: TrendCandidate }) {
  const { t } = useTranslation("trend");
  const features = candidate.assessment.features;
  const counts = candidate.assessment.basis === "observed_counts";
  const number = (value: number | null | undefined) =>
    typeof value === "number" ? formatNumber(value, { maximumFractionDigits: 3 }) : t("unknown");
  const first = candidate.measurements.earliest;
  const documents = new Set(
    job
      .result!.works.filter((work) => candidate.work_ids.includes(work.id))
      .flatMap((work) => work.document_ids),
  );
  const sources = job.sources.filter((source) => documents.has(source.document_id));
  const byProvider = new Map<string, number>();
  for (const source of sources)
    byProvider.set(source.source.provider, (byProvider.get(source.source.provider) ?? 0) + 1);
  return (
    <StatStrip
      columns={6}
      items={[
        {
          label: t("topic.facts.first"),
          value: first ? first.date.value : t("unknown"),
          hint: first ? t(`precision_${first.date.precision}`) : undefined,
        },
        { label: t("topic.facts.works"), value: number(features.dated_scientific_works) },
        { label: t("topic.facts.quarters"), value: number(features.active_scientific_quarters) },
        {
          label: t("topic.facts.growth"),
          value: number(counts ? features.log_count_ratio : features.log_rate_ratio),
          hint: t(counts ? "growthCounts" : "growth"),
        },
        {
          label: t("topic.facts.recent"),
          value: number(counts ? features.recent_found_works : features.recent_rate),
          hint: t(counts ? "recentFound" : "recentShare"),
        },
        {
          label: t("topic.facts.sources"),
          value: formatNumber(sources.length),
          hint: [...byProvider]
            .map(([name, count]) => `${providerLabel(name)} ${formatNumber(count)}`)
            .join(" · "),
        },
      ]}
    />
  );
}

export function TrendPage() {
  const { run = "", candidate: candidateID = "" } = useParams();
  const { t } = useTranslation("trend");
  const [params] = useSearchParams();
  const location = useLocation();
  const savedReturn = params.get("return") || "";
  const { job, error, retry } = useAnalysis(run);
  const candidate = job?.result?.candidates.find((item) => item.id === candidateID);
  const narrative = useDescription(job, candidate);
  useReturnFocus(!!candidate);
  const back = /^\/trends\?[^#]*$/.test(savedReturn)
    ? savedReturn
    : analysisPath(run, job?.result?.query);
  const ordered = job?.result?.ranking.ordered_ids ?? [];
  const rank = candidate ? ordered.indexOf(candidate.id) : -1;
  const label = narrative.description?.result?.label;
  const summary = narrative.description?.result?.summary;
  const descriptionState = t(
    narrative.description?.status === "prepared"
      ? "descriptionPreparing"
      : narrative.description?.status === "validated"
        ? "descriptionValidated"
        : "template",
  );
  return (
    <PageLayout className="space-y-8">
      <PageToolbar>
        <BackButton asChild>
          <Link to={`${back}#topic-${candidateID}`}>
            <ArrowLeft aria-hidden />
            {t("backResults")}
          </Link>
        </BackButton>
      </PageToolbar>
      {!job?.result ? (
        <AnalysisState job={job} error={error} retry={retry} />
      ) : !candidate ? (
        <Notice tone="risk" role="alert">
          {t("notFound")}
        </Notice>
      ) : (
        <>
          <div className="space-y-4">
            <PageHeader
              kicker={[
                rank >= 0
                  ? t("topic.rank", { rank: rank + 1, total: ordered.length })
                  : t("topic.candidate"),
                t("topic.query", { query: job.result.query }),
                t("cutoffAt", { date: formatDate(job.result.cutoff) }),
              ].join(" · ")}
              title={label || candidate.title}
              badge={
                <Badge
                  variant={
                    rank >= 0 ? stageTones.emerging_signal.badge : stageTones.unassessed.badge
                  }
                >
                  {t(rank >= 0 ? "topic.stage.emerging" : "topic.stage.candidate")}
                </Badge>
              }
              description={label ? t("sourceTitle", { title: candidate.title }) : undefined}
              actions={
                <Button variant="link" size="inline" asChild>
                  <Link
                    id="method-link"
                    to={`/trends/${run}/method?${new URLSearchParams({ candidate: candidate.id, return: location.pathname + location.search })}`}
                  >
                    {t("methodLink")}
                  </Link>
                </Button>
              }
            />
            {rank < 0 && <Notice>{t("candidateOnly")}</Notice>}
            {job.method.quality_status !== "evaluated" && <Notice>{t("experimental")}</Notice>}
          </div>
          <div className="grid items-start gap-6 xl:grid-cols-[minmax(0,2fr)_minmax(0,1fr)]">
            <div className="space-y-1">
              {summary ? (
                <>
                  <p className="max-w-prose text-base leading-relaxed">{summary}</p>
                  <p className="text-xs text-muted-foreground">{t("summaryLabel")}</p>
                </>
              ) : (
                <p className="max-w-prose text-sm text-muted-foreground">{t("topic.noSummary")}</p>
              )}
            </div>
            <TopicSparkline candidate={candidate} />
          </div>
          <TopicFacts job={job} candidate={candidate} />
          <Section title={t("topic.why")} description={t("topic.whyHint")}>
            <TrendCriteria job={job} candidate={candidate} />
          </Section>
          <div className="grid items-start gap-10 xl:grid-cols-2">
            <Section title={t("topic.motivation")} description={descriptionState}>
              <TrendBrief
                job={job}
                candidate={candidate}
                description={narrative.description?.result?.fields}
                fields={["problem", "advantage"]}
              />
            </Section>
            <Section title={t("topic.case")} description={t("topic.caseHint")}>
              <TrendCase
                job={job}
                candidate={candidate}
                description={narrative.description?.result?.fields}
              />
            </Section>
          </div>
          <Section title={t("timeline")}>
            <TrendTimeline candidate={candidate} />
            <TrendLadder job={job} candidate={candidate} />
          </Section>
          <Section title={t("sources")}>
            <TrendSources job={job} candidate={candidate} />
          </Section>
          <Disclosure title={t("metrics.title")}>
            <section aria-label={t("metrics.title")} className="space-y-4">
              <p className="max-w-prose text-sm text-muted-foreground">{t("metrics.intro")}</p>
              <DirectionMetrics
                run={run}
                topic={candidate.id}
                formula={job.method.explanation.formula}
              />
            </section>
          </Disclosure>
        </>
      )}
    </PageLayout>
  );
}
