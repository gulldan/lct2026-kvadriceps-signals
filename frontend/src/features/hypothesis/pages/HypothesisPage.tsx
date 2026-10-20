import { PageLayout } from "@/shared/ui/PageLayout";
import { useCallback, useEffect, useState } from "react";
import { useLocation, useNavigate, useParams } from "react-router-dom";
import { useTranslation } from "react-i18next";
import { ArrowLeft, FileText } from "lucide-react";

import {
  getHypothesis,
  listHypothesisRevisions,
  type ApiHypothesis,
} from "@/features/hypothesis/api";
import { listClusters } from "@/features/cluster";
import { listKPIs, type ApiKPI } from "@/features/kpi";
import { HypothesisDetailPanel } from "@/features/hypothesis/ui/HypothesisDetailPanel";
import { DirectionDetailPanel } from "@/features/hypothesis/ui/DirectionDetailPanel";
import {
  cleanInternalTitle,
  hypothesisClusterId,
  isResearchDirection,
} from "@/features/hypothesis/model";
import { useOwnerNames } from "@/features/hypothesis/owners";
import { buildHypothesisReport } from "@/features/hypothesis/report/hypothesisReport";
import { betPassportDoc } from "@/features/hypothesis/betPassport";
import type { RadarProvenance } from "@/features/hypothesis/radarProvenance";
import { PageToolbar, BackButton } from "@/shared/ui/PageHeader";
import { ReportExport } from "@/shared/report/ReportExport";
import { Spinner } from "@/shared/ui/Spinner";
import { ErrorState } from "@/shared/ui/ErrorState";

type Phase = "loading" | "ready" | "error";

export function HypothesisPage() {
  const { t } = useTranslation("hypothesisDetail");
  const { id = "" } = useParams<{ id: string }>();
  const navigate = useNavigate();
  const location = useLocation();
  const isDirectionRoute = location.pathname.startsWith("/directions/");

  const [phase, setPhase] = useState<Phase>("loading");
  const [hyp, setHyp] = useState<ApiHypothesis | null>(null);
  const [kpi, setKpi] = useState<ApiKPI | undefined>(undefined);
  const [clusterTitle, setClusterTitle] = useState<string | undefined>(undefined);
  const ownerName = useOwnerNames();

  const load = useCallback(
    async (silent = false) => {
      if (!silent) {
        setPhase("loading");
      }
      try {
        const [h, kpis, clusters] = await Promise.all([
          getHypothesis(id),
          listKPIs(),
          listClusters(),
        ]);
        setHyp(h);
        setKpi(h.kpi_id ? kpis.find((k) => k.id === h.kpi_id) : undefined);
        const clusterId = hypothesisClusterId(h);
        setClusterTitle(
          clusterId
            ? cleanInternalTitle(clusters.find((c) => c.id === clusterId)?.label ?? "")
            : undefined,
        );
        setPhase("ready");
      } catch {
        if (!silent) {
          setPhase("error");
        }
      }
    },
    [id],
  );

  useEffect(() => {
    void load();
  }, [load]);

  useEffect(() => {
    const needsAutoProcessing = hyp && (hyp.trl === null || !hyp.assessment?.check?.verdict);
    if (phase !== "ready" || !hyp || !needsAutoProcessing || isResearchDirection(hyp)) return;
    const intervalId = window.setInterval(() => void load(true), 12_000);
    return () => window.clearInterval(intervalId);
  }, [hyp, load, phase]);

  const reportDoc = async (h: ApiHypothesis) =>
    buildHypothesisReport({
      h,
      kpi,
      clusterTitle: clusterTitle || undefined,
      ownerName: ownerName(h.owner_id),
      revisions: await listHypothesisRevisions(h.id).catch(() => []),
    });

  const passportItem = (h: ApiHypothesis, radar: RadarProvenance) => ({
    label: t("actions.exportPassport"),
    icon: <FileText className="text-integration-word" aria-hidden />,
    run: async () => {
      const [{ downloadBlocksDocx }, revisions] = await Promise.all([
        import("@/shared/report/docx"),
        listHypothesisRevisions(h.id).catch(() => []),
      ]);
      await downloadBlocksDocx(betPassportDoc({ h, radar, revisions }));
    },
  });

  const showExport = phase === "ready" && hyp !== null && !isResearchDirection(hyp);

  return (
    <PageLayout>
      <PageToolbar>
        <BackButton onClick={() => navigate(-1)}>
          <ArrowLeft className="size-4" aria-hidden />
          {t("actions.back")}
        </BackButton>
        {showExport && (
          <ReportExport
            items={hyp.detail.radar ? [passportItem(hyp, hyp.detail.radar)] : undefined}
            doc={() => reportDoc(hyp)}
            onPrint={() => navigate(`/hypotheses/${id}/report`)}
          />
        )}
      </PageToolbar>

      {phase === "loading" && (
        <div className="flex justify-center py-14">
          <Spinner
            size="lg"
            label={t(isDirectionRoute ? "page.loadingDirection" : "page.loadingHypothesis")}
          />
        </div>
      )}

      {phase === "error" && (
        <ErrorState
          message={t(isDirectionRoute ? "page.errorDirection" : "page.errorHypothesis")}
          onRetry={() => void load()}
        />
      )}

      {phase === "ready" && hyp && isResearchDirection(hyp) && (
        <DirectionDetailPanel direction={hyp} clusterTitle={clusterTitle} />
      )}

      {phase === "ready" && hyp && !isResearchDirection(hyp) && (
        <HypothesisDetailPanel
          hypothesis={hyp}
          kpiTitle={kpi?.title}
          clusterTitle={clusterTitle}
          onChanged={setHyp}
        />
      )}
    </PageLayout>
  );
}
