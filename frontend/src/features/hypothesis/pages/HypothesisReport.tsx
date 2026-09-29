import { useCallback, useEffect, useState } from "react";
import { Link } from "react-router-dom";
import { useTranslation } from "react-i18next";
import { ArrowLeft } from "lucide-react";

import { loadReportData, portfolioReportDoc } from "@/features/hypothesis/report/reportData";
import type { ReportDoc } from "@/shared/report/blocks";
import { ReportBlocks } from "@/shared/report/ReportBlocks";
import { ReportExport } from "@/shared/report/ReportExport";
import { PageLayout } from "@/shared/ui/PageLayout";
import { PageToolbar, BackButton } from "@/shared/ui/PageHeader";
import { ReadingContent } from "@/shared/ui/ReadingContent";
import { ErrorState } from "@/shared/ui/ErrorState";
import { Skeleton } from "@/shared/ui/Skeleton";

export function HypothesisReport() {
  const { t } = useTranslation("hypothesis");
  const [report, setReport] = useState<ReportDoc | null>(null);
  const [failed, setFailed] = useState(false);

  const load = useCallback(async () => {
    setFailed(false);
    setReport(null);
    try {
      setReport(portfolioReportDoc(await loadReportData()));
    } catch {
      setFailed(true);
    }
  }, []);

  useEffect(() => {
    void load();
  }, [load]);

  if (failed) {
    return <ErrorState message={t("report.loadError")} onRetry={() => void load()} />;
  }
  if (report === null) {
    return <Skeleton className="mx-auto mt-10 h-[600px] w-full max-w-3xl rounded-xl" />;
  }

  return (
    <PageLayout className="print:max-w-none print:p-0">
      <style>{`@media print { @page { size: A4; margin: 16mm; } tr { break-inside: avoid; } }`}</style>

      <PageToolbar>
        <BackButton asChild>
          <Link to="/hypotheses">
            <ArrowLeft className="size-4" aria-hidden />
            {t("report.back")}
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
