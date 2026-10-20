import { useTranslation } from "react-i18next";
import { formatNumber } from "@/shared/lib/format";
import { Button } from "@/shared/ui/Button";
import { Tooltip, TooltipContent, TooltipTrigger } from "@/shared/ui/Tooltip";
import { MONEY_KINDS, moneyTotals } from "../earlyMoney";
import type { MoneySummary } from "../moneyApi";
import { type MapRegion } from "../worldMap";
import { coinsFor, CoinStack, InvestorsTooltip } from "./WorldMapInvestors";

export type FundingMarker = {
  id: MapRegion;
  value: number | null;
  x: number;
  y: number;
  height: number;
};

export function FundingSpike({ height, selected = false }: { height: number; selected?: boolean }) {
  return (
    <svg
      width="28"
      height={height + 10}
      viewBox={`0 0 28 ${height + 10}`}
      className="size-auto overflow-visible"
      aria-hidden
    >
      <ellipse
        cx="14"
        cy={height + 5}
        rx={selected ? 12 : 8}
        ry={selected ? 5 : 3}
        className="fill-background stroke-map-funding"
        strokeWidth={selected ? 2 : 1}
      />
      {height > 0 && (
        <>
          <path
            d={`M 14 1 L 5 ${height + 4} Q 14 ${height + 10} 23 ${height + 4} Z`}
            className="fill-map-funding"
          />
          <path
            d={`M 14 1 L 14 ${height + 7} Q 19 ${height + 7} 23 ${height + 4} Z`}
            className="fill-background"
            opacity="0.22"
          />
        </>
      )}
    </svg>
  );
}

export function WorldMapFunding({
  markers,
  board,
  container,
  region,
  regionName,
  investors,
  onSelect,
}: {
  markers: FundingMarker[];
  /** Число фондов и инвесторов с офисом в регионе — монеты под конусом событий. */
  investors?: Record<string, number>;
  board: MoneySummary;
  container: HTMLElement | null;
  region: MapRegion | null;
  regionName: (value: MapRegion) => string;
  onSelect: (value: MapRegion) => void;
}) {
  const { t } = useTranslation("trend");
  const richest = Math.max(1, ...Object.values(investors ?? {}));
  return markers.map(({ id, value, x, y, height }) => {
    const funds = investors?.[id] ?? 0;
    const kinds = moneyTotals(board.cells.filter((cell) => cell.region === id)).kinds;
    return (
      <div
        key={id}
        className="absolute top-0 left-0 transition-transform duration-200 ease-out motion-reduce:transition-none"
        style={{ transform: `translate(${x}px, ${y}px)` }}
      >
        <Tooltip delayDuration={250}>
          <TooltipTrigger asChild>
            <Button
              variant="ghost"
              size="sm"
              className="absolute bottom-0 left-0 w-max max-w-none min-w-12 -translate-x-1/2 flex-col gap-0 rounded-sm p-0"
              data-map-region={id}
              aria-pressed={region === id}
              aria-label={`${regionName(id)}: ${value === null ? t("world.noData") : t("world.fundingCount", { count: formatNumber(value) })}`}
              aria-controls="world-selection"
              onClick={() => onSelect(id)}
            >
              <span
                data-map-region-label
                aria-hidden
                className="rounded-sm bg-background px-1 text-micro leading-tight whitespace-nowrap"
              >
                {t(`world.markerRegions.${id}`)}
              </span>
              <span
                aria-hidden
                className="rounded-sm bg-background px-1 font-mono text-xs font-semibold whitespace-nowrap tabular-nums"
              >
                {value === null ? "—" : formatNumber(value)}
              </span>
              <FundingSpike height={height} selected={region === id} />
            </Button>
          </TooltipTrigger>
          <TooltipContent
            container={container}
            variant="card"
            side="top"
            sideOffset={16}
            collisionPadding={16}
            onEscapeKeyDown={(event) => event.stopPropagation()}
            data-world-funding-tooltip
            className="w-64 max-w-[calc(100vw-2rem)]"
          >
            <p className="font-semibold">{regionName(id)}</p>
            {value === null ? (
              <p className="mt-2">{t("world.region.noSources")}</p>
            ) : (
              <dl className="mt-3 grid grid-cols-[1fr_auto] gap-x-3 gap-y-1">
                {MONEY_KINDS.map((kind) => (
                  <div key={kind} className="contents">
                    <dt>{t(`money.kinds.${kind}`)}</dt>
                    <dd className="font-mono tabular-nums">{formatNumber(kinds[kind])}</dd>
                  </div>
                ))}
              </dl>
            )}
            <p className="mt-3 border-t pt-2 text-muted-foreground">{t("world.moneyScope")}</p>
          </TooltipContent>
        </Tooltip>
        {funds > 0 && (
          <Tooltip delayDuration={150}>
            <TooltipTrigger asChild>
              <Button
                variant="ghost"
                size="sm"
                className="absolute bottom-0 left-3 h-auto w-max max-w-none rounded-sm p-0"
                data-map-investors-marker={id}
                aria-pressed={region === id}
                aria-label={t("world.investors.marker", {
                  region: regionName(id),
                  count: formatNumber(funds),
                })}
                aria-controls="world-selection"
                onClick={() => onSelect(id)}
              >
                <CoinStack coins={coinsFor(funds, richest)} selected={region === id} />
              </Button>
            </TooltipTrigger>
            <TooltipContent
              container={container}
              variant="card"
              side="right"
              sideOffset={12}
              collisionPadding={16}
              onEscapeKeyDown={(event) => event.stopPropagation()}
              data-world-investors-tooltip
              className="w-64 max-w-[calc(100vw-2rem)]"
            >
              <InvestorsTooltip region={id} place={regionName(id)} count={funds} />
            </TooltipContent>
          </Tooltip>
        )}
      </div>
    );
  });
}
