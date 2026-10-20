import { useMemo, useState } from "react";
import { useTranslation } from "react-i18next";
import { ExternalLink } from "lucide-react";
import { describeError } from "@/shared/api/errors";
import { formatDate, formatDateTime, formatMonth, formatNumber } from "@/shared/lib/format";
import { researchCategoryLabel, researchTaxonomy } from "@/shared/lib/researchTaxonomy";
import { useRequest } from "@/shared/lib/useRequest";
import { BarChart } from "@/shared/ui/BarChart";
import { Button } from "@/shared/ui/Button";
import { Disclosure } from "@/shared/ui/Disclosure";
import { FieldRow } from "@/shared/ui/Fields";
import { FilterBar } from "@/shared/ui/FilterBar";
import { Notice } from "@/shared/ui/Notice";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/shared/ui/Select";
import { Skeleton } from "@/shared/ui/Skeleton";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/shared/ui/Table";
import {
  MONEY_KINDS,
  MONEY_REGIONS,
  moneyAmount,
  moneyCells,
  moneyMonths,
  monthRange,
  moneyQuadrant,
  moneyRegions,
  moneyRubrics,
  moneyStages,
  moneyTotals,
  moneyWindows,
} from "../earlyMoney";
import { getLiveMoney, type MoneyEvent, type MoneyFilter } from "../moneyApi";
import { MetricTile } from "@/shared/ui/MetricTile";
import { MoneyRubricTable } from "./MoneyRubricTable";

const CACHE_MS = 5 * 60_000;

export function useRubricLabel() {
  const { i18n } = useTranslation();
  return useMemo(() => {
    const byId = new Map(researchTaxonomy.categories.map((category) => [category.id, category]));
    return (id: string) => {
      const category = byId.get(id);
      return category ? researchCategoryLabel(category, i18n.language) : id;
    };
  }, [i18n.language]);
}

/** Сделка одной строкой: компания, стадия, сумма и ссылка на первоисточник. */
export function MoneyEventLine({ event }: { event: MoneyEvent }) {
  const { t } = useTranslation("trend");
  const label = useRubricLabel();
  const facts = [
    t(`money.stages.${event.stage}`),
    moneyAmount(event),
    event.investors,
    event.rubric && label(event.rubric),
    t(`money.regions.${event.region}`),
    formatDate(event.date),
  ].filter(Boolean);
  return (
    <li className="space-y-1 text-sm">
      <Button asChild variant="link" size="inline" className="text-left">
        <a href={event.url} target="_blank" rel="noopener noreferrer">
          {event.company || event.title}
          <ExternalLink aria-hidden />
        </a>
      </Button>
      <p className="text-xs text-muted-foreground">{facts.join(" · ")}</p>
      {event.quote && event.quote !== event.title && (
        <p className="text-xs leading-relaxed">«{event.quote}»</p>
      )}
      <p className="text-xs text-muted-foreground">
        {event.source}
        {event.rubric_basis === "name" && <> · {t("money.rubricBasisName")}</>}
      </p>
    </li>
  );
}

/** Последние сделки таблицей: цитата первоисточника — в подсказке ссылки, чтобы строка читалась одним взглядом. */
function MoneyLatestTable({
  events,
  label,
}: {
  events: MoneyEvent[];
  label: (rubric: string) => string;
}) {
  const { t } = useTranslation("trend");
  return (
    <Table aria-label={t("money.latestTitle")} className="text-sm">
      <TableHeader>
        <TableRow>
          <TableHead>{t("money.latest.date")}</TableHead>
          <TableHead>{t("money.latest.company")}</TableHead>
          <TableHead>{t("money.latest.stage")}</TableHead>
          <TableHead className="text-right">{t("money.latest.amount")}</TableHead>
          <TableHead>{t("money.latest.investors")}</TableHead>
          <TableHead>{t("money.table.industry")}</TableHead>
          <TableHead>{t("money.regionLabel")}</TableHead>
        </TableRow>
      </TableHeader>
      <TableBody>
        {events.map((event) => (
          <TableRow key={event.id}>
            <TableCell className="font-mono text-xs whitespace-nowrap tabular-nums">
              {formatDate(event.date)}
            </TableCell>
            <TableCell className="max-w-60 min-w-36">
              <Button asChild variant="link" size="inline" className="text-left">
                <a
                  href={event.url}
                  target="_blank"
                  rel="noopener noreferrer"
                  title={[event.quote, event.source].filter(Boolean).join(" — ")}
                >
                  {/* Заголовок заметки в колонке «Компания» читался как имя; цитата и источник — в подсказке. */}
                  {event.company || (
                    <span className="text-muted-foreground">{t("money.latest.unnamed")}</span>
                  )}
                  <ExternalLink aria-hidden />
                </a>
              </Button>
            </TableCell>
            <TableCell className="whitespace-nowrap">{t(`money.stages.${event.stage}`)}</TableCell>
            <TableCell className="text-right font-mono text-xs whitespace-nowrap tabular-nums">
              {moneyAmount(event) ?? "—"}
            </TableCell>
            <TableCell className="max-w-56 min-w-32 text-xs">
              <span className="line-clamp-2" title={event.investors}>
                {event.investors || "—"}
              </span>
            </TableCell>
            <TableCell className="text-xs">{event.rubric ? label(event.rubric) : "—"}</TableCell>
            <TableCell className="text-xs whitespace-nowrap">
              {t(`money.regions.${event.region}`)}
            </TableCell>
          </TableRow>
        ))}
      </TableBody>
    </Table>
  );
}

