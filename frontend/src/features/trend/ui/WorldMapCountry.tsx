import { memo } from "react";
import { useTranslation } from "react-i18next";
import { cn } from "@/shared/lib/cn";
import { formatNumber } from "@/shared/lib/format";
import { Tooltip, TooltipContent, TooltipTrigger } from "@/shared/ui/Tooltip";
import type { MoneySummary } from "../moneyApi";
import {
  growthColor,
  publicationDelta,
  publicationGrowth,
  regionValue,
  type MapCountry,
  type MapRegion,
} from "../worldMap";

type Counts = { recent: number; prior: number };

export function WorldMapTooltip({
  title,
  counts,
  region,
  board,
  investors,
  container,
}: {
  title: string;
  counts?: Counts;
  region: MapRegion;
  board?: MoneySummary;
  investors?: number;
  container: HTMLElement | null;
}) {
  const { t } = useTranslation("trend");
  const funding = regionValue(board, region);
  const regionTitle =
    region === "cis" || region === "mena"
      ? t(`world.regions.${region}`)
      : t(`money.regions.${region}`);
  return (
    <TooltipContent
      container={container}
      variant="card"
      side="top"
      sideOffset={16}
      collisionPadding={16}
      onEscapeKeyDown={(event) => event.stopPropagation()}
      className="w-64 max-w-[calc(100vw-2rem)]"
      data-world-tooltip
    >
      <p className="text-sm font-semibold">{title}</p>
      <dl className="mt-3 grid grid-cols-[1fr_auto] gap-x-3 gap-y-1 text-xs">
        <dt>{t("world.scienceLayer")}</dt>
        <dd className="font-mono font-semibold tabular-nums">
          {counts ? formatNumber(counts.recent) : t("world.noData")}
        </dd>
        <dt className="text-muted-foreground">{t("world.table.growth")}</dt>
        <dd className="font-mono tabular-nums">
          {counts ? publicationDelta(counts.recent, counts.prior) : "—"}
        </dd>
      </dl>
      {counts && (
        <p className="mt-1 text-xs text-muted-foreground">
          {counts.prior
            ? t("world.previous", { count: formatNumber(counts.prior) })
            : t("world.noBaseline")}
        </p>
      )}
      <div className="mt-3 flex justify-between gap-3 border-t pt-2 text-xs">
        <span>{t("world.fundingRegion", { region: regionTitle })}</span>
        <span className="font-mono tabular-nums">
          {funding === null ? t("world.noData") : formatNumber(funding)}
        </span>
      </div>
      {investors !== undefined && (
        <div className="mt-1 flex justify-between gap-3 text-xs">
          <span>{t("world.investors.inCountry")}</span>
          <span className="font-mono tabular-nums">{formatNumber(investors)}</span>
        </div>
      )}
    </TooltipContent>
  );
}

// Наведение на тренд меняет только контуры его стран, не пересоздавая все подсказки карты.
export const WorldMapCountry = memo(function WorldMapCountry({
  item,
  title,
  counts,
  board,
  investors,
  container,
  emphasized,
  muted,
  patternId,
  onSelect,
}: {
  item: MapCountry;
  title: string;
  counts?: Counts;
  board?: MoneySummary;
  investors?: number;
  container: HTMLElement | null;
  emphasized: boolean;
  muted: boolean;
  patternId: string;
  onSelect: (code: string) => void;
}) {
  const growth = counts ? publicationGrowth(counts.recent, counts.prior) : null;
  return (
    <Tooltip delayDuration={250}>
      <TooltipTrigger asChild>
        <path
          d={item.d}
          data-map-country={item.c ?? item.n}
          data-emphasized={emphasized || undefined}
          data-muted={muted || undefined}
          strokeWidth={emphasized ? "var(--map-active-stroke)" : "var(--map-border-stroke)"}
          strokeLinejoin="round"
          opacity={muted ? 0.23 : 1}
          fill={growth === null ? `url(#${patternId})` : growthColor(growth)}
          onClick={() => {
            if (item.c) onSelect(item.c);
          }}
          className={cn(
            "cursor-pointer hover:stroke-foreground",
            emphasized ? "stroke-foreground" : "stroke-background",
          )}
        />
      </TooltipTrigger>
      <WorldMapTooltip
        title={title}
        counts={counts}
        region={item.r}
        board={board}
        investors={investors}
        container={container}
      />
    </Tooltip>
  );
});
