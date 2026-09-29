import { useTranslation } from "react-i18next";
import { formatDateTime, formatNumber } from "@/shared/lib/format";
import { useRequest } from "@/shared/lib/useRequest";
import { ErrorState } from "@/shared/ui/ErrorState";
import { MetricTile } from "@/shared/ui/MetricTile";
import { Section } from "@/shared/ui/Section";
import { Skeleton } from "@/shared/ui/Skeleton";
import { getOpenAlexBudgets } from "../api";

export function OpenAlexBudget() {
  const { t } = useTranslation("admin");
  const { status, data, retry } = useRequest("admin-openalex", getOpenAlexBudgets);
  return (
    <Section
      className="mt-8"
      title={t("metrics.openalex.title")}
      description={t("metrics.openalex.description")}
    >
      {status === "error" ? (
        <ErrorState message={t("metrics.loadFailed")} onRetry={retry} />
      ) : !data ? (
        <Skeleton className="h-24 w-full" />
      ) : (
        <div className="grid grid-cols-1 gap-x-6 border-y py-2 sm:grid-cols-2">
          {data.budgets.map((budget) => (
            <MetricTile
              key={budget.credential}
              variant="plain"
              label={t(`metrics.openalex.${budget.credential}`)}
              value={
                budget.known
                  ? t("metrics.openalex.remaining", {
                      remaining: formatNumber(budget.remaining ?? 0),
                      limit: formatNumber(budget.limit ?? 0),
                    })
                  : t("metrics.openalex.unknown")
              }
              sub={
                budget.known
                  ? t("metrics.openalex.sub", {
                      spent: formatNumber(budget.spent ?? 0),
                      reserve: formatNumber(budget.reserve ?? 0),
                      reset: formatDateTime(budget.reset_at ?? ""),
                    })
                  : undefined
              }
            />
          ))}
        </div>
      )}
    </Section>
  );
}
