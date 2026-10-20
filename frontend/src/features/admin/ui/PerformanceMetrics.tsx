import { useTranslation } from "react-i18next";
import { RefreshCw } from "lucide-react";
import { getPerf } from "../api";
import { JOB_TITLES, WORKER_LABELS, workerState, type WorkerStatus } from "@/features/task";
import { useRequest } from "@/shared/lib/useRequest";
import { formatDateTime, formatNumber } from "@/shared/lib/format";
import { Badge } from "@/shared/ui/Badge";
import { Button } from "@/shared/ui/Button";
import { ErrorState } from "@/shared/ui/ErrorState";
import { Section } from "@/shared/ui/Section";
import { Skeleton } from "@/shared/ui/Skeleton";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/shared/ui/Table";

const SERVICES = [
  {
    svc: "main-service",
    phase: "ingest",
    stages: [{ key: "upload", labelKey: "metrics.trace.stages.upload" }],
  },
  {
    svc: "archive-worker",
    phase: "ingest",
    stages: [{ key: "parse.archive", labelKey: "metrics.trace.stages.parseArchive" }],
  },
  {
    svc: "pdf-parser",
    phase: "ingest",
    stages: [{ key: "parse.pdf", labelKey: "metrics.trace.stages.parsePdf" }],
  },
  {
    svc: "office-parser",
    phase: "ingest",
    stages: [{ key: "parse.office", labelKey: "metrics.trace.stages.parseOffice" }],
  },
  {
    svc: "email-parser",
    phase: "ingest",
    stages: [{ key: "parse.email", labelKey: "metrics.trace.stages.parseEmail" }],
  },
  {
    svc: "ocr-service",
    phase: "ingest",
    stages: [{ key: "parse.ocr", labelKey: "metrics.trace.stages.parseOcr" }],
  },
  {
    svc: "chunk-splitter",
    phase: "ingest",
    stages: [
      { key: "chunk_split", labelKey: "metrics.trace.stages.chunkSplit" },
      { key: "chunk_embed", labelKey: "metrics.trace.stages.chunkEmbed" },
      { key: "chunk_index", labelKey: "metrics.trace.stages.chunkIndex" },
    ],
  },
  {
    svc: "llm-service",
    phase: "query",
    stages: [
      { key: "embed", labelKey: "metrics.trace.stages.embed" },
      { key: "retrieve_vector", labelKey: "metrics.trace.stages.retrieveVector" },
      { key: "retrieve_lexical", labelKey: "metrics.trace.stages.retrieveLexical" },
      { key: "rerank", labelKey: "metrics.trace.stages.rerank" },
      { key: "generate", labelKey: "metrics.trace.stages.generate" },
    ],
  },
] as const;

