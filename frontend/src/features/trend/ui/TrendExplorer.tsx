import { useEffect, useMemo, useState } from "react";
import { useSearchParams } from "react-router-dom";
import { useTranslation } from "react-i18next";
import { Pause, Play } from "lucide-react";
import { Button } from "@/shared/ui/Button";
import { LegendItem } from "@/shared/ui/LegendItem";
import { Card } from "@/shared/ui/Card";
import { Range } from "@/shared/ui/Range";
import { Label } from "@/shared/ui/Label";
import { Tabs, TabPanel } from "@/shared/ui/Tabs";
import { SearchField } from "@/shared/ui/SearchField";
import { Disclosure } from "@/shared/ui/Disclosure";
import { Notice } from "@/shared/ui/Notice";
import { Sheet, SheetContent, SheetHeader, SheetTitle, SheetDescription } from "@/shared/ui/Sheet";
import { mapLayout, technologyMap, visibleWorks } from "../mapData";
import type { AnalysisJob } from "../trendApi";
import type { DescriptionJob } from "../descriptionApi";
import { TechnologyNetwork, publicationColor } from "./TechnologyNetwork";
import { TechnologyTimeline } from "./TechnologyTimeline";
import { TrendMapInspector } from "./TrendMapInspector";
import { TrendList } from "./TrendList";
import { TrendOverview } from "./TrendOverview";

const NO_DESCRIPTIONS: Record<string, DescriptionJob> = {};

function relabel(job: AnalysisJob, descriptions: Record<string, DescriptionJob>): AnalysisJob {
  const labels = new Map<string, string>();
  for (const [id, d] of Object.entries(descriptions))
    if (d.result?.label) labels.set(id, d.result.label);
  if (!labels.size || !job.result) return job;
  return {
    ...job,
    result: {
      ...job.result,
      candidates: job.result.candidates.map((c) => ({ ...c, title: labels.get(c.id) ?? c.title })),
    },
  };
}

