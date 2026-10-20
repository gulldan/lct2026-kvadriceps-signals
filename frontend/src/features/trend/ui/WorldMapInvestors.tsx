import { useTranslation } from "react-i18next";
import { ExternalLink } from "lucide-react";
import { formatDate, formatNumber } from "@/shared/lib/format";
import { researchCategoryLabelById } from "@/shared/lib/researchTaxonomy";
import { useRequest } from "@/shared/lib/useRequest";
import { Badge } from "@/shared/ui/Badge";
import { Button } from "@/shared/ui/Button";
import { Skeleton } from "@/shared/ui/Skeleton";
import { getLiveInvestors, type Investor, type InvestorFilter } from "../investorsApi";
import type { MapRegion } from "../worldMap";

const CACHE_MS = 10 * 60_000;
const TOP = 8;
const RUBRICS = 8;
const TYPES = 4;
const SOURCES = 3;

const MAX_COINS = 7;

/** Стопка монет: высота по корню из числа фондов, чтобы малые регионы не сливались в одну монету. */
export function coinsFor(count: number, maximum: number) {
  return count > 0
    ? Math.max(1, Math.round(MAX_COINS * Math.sqrt(count / Math.max(1, maximum))))
    : 0;
}

export function CoinStack({ coins, selected = false }: { coins: number; selected?: boolean }) {
  const height = 4 * coins + 10;
  return (
    <svg
      width="24"
      height={height}
      viewBox={`0 0 24 ${height}`}
      className="size-auto overflow-visible"
      aria-hidden
    >
      {Array.from({ length: coins }, (_, index) => {
        const y = height - 7 - 4 * index;
        return (
          <g key={index}>
            <path
              d={`M 3 ${y} L 3 ${y + 3} A 9 3.5 0 0 0 21 ${y + 3} L 21 ${y} Z`}
              className={
                selected ? "fill-map-coin stroke-foreground" : "fill-map-coin stroke-map-coin-edge"
              }
              strokeWidth="1"
            />
            <ellipse
              cx="12"
              cy={y}
              rx="9"
              ry="3.5"
              className={
                selected
                  ? "fill-map-coin-face stroke-foreground"
                  : "fill-map-coin-face stroke-map-coin-edge"
              }
              strokeWidth={selected ? 1.5 : 1}
            />
          </g>
        );
      })}
    </svg>
  );
}

const NONE: InvestorFilter = {
  region: "",
  country: "",
  type: "",
  stage: "",
  rubric: "",
  q: "",
  sort: "deals",
};

/** Счётчики фондов по регионам и странам для маркеров и подсказок карты: один запрос на всю карту. */
export function useInvestorCounts() {
  const page = useRequest("live-investors:counts", (signal) => getLiveInvestors(NONE, 0, signal), {
    cacheMs: CACHE_MS,
  });
  return page.data?.status === "ready" ? page.data.facets : undefined;
}

const top = (facet: Record<string, number>, count: number) =>
  Object.entries(facet)
    .filter(([key, value]) => key && value > 0)
    .toSorted((a, b) => b[1] - a[1] || a[0].localeCompare(b[0]))
    .slice(0, count);

function InvestorRow({ investor }: { investor: Investor }) {
  const { t } = useTranslation("trend");
  const facts = [
    t(`investors.types.${investor.type}`),
    investor.stages.map((stage) => t(`investors.stages.${stage}`)).join(", "),
    investor.city,
  ].filter(Boolean);
  const deals = investor.deals?.total ?? 0;
  const latest = investor.deals?.latest
    .map((deal) =>
      [
        deal.company,
        t(`money.stages.${deal.stage}`, { defaultValue: deal.stage }),
        formatDate(deal.date),
      ]
        .filter(Boolean)
        .join(" · "),
    )
    .join("\n");
  return (
    <li className="grid grid-cols-[minmax(0,1fr)_auto] items-baseline gap-x-3 py-1.5">
      <span className="min-w-0">
        {investor.website ? (
          <Button asChild variant="link" size="inline" className="text-left">
            <a href={investor.website} target="_blank" rel="noopener noreferrer">
              {investor.name}
              <ExternalLink aria-hidden />
            </a>
          </Button>
        ) : (
          <span className="font-medium">{investor.name}</span>
        )}
        <span className="block truncate text-muted-foreground">{facts.join(" · ")}</span>
      </span>
      <span
        className="font-mono whitespace-nowrap tabular-nums"
        title={latest || undefined}
        data-investor-deals
      >
        {deals > 0 ? t("world.investors.deals", { count: deals }) : "—"}
      </span>
    </li>
  );
}