export function PerformanceMetrics({
  workers,
  onRefresh,
}: {
  workers: WorkerStatus[];
  onRefresh: () => void;
}) {
  const { t } = useTranslation("admin");
  const {
    data: perf,
    status: phase,
    retry,
  } = useRequest("admin-perf", getPerf, {
    pollMs: 30_000,
    keepPrevious: true,
  });
  const load = () => {
    retry();
    onRefresh();
  };
  const seconds = (value: number | undefined) =>
    value === undefined || !Number.isFinite(value)
      ? "—"
      : value < 1
        ? `${formatNumber(value * 1000, { maximumFractionDigits: 0 })} ${t("units.ms")}`
        : `${formatNumber(value, { maximumFractionDigits: 1 })} ${t("units.s")}`;
  const trace = { ...perf?.ingestion, ...perf?.pipeline };
  return (
    <div className="mt-6">
      <div className="mb-5 flex flex-wrap items-center justify-between gap-3">
        <p className="max-w-2xl text-sm text-muted-foreground">
          {t("metrics.readable.latencyHelp")}
        </p>
        <Button
          variant="ghost"
          size="sm"
          disabled={phase === "loading"}
          onClick={() => void load()}
        >
          <RefreshCw className="size-3.5" aria-hidden />
          {t("actions.refresh")}
        </Button>
      </div>
      {!perf && phase !== "error" ? (
        <Skeleton className="h-64 w-full" />
      ) : phase === "error" ? (
        <ErrorState message={t("metrics.loadFailed")} onRetry={() => void load()} />
      ) : (
        <>
          {perf?.telemetry && perf.telemetry !== "available" && (
            <output className="mb-5 block text-sm text-risk">
              {t("metrics.telemetryUnavailable")}
            </output>
          )}
          {perf?.observed_at && (
            <p className="mb-5 text-xs text-muted-foreground">
              {t("metrics.observedAt", { date: formatDateTime(perf.observed_at) })}
            </p>
          )}
          <div className="grid items-start gap-8 xl:grid-cols-2">
            {(["query", "ingest"] as const).map((group) => {
              const rows = SERVICES.filter((service) => service.phase === group).flatMap(
                (service) =>
                  service.stages
                    .filter((stage) => trace[stage.key])
                    .map((stage) => ({
                      key: stage.key,
                      labelKey: stage.labelKey,
                      service: service.svc,
                      latency: trace[stage.key]!,
                    })),
              );
              const slowest = Math.max(...rows.map((row) => row.latency.p95 ?? 0), 0);
              return (
                <Section
                  key={group}
                  title={t(
                    group === "query" ? "metrics.trace.queryPath" : "metrics.trace.ingestPath",
                  )}
                >
                  <Table>
                    <TableHeader>
                      <TableRow>
                        <TableHead>{t("metrics.readable.stage")}</TableHead>
                        <TableHead className="text-right">{t("metrics.jobs.median")}</TableHead>
                        <TableHead className="text-right">{t("metrics.readable.p95")}</TableHead>
                      </TableRow>
                    </TableHeader>
                    <TableBody>
                      {rows.length === 0 && (
                        <TableRow>
                          <TableCell colSpan={3} className="py-6 text-muted-foreground">
                            {t("metrics.trace.empty")}
                          </TableCell>
                        </TableRow>
                      )}
                      {rows.map((row) => (
                        <TableRow key={row.key}>
                          <TableCell>
                            <span className="font-medium">{t(row.labelKey)}</span>
                            <p className="mt-1 text-xs text-muted-foreground">{row.service}</p>
                            {slowest > 0 && row.latency.p95 === slowest && (
                              <Badge variant="warn" className="mt-1">
                                {t("metrics.readable.slowest")}
                              </Badge>
                            )}
                          </TableCell>
                          <TableCell className="text-right font-mono">
                            {seconds(row.latency.p50)}
                          </TableCell>
                          <TableCell className="text-right font-mono">
                            {seconds(row.latency.p95)}
                          </TableCell>
                        </TableRow>
                      ))}
                    </TableBody>
                  </Table>
                </Section>
              );
            })}
          </div>
          <Section
            className="mt-7 border-t pt-5"
            title={t("metrics.jobs.title")}
            description={t("metrics.jobs.description")}
          >
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead>{t("metrics.readable.operation")}</TableHead>
                  <TableHead className="text-right">{t("metrics.jobs.median")}</TableHead>
                  <TableHead className="text-right">{t("metrics.jobs.max")}</TableHead>
                  <TableHead className="text-right">{t("metrics.readable.completed")}</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {(perf?.hypothesis_jobs ?? []).map((job) => (
                  <TableRow key={job.kind}>
                    <TableCell>{JOB_TITLES[job.kind] ?? job.kind}</TableCell>
                    <TableCell className="text-right font-mono">{seconds(job.p50_sec)}</TableCell>
                    <TableCell className="text-right font-mono">{seconds(job.max_sec)}</TableCell>
                    <TableCell className="text-right font-mono">
                      {formatNumber(job.count)}
                    </TableCell>
                  </TableRow>
                ))}
                {!perf?.hypothesis_jobs.length && (
                  <TableRow>
                    <TableCell colSpan={4} className="py-5 text-muted-foreground">
                      {t("noData")}
                    </TableCell>
                  </TableRow>
                )}
              </TableBody>
            </Table>
          </Section>
        </>
      )}
      <Section className="mt-7 border-t pt-5" title={t("metrics.workers.title")}>
        <ul className="divide-y">
          {workers
            .filter((worker) => worker.name !== "eval")
            .map((worker) => {
              const state = workerState(worker);
              return (
                <li
                  key={worker.name}
                  className="flex flex-wrap items-center justify-between gap-3 py-3"
                >
                  <div className="min-w-0 flex-1">
                    <p className="text-sm font-medium">
                      {WORKER_LABELS[worker.name] ?? worker.name}
                    </p>
                    {worker.state === "error" && (
                      <p className="mt-1 text-xs break-words text-risk">{worker.lastError}</p>
                    )}
                    <p className="mt-1 text-xs text-muted-foreground">
                      {worker.updatedAt
                        ? t("metrics.workers.heartbeat", { date: formatDateTime(worker.updatedAt) })
                        : t("metrics.workers.neverRan")}
                    </p>
                    <p className="mt-1 text-xs text-muted-foreground">
                      {worker.lastSuccessAt
                        ? t("metrics.workers.lastSuccess", {
                            date: formatDateTime(worker.lastSuccessAt),
                          })
                        : t("metrics.workers.noSuccess")}
                      {worker.lastRunSeconds !== null
                        ? ` · ${t("metrics.workers.run", { duration: seconds(worker.lastRunSeconds) })}`
                        : ""}
                    </p>
                    {state === "running" && worker.progressTotal !== null && (
                      <p className="mt-1 font-mono text-xs text-muted-foreground">
                        {t("metrics.workers.progress", {
                          done: formatNumber(worker.progressDone ?? 0),
                          total: formatNumber(worker.progressTotal),
                        })}
                      </p>
                    )}
                  </div>
                  <Badge
                    variant={
                      state === "error"
                        ? "risk"
                        : state === "running"
                          ? "brand"
                          : state === "stale"
                            ? "warn"
                            : "muted"
                    }
                  >
                    {t(`metrics.readable.workerState.${state}`)}
                  </Badge>
                </li>
              );
            })}
        </ul>
      </Section>
    </div>
  );
}
