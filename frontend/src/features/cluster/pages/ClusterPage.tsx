import { PageLayout } from "@/shared/ui/PageLayout";
import { useCallback, useEffect, useRef, useState } from "react";
import { Link, useNavigate, useParams } from "react-router-dom";
import { useTranslation } from "react-i18next";
import { ArrowLeft, FileText, Radar, Wand2 } from "lucide-react";

import { type ApiCluster, getCluster } from "@/features/cluster/api";
import {
  type ApiHypothesis,
  cleanInternalTitle,
  generateHypotheses,
  hasInternalNoise,
  ItcSummary,
} from "@/features/hypothesis";
import { DocPreviewSheet, useDocPreview } from "@/features/document";
import { Badge } from "@/shared/ui/Badge";
import { Button } from "@/shared/ui/Button";
import { PageHeader, PageToolbar, BackButton } from "@/shared/ui/PageHeader";
import { RichText } from "@/shared/ui/RichText";
import { Section } from "@/shared/ui/Section";
import { InfoHint } from "@/shared/ui/InfoHint";
import { SegmentBar } from "@/shared/ui/SegmentBar";
import { Spinner } from "@/shared/ui/Spinner";
import { ErrorState } from "@/shared/ui/ErrorState";
import { useBackgroundTasks } from "@/features/task";
import { formatDateShort } from "@/shared/lib/format";
import { i18n } from "@/shared/i18n";
import {
  fetchHypothesisRuntimeSettings,
  loadHypothesisRuntimeSettings,
} from "@/shared/appSettings";

type Phase = "loading" | "ready" | "error";

type UnknownObj = Record<string, unknown>;

function isObj(v: unknown): v is UnknownObj {
  return typeof v === "object" && v !== null;
}

function numOf(v: unknown): number | null {
  return typeof v === "number" && Number.isFinite(v) ? v : null;
}

function clusterDensity(c: ApiCluster): number | null {
  const metrics = c.params?.metrics;
  return isObj(metrics) ? numOf(metrics.avg_similarity) : null;
}

interface LineageInfo {
  text: string;
  jaccard: number | null;
}

function clusterLineage(c: ApiCluster): LineageInfo | null {
  const lineage = c.params?.lineage;
  if (!isObj(lineage)) return null;
  const jaccard = numOf(lineage.jaccard);
  const mergedFrom = Array.isArray(lineage.merged_from) ? lineage.merged_from.length : 0;
  const splitFrom = Array.isArray(lineage.split_from) ? lineage.split_from.length : 0;
  const prev = typeof lineage.previous_cluster_id === "string" && lineage.previous_cluster_id;
  if (mergedFrom >= 2) {
    return { text: i18n.t("cluster:lineage.merged", { count: mergedFrom }), jaccard };
  }
  if (splitFrom > 0) {
    return { text: i18n.t("cluster:lineage.split"), jaccard };
  }
  if (prev && jaccard !== null) {
    return {
      text:
        jaccard >= 0.995
          ? i18n.t("cluster:lineage.unchanged")
          : i18n.t("cluster:lineage.continues", { percent: Math.round(jaccard * 100) }),
      jaccard,
    };
  }
  return null;
}

const RADAR_KEYWORDS = 4;
const RADAR_QUERY_LIMIT = 200;

function radarQuery(c: ApiCluster): string {
  const parts = [c.label, ...c.keywords.slice(0, RADAR_KEYWORDS)].map(cleanInternalTitle);
  return [...new Set(parts.filter(Boolean))].reduce((query, part) => {
    const next = query === "" ? part : `${query}, ${part}`;
    return next.length <= RADAR_QUERY_LIMIT ? next : query;
  }, "");
}

