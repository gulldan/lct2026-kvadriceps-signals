import { useCallback, useEffect, useMemo, useState } from "react";
import { useTranslation } from "react-i18next";
import { Layers, RefreshCw } from "lucide-react";
import { invalidateClusters, listClusters, type ApiCluster } from "@/features/cluster";
import { cleanInternalTitle } from "@/features/hypothesis";
import { interleaveBy } from "@/shared/lib/interleaveBy";
import { researchDomain } from "@/shared/lib/researchDomain";
import { Button } from "@/shared/ui/Button";
import { FilterBar } from "@/shared/ui/FilterBar";
import { PageLayout } from "@/shared/ui/PageLayout";
import { Pagination } from "@/shared/ui/Pagination";
import { SearchField } from "@/shared/ui/SearchField";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/shared/ui/Select";
import { EmptyState } from "@/shared/ui/EmptyState";
import { Skeleton } from "@/shared/ui/Skeleton";
import { ErrorState } from "@/shared/ui/ErrorState";
import { PageHeader } from "@/shared/ui/PageHeader";
import { Tabs, TabPanel } from "@/shared/ui/Tabs";
import { THEME_SORTS, themePoints, type ThemeSelection } from "../themeMap";
import { ThemeMap } from "../ui/ThemeMap";
import { ThemeComparison } from "../ui/ThemeComparison";

type Phase = "loading" | "ready" | "error";
type ThemeSort = keyof typeof THEME_SORTS | "diverse";
const PAGE_SIZE = 12;

