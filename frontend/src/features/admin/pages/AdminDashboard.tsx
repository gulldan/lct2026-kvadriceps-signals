import { useCallback, useEffect, useState } from "react";
import { Link } from "react-router-dom";
import { useTranslation } from "react-i18next";
import { ArrowRight, Files, RefreshCw } from "lucide-react";
import { adminDocumentsStats, type AdminDocumentsStats } from "@/features/admin/api";
import { documentTitle } from "@/features/document";
import { listClusters } from "@/features/cluster";
import { listKPIs } from "@/features/kpi";
import { getHypothesisBoard } from "@/features/hypothesis";
import { formatDateShort, formatNumber, humanSize } from "@/shared/lib/format";
import { BarChart } from "@/shared/ui/BarChart";
import { Button } from "@/shared/ui/Button";
import { EmptyState } from "@/shared/ui/EmptyState";
import { ErrorState } from "@/shared/ui/ErrorState";
import { MetricTile } from "@/shared/ui/MetricTile";
import { PageHeader } from "@/shared/ui/PageHeader";
import { PageLayout } from "@/shared/ui/PageLayout";
import { Section } from "@/shared/ui/Section";
import { Skeleton } from "@/shared/ui/Skeleton";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/shared/ui/Table";
import { TopicDistribution } from "../ui/TopicDistribution";

