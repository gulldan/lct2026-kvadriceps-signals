import { Link } from "react-router-dom";
import { useTranslation } from "react-i18next";
import { ArrowUpRight, X } from "lucide-react";
import { cn } from "@/shared/lib/cn";
import { formatNumber, regionNames } from "@/shared/lib/format";
import { Button } from "@/shared/ui/Button";
import { Notice } from "@/shared/ui/Notice";
import { InfoHint } from "@/shared/ui/InfoHint";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/shared/ui/Table";
import { MONEY_KINDS, moneyTotals } from "../earlyMoney";
import type { MapSignal } from "../mapApi";
import type { MoneySummary } from "../moneyApi";
import {
  type MapRegion,
  regionValue,
  signalStageKey as stageKey,
  publicationDelta as delta,
} from "../worldMap";

const LEAD = 3;

export function SignalDetail({
  signal,
  name,
  onOpen,
  onClose,
}: {
  signal: MapSignal;
  name: string;
  onOpen: () => void;
  onClose: () => void;
}) {
  const { t, i18n } = useTranslation("trend");
  const countryName = regionNames(i18n.language);
  return (
    <section
      className="space-y-3 border-t pt-4"
      aria-label={t("world.selection")}
      data-map-selection={signal.id}
    >
      <div className="flex items-start justify-between gap-2">
        <div className="min-w-0">
          <h3 className="text-sm font-semibold wrap-anywhere">{name}</h3>
          <p className="mt-1 text-xs text-muted-foreground">
            {t(`study.live.table.stages.${stageKey(signal.stage)}`)}
          </p>
        </div>
        <Button
          variant="ghost"
          size="icon-sm"
          className="shrink-0"
          aria-label={t("world.closeSelection")}
          onClick={onClose}
        >
          <X aria-hidden />
        </Button>
      </div>
      <dl className="grid grid-cols-[minmax(0,1fr)_auto] gap-x-4 gap-y-1 text-sm">
        <dt className="text-muted-foreground">{t("world.tip.science")}</dt>
        <dd className="text-right font-mono tabular-nums">
          {signal.lead
            .slice(0, LEAD)
            .map((item) => `${countryName(item.country)} ${formatNumber(item.count)}`)
            .join(" · ") || "—"}
        </dd>
        <dt className="text-muted-foreground">{t("world.tip.direction")}</dt>
        <dd className="text-right font-mono tabular-nums">
          {formatNumber(signal.science_recent)} ({formatNumber(signal.science_prior)})
        </dd>
        <dt className="text-muted-foreground">{t("world.tip.radar")}</dt>
        <dd className="text-right font-mono tabular-nums">
          {formatNumber(signal.radar_recent)} · {delta(signal.radar_recent, signal.radar_prior)}
        </dd>
        <dt className="text-muted-foreground">{t("world.tip.teams")}</dt>
        <dd className="text-right font-mono tabular-nums">
          {formatNumber(signal.new_groups)} / {formatNumber(signal.groups)}
        </dd>
        <dt className="text-muted-foreground">{t("world.tip.deals")}</dt>
        <dd className="text-right font-mono tabular-nums">{formatNumber(signal.money.total)}</dd>
        <dt className="text-muted-foreground">{t("world.tip.technologies")}</dt>
        <dd className="text-right">
          {signal.technologies === null
            ? t("world.tip.pending")
            : formatNumber(signal.technologies)}
        </dd>
      </dl>
      <Button variant="outline" size="sm" className="w-full" onClick={onOpen}>
        {t("world.openDirection")}
        <ArrowUpRight aria-hidden />
      </Button>
    </section>
  );
}