export function TrendExplorer({
  job,
  descriptions = NO_DESCRIPTIONS,
}: {
  job: AnalysisJob;
  descriptions?: Record<string, DescriptionJob>;
}) {
  const { t } = useTranslation("trend");
  const [params, setParams] = useSearchParams();
  const shown = useMemo(() => relabel(job, descriptions), [job, descriptions]);
  const map = useMemo(() => technologyMap(shown), [shown]);
  // Новые названия и фильтр не двигают точки и не сбрасывают вид карты.
  const layout = useMemo(() => mapLayout(technologyMap(job).topics), [job]);
  const [search, setSearch] = useState("");
  const [playing, setPlaying] = useState(false);
  const [narrow, setNarrow] = useState(() => window.matchMedia("(max-width: 1023px)").matches);
  const [sheetOpen, setSheetOpen] = useState(false);
  const view = ["timeline", "list"].includes(params.get("view") || "")
    ? params.get("view")!
    : "map";
  const rawYear = Number(params.get("mapYear"));
  const year = map.years.includes(rawYear) ? rawYear : null;
  const yearIndex = year === null ? map.years.length : map.years.indexOf(year);
  const topics = useMemo(
    () =>
      map.topics.filter((topic) =>
        topic.candidate.title.toLocaleLowerCase().includes(search.toLocaleLowerCase().trim()),
      ),
    [map.topics, search],
  );
  const selected =
    topics.find((topic) => topic.candidate.id === params.get("mapTopic")) ?? topics[0];
  const workID = params.get("mapWork") || "";
  const update = (values: Record<string, string>, replace = false) =>
    setParams(
      (current) => {
        const next = new URLSearchParams(current);
        next.delete("topicsPage");
        for (const [key, value] of Object.entries(values)) {
          if (value) next.set(key, value);
          else next.delete(key);
        }
        return next;
      },
      { replace },
    );
  const changeYear = (index: number) =>
    update({ mapYear: String(map.years[index] ?? ""), mapWork: "" }, true);
  const select = (topic: string, work = "") => {
    setPlaying(false);
    update({ mapTopic: topic, mapWork: work });
    if (narrow) setSheetOpen(true);
  };
  useEffect(() => {
    const query = window.matchMedia("(max-width: 1023px)");
    const change = () => {
      setNarrow(query.matches);
      setSheetOpen(false);
    };
    query.addEventListener("change", change);
    return () => query.removeEventListener("change", change);
  }, []);
  useEffect(() => {
    if (!playing) return;
    const timer = window.setTimeout(() => {
      if (yearIndex === map.years.length) {
        setPlaying(false);
        return;
      }
      setParams(
        (current) => {
          const next = new URLSearchParams(current);
          const value = map.years[yearIndex + 1];
          if (value === undefined) next.delete("mapYear");
          else next.set("mapYear", String(value));
          return next;
        },
        { replace: true },
      );
    }, 1200);
    return () => window.clearTimeout(timer);
  }, [playing, yearIndex, map.years, setParams]);
  const visible = topics.flatMap((topic) => visibleWorks(topic, year));
  const uniqueWorks = new Set(visible.map((work) => work.id));
  const visibleTopics = topics.filter((topic) => visibleWorks(topic, year).length).length;
  const inspector = selected && (
    <TrendMapInspector
      job={job}
      topic={selected}
      workID={workID}
      year={year}
      years={map.years}
      onSelect={select}
    />
  );
  return (
    <div className="space-y-4">
      {job.result!.limitations.includes("collection_coverage_not_read_page_budget") && (
        <Notice>{t("coverageUnverified")}</Notice>
      )}
      <div className="flex flex-wrap items-end justify-between gap-3">
        <Tabs
          id="technology-views"
          tabs={[
            { id: "map", label: t("map.mapTab") },
            { id: "timeline", label: t("map.timelineTab") },
            { id: "list", label: t("map.listTab") },
          ]}
          active={view}
          onChange={(value) => {
            setPlaying(false);
            update({ view: value });
          }}
        />
        <p className="text-sm text-muted-foreground" aria-live="polite">
          {t("map.counts", {
            topics: view === "list" ? map.topics.length : visibleTopics,
            works: view === "list" ? map.works.length : uniqueWorks.size,
          })}
        </p>
      </div>
      <TabPanel tabsId="technology-views" active={view} className="space-y-3">
        {view === "list" ? (
          <TrendList job={job} descriptions={descriptions} />
        ) : (
          <>
            <div className="flex flex-wrap items-center justify-between gap-3">
              <SearchField
                value={search}
                onChange={setSearch}
                placeholder={t("map.filter")}
                ariaLabel={t("map.filter")}
                className="w-full sm:max-w-xs"
              />
              <div className="flex flex-wrap items-center gap-x-3 gap-y-1 text-xs text-muted-foreground">
                <LegendItem marker="bg-foreground" size="md">
                  {t("map.topic")}
                </LegendItem>
                <LegendItem marker="bg-foreground" size="xs">
                  {t("map.paper")}
                </LegendItem>
                <LegendItem marker="bg-foreground" size="xs" shape="square">
                  {t("map.otherWork")}
                </LegendItem>
              </div>
            </div>
            <Card className="overflow-hidden">
              {topics.length && uniqueWorks.size ? (
                <div className="grid min-w-0 lg:grid-cols-[minmax(0,1fr)_290px]">
                  {view === "map" ? (
                    <TechnologyNetwork
                      topics={topics}
                      layout={layout}
                      years={map.years}
                      year={year}
                      selectedTopic={selected!.candidate.id}
                      selectedWork={workID}
                      onSelect={select}
                    />
                  ) : (
                    <TechnologyTimeline
                      topics={topics}
                      years={map.years}
                      year={year}
                      selectedTopic={selected!.candidate.id}
                      onSelect={select}
                    />
                  )}
                  {!narrow && <div className="max-h-[590px] overflow-y-auto">{inspector}</div>}
                </div>
              ) : (
                <p className="px-4 py-12 text-sm text-muted-foreground">
                  {t(search ? "map.noFilterMatches" : "map.noYearWorks")}
                </p>
              )}
              <div className="flex flex-wrap items-center gap-3 border-t px-3 py-2">
                <Button
                  variant="ghost"
                  size="icon-sm"
                  aria-label={t(playing ? "map.pause" : "map.play")}
                  title={t(playing ? "map.pause" : "map.play")}
                  onClick={() => {
                    if (!playing && yearIndex === map.years.length) changeYear(0);
                    setPlaying((value) => !value);
                  }}
                >
                  {playing ? <Pause aria-hidden /> : <Play aria-hidden />}
                </Button>
                <div className="min-w-0 flex-1 basis-44">
                  <Label htmlFor="map-year" className="flex justify-between gap-3">
                    <span>{t("map.publicationFilter")}</span>
                    <span className="font-mono">{year ?? t("map.allYears")}</span>
                  </Label>
                  <Range
                    id="map-year"
                    min={0}
                    max={map.years.length}
                    step={1}
                    value={yearIndex}
                    aria-valuetext={
                      year === null ? t("map.allYears") : t("map.untilYear", { year })
                    }
                    onChange={(event) => {
                      setPlaying(false);
                      changeYear(Number(event.target.value));
                    }}
                  />
                </div>
                {year !== null && (
                  <Button
                    variant="link"
                    size="inline"
                    onClick={() => {
                      setPlaying(false);
                      changeYear(map.years.length);
                    }}
                  >
                    {t("map.allYears")}
                  </Button>
                )}
                {narrow && selected && (
                  <Button variant="outline" size="sm" onClick={() => setSheetOpen(true)}>
                    {t("map.openSelection")}
                  </Button>
                )}
              </div>
            </Card>
            <div className="flex flex-wrap items-center gap-x-4 gap-y-2 text-xs text-muted-foreground">
              <span>{t("map.colorLegend")}</span>
              {map.years.map((value) => (
                <LegendItem key={value} color={publicationColor(value, map.years)}>
                  {value}
                </LegendItem>
              ))}
              <LegendItem marker="bg-muted-foreground">{t("map.undated")}</LegendItem>
            </div>
            {narrow && (
              <Sheet open={sheetOpen} onOpenChange={setSheetOpen}>
                <SheetContent side="bottom" className="max-h-[80dvh] overflow-y-auto p-0">
                  <SheetHeader className="px-4 pt-4">
                    <SheetTitle>{t("map.inspector")}</SheetTitle>
                    <SheetDescription>{t("map.selectionHint")}</SheetDescription>
                  </SheetHeader>
                  {inspector}
                </SheetContent>
              </Sheet>
            )}
          </>
        )}
      </TabPanel>
      <Disclosure title={t("map.about")}>
        <div className="max-w-prose space-y-3 text-sm leading-relaxed text-muted-foreground">
          <p>{t("map.membershipExplanation")}</p>
          <p>{t("map.historyExplanation")}</p>
          <p>{t("map.rankingExplanation", { count: job.result!.ranking.ordered_ids.length })}</p>
        </div>
        <div className="mt-5 space-y-4">
          {!job.active && <Notice>{t("inactive")}</Notice>}
          {job.method.quality_status !== "evaluated" && <Notice>{t("experimental")}</Notice>}
          {job.result!.search.mode === "lexical_fallback" && <Notice>{t("fallback")}</Notice>}
          <TrendOverview job={job} />
        </div>
      </Disclosure>
    </div>
  );
}