export function ClusterPage() {
  const { id = "" } = useParams<{ id: string }>();
  const navigate = useNavigate();
  const { t } = useTranslation("cluster");

  const [phase, setPhase] = useState<Phase>("loading");
  const [cluster, setCluster] = useState<ApiCluster | null>(null);
  const [runtimeSettings, setRuntimeSettings] = useState(loadHypothesisRuntimeSettings);
  const mounted = useRef(true);
  const { runTask, isTaskRunning } = useBackgroundTasks();
  const { preview, openCitation, close: closePreview, retry: retryPreview } = useDocPreview();
  const genBusy = isTaskRunning("hypotheses:generate");
  const displayLabel = cluster ? cleanInternalTitle(cluster.label) || t("untitled") : "";
  const displaySummary =
    cluster && cluster.summary && !hasInternalNoise(cluster.summary) ? cluster.summary : "";
  const density = cluster ? clusterDensity(cluster) : null;
  const lineage = cluster ? clusterLineage(cluster) : null;
  const radar = cluster ? radarQuery(cluster) : "";

  useEffect(() => {
    return () => {
      mounted.current = false;
    };
  }, []);

  useEffect(() => {
    fetchHypothesisRuntimeSettings()
      .then((next) => {
        if (mounted.current) setRuntimeSettings(next);
      })
      .catch(() => setRuntimeSettings(loadHypothesisRuntimeSettings()));
  }, []);

  const load = useCallback(async () => {
    setPhase("loading");
    try {
      const found = await getCluster(id);
      setCluster(found);
      setPhase("ready");
    } catch {
      setCluster(null);
      setPhase("error");
    }
  }, [id]);

  useEffect(() => {
    void load();
  }, [load]);

  const generateFromCluster = useCallback(
    (c: ApiCluster) => {
      const label = cleanInternalTitle(c.label) || t("task.fallbackLabel");
      const kpi_description = [c.summary, (c.keywords || []).join(", ")].filter(Boolean).join(". ");
      void runTask({
        key: "hypotheses:generate",
        title: t("task.title"),
        description: t("task.theme", { label }),
        successMessage: (created: ApiHypothesis[]) =>
          t("task.success", { label, count: created.length }),
        errorMessage: t("task.error"),
        run: (onJobCreated) =>
          generateHypotheses(
            {
              kpi_title: c.label,
              kpi_description,
              count: runtimeSettings.clusterGenerateCount,
            },
            onJobCreated,
          ),
        onSuccess: () => {
          if (mounted.current) navigate("/hypotheses");
        },
      });
    },
    [navigate, runTask, runtimeSettings.clusterGenerateCount, t],
  );

  return (
    <PageLayout>
      <PageToolbar>
        <BackButton onClick={() => navigate(-1)}>
          <ArrowLeft className="size-4" aria-hidden />
          {t("back")}
        </BackButton>
      </PageToolbar>

      {phase === "loading" && (
        <div className="flex justify-center py-14">
          <Spinner size="lg" label={t("loading")} />
        </div>
      )}

      {phase === "error" && (
        <ErrorState
          message={cluster === null ? t("errorNotFound") : t("errorLoad")}
          onRetry={() => void load()}
        />
      )}

      {phase === "ready" && cluster && (
        <div className="space-y-5 pb-10">
          <div className="space-y-2">
            <PageHeader title={displayLabel} />
            <p className="font-mono text-xs text-muted-foreground">
              {t("meta.documents", { count: cluster.document_count })} ·{" "}
              {t("meta.fragments", { count: cluster.chunk_count })} ·{" "}
              {t("meta.updated", { date: formatDateShort(cluster.updated_at) })}
            </p>
          </div>

          {(density !== null || lineage) && (
            <div className="space-y-2.5 border-t pt-4">
              {density !== null && (
                <div className="flex flex-wrap items-center gap-2 text-sm">
                  <span className="inline-flex items-center gap-1">
                    {t("density.label")}
                    <InfoHint>{t("density.hint")}</InfoHint>
                  </span>
                  <SegmentBar
                    value={Math.round(density * 9)}
                    tone={density >= 0.6 ? "ok" : density >= 0.4 ? "brand" : "warn"}
                    label={t("density.bar", { percent: Math.round(density * 100) })}
                  />
                  <span className="font-mono text-xs text-muted-foreground">
                    {Math.round(density * 100)} %
                  </span>
                </div>
              )}
              {lineage && <p className="text-sm text-muted-foreground">{lineage.text}</p>}
            </div>
          )}

          {cluster.params?.itc && (
            <ItcSummary
              itc={cluster.params.itc}
              title={t("itc.title")}
              evidenceCitationLabel={t("itc.citations")}
            />
          )}
          {displaySummary !== "" && (
            <RichText className="text-sm text-muted-foreground">{displaySummary}</RichText>
          )}
          {cluster.keywords.map(cleanInternalTitle).filter(Boolean).length > 0 && (
            <div className="flex flex-wrap gap-1.5">
              {[...new Set(cluster.keywords.map(cleanInternalTitle).filter(Boolean))].map((k) => (
                <Badge key={k} variant="outline">
                  {k}
                </Badge>
              ))}
            </div>
          )}
          {cluster.representatives && cluster.representatives.length > 0 && (
            <Section title={t("docsSection")}>
              <div className="divide-y border-b">
                {cluster.representatives.map((r) => {
                  // Ключ — из содержимого фрагмента: стабильного id у представителя нет.
                  const key = [r.document_id, r.filename, r.snippet].join("|");
                  return (
                    <Button
                      variant="ghost"
                      size="row"
                      disabled={!r.document_id}
                      key={key}
                      onClick={() =>
                        openCitation({
                          documentId: r.document_id ?? "",
                          filename: r.filename,
                          snippet: r.snippet,
                        })
                      }
                    >
                      {r.filename && (
                        <p className="mb-1 flex min-w-0 items-start gap-1 font-mono text-xs text-muted-foreground">
                          <FileText className="mt-0.5 size-3 shrink-0" aria-hidden />
                          <span className="min-w-0 break-all">{r.filename}</span>
                        </p>
                      )}
                      {r.snippet && <p className="text-sm break-words">{r.snippet}</p>}
                    </Button>
                  );
                })}
              </div>
            </Section>
          )}
          <div className="space-y-2">
            <div className="flex flex-wrap gap-2">
              <Button disabled={genBusy} onClick={() => void generateFromCluster(cluster)}>
                <Wand2 className="size-4" aria-hidden />
                {genBusy ? t("generating") : t("generate")}
              </Button>
              {radar !== "" && (
                <Button variant="outline" asChild>
                  <Link to={`/trends?q=${encodeURIComponent(radar)}`}>
                    <Radar className="size-4" aria-hidden />
                    {t("radar.action")}
                  </Link>
                </Button>
              )}
            </div>
            {radar !== "" && <p className="text-sm text-muted-foreground">{t("radar.hint")}</p>}
          </div>
        </div>
      )}

      <DocPreviewSheet
        doc={preview ? (preview.full ?? preview.doc) : null}
        fragmentId={preview?.fragmentId}
        highlight={preview?.highlight}
        loading={preview ? !preview.full && !preview.failed : false}
        error={preview?.failed ?? false}
        onRetry={retryPreview}
        onClose={closePreview}
      />
    </PageLayout>
  );
}