export function WorldTop({
  container,
  signals,
  active,
  selected,
  name,
  updated,
  onHover,
  onSelect,
  rowRefs,
}: {
  signals: MapSignal[];
  container: HTMLElement | null;
  active: string | null;
  selected: string | null;
  name: (signal: MapSignal) => string;
  updated: string;
  onHover: (id: string | null) => void;
  onSelect: (id: string) => void;
  rowRefs: Map<string, HTMLButtonElement>;
}) {
  const { t } = useTranslation("trend");
  return (
    <section className="flex min-h-0 flex-1 flex-col gap-2">
      <div className="flex items-baseline justify-between gap-2">
        <h3 className="text-sm font-semibold">{t("world.table.aria")}</h3>
        <span className="font-mono text-xs text-muted-foreground">{signals.length}</span>
      </div>
      <Table
        caption={t("world.table.aria")}
        className="table-fixed text-xs"
        onPointerLeave={() => onHover(null)}
      >
        <TableHeader className="sticky top-0 z-10 bg-background">
          <TableRow>
            <TableHead className="px-1">{t("world.table.name")}</TableHead>
            <TableHead className="w-24 px-1 text-right">
              <span className="inline-flex items-center gap-1">
                {t("world.table.papers")}
                <InfoHint container={container} label={t("world.table.papersHelp")}>
                  {t("world.table.papersMeaning")}
                </InfoHint>
              </span>
            </TableHead>
            <TableHead className="w-20 px-1 text-right">
              <span className="inline-flex items-center gap-1">
                {t("world.table.funding")}
                <InfoHint container={container} label={t("world.fundingHelp")}>
                  {t("world.table.fundingMeaning")}
                </InfoHint>
              </span>
            </TableHead>
          </TableRow>
        </TableHeader>
        <TableBody>
          {signals.map((signal) => (
            <TableRow
              key={signal.id}
              data-map-row={signal.id}
              data-state={active === signal.id ? "selected" : undefined}
              onPointerEnter={() => onHover(signal.id)}
            >
              <TableCell className="px-1 py-1">
                <Button
                  ref={(element) => {
                    if (element) rowRefs.set(signal.id, element);
                    else rowRefs.delete(signal.id);
                  }}
                  variant="ghost"
                  size="sm"
                  aria-pressed={selected === signal.id}
                  aria-controls="world-selection"
                  className="w-full justify-start rounded-sm p-0 text-left"
                  onFocus={() => onHover(signal.id)}
                  onBlur={() => onHover(null)}
                  onClick={() => onSelect(signal.id)}
                >
                  <span className="leading-snug">
                    <span className={cn(selected === signal.id && "text-accent-foreground")}>
                      {name(signal)}
                    </span>
                    <span className="sr-only">
                      {" "}
                      · {t(`study.live.table.stages.${stageKey(signal.stage)}`)}
                    </span>
                  </span>
                </Button>
              </TableCell>
              <TableCell className="px-1 py-1 text-right font-mono tabular-nums">
                <span className="block font-medium">{formatNumber(signal.radar_recent)}</span>
                <span className="block text-micro whitespace-nowrap text-muted-foreground">
                  {delta(signal.radar_recent, signal.radar_prior)}
                </span>
              </TableCell>
              <TableCell className="px-1 py-1 text-right font-mono whitespace-nowrap tabular-nums">
                {formatNumber(signal.money.total)}
              </TableCell>
            </TableRow>
          ))}
        </TableBody>
      </Table>
      {updated && (
        <p className="text-micro text-muted-foreground">{t("world.updated", { date: updated })}</p>
      )}
    </section>
  );
}

export function RegionPanel({
  region,
  board,
  regionName,
}: {
  region: MapRegion | null;
  board: MoneySummary;
  regionName: (value: MapRegion) => string;
}) {
  const { t } = useTranslation("trend");
  const totals = moneyTotals(
    board.cells.filter((cell) => (region ? cell.region === region : cell.region !== "unknown")),
  );
  const available = region
    ? regionValue(board, region) !== null
    : totals.total > 0 ||
      board.sources.some((source) => source.region !== "unknown" && !source.error);
  return (
    <section
      id="world-funding-summary"
      className="space-y-3 border-t pt-3 text-xs"
      data-map-panel={region ?? "all"}
    >
      <div className="flex flex-wrap items-baseline justify-between gap-x-4 gap-y-1">
        <h3 className="font-medium">
          {t("world.moneyLayer")} · {region ? regionName(region) : t("world.allRegions")}
        </h3>
        <Button asChild variant="link" size="inline">
          <Link to="/trends?entity=money">{t("world.region.all")}</Link>
        </Button>
      </div>
      {!available ? (
        <Notice tone="muted">{t("world.region.noSources")}</Notice>
      ) : (
        <dl className="grid grid-cols-3 gap-4" aria-label={t("world.fundingBreakdown")}>
          {MONEY_KINDS.map((kind) => (
            <div key={kind} data-map-segment={kind} className="flex min-w-0 flex-col gap-1">
              <dt className="text-muted-foreground">{t(`money.kinds.${kind}`)}</dt>
              <dd data-map-segment-value className="font-mono text-base font-semibold tabular-nums">
                {formatNumber(totals.kinds[kind])}
              </dd>
            </div>
          ))}
        </dl>
      )}
    </section>
  );
}
