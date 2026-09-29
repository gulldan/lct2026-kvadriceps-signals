import { Link, useParams, useSearchParams } from "react-router-dom";
import { useTranslation } from "react-i18next";
import { ArrowLeft } from "lucide-react";
import { ReadingContent } from "@/shared/ui/ReadingContent";
import { Section } from "@/shared/ui/Section";
import { PageLayout } from "@/shared/ui/PageLayout";
import { PageHeader, PageToolbar, BackButton } from "@/shared/ui/PageHeader";

import { Disclosure } from "@/shared/ui/Disclosure";
import { Notice } from "@/shared/ui/Notice";
import { formatNumber } from "@/shared/lib/format";
import { useAnalysis } from "../useAnalysis";
import { analysisPath, trendPath } from "../trendApi";
import { AnalysisState } from "../ui/AnalysisState";

const PARAMETERS = [
  "recent_quarters",
  "prior_quarters",
  "max_first_age_quarters",
  "min_scientific_works",
  "min_active_quarters",
  "min_recent_works",
  "smoothing",
] as const;

const RULES = ["definition", "counts", "history", "earliest", "brief", "order"] as const;

export function TrendMethod() {
  const { run = "" } = useParams();
  const [params] = useSearchParams();
  const candidate = params.get("candidate") || "";
  const savedReturn = params.get("return") || "";
  const back = !run
    ? savedReturn === "example"
      ? "/trends/example"
      : "/trends?view=analysis"
    : /^\/trends(?:\?[^#]*|\/[\w-]+\/topics\/[\w-]+(?:\?[^#]*)?)$/.test(savedReturn)
      ? savedReturn
      : candidate
        ? trendPath(run, candidate)
        : analysisPath(run);
  const { t, i18n } = useTranslation("trend");
  const { job, error, retry } = useAnalysis(run);
  const method = job?.result ? job.method : null;
  return (
    <PageLayout className="space-y-6">
      <PageToolbar>
        <BackButton asChild>
          <Link to={`${back}#method-link`}>
            <ArrowLeft aria-hidden />
            {t(
              !run
                ? savedReturn === "example"
                  ? "demo.back"
                  : "backRadar"
                : candidate
                  ? "backTopic"
                  : "backResults",
            )}
          </Link>
        </BackButton>
      </PageToolbar>
      <PageHeader title={t("methodTitle")} description={t(run ? "methodSaved" : "methodIntro")} />
      {run && !method ? (
        <AnalysisState job={job} error={error} retry={retry} />
      ) : (
        <>
          {(!method || method.quality_status !== "evaluated") && (
            <Notice>{t("experimental")}</Notice>
          )}
          {method && !i18n.language.startsWith("ru") && (
            <p className="text-sm text-muted-foreground">{t("methodOriginal")}</p>
          )}
          <ReadingContent>
            {RULES.map((key) => (
              <Section key={key} reading title={t(`methodSections.${key}`)}>
                <p lang={method ? "ru" : undefined}>
                  {method ? method.explanation[key] : t(`methodGuide.${key}`)}
                </p>
              </Section>
            ))}
            <Section reading title={t("methodLimitations")}>
              {method ? (
                <ul lang="ru" className="list-disc space-y-3 pl-5">
                  {method.explanation.limitations.map((text) => (
                    <li key={text}>{text}</li>
                  ))}
                </ul>
              ) : (
                <p>{t("methodGuide.limitations")}</p>
              )}
            </Section>
          </ReadingContent>
          {method && (
            <Disclosure title={t("methodCalculation")} reading>
              <div className="max-w-4xl min-w-0 space-y-6 text-base leading-7 wrap-anywhere">
                <p lang="ru">{method.explanation.formula}</p>
                <Section reading title={t("methodParameters")}>
                  <dl className="divide-y">
                    {PARAMETERS.map((key) => (
                      <div
                        key={key}
                        className="grid grid-cols-[minmax(0,1fr)_auto] items-baseline gap-5 py-3"
                      >
                        <dt className="min-w-0">{t(key)}</dt>
                        <dd className="tabular-nums">
                          {typeof method.parameters.emergence[key] !== "number"
                            ? t("unknown")
                            : formatNumber(method.parameters.emergence[key])}
                        </dd>
                      </div>
                    ))}
                  </dl>
                </Section>
              </div>
            </Disclosure>
          )}
          {method && (
            <Disclosure title={t("technical")} reading>
              <dl className="space-y-4 text-sm wrap-anywhere">
                <div>
                  <dt>{t("technicalFields.method")}</dt>
                  <dd>{method.version}</dd>
                </div>
                <div>
                  <dt>{t("technicalFields.methodSha")}</dt>
                  <dd>{job?.method_sha256}</dd>
                </div>
                <div>
                  <dt>{t("technicalFields.pythonSha")}</dt>
                  <dd>{method.code_sha256}</dd>
                </div>
                <div>
                  <dt>{t("technicalFields.resultSha")}</dt>
                  <dd>{job?.result_sha256}</dd>
                </div>
                <div>
                  <dt>{t("technicalFields.run")}</dt>
                  <dd>{job?.id}</dd>
                </div>
              </dl>
              <pre className="mt-5 w-full max-w-full min-w-0 text-sm leading-6 wrap-anywhere whitespace-pre-wrap">
                {JSON.stringify(method.parameters, null, 2)}
              </pre>
            </Disclosure>
          )}
        </>
      )}
    </PageLayout>
  );
}
