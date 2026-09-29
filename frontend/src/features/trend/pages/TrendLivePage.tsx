import { useEffect, useRef } from "react";
import { useSearchParams } from "react-router-dom";
import { useTranslation } from "react-i18next";
import { ApiError } from "@/shared/api/client";
import { loadable } from "@/shared/lib/loadable";
import { loadNamespace } from "@/shared/i18n";
import { useRequest } from "@/shared/lib/useRequest";
import { Button } from "@/shared/ui/Button";
import { Notice } from "@/shared/ui/Notice";
import { StatusPage } from "@/shared/ui/StatusPage";
import { LiveSearchProgress } from "../ui/LiveSearchProgress";
import { useLiveSearch } from "../useLiveSearch";
import { useLibraryCompletion } from "../useLibraryCompletion";
import { Skeleton } from "@/shared/ui/Skeleton";
import { getLiveOverview, getLiveTopic, type LiveDetail, type LiveOverview } from "../liveApi";
import { LiveOverviewView, liveTopicName } from "../ui/LiveOverview";
import type { CandidatesView } from "../ui/LiveCandidates";
import { LiveQueryBar } from "../ui/LiveQueryBar";
import { signalSources } from "../signalSources";

const skeleton = <Skeleton className="h-96 w-full" />;
const LiveDetailView = loadable(
  () => import("../ui/LiveDetail").then((m) => m.LiveDetailView),
  skeleton,
);
const LiveSignalReport = loadable(
  () => import("../ui/LiveSignalReport").then((m) => m.LiveSignalReport),
  skeleton,
);
// Нужна только выдаче, открытой из тезиса, а тянет словарь и API тезисов — не в стартовый чанк.
const ThesisLink = loadable(() =>
  Promise.all([import("../ui/ThesisLink"), loadNamespace("kpi")]).then(([m]) => m.ThesisLink),
);

const POLL_MS = 30_000;
const DIGEST_POLL_MS = 5_000;
const CACHE_MS = 5 * 60_000;

