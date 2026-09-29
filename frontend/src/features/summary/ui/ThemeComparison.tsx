import { useEffect, useState } from "react";
import { Link } from "react-router-dom";
import { useTranslation } from "react-i18next";
import { ArrowUpRight } from "lucide-react";
import { getCluster, type ApiCluster } from "@/features/cluster";
import {
  cleanInternalTitle,
  hasInternalNoise,
  ITC_FACTORS,
  ItcSummary,
  itcFactorValue,
  itcIndex,
} from "@/features/hypothesis";
import { formatNumber } from "@/shared/lib/format";
import { cn } from "@/shared/lib/cn";
import { Button } from "@/shared/ui/Button";
import { Disclosure } from "@/shared/ui/Disclosure";
import { ErrorState } from "@/shared/ui/ErrorState";
import { Skeleton } from "@/shared/ui/Skeleton";

const COLUMNS =
  "grid min-w-0 flex-1 grid-cols-[minmax(0,1fr)_4.5rem] items-center gap-4 md:grid-cols-[minmax(0,1fr)_4.5rem_repeat(4,4.5rem)]";
const number = (value: number | null) =>
  value === null ? "—" : formatNumber(value, { maximumFractionDigits: 1 });

function ThemeDetails({ id }: { id: string }) {
  const { t } = useTranslation("summary");
  const [detail, setDetail] = useState<ApiCluster | null>(null);
  const [error, setError] = useState(false);
  const [attempt, setAttempt] = useState(0);
  useEffect(() => {
    let current = true;
    setError(false);
    getCluster(id).then(
      (value) => {
        if (current) setDetail(value);
      },
      () => {
        if (current) setError(true);
      },
    );
    return () => {
      current = false;
    };
  }, [id, attempt]);

  if (error)
    return <ErrorState message={t("detail.error")} onRetry={() => setAttempt((v) => v + 1)} />;
  if (!detail)
    return (
      <output aria-label={t("detail.loading")} className="block space-y-3 py-4">
        <Skeleton className="h-5 w-2/3" />
        <Skeleton className="h-64 w-full" />
      </output>
    );
  return (
    <div className="border-t pt-5">
      <div className="mb-5 flex flex-wrap items-start justify-between gap-4">
        {detail.summary && !hasInternalNoise(detail.summary) && (
          <p className="max-w-prose text-sm leading-relaxed text-muted-foreground">
            {detail.summary}
          </p>
        )}
        <Button asChild variant="link" size="inline" className="shrink-0">
          <Link to={`/clusters/${id}`}>
            {t("detail.open")}
            <ArrowUpRight className="size-4" aria-hidden />
          </Link>
        </Button>
      </div>
      {detail.params?.itc ? (
        <ItcSummary itc={detail.params.itc} bare />
      ) : (
        <p className="text-sm text-muted-foreground">{t("detail.pending")}</p>
      )}
    </div>
  );
}

export function ThemeComparison({ themes }: { themes: ApiCluster[] }) {
  const { t } = useTranslation("summary");
  const [expanded, setExpanded] = useState<string | null>(null);
  return (
    <div className="border-b">
      <div className={cn(COLUMNS, "py-3 pr-7 text-xs text-muted-foreground")} aria-hidden>
        <span>{t("comparison.theme")}</span>
        <span className="text-right">{t("comparison.index")}</span>
        {ITC_FACTORS.map(({ key }) => (
          <span className="hidden text-right md:block" key={key}>
            {t(`comparison.${key}`)}
          </span>
        ))}
      </div>
      {themes.map((theme) => (
        <Disclosure
          key={theme.id}
          name="theme-comparison"
          className="[&[open]>summary]:bg-secondary"
          summaryClassName="rounded-none px-1 hover:bg-secondary"
          onToggle={(event) => {
            const open = event.currentTarget.open;
            setExpanded((previous) => (open ? theme.id : previous === theme.id ? null : previous));
          }}
          title={
            <div className={COLUMNS}>
              <div className="min-w-0">
                <span className="block text-sm leading-snug font-medium wrap-anywhere">
                  {cleanInternalTitle(theme.label)}
                </span>
                <span className="mt-1.5 block text-xs font-normal text-muted-foreground">
                  {t("comparison.documents", { count: theme.document_count })}
                  {theme.params?.itc?.signals?.pub_count !== undefined && (
                    <>
                      {" "}
                      ·{" "}
                      {t("comparison.dates", {
                        known: theme.params.itc.signals.pub_count,
                        total: theme.document_count,
                      })}
                    </>
                  )}
                </span>
              </div>
              <span className="text-right font-mono text-base font-medium text-brand tabular-nums">
                <span className="sr-only">{t("comparison.index")}: </span>
                {number(itcIndex(theme.params?.itc))}
              </span>
              {ITC_FACTORS.map(({ key }) => (
                <span
                  className="hidden text-right font-mono text-sm font-normal tabular-nums md:block"
                  key={key}
                >
                  <span className="sr-only">{t(`comparison.${key}`)}: </span>
                  {number(itcFactorValue(theme.params?.itc, key))}
                </span>
              ))}
            </div>
          }
        >
          {expanded === theme.id && <ThemeDetails id={theme.id} />}
        </Disclosure>
      ))}
    </div>
  );
}
