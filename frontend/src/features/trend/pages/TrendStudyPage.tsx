import { useEffect, useState } from "react";
import { Link, useSearchParams } from "react-router-dom";
import { useTranslation } from "react-i18next";
import { ArrowRight, RefreshCw } from "lucide-react";
import { PageLayout } from "@/shared/ui/PageLayout";
import { PageHeader } from "@/shared/ui/PageHeader";
import { Button } from "@/shared/ui/Button";
import { Notice } from "@/shared/ui/Notice";
import { Skeleton } from "@/shared/ui/Skeleton";
import { getTrendStudy, type TrendStudy } from "../studyApi";
import { StudyExplorer } from "../ui/StudyExplorer";
import { useReturnFocus } from "../useReturnFocus";
import { TrendViewSwitch } from "../ui/TrendViewSwitch";

export function TrendStudyPage() {
  const { t } = useTranslation("trend");
  const [params, setParams] = useSearchParams();
  const [study, setStudy] = useState<TrendStudy>();
  const [failed, setFailed] = useState(false);
  const [attempt, setAttempt] = useState(0);
  const [busy, setBusy] = useState(true);
  useReturnFocus(!!study);
  useEffect(() => {
    const controller = new AbortController();
    setBusy(true);
    setFailed(false);
    getTrendStudy(controller.signal)
      .then((value) => {
        if (!controller.signal.aborted) setStudy(value);
      })
      .catch(() => {
        if (!controller.signal.aborted) setFailed(true);
      })
      .finally(() => {
        if (!controller.signal.aborted) setBusy(false);
      });
    return () => controller.abort();
  }, [attempt]);
  return (
    <PageLayout className="space-y-6">
      <PageHeader
        title={t("study.title")}
        actions={
          <>
            <TrendViewSwitch value="study" />
            <Button
              variant="outline"
              size="icon"
              aria-label={t("study.refresh")}
              title={t("study.refresh")}
              onClick={() => setAttempt((n) => n + 1)}
              disabled={busy}
            >
              <RefreshCw aria-hidden />
            </Button>
            <Button asChild>
              <Link to="/trends?view=analysis">
                {t("study.openMap")}
                <ArrowRight aria-hidden />
              </Link>
            </Button>
          </>
        }
      />
      {failed && (
        <Notice tone="risk" role="alert">
          {t("study.error")}{" "}
          <Button variant="link" size="inline" onClick={() => setAttempt((n) => n + 1)}>
            {t("retry")}
          </Button>
        </Notice>
      )}
      {!study && busy && (
        <div>
          <Skeleton className="h-96 w-full" />
          <output className="sr-only">{t("study.loading")}</output>
        </div>
      )}
      {study && <StudyExplorer study={study} params={params} setParams={setParams} busy={busy} />}
    </PageLayout>
  );
}
