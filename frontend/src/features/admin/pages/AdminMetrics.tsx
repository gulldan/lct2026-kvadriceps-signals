import { useState } from "react";
import { useTranslation } from "react-i18next";
import { useBackgroundTasks } from "@/features/task";
import { PageLayout } from "@/shared/ui/PageLayout";
import { PageHeader } from "@/shared/ui/PageHeader";
import { Tabs, TabPanel } from "@/shared/ui/Tabs";
import { UsageMetrics } from "../ui/UsageMetrics";
import { PerformanceMetrics } from "../ui/PerformanceMetrics";
import { OpenAlexBudget } from "../ui/OpenAlexBudget";

export function AdminMetrics() {
  const { t } = useTranslation("admin");
  const [view, setView] = useState("budget");
  const { activity, refreshActivity } = useBackgroundTasks();
  return (
    <PageLayout>
      <PageHeader title={t("metrics.title")} description={t("metrics.description")} />
      <Tabs
        id="metrics-tabs"
        className="mt-6"
        active={view}
        onChange={setView}
        tabs={[
          { id: "budget", label: t("metrics.viewBudget") },
          { id: "perf", label: t("metrics.viewPerf") },
        ]}
      />
      <TabPanel tabsId="metrics-tabs" active={view}>
        {view === "budget" ? (
          <>
            <UsageMetrics />
            <OpenAlexBudget />
          </>
        ) : (
          <PerformanceMetrics workers={activity?.workers ?? []} onRefresh={refreshActivity} />
        )}
      </TabPanel>
    </PageLayout>
  );
}