/** Подсказка монет региона: что за число, кто эти инвесторы и во что вкладывают; список — по щелчку под картой. */
export function InvestorsTooltip({
  region,
  place,
  count,
}: {
  region: MapRegion;
  place: string;
  count: number;
}) {
  const { t, i18n } = useTranslation("trend");
  const filter: InvestorFilter = { ...NONE, region };
  // Тот же ключ, что у сводки под картой: щелчок после наведения не ждёт второго ответа.
  const page = useRequest(
    `live-investors:${region}:`,
    (signal) => getLiveInvestors(filter, 0, signal),
    {
      cacheMs: CACHE_MS,
    },
  );
  const data = page.data?.status === "ready" ? page.data : undefined;
  return (
    <div className="space-y-2 text-xs">
      <p className="text-sm font-semibold">{t("world.investors.tooltipTitle", { place })}</p>
      <p>
        <span className="font-mono text-base font-semibold tabular-nums">
          {formatNumber(count)}
        </span>{" "}
        {t("world.investors.tooltipCount")}
      </p>
      {data ? (
        <dl className="space-y-1">
          <dt className="text-muted-foreground">{t("world.investors.tooltipTypes")}</dt>
          <dd>
            {top(data.facets.types, 3)
              .map(
                ([type, n]) =>
                  `${t(`investors.types.${type as Investor["type"]}`)} ${formatNumber(n)}`,
              )
              .join(" · ")}
          </dd>
          <dt className="text-muted-foreground">{t("world.investors.rubrics")}</dt>
          <dd>
            {top(data.facets.rubrics, 3)
              .map(
                ([rubric, n]) =>
                  `${researchCategoryLabelById(rubric, i18n.language) ?? rubric} ${formatNumber(n)}`,
              )
              .join(" · ") || t("investors.noRubrics")}
          </dd>
        </dl>
      ) : (
        <Skeleton className="h-10 w-full" />
      )}
      <p className="border-t pt-2 text-muted-foreground">{t("world.investors.tooltipHint")}</p>
    </div>
  );
}

/** Кто вкладывает в выбранном регионе или стране: типы, отрасли, активные фонды и источники — без отдельной страницы. */
export function WorldMapInvestors({
  region,
  country,
  place,
}: {
  region: MapRegion | null;
  country: string | null;
  place: string;
}) {
  const { t, i18n } = useTranslation("trend");
  const filter: InvestorFilter = {
    ...NONE,
    region: country ? "" : (region ?? ""),
    country: country ?? "",
  };
  const page = useRequest(
    `live-investors:${filter.region}:${filter.country}`,
    (signal) => getLiveInvestors(filter, 0, signal),
    { cacheMs: CACHE_MS },
  );
  if (page.status === "error" || page.data?.status === "failed") return null;
  if (!page.data) return <Skeleton className="h-32 w-full" />;
  const data = page.data;
  const types = top(data.facets.types, TYPES);
  const rubrics = top(data.facets.rubrics, RUBRICS);
  const sources = data.sources.toSorted((a, b) => b.count - a.count);
  return (
    <section
      className="space-y-3 border-t pt-3 text-xs"
      data-map-investors={country ?? region ?? "all"}
      aria-label={t("world.investors.title", { place, count: data.matched })}
    >
      <div className="flex flex-wrap items-baseline justify-between gap-x-4 gap-y-1">
        <h3 className="flex items-center gap-1.5 text-sm font-medium">
          <CoinStack coins={2} />
          {t("world.investors.title", { place, count: formatNumber(data.matched) })}
        </h3>
        <span className="text-muted-foreground">
          {types
            .map(
              ([type, count]) =>
                `${t(`investors.types.${type as Investor["type"]}`)} ${formatNumber(count)}`,
            )
            .join(" · ")}
        </span>
      </div>
      {data.matched === 0 ? (
        <p className="text-muted-foreground">{t("world.investors.none")}</p>
      ) : (
        <div className="grid gap-4 @3xl/world:grid-cols-[minmax(0,1fr)_minmax(0,1.3fr)]">
          <div className="space-y-2">
            <p className="font-medium">{t("world.investors.rubrics")}</p>
            {rubrics.length ? (
              <ul className="flex flex-wrap gap-1.5">
                {rubrics.map(([rubric, count]) => (
                  <li key={rubric}>
                    <Badge variant="outline">
                      {researchCategoryLabelById(rubric, i18n.language) ?? rubric}
                      <span className="ml-1 font-mono text-muted-foreground tabular-nums">
                        {formatNumber(count)}
                      </span>
                    </Badge>
                  </li>
                ))}
              </ul>
            ) : (
              <p className="text-muted-foreground">{t("investors.noRubrics")}</p>
            )}
            <p className="text-muted-foreground">
              {t("world.investors.sourcesLead")}{" "}
              {sources.slice(0, SOURCES).map((source, index) => (
                <span key={source.name} title={source.license}>
                  {index > 0 && "; "}
                  {source.name} {formatNumber(source.count)}
                </span>
              ))}
              {sources.length > SOURCES &&
                ` ${t("world.investors.moreSources", { count: sources.length - SOURCES })}`}
              . {t("investors.attribution")}
            </p>
          </div>
          <div>
            <p className="font-medium">{t("world.investors.top")}</p>
            <ol className="divide-y">
              {data.items.slice(0, TOP).map((investor) => (
                <InvestorRow
                  key={`${investor.name}:${investor.country ?? ""}`}
                  investor={investor}
                />
              ))}
            </ol>
          </div>
        </div>
      )}
    </section>
  );
}