export function TrendLivePage() {
  const { t } = useTranslation("trend");
  const [params, setParams] = useSearchParams();
  const topic = params.get("topic") ?? "";
  const query = params.get("q") ?? "";
  const thesis = params.get("thesis") ?? "";
  const overviewRequest = useRequest<LiveOverview>("live-overview", getLiveOverview, {
    pollMs: (value) => (value.status !== "ready" || value.topics.length === 0 ? POLL_MS : 0),
    keepPrevious: true,
    cacheMs: CACHE_MS,
  });
  const overview = overviewRequest.data;
  const overviewFailed = overviewRequest.status === "error";
  const search = useLiveSearch(query);
  const completion = useLibraryCompletion(topic ? "" : query, search.state);
  const awaitingResult =
    !topic &&
    !!query &&
    (!!completion.progressState || search.busy || (!search.state && !search.error));
  useEffect(() => {
    if (completion.finishing) void LiveDetailView.warm();
  }, [completion.finishing]);
  const detailRequest = useRequest<LiveDetail>(
    topic ? `topic:${topic}` : null,
    (signal) => getLiveTopic(topic, signal),
    { cacheMs: CACHE_MS, pollMs: (value) => (value.digests_pending ? DIGEST_POLL_MS : 0) },
  );
  const detail = detailRequest.data;
  const detailState: "idle" | "loading" | "failed" =
    detailRequest.status === "loading"
      ? "loading"
      : detailRequest.status === "error"
        ? "failed"
        : "idle";
  const navigate = (selection: { topic?: string; q?: string } = {}) =>
    setParams((current) => {
      const next = new URLSearchParams(current);
      next.delete("topic");
      next.delete("q");
      // Раскрытый сигнал принадлежит конкретной выдаче, иначе ссылка переживает ответ.
      next.delete("signal");
      next.delete("view");
      if (selection.topic) next.set("topic", selection.topic);
      if (selection.q) next.set("q", selection.q);
      // Иначе следующий запрос с карты записался бы в тезис, к которому не относится.
      if (!selection.topic && !selection.q) next.delete("thesis");
      return next;
    });
  const back = () => navigate();
  const searchDetail = search.state?.status === "ready" ? search.state.detail : undefined;
  const shown = topic ? detail : searchDetail;
  const searchFailed = (!searchDetail && !!search.error) || search.state?.status === "failed";
  // Сырую ошибку сборщика не выносим — она английская.
  const status = search.error instanceof ApiError ? search.error.status : 0;
  const searchProblem =
    status === 400
      ? t("study.live.badQuery")
      : status === 503
        ? t("study.live.notReady")
        : status >= 500 || search.state?.status === "failed"
          ? t("study.live.searchFailed")
          : t("study.live.searchError");
  const signal = params.get("signal") ?? "";
  const measurements = params.get("view") === "measurements";
  const candidates = shown?.candidates ?? [];
  const candidateIndex = candidates.findIndex((row) => row.id === signal);
  const candidate = candidateIndex >= 0 ? candidates[candidateIndex] : undefined;
  // Возврат к обзору прокрутку не трогает: на карте нужно остаться у своего кружка.
  const shownKey = signal || topic || query;
  const openedKey = useRef(shownKey);
  useEffect(() => {
    if (shownKey && shownKey !== openedKey.current) window.scrollTo({ top: 0 });
    openedKey.current = shownKey;
  }, [shownKey]);
  const openSignal = (id: string) =>
    setParams((current) => {
      const next = new URLSearchParams(current);
      next.set("signal", id);
      next.delete("view");
      return next;
    });
  const closeSignal = () =>
    setParams((current) => {
      const next = new URLSearchParams(current);
      next.delete("signal");
      next.delete("view");
      return next;
    });
  const listView: CandidatesView = params.get("list") === "table" ? "table" : "cards";
  const openListView = (next: CandidatesView) =>
    setParams(
      (current) => {
        const updated = new URLSearchParams(current);
        if (next === "table") updated.set("list", "table");
        else updated.delete("list");
        return updated;
      },
      { replace: true },
    );
  const openMeasurements = () =>
    setParams((current) => {
      const next = new URLSearchParams(current);
      next.set("view", "measurements");
      return next;
    });
  const closeMeasurements = () =>
    setParams((current) => {
      const next = new URLSearchParams(current);
      next.delete("view");
      return next;
    });
  const candidateDetail: LiveDetail | undefined =
    shown && candidate
      ? {
          ...shown,
          topic: candidate,
          papers: signalSources(shown, candidate).map(({ paper }) => paper),
          candidates: undefined,
          rejected: undefined,
        }
      : undefined;
  return (
    <>
      {thesis && (
        <div className="print:hidden">
          <ThesisLink thesisId={thesis} query={query} />
        </div>
      )}
      {(topic || query) && !awaitingResult && (
        <div className="print:hidden">
          <LiveQueryBar key={query} query={query} onSearch={(value) => navigate({ q: value })} />
        </div>
      )}
      {!topic && query && searchDetail && !!search.error && (
        <Notice tone="warn">
          <output>{t("study.live.failedTitle")}</output>
        </Notice>
      )}
      {topic || query ? (
        query && search.busy ? (
          <Notice>
            {t("study.live.busy")} {t("study.live.busyWaiting", { seconds: search.waited })}
          </Notice>
        ) : completion.progressState ? (
          <LiveSearchProgress
            key={query}
            state={completion.progressState}
            reconnecting={!completion.finishing && !!search.error}
            finishing={completion.finishing}
            onFinished={completion.onFinished}
          />
        ) : query && status === 503 && !searchDetail ? (
          <StatusPage status={503} titleAs="h2" onRetry={search.retry} onBack={back} />
        ) : (topic && detailState === "loading") || (query && !search.state && !search.error) ? (
          <div className="space-y-3">
            <output className="block text-sm text-muted-foreground">
              {query ? t("study.live.searching") : t("study.loading")}
            </output>
            <Skeleton className="h-96 w-full" />
          </div>
        ) : (topic ? detailState === "failed" : searchFailed) || !shown ? (
          <Notice tone="risk" role="alert">
            {query ? searchProblem : t("study.error")}{" "}
            <Button variant="link" size="inline" onClick={back}>
              {t("study.live.back")}
            </Button>
          </Notice>
        ) : candidateDetail && candidate && !measurements ? (
          <LiveSignalReport
            detail={candidateDetail}
            candidate={candidate}
            query={topic ? liveTopicName(shown.topic, t) : query}
            direction={!!topic}
            topic={topic || undefined}
            rank={candidateIndex + 1}
            total={candidates.length}
            onBack={closeSignal}
            onMeasurements={openMeasurements}
          />
        ) : candidateDetail ? (
          <LiveDetailView
            detail={candidateDetail}
            scope="signal"
            onBack={closeMeasurements}
            backLabel={t("report.backToTechnology")}
          />
        ) : (
          <div className="library-result-reveal">
            <LiveDetailView
              detail={shown}
              query={topic ? undefined : query}
              candidatesView={listView}
              onCandidatesView={openListView}
              onOpenCandidate={(row) => openSignal(row.id)}
              watch={
                topic
                  ? { kind: "topic", target: topic, title: liveTopicName(shown.topic, t) }
                  : { kind: "query", target: query, title: query }
              }
              onBack={back}
            />
          </div>
        )
      ) : (
        <LiveOverviewView
          overview={overview}
          failed={overviewFailed}
          onRetry={overviewRequest.retry}
          onOpen={(id) => navigate({ topic: id })}
          onSearch={(value) => navigate({ q: value })}
        />
      )}
    </>
  );
}
