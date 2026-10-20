import { useCallback, useEffect, useMemo, useState } from "react";
import { Link, useParams } from "react-router-dom";
import { useTranslation } from "react-i18next";
import { ArrowLeft } from "lucide-react";

import {
  getHypothesis,
  listHypothesisRevisions,
  type ApiHypothesis,
  type ApiRevision,
} from "@/features/hypothesis/api";
import { listClusters } from "@/features/cluster";
import { listKPIs, type ApiKPI } from "@/features/kpi";
import { cleanInternalTitle, hypothesisClusterId } from "@/features/hypothesis/model";
import { useOwnerNames } from "@/features/hypothesis/owners";
import { buildHypothesisReport } from "@/features/hypothesis/report/hypothesisReport";
import { ReportBlocks } from "@/shared/report/ReportBlocks";
import { ReportExport } from "@/shared/report/ReportExport";
import { PageLayout } from "@/shared/ui/PageLayout";
import { PageToolbar, BackButton } from "@/shared/ui/PageHeader";
import { ReadingContent } from "@/shared/ui/ReadingContent";
import { ErrorState } from "@/shared/ui/ErrorState";
import { Skeleton } from "@/shared/ui/Skeleton";

type Phase = "loading" | "error" | "ready";

export function HypothesisReportPage() {
  const { t } = useTranslation("hypothesis");
  const { id = "" } = useParams<{ id: string }>();
  const [phase, setPhase] = useState<Phase>("loading");
  const [hyp, setHyp] = useState<ApiHypothesis | null>(null);
  const [kpi, setKpi] = useState<ApiKPI | undefined>(undefined);
  const [clusterTitle, setClusterTitle] = useState<string | undefined>(undefined);
  const [revisions, setRevisions] = useState<ApiRevision[]>([]);
  const ownerName = useOwnerNames();

  const load = useCallback(async () => {
    setPhase("loading");
    try {
      const [h, kpis, clusters, revs] = await Promise.all([
        getHypothesis(id),
        listKPIs().catch(() => [] as ApiKPI[]),
        listClusters().catch(() => []),
        listHypothesisRevisions(id).catch(() => [] as ApiRevision[]),
      ]);
      setHyp(h);
      setKpi(h.kpi_id ? kpis.find((k) => k.id === h.kpi_id) : undefined);
      const clusterId = hypothesisClusterId(h);
      setClusterTitle(
        clusterId
          ? cleanInternalTitle(clusters.find((c) => c.id === clusterId)?.label ?? "") || undefined
          : undefined,
      );
      setRevisions(revs);
      setPhase("ready");
    } catch {
      setPhase("error");
    }
  }, [id]);

  useEffect(() => {
    void load();
  }, [load]);

  const report = useMemo(
    () =>
      hyp
        ? buildHypothesisReport({
            h: hyp,
            kpi,
            clusterTitle,
            ownerName: ownerName(hyp.owner_id),
            revisions,
          })
        : null,
    [hyp, kpi, clusterTitle, ownerName, revisions],
  );

  if (phase === "error") {
    return <ErrorState message={t("report.loadError")} onRetry={() => void load()} />;
  }
  if (phase === "loading" || report === null) {
    return <Skeleton className="mx-auto mt-10 h-[600px] w-full max-w-3xl rounded-xl" />;
  }

  return (
    <PageLayout className="print:max-w-none print:p-0">
      <style>{`@media print { @page { size: A4; margin: 16mm; } tr { break-inside: avoid; } }`}</style>

      <PageToolbar>
        <BackButton asChild>
          <Link to={`/hypotheses/${id}`}>
            <ArrowLeft className="size-4" aria-hidden />
            {t("hreport.backToHypothesis")}
          </Link>
        </BackButton>
        <ReportExport doc={() => report} />
      </PageToolbar>

      <ReadingContent className="print:max-w-none print:text-sm">
        <ReportBlocks blocks={report.blocks} />
      </ReadingContent>
    </PageLayout>
  );
}