export function EarlyMoney() {
  const { t, i18n } = useTranslation("trend");
  const { t: tCommon } = useTranslation("common");
  const label = useRubricLabel();
  const [filter, setFilter] = useState<MoneyFilter>({ region: "all", kind: "all" });
  const money = useRequest(
    `live-money:${filter.region}:${filter.kind}`,
    (signal) => getLiveMoney(filter, signal),
    { cacheMs: CACHE_MS },
  );
  if (money.status === "error")
    return <Notice tone="risk">{describeError(money.error, tCommon)}</Notice>;
  if (money.status !== "ready" || !money.data) return <Skeleton className="h-64 w-full" />;
  const summary = money.data;
  if (summary.status !== "ready")
    return <Notice>{t(summary.status === "loading" ? "money.loading" : "money.failed")}</Notice>;
  const cells = moneyCells(summary, filter);
  const totals = moneyTotals(cells);
  const quadrant = moneyQuadrant(summary, cells);
  const regions = moneyRegions(cells);
  const widest = Math.max(1, ...regions.map((item) => item.count));
  const stages = moneyStages(cells);
  const windows = moneyWindows(summary.months);
  const months = moneyMonths(cells, [...windows.prior, ...windows.recent, windows.current ?? ""]);
  const range = (value: string | string[] | undefined) => monthRange(value, i18n.language) ?? "—";
  const windowLabel = (word: string, value: string | string[] | undefined) => (
    <>
      <span className="block font-medium text-foreground">{word}</span>
      {range(value)}
    </>
  );
  return (
    <div className="space-y-6 py-2">
      <div className="space-y-1">
        <h2 className="text-lg font-semibold">{t("money.title")}</h2>
        <p className="max-w-prose text-sm text-muted-foreground">
          {t("money.intro", {
            from: formatDate(summary.from ?? ""),
            through: formatDate(summary.through ?? ""),
          })}
        </p>
      </div>
      <FilterBar className="gap-3">
        <FieldRow id="money-region" label={t("money.regionLabel")}>
          <Select
            value={filter.region}
            onValueChange={(value) =>
              setFilter({ ...filter, region: value as MoneyFilter["region"] })
            }
          >
            <SelectTrigger id="money-region" className="w-56">
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              {(["all", ...MONEY_REGIONS] as const).map((value) => (
                <SelectItem key={value} value={value}>
                  {value === "all" ? t("money.all") : t(`money.regions.${value}`)}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
        </FieldRow>
        <FieldRow id="money-kind" label={t("money.kindLabel")}>
          <Select
            value={filter.kind}
            onValueChange={(value) => setFilter({ ...filter, kind: value as MoneyFilter["kind"] })}
          >
            <SelectTrigger id="money-kind" className="w-48">
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              {(["all", ...MONEY_KINDS] as const).map((value) => (
                <SelectItem key={value} value={value}>
                  {value === "all" ? t("money.all") : t(`money.kinds.${value}`)}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
        </FieldRow>
      </FilterBar>
      <div className="grid grid-cols-2 gap-x-6 gap-y-3 border-b pb-4 sm:grid-cols-3 xl:grid-cols-6">
        <MetricTile
          variant="plain"
          label={t("money.kpi.total")}
          value={formatNumber(totals.total)}
          sub={t("money.kpi.classified", {
            share: formatNumber(totals.total ? (totals.classified / totals.total) * 100 : 0, {
              maximumFractionDigits: 0,
            }),
          })}
        />
        {MONEY_KINDS.map((kind) => (
          <MetricTile
            key={kind}
            variant="plain"
            label={t(`money.kinds.${kind}`)}
            value={formatNumber(totals.kinds[kind])}
            sub={
              kind === "round" && totals.kinds.round > 0
                ? t("money.kpi.stages", {
                    preSeed: formatNumber(stages.pre_seed ?? 0),
                    seed: formatNumber(stages.seed ?? 0),
                    seriesA: formatNumber(stages.series_a ?? 0),
                  })
                : t(`money.kpi.${kind}`)
            }
          />
        ))}
      </div>
      {totals.total === 0 ? (
        <Notice tone="muted">{t("money.empty")}</Notice>
      ) : (
        <div className="grid items-start gap-8 xl:grid-cols-[minmax(0,2fr)_minmax(0,1fr)]">
          <MoneyRubricTable
            rubrics={moneyRubrics(cells)}
            points={quadrant.points}
            label={label}
            periods={[
              t("money.periods.money", {
                prior: range(windows.prior),
                recent: range(windows.recent),
              }),
              t("money.periods.science", {
                prior: range(summary.science_prior),
                recent: range(summary.science_recent),
              }),
            ]}
          />
          <div className="min-w-0 space-y-8">
            <BarChart
              label={t("money.monthsTitle")}
              data={months.map((item) => ({
                label: formatMonth(item.month),
                value: item.count,
                displayValue: t("money.monthEvents", { count: formatNumber(item.count) }),
              }))}
              spans={[
                {
                  id: "prior",
                  size: windows.prior.length,
                  label: windowLabel(t("money.window.prior"), windows.prior),
                },
                {
                  id: "recent",
                  size: windows.recent.length,
                  label: windowLabel(t("money.window.recent"), windows.recent),
                },
                {
                  id: "current",
                  size: 1,
                  label: windowLabel(t("money.window.current"), windows.current),
                },
              ]}
            />
            <figure className="min-w-0 space-y-3">
              <figcaption className="text-sm font-medium">{t("money.regionsTitle")}</figcaption>
              <ol className="space-y-2" aria-label={t("money.regionsTitle")}>
                {regions.map((item) => (
                  <li
                    key={item.region}
                    className="grid grid-cols-[minmax(0,9rem)_minmax(0,1fr)_3.5rem] items-center gap-3 text-sm"
                  >
                    <span className="truncate">{t(`money.regions.${item.region}`)}</span>
                    <span className="h-2 rounded-sm bg-muted" aria-hidden>
                      <span
                        className="block h-2 rounded-sm bg-chart-1"
                        style={{ width: `${(item.count / widest) * 100}%` }}
                      />
                    </span>
                    <span className="text-right font-mono text-xs tabular-nums">
                      {formatNumber(item.count)}
                    </span>
                  </li>
                ))}
              </ol>
            </figure>
          </div>
        </div>
      )}
      {summary.latest.length > 0 && (
        <section className="space-y-3">
          <h3 className="text-sm font-medium">{t("money.latestTitle")}</h3>
          <MoneyLatestTable events={summary.latest} label={label} />
        </section>
      )}
      <Disclosure lazy summaryClassName="py-2" title={t("money.sourcesTitle")}>
        <div className="space-y-3 py-2">
          {summary.built_at && (
            <p className="text-xs text-muted-foreground">
              {t("money.built", { date: formatDateTime(summary.built_at) })}
            </p>
          )}
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead>{t("money.sources.name")}</TableHead>
                <TableHead>{t("money.sources.records")}</TableHead>
                <TableHead>{t("money.sources.events")}</TableHead>
                <TableHead>{t("money.sources.license")}</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {summary.sources.map((source) => (
                <TableRow key={source.name}>
                  <TableCell>
                    <span className="block">{source.name}</span>
                    <span className="text-xs text-muted-foreground">
                      {t(`money.sources.kinds.${source.kind}`)} ·{" "}
                      {t(`money.regions.${source.region}`)}
                    </span>
                  </TableCell>
                  <TableCell className="font-mono text-xs">
                    {formatNumber(source.records)}
                  </TableCell>
                  <TableCell className="font-mono text-xs">
                    {source.error ? (
                      <span className="text-risk">
                        {t("money.sources.failed", { error: source.error })}
                      </span>
                    ) : (
                      formatNumber(source.events)
                    )}
                  </TableCell>
                  <TableCell className="text-xs">{source.license}</TableCell>
                </TableRow>
              ))}
            </TableBody>
          </Table>
          <p className="max-w-prose text-xs text-muted-foreground">{t("money.gaps")}</p>
        </div>
      </Disclosure>
    </div>
  );
}
