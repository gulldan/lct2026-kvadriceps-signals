import type { ReactNode } from "react";
import { Link, useLocation } from "react-router-dom";
import { useTranslation } from "react-i18next";
import { ClipboardCheck } from "lucide-react";
import { PageLayout } from "@/shared/ui/PageLayout";
import { PageTitle } from "@/shared/ui/PageHeader";
import { Button } from "@/shared/ui/Button";
import { ListSkeleton } from "@/shared/ui/ListSkeleton";
import { Skeleton } from "@/shared/ui/Skeleton";
import { TrendScope } from "./TrendScope";

export function TrendWorkspace({ children }: { children: ReactNode }) {
  const { t } = useTranslation("trend");
  const location = useLocation();
  return (
    <PageLayout className="space-y-3 print:max-w-none print:p-0">
      <div className="flex flex-wrap items-start justify-between gap-3 print:hidden">
        <div className="flex min-w-0 items-center gap-2">
          <PageTitle>{t("study.title")}</PageTitle>
          <Button asChild variant="ghost" size="icon-sm">
            <Link
              to="/trends/model"
              state={{ back: location.pathname + location.search }}
              aria-label={t("modelCheck.link")}
              title={t("modelCheck.link")}
            >
              <ClipboardCheck aria-hidden />
            </Link>
          </Button>
        </div>
        <TrendScope />
      </div>
      {children}
    </PageLayout>
  );
}

export function TrendCatalogueLoading() {
  const { t } = useTranslation("trend");
  return (
    <output aria-label={t("study.loading")} className="block space-y-3">
      <Skeleton className="h-control w-full" />
      <Skeleton className="h-18 w-full" />
      <ListSkeleton rows={5} />
    </output>
  );
}
