import { useCallback, useEffect, useState } from "react";
import { FilterBar } from "@/shared/ui/FilterBar";
import { useTranslation } from "react-i18next";
import { RefreshCw } from "lucide-react";
import { getLlmUsage, type LlmUsage } from "../api";
import { usageDays } from "../usageSeries";
import { formatNumber } from "@/shared/lib/format";
import { BarChart } from "@/shared/ui/BarChart";
import { Button } from "@/shared/ui/Button";
import { Disclosure } from "@/shared/ui/Disclosure";
import { ErrorState } from "@/shared/ui/ErrorState";
import { MetricTile } from "@/shared/ui/MetricTile";
import { Section } from "@/shared/ui/Section";
import { Segmented } from "@/shared/ui/Segmented";
import { Skeleton } from "@/shared/ui/Skeleton";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/shared/ui/Table";

const cost = (value: number) => `${formatNumber(value, { maximumFractionDigits: 2 })} ₽`;

export function UsageMetrics() {
  const { t } = useTranslation("admin");
  const [days, setDays] = useState<7 | 30>(7);
  const [usage, setUsage] = useState<LlmUsage | null>(null);
  const [phase, setPhase] = useState<"loading" | "ready" | "error">("loading");
  const [metric, setMetric] = useState<"requests" | "tokens">("requests");
  const load = useCallback(async () => {
    setPhase("loading");
    try {
      setUsage(await getLlmUsage(days));
      setPhase("ready");
    } catch {
      setPhase("error");
    }
  }, [days]);
  useEffect(() => {
    void load();
  }, [load]);

  const totals = usage?.totals;
  const unpriced =
    usage?.providers.some((provider) => provider.kind === "notional" && provider.cost_usd === 0) ??
    false;
  const controls = (
    <FilterBar>
      <Segmented
        aria-label={t("metrics.usage.periodAria")}
        value={String(days)}
        onChange={(value) => setDays(value === "30" ? 30 : 7)}
        options={[
          { value: "7", label: t("metrics.usage.days7") },
          { value: "30", label: t("metrics.usage.days30") },
        ]}
      />
      <Button variant="ghost" disabled={phase === "loading"} onClick={() => void load()}>
        <RefreshCw className="size-3.5" aria-hidden />
        {t("actions.refresh")}
      </Button>
    </FilterBar>
  );
  return (
    <Section
      className="mt-6"
      title={t("metrics.usage.title")}
      description={
        usage
          ? t("metrics.usage.rangeDescription", {
              provider: usage.provider,
              days: usage.range_days,
            })
          : undefined
      }
      action={controls}
    >
      {phase === "loading" ? (
        <Skeleton className="h-56 w-full" />
      ) : phase === "error" ? (
        <ErrorState message={t("metrics.loadFailed")} onRetry={() => void load()} />
      ) : (
        usage &&
        totals && (
          <>
            <div className="grid grid-cols-2 gap-x-6 border-y py-2 sm:grid-cols-3">
              <MetricTile
                variant="plain"
                label={t("metrics.readable.requests")}
                value={formatNumber(totals.requests)}
                sub={t("metrics.usage.requestsTodaySubShort", {
                  count: usage.quota.today_requests,
                })}
              />
              <MetricTile
                variant="plain"
                label={t("metrics.usage.tokensForRange")}
                value={formatNumber(totals.total_tokens)}
                sub={t("metrics.readable.tokenSplit", {
                  input: formatNumber(totals.prompt_tokens),
                  output: formatNumber(totals.completion_tokens),
                })}
              />
              <MetricTile
                variant="plain"
                label={t("metrics.usage.cost")}
                value={
                  unpriced && usage.total_cost_rub === 0
                    ? t("metrics.readable.unpriced")
                    : cost(usage.total_cost_rub)
                }
                sub={
                  unpriced
                    ? t("metrics.readable.noRate")
                    : usage.cost_estimated
                      ? t("metrics.usage.estimatedNote")
                      : t("metrics.readable.costSource")
                }
              />
            </div>
            <div className="mt-6 grid items-start gap-8 xl:grid-cols-5">
              <div className="min-w-0 xl:col-span-3">
                <BarChart
                  action={
                    <Segmented
                      aria-label={t("metrics.usage.chartMetricAria")}
                      value={metric}
                      onChange={setMetric}
                      options={[
                        { value: "requests", label: t("metrics.usage.chartRequests") },
                        { value: "tokens", label: t("metrics.usage.chartTokens") },
                      ]}
                    />
                  }
                  label={
                    metric === "requests"
                      ? t("metrics.usage.requestsByDay")
                      : t("metrics.usage.tokensByDay")
                  }
                  data={usageDays(usage, metric)}
                />
                {totals.requests === 0 && (
                  <p className="mt-4 text-sm text-muted-foreground">{t("metrics.usage.noData")}</p>
                )}
              </div>
              <Section title={t("metrics.usage.byOperation")} className="min-w-0 xl:col-span-2">
                <UsageTable
                  rows={usage.by_operation
                    .toSorted((a, b) => b.requests - a.requests)
                    .map((row) => ({
                      label: t(`metrics.operations.${row.operation}`, {
                        defaultValue: row.operation,
                      }),
                      requests: row.requests,
                      tokens: row.total_tokens,
                    }))}
                />
              </Section>
            </div>
            <div className="mt-8 grid gap-8 lg:grid-cols-2">
              <Section title={t("metrics.usage.byModel")}>
                <UsageTable
                  rows={usage.by_model.map((row) => ({
                    label: row.model,
                    requests: row.requests,
                    tokens: row.total_tokens,
                  }))}
                />
              </Section>
              <Section title={t("metrics.usage.byProviderTitle")}>
                <Table>
                  <TableHeader>
                    <TableRow>
                      <TableHead>{t("metrics.readable.provider")}</TableHead>
                      <TableHead className="text-right">{t("metrics.usage.cost")}</TableHead>
                    </TableRow>
                  </TableHeader>
                  <TableBody>
                    {usage.providers.map((provider) => (
                      <TableRow key={provider.key}>
                        <TableCell>
                          <span className="font-medium">{provider.label}</span>
                          <p className="mt-1 text-xs text-muted-foreground">
                            {t("metrics.readable.providerVolume", {
                              requests: formatNumber(provider.requests),
                              tokens: formatNumber(provider.total_tokens),
                            })}
                          </p>
                        </TableCell>
                        <TableCell className="text-right font-mono">
                          {provider.kind === "free"
                            ? t("metrics.usage.free")
                            : provider.kind === "notional" && provider.cost_usd === 0
                              ? t("metrics.readable.unpriced")
                              : `${formatNumber(provider.currency === "₽" || provider.currency === "RUB" ? provider.cost_rub : provider.cost_usd, { maximumFractionDigits: 4 })} ${provider.currency}`}
                          <p className="mt-1 font-sans text-xs text-muted-foreground">
                            {provider.cost_estimated ? t("metrics.usage.estimatedNote") : ""}
                          </p>
                        </TableCell>
                      </TableRow>
                    ))}
                  </TableBody>
                </Table>
              </Section>
            </div>
            <Disclosure className="mt-6" title={t("metrics.readable.limits")}>
              <dl className="grid gap-4 text-sm sm:grid-cols-3">
                <div>
                  <dt className="text-muted-foreground">{t("metrics.usage.requestsToday")}</dt>
                  <dd className="mt-1 font-mono">
                    {formatNumber(usage.quota.today_requests)}
                    {usage.quota.daily_limit > 0
                      ? ` / ${formatNumber(usage.quota.daily_limit)}`
                      : ""}
                  </dd>
                  <p className="mt-1 text-xs text-muted-foreground">
                    {usage.quota.daily_limit > 0
                      ? t("metrics.readable.dailyLimit")
                      : t("metrics.usage.noQuota")}
                  </p>
                </div>
                <div>
                  <dt className="text-muted-foreground">{t("metrics.usage.credits")}</dt>
                  <dd className="mt-1">
                    {usage.budget.credits_total > 0
                      ? `${formatNumber(usage.budget.credits_remaining)} ${usage.currency}`
                      : t("metrics.usage.budgetUnset")}
                  </dd>
                </div>
                <div>
                  <dt className="text-muted-foreground">{t("metrics.usage.perHypothesis")}</dt>
                  <dd className="mt-1">
                    {usage.per_hypothesis.hypotheses > 0
                      ? cost(usage.per_hypothesis.cost_rub)
                      : t("noData")}
                  </dd>
                  <p className="mt-1 text-xs text-muted-foreground">
                    {t("metrics.usage.averagedOver", {
                      count: usage.per_hypothesis.hypotheses,
                      formatted: formatNumber(usage.per_hypothesis.hypotheses),
                    })}
                  </p>
                </div>
              </dl>
            </Disclosure>
          </>
        )
      )}
    </Section>
  );
}

function UsageTable({ rows }: { rows: { label: string; requests: number; tokens: number }[] }) {
  const { t } = useTranslation("admin");
  return (
    <Table>
      <TableHeader>
        <TableRow>
          <TableHead>{t("metrics.readable.name")}</TableHead>
          <TableHead className="text-right">{t("metrics.usage.chartRequests")}</TableHead>
          <TableHead className="text-right">{t("metrics.usage.chartTokens")}</TableHead>
        </TableRow>
      </TableHeader>
      <TableBody>
        {rows.length === 0 && (
          <TableRow>
            <TableCell colSpan={3}>{t("noData")}</TableCell>
          </TableRow>
        )}
        {rows.map((row) => (
          <TableRow key={row.label}>
            <TableCell className="max-w-64 break-words">{row.label}</TableCell>
            <TableCell className="text-right font-mono">{formatNumber(row.requests)}</TableCell>
            <TableCell className="text-right font-mono">{formatNumber(row.tokens)}</TableCell>
          </TableRow>
        ))}
      </TableBody>
    </Table>
  );
}
