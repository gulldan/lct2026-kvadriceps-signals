import { useCallback, useMemo, useRef, useState } from "react";
import { useSearchParams } from "react-router-dom";
import { useTranslation } from "react-i18next";
import { describeError } from "@/shared/api/errors";
import { formatDate, regionNames } from "@/shared/lib/format";
import { useRequest } from "@/shared/lib/useRequest";
import { Button } from "@/shared/ui/Button";
import { Notice } from "@/shared/ui/Notice";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/shared/ui/Select";
import { Skeleton } from "@/shared/ui/Skeleton";
import { getLiveMap, type MapSignal } from "../mapApi";
import { getLiveMoney } from "../moneyApi";
import { WORLD, type MapRegion } from "../worldMap";
import { liveTopicName } from "./LiveOverview";
import { RegionPanel, SignalDetail, WorldTop } from "./WorldMapDetails";
import { WorldMapCanvas } from "./WorldMapCanvas";
import { useInvestorCounts, WorldMapInvestors } from "./WorldMapInvestors";

const CACHE_MS = 5 * 60_000;

export function WorldMap() {
  const { t, i18n } = useTranslation("trend");
  const { t: tCommon } = useTranslation("common");
  const [, setParams] = useSearchParams();
  const [hovered, setHovered] = useState<string | null>(null);
  const [selected, setSelected] = useState<string | null>(null);
  const [country, setCountry] = useState<string | null>(null);
  const [region, setRegion] = useState<MapRegion | null>(null);
  const [container, setContainer] = useState<HTMLDivElement | null>(null);
  const details = useRef<HTMLDivElement>(null);
  const rowRefs = useRef(new Map<string, HTMLButtonElement>());
  const countryName = useMemo(() => regionNames(i18n.language), [i18n.language]);
  const chooseCountry = useCallback((code: string) => {
    setCountry((value) => (code === "all" || code === value ? null : code));
    setRegion(null);
    setSelected(null);
    setHovered(null);
  }, []);
  const reset = useCallback(() => {
    setCountry(null);
    setRegion(null);
    setSelected(null);
    setHovered(null);
  }, []);
  const map = useRequest("live-map", getLiveMap, {
    cacheMs: CACHE_MS,
    pollMs: (value) => (value.status === "loading" ? 30_000 : 0),
  });
  const money = useRequest(
    "live-money:all:all",
    (signal) => getLiveMoney({ region: "all", kind: "all" }, signal),
    { cacheMs: CACHE_MS, pollMs: (value) => (value.status === "loading" ? 30_000 : 0) },
  );
  const investors = useInvestorCounts();
  const summary = map.data;
  const board = money.data?.status === "ready" ? money.data : undefined;
  if (map.status === "error")
    return <Notice tone="risk">{describeError(map.error, tCommon)}</Notice>;
  if (!summary) return <Skeleton className="h-96 w-full" />;
  if (summary.status !== "ready")
    return <Notice>{t(summary.status === "loading" ? "world.loading" : "world.failed")}</Notice>;

  const regionName = (value: MapRegion) =>
    value === "cis" || value === "mena" ? t(`world.regions.${value}`) : t(`money.regions.${value}`);
  const name = (signal: MapSignal) => liveTopicName(signal, t);
  const selection = summary.signals.find((signal) => signal.id === selected);
  const emphasis = summary.signals.find((signal) => signal.id === (hovered ?? selected));
  const leading = new Set(emphasis?.lead.map((item) => item.country) ?? []);
  const countries = [
    ...new Map(WORLD.countries.filter((item) => item.c).map((item) => [item.c, item])).values(),
  ];
  const shown = summary.signals.filter((signal) => !country || signal.lead[0]?.country === country);
  const open = (signal: MapSignal) =>
    setParams(
      signal.source === "search"
        ? { q: signal.name, back: "map" }
        : { topic: signal.id, back: "map" },
    );
  const revealDetails = () =>
    requestAnimationFrame(() => details.current?.scrollIntoView({ block: "nearest" }));

  return (
    <div
      ref={setContainer}
      className="@container/world flex flex-col gap-4 bg-background [&:fullscreen]:overflow-auto [&:fullscreen]:p-6"
    >
      <div className="flex flex-wrap items-center justify-between gap-3">
        <h2 className="text-base font-semibold">{t("world.title")}</h2>
        <Select value={country ?? "all"} onValueChange={chooseCountry}>
          <SelectTrigger size="sm" className="w-44" aria-label={t("world.countryFilter")}>
            <SelectValue />
          </SelectTrigger>
          <SelectContent container={container}>
            <SelectItem value="all">{t("world.world")}</SelectItem>
            {countries
              .toSorted((a, b) => countryName(a.c!).localeCompare(countryName(b.c!), i18n.language))
              .map((item) => (
                <SelectItem key={item.c} value={item.c!}>
                  {countryName(item.c!)}
                </SelectItem>
              ))}
          </SelectContent>
        </Select>
      </div>
      <div
        data-map-layout
        className="grid min-w-0 flex-1 gap-5 @5xl/world:min-h-[calc(100dvh-15rem)] @5xl/world:grid-cols-[minmax(0,1fr)_22rem] @7xl/world:grid-cols-[minmax(0,1fr)_24rem]"
      >
        <div className="flex min-w-0 flex-col gap-3">
          <WorldMapCanvas
            summary={summary}
            board={board}
            country={country}
            region={region}
            leading={leading}
            container={container}
            countryName={countryName}
            regionName={regionName}
            onCountry={chooseCountry}
            onReset={reset}
            failed={money.status === "error" || money.data?.status === "failed"}
            retry={money.retry}
            investors={investors}
            onRegion={(value) => {
              setCountry(null);
              setSelected(null);
              setHovered(null);
              setRegion(value === region ? null : value);
            }}
          />
          <div id="world-selection" ref={details} className="scroll-mt-4 empty:hidden">
            {/* Сводка денег — только по выбранному региону: постоянный блок над картой давал прокрутку. */}
            {!selection && region && board && (
              <RegionPanel region={region} board={board} regionName={regionName} />
            )}
            {!selection && (region || country) && (
              <WorldMapInvestors
                region={region}
                country={country}
                place={country ? countryName(country) : regionName(region!)}
              />
            )}
            {selection && (
              <SignalDetail
                signal={selection}
                name={name(selection)}
                onOpen={() => open(selection)}
                onClose={() => {
                  setSelected(null);
                  rowRefs.current.get(selection.id)?.focus({ preventScroll: true });
                }}
              />
            )}
          </div>
        </div>
        <aside className="flex min-h-0 min-w-0 flex-col gap-3 border-t pt-4 @5xl/world:border-t-0 @5xl/world:border-l @5xl/world:pt-0 @5xl/world:pl-4">
          {country && (
            <div className="flex items-center justify-between gap-2 text-sm">
              <span>{countryName(country)}</span>
              <Button variant="link" size="inline" onClick={reset}>
                {t("world.reset")}
              </Button>
            </div>
          )}
          <WorldTop
            signals={shown}
            container={container}
            active={hovered ?? selected}
            selected={selected}
            name={name}
            updated={summary.built_at ? formatDate(summary.built_at) : ""}
            onHover={setHovered}
            onSelect={(id) => {
              setSelected(id);
              setRegion(null);
              revealDetails();
            }}
            rowRefs={rowRefs.current}
          />
          {!shown.length && (
            <p className="text-sm text-muted-foreground">{t("world.region.noSignals")}</p>
          )}
        </aside>
      </div>
    </div>
  );
}
