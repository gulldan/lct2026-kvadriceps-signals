import { useEffect, useMemo, useState } from "react";
import { Link, useSearchParams } from "react-router-dom";
import { useTranslation } from "react-i18next";
import { ApiError } from "@/shared/api/client";
import { PageLayout } from "@/shared/ui/PageLayout";
import { PageHeader } from "@/shared/ui/PageHeader";
import { Button } from "@/shared/ui/Button";
import { Notice } from "@/shared/ui/Notice";
import { useAnalysis } from "../useAnalysis";
import { useReturnFocus } from "../useReturnFocus";
import { useDescriptions } from "../useDescriptions";
import {
  analysisPath,
  analysisRequestParams,
  submitAnalysis,
  type AnalysisRequest,
} from "../trendApi";
import { TrendForm, type TrendFormValue } from "../ui/TrendForm";
import { AnalysisState } from "../ui/AnalysisState";
import { ReportExport, serverFiles } from "@/shared/report/ReportExport";
import { TrendExamplePreview } from "../ui/TrendExamplePreview";
import { TrendExplorer } from "../ui/TrendExplorer";
import { TrendScope } from "../ui/TrendScope";

export function TrendRadar() {
  const { t } = useTranslation("trend");
  const [params, setParams] = useSearchParams();
  const run = params.get("run") || "";
  const requestID = params.get("request") || "";
  const previous = params.get("previous") || "";
  const { job, error, retry } = useAnalysis(run);
  const [submission, setSubmission] = useState<{ id: string; failed: boolean; status?: number }>({
    id: "",
    failed: false,
  });
  const [attempt, setAttempt] = useState(0);
  const query = params.get("q") || "";
  const start = params.get("start") || "2023-01-01";
  const cutoff = params.get("cutoff") || "";
  // Старые имена режимов приходят в сохранённых ссылках: их сопоставляем, а не отвергаем.
  const asked = params.get("mode");
  const mode =
    asked === "retrospective_current_membership" || asked === "reconstruction"
      ? "retrospective_current_membership"
      : "as_of";
  const scopes = useMemo(() => params.getAll("scope"), [params]);
  const result = job?.status === "published" ? job.result : null;
  // Описания ставятся в очередь в порядке показа, чтобы верх списка получал названия первым.
  const relevant = useMemo(() => {
    if (!result) return [];
    const rank = new Map(result.ranking.ordered_ids.map((id, index) => [id, index]));
    return result.candidates
      .filter((candidate) => candidate.relevance.relevant)
      .toSorted((a, b) => (rank.get(a.id) ?? Infinity) - (rank.get(b.id) ?? Infinity));
  }, [result]);
  const descriptions = useDescriptions(job, relevant);
  useReturnFocus(!!result || (!run && !requestID));
  useEffect(() => {
    if (!requestID || run) return;
    const controller = new AbortController();
    setSubmission({ id: requestID, failed: false });
    const input: AnalysisRequest = {
      request_id: requestID,
      query,
      start,
      cutoff,
      mode,
      ...(scopes.length ? { collection_scope_ids: scopes } : {}),
    };
    submitAnalysis(input, controller.signal)
      .then((accepted) => {
        if (controller.signal.aborted) return;
        setParams(
          (current) => {
            if (current.get("request") !== requestID) return current;
            const next = new URLSearchParams(current);
            next.delete("request");
            next.set("run", accepted.id);
            return next;
          },
          { replace: true },
        );
      })
      .catch((submissionError: unknown) => {
        if (!controller.signal.aborted)
          setSubmission({
            id: requestID,
            failed: true,
            status: submissionError instanceof ApiError ? submissionError.status : undefined,
          });
      });
    return () => controller.abort();
  }, [requestID, run, query, start, cutoff, mode, scopes, attempt, setParams]);
  const launch = (value: TrendFormValue) => {
    setParams(
      analysisRequestParams(
        {
          ...value,
          collection_scope_ids: job?.request?.collection_scope_ids ?? scopes,
        },
        run || previous,
      ),
    );
  };
  const initial: TrendFormValue = {
    query: job?.request?.query || query || result?.query || "",
    start: (job?.request?.start || start).slice(0, 10),
    cutoff: (job?.request?.cutoff || cutoff || result?.cutoff || new Date().toISOString()).slice(
      0,
      10,
    ),
    mode: job?.request?.mode || mode,
  };
  return (
    <PageLayout className="space-y-4">
      <PageHeader
        title={t("title")}
        description={result ? undefined : t("intro")}
        actions={
          result &&
          job && (
            <div className="flex flex-wrap items-center gap-3">
              <ReportExport
                items={serverFiles(`/trends/analyses/${job.id}/export`, `analysis-${job.id}`)}
              />
              <Button variant="link" size="inline" asChild>
                <Link
                  id="method-link"
                  to={`/trends/${job.id}/method?${new URLSearchParams({ return: "/trends?" + params })}`}
                >
                  {t("methodLink")}
                </Link>
              </Button>
            </div>
          )
        }
      />
      <TrendScope />
      {(job?.request?.collection_scope_ids?.length || scopes.length) > 0 && (
        <p className="text-sm text-muted-foreground">{t("corpusSelection")}</p>
      )}
      {job?.request?.search_terms?.length ? (
        <p className="text-sm text-muted-foreground">
          {t("searchTerms", { terms: job.request.search_terms.join(" · ") })}
        </p>
      ) : null}
      <TrendForm
        key={`form:${requestID || run || "new"}`}
        initial={initial}
        onSubmit={launch}
        compact={!!result}
        busy={
          !!(requestID && !submission.failed) ||
          !!(job && ["prepared", "validated"].includes(job.status))
        }
      />
      {!run && !requestID && (
        <TrendExamplePreview onRun={() => launch({ ...initial, query: "RAG" })} />
      )}
      {requestID &&
        !run &&
        (submission.id === requestID && submission.failed ? (
          <Notice tone="risk" role="alert">
            <p>
              {t(
                submission.status === 429
                  ? "queueFull"
                  : submission.status === 507
                    ? "historyFull"
                    : "submitError",
              )}
            </p>
            {submission.status !== 507 && (
              <Button
                className="mt-3"
                variant="outline"
                onClick={() => setAttempt((value) => value + 1)}
              >
                {t("retry")}
              </Button>
            )}
          </Notice>
        ) : (
          <AnalysisState retry={retry} refreshing={!!previous} />
        ))}
      {run && !result && (
        <AnalysisState job={job} error={error} retry={retry} refreshing={!!previous} />
      )}
      {previous && (
        <Button variant="link" size="inline" asChild>
          <Link to={analysisPath(previous)}>{t("previous")}</Link>
        </Button>
      )}
      {result && job && (
        <TrendExplorer key={`explorer:${job.id}`} job={job} descriptions={descriptions} />
      )}
    </PageLayout>
  );
}