export function SummaryPage() {
  const { t } = useTranslation("summary");
  const { t: tCommon } = useTranslation("common");
  const [clusters, setClusters] = useState<ApiCluster[]>([]);
  const [phase, setPhase] = useState<Phase>("loading");
  const [query, setQuery] = useState("");
  const [sort, setSort] = useState<ThemeSort>("diverse");
  const [scoredOnly, setScoredOnly] = useState(false);
  const [page, setPage] = useState(1);
  const [view, setView] = useState("map");
  const [mapSelection, setMapSelection] = useState<ThemeSelection | null>(null);

  const load = useCallback(async () => {
    setPhase("loading");
    try {
      setClusters(await listClusters());
      setPhase("ready");
    } catch {
      setPhase("error");
    }
  }, []);
  useEffect(() => {
    void load();
  }, [load]);
  useEffect(() => {
    setPage(1);
  }, [query, sort, scoredOnly]);

  const themes = useMemo(
    () => clusters.filter((c) => cleanInternalTitle(c.label) !== ""),
    [clusters],
  );
  const filtered = useMemo(() => {
    const text = query.trim().toLowerCase();
    const ranked = themes
      .filter(
        (c) =>
          (!scoredOnly || c.params?.itc) &&
          (!text || [c.label, ...(c.keywords ?? [])].join(" ").toLowerCase().includes(text)),
      )
      .toSorted(THEME_SORTS[sort === "diverse" ? "tech" : sort].cmp);
    return sort === "diverse"
      ? interleaveBy(ranked, (theme) => researchDomain(theme.label, ...(theme.keywords ?? [])))
      : ranked;
  }, [themes, query, sort, scoredOnly]);
  const points = useMemo(() => themePoints(filtered), [filtered]);
  const excludedFromMap = filtered.length - points.length;
  const selectionIds = new Set(mapSelection?.points.map((point) => point.id));
  const listed =
    view === "map" && mapSelection
      ? filtered.filter((theme) => selectionIds.has(theme.id))
      : filtered;
  const pageCount = Math.max(1, Math.ceil(listed.length / PAGE_SIZE));
  const safePage = Math.min(page, pageCount);
  const shown = listed.slice((safePage - 1) * PAGE_SIZE, safePage * PAGE_SIZE);
  const reset = () => {
    setQuery("");
    setScoredOnly(false);
    setMapSelection(null);
  };
  const refresh = () => {
    invalidateClusters();
    setMapSelection(null);
    setPage(1);
    void load();
  };

  return (
    <PageLayout>
      <PageHeader
        title={t("header.title")}
        description={t("header.description")}
        actions={
          <Button variant="outline" disabled={phase === "loading"} onClick={refresh}>
            <RefreshCw className="size-4" aria-hidden />
            {tCommon("actions.refresh")}
          </Button>
        }
      />
      {phase === "loading" && (
        <output className="mt-6 block space-y-4" aria-label={t("loading")}>
          {[0, 1, 2, 3].map((key) => (
            <Skeleton key={key} className="h-16 w-full" />
          ))}
        </output>
      )}
      {phase === "error" && <ErrorState message={t("error")} onRetry={refresh} />}
      {phase === "ready" &&
        (themes.length === 0 ? (
          <EmptyState icon={Layers} title={t("empty.title")} description={t("empty.description")} />
        ) : (
          <div className="mt-6">
            <Tabs
              id="theme-views"
              tabs={[
                { id: "map", label: t("views.map") },
                { id: "comparison", label: t("views.comparison"), badge: themes.length },
              ]}
              active={view}
              onChange={(value) => {
                setView(value);
                setMapSelection(null);
                setPage(1);
              }}
            />
            <FilterBar className="my-5">
              <SearchField
                className="min-w-56 flex-1"
                value={query}
                onChange={(value) => {
                  setQuery(value);
                  setMapSelection(null);
                }}
                placeholder={t("filters.searchPlaceholder")}
                ariaLabel={t("filters.searchAria")}
              />
              <Select value={sort} onValueChange={(value) => setSort(value as ThemeSort)}>
                <SelectTrigger className="w-auto min-w-44" aria-label={t("filters.sortAria")}>
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  <SelectItem value="diverse">{t("sort.diverse")}</SelectItem>
                  {Object.entries(THEME_SORTS).map(([key, item]) => (
                    <SelectItem key={key} value={key}>
                      {t(item.labelKey)}
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
              <Button
                variant="outline"
                aria-pressed={scoredOnly}
                onClick={() => {
                  setScoredOnly((value) => !value);
                  setMapSelection(null);
                }}
              >
                {t("filters.scoredOnly")}
              </Button>
            </FilterBar>
            <TabPanel tabsId="theme-views" active={view}>
              {filtered.length === 0 ? (
                <EmptyState
                  icon={Layers}
                  title={t("noResults.title")}
                  description={t("noResults.description")}
                  action={
                    <Button variant="outline" size="sm" onClick={reset}>
                      {t("noResults.resetFilters")}
                    </Button>
                  }
                />
              ) : (
                <>
                  {view === "map" ? (
                    <>
                      {points.length > 0 && (
                        <ThemeMap
                          key={`${query}:${scoredOnly}`}
                          points={points}
                          maxDocuments={Math.max(1, ...themes.map((theme) => theme.document_count))}
                          selection={mapSelection}
                          onSelect={(selection) => {
                            setMapSelection(selection);
                            setPage(1);
                          }}
                        />
                      )}
                      {excludedFromMap > 0 && (
                        <p className="mt-3 text-sm text-muted-foreground">
                          {t("map.excluded", { count: excludedFromMap })}
                        </p>
                      )}
                    </>
                  ) : (
                    <p className="mb-2 max-w-prose text-sm text-muted-foreground">
                      {t("comparison.description")}
                    </p>
                  )}
                  <section
                    id="theme-list"
                    tabIndex={-1}
                    aria-label={t("map.listHint")}
                    className="focus-visible:outline-2 focus-visible:outline-offset-4 focus-visible:outline-ring"
                  >
                    {view === "map" && (
                      <h2 className="mt-5 text-sm font-medium" aria-live="polite">
                        {mapSelection
                          ? mapSelection.kind === "region"
                            ? t("map.regionCount", {
                                label: t(`map.regions.${mapSelection.id}.label`),
                                count: listed.length,
                              })
                            : t("map.selectedCount", { count: listed.length })
                          : t("map.listHint")}
                      </h2>
                    )}
                    <ThemeComparison themes={shown} />
                  </section>
                  <div className="mt-5 flex flex-wrap items-center justify-between gap-3">
                    <p className="text-xs text-muted-foreground">
                      {t("list.range", {
                        from: (safePage - 1) * PAGE_SIZE + 1,
                        to: (safePage - 1) * PAGE_SIZE + shown.length,
                        total: listed.length,
                      })}
                    </p>
                    <Pagination page={safePage} pageCount={pageCount} onPage={setPage} />
                  </div>
                </>
              )}
            </TabPanel>
          </div>
        ))}
    </PageLayout>
  );
}