export function AdminDashboard() {
  const { t } = useTranslation("admin");
  const [data, setData] = useState<AdminDocumentsStats | null>(null);
  const [products, setProducts] = useState<{
    themes: number;
    hypotheses: number;
    goals: number;
  } | null>(null);
  const [phase, setPhase] = useState<"loading" | "ready" | "error">("loading");
  const load = useCallback(async () => {
    setPhase("loading");
    try {
      setData(await adminDocumentsStats());
      setPhase("ready");
    } catch {
      setPhase("error");
    }
  }, []);
  useEffect(() => {
    void load();
  }, [load]);
  useEffect(() => {
    let cancelled = false;
    Promise.all([listClusters(), getHypothesisBoard({ limit: 1 }), listKPIs()])
      .then(([clusters, board, kpis]) => {
        if (!cancelled)
          setProducts({ themes: clusters.length, hypotheses: board.total, goals: kpis.length });
      })
      .catch(() => {
        if (!cancelled) setProducts(null);
      });
    return () => {
      cancelled = true;
    };
  }, []);
  const indexed = data?.by_status.indexed ?? 0;
  const failed = data?.by_status.failed ?? 0;
  const processing = Object.entries(data?.by_status ?? {})
    .filter(([status]) => !["indexed", "failed"].includes(status))
    .reduce((total, [, count]) => total + count, 0);
  const metrics = data
    ? ([
        {
          key: "total",
          value: data.total,
          detail: humanSize(data.total_size_bytes),
          to: "/operator/documents",
        },
        {
          key: "indexed",
          value: indexed,
          detail: t("dashboard.metrics.indexedSub", { pct: data.indexed_pct }),
          to: "/operator/documents?status=indexed",
        },
        {
          key: "processing",
          value: processing,
          detail: t("dashboard.metrics.processingSub"),
          to: "/operator/documents?status=processing",
        },
        {
          key: "failed",
          value: failed,
          detail: t("dashboard.metrics.failedSub"),
          to: "/operator/documents?status=failed",
        },
      ] as const)
    : [];
  return (
    <PageLayout>
      <PageHeader
        title={t("dashboard.title")}
        description={t("dashboard.description")}
        actions={
          <Button variant="outline" disabled={phase === "loading"} onClick={() => void load()}>
            <RefreshCw className="size-4" aria-hidden />
            {t("actions.refresh")}
          </Button>
        }
      />
      {/* Подсчёт корпуса бывает долгим после выкладки — остальная страница его не ждёт. */}
      {phase === "loading" && <Skeleton className="mt-5 h-20 w-full" />}
      {phase === "error" && (
        <ErrorState message={t("dashboard.loadError")} onRetry={() => void load()} />
      )}
      {phase === "ready" && data && (
        <div className="mt-5 grid grid-cols-2 gap-x-5 border-y py-2 lg:grid-cols-4">
          {metrics.map((metric) => (
            <Link
              key={metric.key}
              to={metric.to}
              className="row-span-3 grid grid-rows-subgrid rounded-sm hover:text-brand focus-visible:outline-2 focus-visible:outline-ring"
            >
              <MetricTile
                variant="plain"
                label={t(`dashboard.metrics.${metric.key}`)}
                value={formatNumber(metric.value)}
                sub={metric.detail}
              />
            </Link>
          ))}
        </div>
      )}
      {products && (
        <nav
          aria-label={t("dashboard.factory.title")}
          className="flex flex-wrap gap-x-6 gap-y-1 border-b py-3"
        >
          {(
            [
              { key: "themes", to: "/summary" },
              { key: "hypotheses", to: "/hypotheses" },
              { key: "goals", to: "/kpi" },
            ] as const
          ).map((item) => (
            <Button key={item.key} variant="ghost" size="sm" asChild>
              <Link to={item.to}>
                <span className="font-mono">{formatNumber(products[item.key])}</span>
                {t(`dashboard.factory.${item.key}`)}
                <ArrowRight className="size-3.5" aria-hidden />
              </Link>
            </Button>
          ))}
          {data && (
            <p className="ml-auto self-center text-xs text-muted-foreground">
              {formatNumber(data.total_chunks)} {t("units.chunks")}
            </p>
          )}
        </nav>
      )}
      {phase === "loading" && <Skeleton className="mt-6 h-48 w-full" />}
      {phase === "ready" &&
        data &&
        (data.total === 0 ? (
          <EmptyState
            icon={Files}
            title={t("dashboard.empty.title")}
            description={t("dashboard.empty.description")}
          />
        ) : (
          <>
            <TopicDistribution data={data} />
            <div className="mt-6 grid gap-8 lg:grid-cols-3">
              <div className="min-w-0 lg:col-span-2">
                <BarChart
                  label={t("dashboard.dynamics.uploadsByDay", { days: 14 })}
                  data={data.uploads_by_day.map((day) => ({
                    label: formatDateShort(`${day.day}T12:00:00`),
                    value: day.count,
                    displayValue: `${formatNumber(day.count)} ${t("units.docs")}`,
                  }))}
                />
              </div>
              <Section title={t("dashboard.dynamics.byType")}>
                <Table>
                  <TableHeader>
                    <TableRow>
                      <TableHead>{t("dashboard.fileType")}</TableHead>
                      <TableHead className="text-right">{t("dashboard.metrics.total")}</TableHead>
                    </TableRow>
                  </TableHeader>
                  <TableBody>
                    {Object.entries(data.by_file_type)
                      .toSorted((a, b) => b[1] - a[1])
                      .map(([type, count]) => (
                        <TableRow key={type}>
                          <TableCell className="py-1">
                            {t(`dashboard.fileTypes.${type}`, { defaultValue: type })}
                          </TableCell>
                          <TableCell className="py-1 text-right font-mono">
                            {formatNumber(count)}
                          </TableCell>
                        </TableRow>
                      ))}
                  </TableBody>
                </Table>
              </Section>
            </div>
            <Section title={t("dashboard.dynamics.topDocs")} className="mt-7 border-t pt-5">
              <Table>
                <TableHeader>
                  <TableRow>
                    <TableHead>{t("dashboard.document")}</TableHead>
                    <TableHead className="text-right">{t("dashboard.metrics.chunks")}</TableHead>
                  </TableRow>
                </TableHeader>
                <TableBody>
                  {data.top_by_chunks.map((document) => (
                    <TableRow key={document.id}>
                      <TableCell>
                        <Button
                          asChild
                          variant="ghost"
                          size="content"
                          className="px-0 py-1 text-left"
                        >
                          <Link
                            to={`/operator/documents?q=${encodeURIComponent(document.filename)}`}
                          >
                            {documentTitle(document)}
                          </Link>
                        </Button>
                      </TableCell>
                      <TableCell className="text-right font-mono">
                        {formatNumber(document.chunk_count)}
                      </TableCell>
                    </TableRow>
                  ))}
                </TableBody>
              </Table>
            </Section>
          </>
        ))}
    </PageLayout>
  );
}
