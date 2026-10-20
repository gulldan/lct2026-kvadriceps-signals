import { PageLayout } from "@/shared/ui/PageLayout";
import { Suspense, lazy, useCallback, useEffect, useMemo, useRef, useState } from "react";
import { Link, useNavigate, useSearchParams } from "react-router-dom";
import { useTranslation } from "react-i18next";
import {
  ArrowLeft,
  Info,
  Lightbulb,
  RotateCcw,
  SlidersHorizontal,
  Sparkles,
  SquareCheckBig,
  Wand2,
  X,
} from "lucide-react";
import { toast } from "@/shared/ui/Sonner";

import { type ApiCluster, listClusters } from "@/features/cluster";
import { type ApiHypothesis, getHypothesisBoard } from "@/features/hypothesis/api";
import { GoalPromptDialog, type ApiKPI, listKPIs } from "@/features/kpi";
import {
  hypothesesToCSV,
  hypothesesToDigest,
  hypothesesToJSON,
  hypothesesToJiraCSV,
  hypothesesToTasksJSON,
  hypothesisClusterId,
  isFallbackDraft,
  isResearchDirection,
  researchTypeMeta,
  researchTypeTag,
  sourceTypeLabel,
  visibleMeta,
  type ResearchTypeTag,
} from "@/features/hypothesis/model";
import {
  BOARD_LIMIT,
  KIND_TABS,
  SORTS,
  WORK_QUEUES,
  clusterLabel,
  isClosed,
  matchesQueue,
  serverOrderBy,
  verdictOf,
  type KindTab,
  type QueueKey,
  type SortKey,
} from "@/features/hypothesis/board/model";
import { HypothesisCard } from "@/features/hypothesis/board/HypothesisCard";
import { FacetMultiSelect, type FacetOption } from "@/features/hypothesis/board/FacetMultiSelect";
import { useOwnerNames } from "@/features/hypothesis/owners";
import { QueueFilter } from "@/features/hypothesis/board/QueueBar";
import { MethodologyPanel } from "@/features/hypothesis/board/MethodologyPanel";
import { DigestDialog } from "@/features/hypothesis/board/dialogs";
import { loadReportData, portfolioReportDoc } from "@/features/hypothesis/report/reportData";
import { ReportExport, type ExportItem } from "@/shared/report/ReportExport";
import { saveFile } from "@/shared/lib/download";
import { interleaveBy } from "@/shared/lib/interleaveBy";
import { hypothesisResearchDomain } from "../board/researchDomain";

import { Button } from "@/shared/ui/Button";
import { Badge } from "@/shared/ui/Badge";
import { Chip } from "@/shared/ui/Chip";
import { Pagination } from "@/shared/ui/Pagination";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/shared/ui/Select";
import { CardGridSkeleton } from "@/shared/ui/CardGridSkeleton";
import { EmptyState } from "@/shared/ui/EmptyState";
import { ErrorState } from "@/shared/ui/ErrorState";
import { PageHeader } from "@/shared/ui/PageHeader";
import { SearchField } from "@/shared/ui/SearchField";
import { FilterBar } from "@/shared/ui/FilterBar";
import { Skeleton } from "@/shared/ui/Skeleton";
import { Tabs, TabPanel } from "@/shared/ui/Tabs";
import {
  fetchHypothesisRuntimeSettings,
  loadHypothesisRuntimeSettings,
} from "@/shared/appSettings";

type Phase = "loading" | "ready" | "error";

const HypothesisGraphView = lazy(() =>
  import("@/features/hypothesis/ui/HypothesisGraphView").then((module) => ({
    default: module.HypothesisGraphView,
  })),
);

const EMPTY_META = {
  hypotheses: { title: "empty.hypotheses.title", empty: "empty.hypotheses.text" },
  directions: { title: "empty.directions.title", empty: "empty.directions.text" },
  drafts: { title: "empty.drafts.title", empty: "empty.drafts.text" },
  graph: { title: "empty.hypotheses.title", empty: "empty.hypotheses.text" },
  methodology: { title: "empty.hypotheses.title", empty: "empty.hypotheses.text" },
} as const satisfies Record<KindTab, { title: string; empty: string }>;

const TAB_HINTS = {
  hypotheses: "tabHints.hypotheses",
  directions: "tabHints.directions",
  drafts: "tabHints.drafts",
  graph: "tabHints.graph",
  methodology: "tabHints.methodology",
} as const satisfies Record<KindTab, string>;

const FACET_KEYS = ["company", "func", "source", "trl", "research", "kpi"] as const;
type FacetKey = (typeof FACET_KEYS)[number];

const CSV_MIME = "text/csv;charset=utf-8";
const JSON_MIME = "application/json;charset=utf-8";

const BOARD_PAGE = 12;
const BOARD_FETCH_CAP = 1000;

export function Hypotheses() {
  const { t } = useTranslation("hypothesis");
  const navigate = useNavigate();
  const [searchParams, setSearchParams] = useSearchParams();
  const [phase, setPhase] = useState<Phase>("loading");
  const [runtimeSettings, setRuntimeSettings] = useState(loadHypothesisRuntimeSettings);
  const [hyps, setHyps] = useState<ApiHypothesis[]>([]);
  const [clusters, setClusters] = useState<ApiCluster[] | null>(null);
  const [kpis, setKpis] = useState<ApiKPI[]>([]);
  const [boardTotal, setBoardTotal] = useState(0);
  const [loadingMore, setLoadingMore] = useState(false);

  const [search, setSearch] = useState("");
  const [queue, setQueue] = useState<QueueKey>(() => {
    const fromUrl = searchParams.get("queue");
    if (fromUrl && WORK_QUEUES.some((q) => q.key === fromUrl)) return fromUrl as QueueKey;
    return "all";
  });
  // URL — единственный источник фасетов, запись — только по действию пользователя: эффект при
  // загрузке мог отменить уже начавшийся переход по меню.
  const selectedFacets = useMemo(() => {
    const parseList = (key: FacetKey) => searchParams.get(key)?.split(",").filter(Boolean) ?? [];
    return {
      company: parseList("company"),
      func: parseList("func"),
      source: parseList("source"),
      trl: parseList("trl"),
      research: parseList("research"),
      kpi: parseList("kpi"),
    };
  }, [searchParams]);
  const setFacet = (key: FacetKey, values: string[]) => {
    setSearchParams(
      (prev) => {
        const next = new URLSearchParams(prev);
        if (values.length > 0) next.set(key, values.join(","));
        else next.delete(key);
        return next;
      },
      { replace: true },
    );
  };
  const [sortBy, setSortBy] = useState<SortKey>("diverse");
  const initialKind = searchParams.get("kind") as KindTab | null;
  const [kind, setKind] = useState<KindTab>(
    initialKind && KIND_TABS.has(initialKind) ? initialKind : "hypotheses",
  );

  const [boardPage, setBoardPage] = useState(1);
  const [digestOpen, setDigestOpen] = useState(false);
  const [genOpen, setGenOpen] = useState(false);
  const [helpOpen, setHelpOpen] = useState(false);
  const [filtersOpen, setFiltersOpen] = useState(false);
  const mounted = useRef(true);
  const ownerName = useOwnerNames();

  useEffect(() => {
    return () => {
      mounted.current = false;
    };
  }, []);

  // Список тем (больше мегабайта) и сокет прогресса первому экрану не нужны — оба ждут простоя.
  const [idle, setIdle] = useState(false);
  useEffect(() => {
    const request = window.requestIdleCallback ?? ((fn: () => void) => window.setTimeout(fn, 1500));
    const cancel = window.cancelIdleCallback ?? window.clearTimeout;
    const handle = request(() => setIdle(true), { timeout: 4000 });
    return () => cancel(handle);
  }, []);

  const boardParams = useMemo(
    () => ({
      q: search.trim() || undefined,
      order_by: serverOrderBy(sortBy),
      limit: BOARD_LIMIT,
    }),
    [search, sortBy],
  );

  const loadedCount = useRef(0);
  const requestSeq = useRef(0);
  useEffect(() => {
    loadedCount.current = hyps.length;
  }, [hyps.length]);

  // silent — без скелетона (сетка размонтировалась бы, и браузер сбросил бы скролл) и без тостов.
  const load = useCallback(
    async (silent = false) => {
      const seq = ++requestSeq.current;
      if (!silent) setPhase("loading");
      try {
        const count = silent ? Math.max(BOARD_LIMIT, loadedCount.current) : BOARD_LIMIT;
        const pages = await Promise.all(
          Array.from({ length: Math.ceil(count / BOARD_LIMIT) }, (_, index) =>
            getHypothesisBoard({ ...boardParams, offset: index * BOARD_LIMIT }),
          ),
        );
        if (seq !== requestSeq.current) return;
        setHyps(pages.flatMap((page) => page.items));
        setBoardTotal(pages[0]!.total);
        setPhase("ready");
      } catch (e) {
        if (seq !== requestSeq.current) return;
        if (!silent) {
          toast.error(e instanceof Error ? e.message : t("board.loadError"));
          setPhase("error");
        }
      }
    },
    [boardParams, t],
  );

  useEffect(() => {
    void load();
    return () => {
      requestSeq.current += 1;
    };
  }, [load]);

  useEffect(() => {
    const reloadSettings = () => {
      setRuntimeSettings(loadHypothesisRuntimeSettings());
    };
    let cancelled = false;
    fetchHypothesisRuntimeSettings()
      .then((next) => {
        if (!cancelled) setRuntimeSettings(next);
      })
      .catch(() => {
        reloadSettings();
      });
    window.addEventListener("storage", reloadSettings);
    window.addEventListener("hypothesis-runtime-settings:update", reloadSettings);
    return () => {
      cancelled = true;
      window.removeEventListener("storage", reloadSettings);
      window.removeEventListener("hypothesis-runtime-settings:update", reloadSettings);
    };
  }, []);

  useEffect(() => {
    let cancelled = false;
    listKPIs()
      .then((k) => {
        if (!cancelled) setKpis(k);
      })
      .catch((e: unknown) =>
        toast.error(e instanceof Error ? e.message : t("board.loadRefsError")),
      );
    return () => {
      cancelled = true;
    };
  }, [t]);

  useEffect(() => {
    if (!idle) return;
    let cancelled = false;
    listClusters()
      .then((c) => {
        if (!cancelled) setClusters(c);
      })
      .catch((e: unknown) => {
        if (!cancelled) setClusters([]);
        toast.error(e instanceof Error ? e.message : t("board.loadRefsError"));
      });
    return () => {
      cancelled = true;
    };
  }, [idle, t]);

  const loadRest = useCallback(async () => {
    if (loadingMore || hyps.length >= boardTotal) return;
    const seq = requestSeq.current;
    const offsets: number[] = [];
    for (
      let offset = hyps.length;
      offset < Math.min(boardTotal, BOARD_FETCH_CAP);
      offset += BOARD_LIMIT
    ) {
      offsets.push(offset);
    }
    setLoadingMore(true);
    try {
      const pages = await Promise.all(
        offsets.map((offset) => getHypothesisBoard({ ...boardParams, offset })),
      );
      if (seq !== requestSeq.current) return;
      setHyps((prev) => [...prev, ...pages.flatMap((page) => page.items)]);
      setBoardTotal(pages.at(-1)?.total ?? boardTotal);
    } catch (e) {
      if (seq !== requestSeq.current) return;
      toast.error(e instanceof Error ? e.message : t("board.loadPageError"));
    } finally {
      setLoadingMore(false);
    }
  }, [boardParams, boardTotal, hyps.length, loadingMore, t]);

  // Общее число известно из первого ответа, поэтому хвост портфеля едет одним залпом.
  useEffect(() => {
    if (
      phase !== "ready" ||
      loadingMore ||
      hyps.length >= boardTotal ||
      hyps.length >= BOARD_FETCH_CAP
    ) {
      return;
    }
    void loadRest();
  }, [phase, loadingMore, hyps.length, boardTotal, loadRest]);

  useEffect(() => {
    setBoardPage(1);
  }, [kind, queue, search, selectedFacets, sortBy]);

  const kpiById = useMemo(() => new Map(kpis.map((k) => [k.id, k])), [kpis]);
  const clusterById = useMemo(
    () => new Map((clusters ?? []).map((c) => [c.id, clusterLabel(c)])),
    [clusters],
  );
  // null — тема есть, но список тем ещё едет: место под подпись держим заранее.
  const clusterTitleOf = (h: ApiHypothesis) => {
    const clusterId = hypothesisClusterId(h);
    if (!clusterId) return undefined;
    return clusters === null ? null : clusterById.get(clusterId);
  };

  const byKind = useMemo(() => {
    const directions: ApiHypothesis[] = [];
    const drafts: ApiHypothesis[] = [];
    const hypotheses: ApiHypothesis[] = [];
    for (const h of hyps) {
      if (isFallbackDraft(h)) {
        drafts.push(h);
      } else if (isResearchDirection(h)) {
        directions.push(h);
      } else {
        hypotheses.push(h);
      }
    }
    return { hypotheses, directions, drafts };
  }, [hyps]);

  const activeKindRows = useMemo(() => {
    if (kind === "directions") return byKind.directions;
    if (kind === "drafts") return byKind.drafts;
    if (kind === "methodology" || kind === "graph") return [];
    return byKind.hypotheses;
  }, [byKind, kind]);

  // Внутри фасета значения объединяются по ИЛИ, между фасетами — И.
  const facetRows = useMemo(
    () =>
      activeKindRows.map((h) => ({
        h,
        company: visibleMeta(h.organization) ?? "",
        func:
          visibleMeta(kpiById.get(h.kpi_id ?? "")?.function_area ?? "") ??
          visibleMeta(h.function_area) ??
          "",
        source: visibleMeta(h.source_type) ?? "",
        trl: h.trl === null ? "" : String(h.trl),
        research: researchTypeTag(h.tags) ?? "",
        kpi: h.kpi_id ?? "",
      })),
    [activeKindRows, kpiById],
  );
  const rowMatchesFacets = useCallback(
    (row: (typeof facetRows)[number], skip?: FacetKey) =>
      FACET_KEYS.every(
        (k) => k === skip || selectedFacets[k].length === 0 || selectedFacets[k].includes(row[k]),
      ),
    [selectedFacets],
  );
  const facetFiltered = useMemo(
    () => facetRows.filter((r) => rowMatchesFacets(r)).map((r) => r.h),
    [facetRows, rowMatchesFacets],
  );

  const facetOptions = useMemo(() => {
    const make = (key: FacetKey, labelOf?: (v: string) => string): FacetOption[] => {
      const counts = new Map<string, number>();
      for (const row of facetRows) {
        if (row[key] !== "" && rowMatchesFacets(row, key)) {
          counts.set(row[key], (counts.get(row[key]) ?? 0) + 1);
        }
      }
      const all = new Set(facetRows.map((r) => r[key]).filter((v) => v !== ""));
      for (const v of selectedFacets[key]) all.add(v);
      const values = [...all].toSorted((a, b) =>
        key === "trl" ? Number(a) - Number(b) : a.localeCompare(b, "ru"),
      );
      return values.map((v) => ({ value: v, label: labelOf?.(v), count: counts.get(v) ?? 0 }));
    };
    return {
      company: make("company"),
      func: make("func"),
      source: make("source", (v) => sourceTypeLabel(v) ?? v),
      trl: make("trl", (v) => t("trl.value", { trl: v })),
      research: make("research", (v) => researchTypeMeta(v as ResearchTypeTag)?.label ?? v),
      kpi: make(
        "kpi",
        (v) => kpiById.get(v)?.title || t("board.goalUnavailable", { id: v.slice(-8) }),
      ),
    };
  }, [facetRows, rowMatchesFacets, selectedFacets, kpiById, t]);

  const filtered = useMemo(
    () => facetFiltered.filter((h) => matchesQueue(h, queue, runtimeSettings, kind)),
    [facetFiltered, queue, runtimeSettings, kind],
  );
  const showingMethodology = kind === "methodology";
  const showingGraph = kind === "graph";

  const queueCounts = useMemo(
    () =>
      WORK_QUEUES.map((q) => ({
        key: q.key,
        label: t(q.labelKey),
        hint: t(q.hintKey),
        count: facetFiltered.filter((h) => matchesQueue(h, q.key, runtimeSettings, kind)).length,
      })),
    [facetFiltered, runtimeSettings, kind, t],
  );
  const autoTRLCount = useMemo(
    () => activeKindRows.filter((h) => !isClosed(h) && h.trl === null).length,
    [activeKindRows],
  );
  const autoVerifyCount = useMemo(
    () => activeKindRows.filter((h) => !isClosed(h) && verdictOf(h) === "").length,
    [activeKindRows],
  );

  useEffect(() => {
    if (phase !== "ready" || showingMethodology || (autoTRLCount === 0 && autoVerifyCount === 0)) {
      return;
    }
    const id = window.setInterval(() => void load(true), 12_000);
    return () => window.clearInterval(id);
  }, [autoTRLCount, autoVerifyCount, load, phase, showingMethodology]);

  const sorted = useMemo(() => {
    const cmp = SORTS.find((s) => s.key === sortBy) ?? SORTS[0];
    if (!cmp) return filtered;
    const ranked = filtered.toSorted((a, b) => cmp.value(b) - cmp.value(a));
    return sortBy === "diverse"
      ? interleaveBy(ranked, (h) => hypothesisResearchDomain(h, kpiById.get(h.kpi_id ?? "")))
      : ranked;
  }, [filtered, sortBy, kpiById]);

  const boardPageCount = Math.max(1, Math.ceil(sorted.length / BOARD_PAGE));
  const safeBoardPage = Math.min(boardPage, boardPageCount);
  const pagedRows = sorted.slice((safeBoardPage - 1) * BOARD_PAGE, safeBoardPage * BOARD_PAGE);

  const kindTabs = useMemo(
    () => [
      { id: "hypotheses", label: t("tabs.hypotheses"), badge: byKind.hypotheses.length },
      { id: "directions", label: t("tabs.directions"), badge: byKind.directions.length },
      { id: "drafts", label: t("tabs.drafts"), badge: byKind.drafts.length },
      { id: "graph", label: t("tabs.graph") },
      { id: "methodology", label: t("tabs.methodology") },
    ],
    [byKind, t],
  );

  const facetsActive = FACET_KEYS.some((k) => selectedFacets[k].length > 0);
  const filtersActive = search !== "" || (kind === "hypotheses" && queue !== "all") || facetsActive;
  const showToolbar = !showingMethodology && (activeKindRows.length > 0 || filtersActive);
  const resetFilters = () => {
    setSearch("");
    setQueue("all");
    setSearchParams(
      (prev) => {
        const next = new URLSearchParams(prev);
        for (const key of FACET_KEYS) next.delete(key);
        next.set("queue", "all");
        return next;
      },
      { replace: true },
    );
  };

  const facetChips = FACET_KEYS.flatMap((key) =>
    selectedFacets[key].map((value) => ({
      key,
      value,
      label: facetOptions[key].find((o) => o.value === value)?.label ?? value,
      remove: () =>
        setFacet(
          key,
          selectedFacets[key].filter((v) => v !== value),
        ),
    })),
  );

  const empty = filtered.length === 0;
  const exportItems: ExportItem[] = [
    {
      label: t("actions.digest"),
      icon: <Sparkles aria-hidden />,
      run: () => setDigestOpen(true),
      disabled: empty,
    },
    {
      format: "csv",
      run: () => saveFile(t("export.csvFileName"), hypothesesToCSV(filtered), CSV_MIME),
      disabled: empty,
    },
    {
      format: "json",
      run: () => saveFile(t("export.jsonFileName"), hypothesesToJSON(filtered), JSON_MIME),
      disabled: empty,
    },
    {
      label: t("actions.exportJira"),
      icon: <JiraIcon />,
      run: () => saveFile(t("export.jiraFileName"), hypothesesToJiraCSV(filtered), CSV_MIME),
      disabled: empty,
    },
    {
      label: t("actions.exportTasks"),
      icon: <SquareCheckBig className="text-brand" aria-hidden />,
      run: () => saveFile(t("export.tasksFileName"), hypothesesToTasksJSON(filtered), JSON_MIME),
      disabled: empty,
    },
  ];

  const digest = useMemo(() => hypothesesToDigest(filtered), [filtered]);
  const thesisFilter = selectedFacets.kpi.length === 1 ? selectedFacets.kpi[0] : undefined;
  const onGenerated = (created: ApiHypothesis[]) => {
    if (!mounted.current) return;
    setHyps((prev) => [...created, ...prev]);
    setBoardTotal((prev) => prev + created.length);
  };

  return (
    <PageLayout>
      <PageHeader
        title={t("board.title")}
        badge={
          <Button
            variant="ghost"
            size="icon-sm"
            aria-label={t("board.aboutView")}
            aria-expanded={helpOpen}
            aria-controls="hypotheses-help"
            onClick={() => setHelpOpen((open) => !open)}
          >
            <Info className="size-4" aria-hidden />
          </Button>
        }
        actions={
          <>
            <ReportExport
              items={exportItems}
              doc={async () => portfolioReportDoc(await loadReportData())}
              onPrint={() => navigate("/hypotheses/report")}
              disabled={showingMethodology || showingGraph}
            />
            <Button onClick={() => setGenOpen(true)}>
              <Wand2 className="size-4" aria-hidden />
              {t("actions.generate")}
            </Button>
          </>
        }
      />
      {thesisFilter && kpiById.has(thesisFilter) && (
        <p className="flex flex-wrap items-center gap-x-3 gap-y-1 text-sm text-muted-foreground">
          <span>{t("board.thesis", { title: kpiById.get(thesisFilter)?.title })}</span>
          <Button variant="link" size="inline" asChild>
            <Link to={`/kpi/${thesisFilter}`}>
              <ArrowLeft className="size-4" aria-hidden />
              {t("board.backToThesis")}
            </Link>
          </Button>
        </p>
      )}
      <p
        id="hypotheses-help"
        hidden={!helpOpen}
        className="mt-3 max-w-prose text-sm text-muted-foreground"
      >
        {t(TAB_HINTS[kind])}
      </p>

      <Tabs
        id="hypotheses-tabs"
        className="mt-6"
        tabs={kindTabs}
        active={kind}
        onChange={(id) => setKind(id as KindTab)}
      />
      <TabPanel tabsId="hypotheses-tabs" active={kind}>
        {showingMethodology ? (
          <MethodologyPanel />
        ) : showingGraph ? (
          <Suspense fallback={<Skeleton className="mt-6 h-[680px] w-full rounded-xl" />}>
            <HypothesisGraphView />
          </Suspense>
        ) : (
          <>
            {kind !== "directions" && showToolbar && (
              <div className="mt-5 flex flex-wrap items-center justify-between gap-3">
                {kind === "hypotheses" && (
                  <QueueFilter queues={queueCounts} active={queue} onSelect={setQueue} />
                )}
                <Select value={sortBy} onValueChange={(v) => setSortBy(v as SortKey)}>
                  <SelectTrigger size="sm" className="w-auto min-w-48" aria-label={t("sortAria")}>
                    <SelectValue />
                  </SelectTrigger>
                  <SelectContent>
                    {SORTS.map((s) => (
                      <SelectItem key={s.key} value={s.key}>
                        {t(s.labelKey)}
                      </SelectItem>
                    ))}
                  </SelectContent>
                </Select>
              </div>
            )}

            {showToolbar && (
              <FilterBar className="mt-4">
                <SearchField
                  className="min-w-56 flex-1"
                  value={search}
                  onChange={setSearch}
                  placeholder={t("board.searchPlaceholder")}
                  ariaLabel={t("board.searchAria")}
                />
                <Button
                  variant="outline"
                  aria-expanded={filtersOpen}
                  aria-controls="hypotheses-filters"
                  onClick={() => setFiltersOpen((open) => !open)}
                >
                  <SlidersHorizontal className="size-4" aria-hidden />
                  {t("board.filters")}
                  {facetChips.length > 0 && (
                    <Badge variant="brand" className="px-1.5 tabular-nums">
                      {facetChips.length}
                    </Badge>
                  )}
                </Button>
                {(search !== "" || facetsActive) && (
                  <Button variant="outline" onClick={resetFilters}>
                    <RotateCcw className="size-4" aria-hidden />
                    {t("board.reset")}
                  </Button>
                )}
              </FilterBar>
            )}

            {showToolbar && filtersOpen && (
              <div
                id="hypotheses-filters"
                className="mt-3 flex flex-wrap items-center gap-2 border-y py-3"
              >
                <FacetMultiSelect
                  label={t("board.facetCompany")}
                  options={facetOptions.company}
                  selected={selectedFacets.company}
                  onChange={(values) => setFacet("company", values)}
                />
                <FacetMultiSelect
                  label={t("board.facetFunc")}
                  options={facetOptions.func}
                  selected={selectedFacets.func}
                  onChange={(values) => setFacet("func", values)}
                />
                <FacetMultiSelect
                  label={t("board.facetSource")}
                  options={facetOptions.source}
                  selected={selectedFacets.source}
                  onChange={(values) => setFacet("source", values)}
                />
                <FacetMultiSelect
                  label={t("board.facetTrl")}
                  options={facetOptions.trl}
                  selected={selectedFacets.trl}
                  onChange={(values) => setFacet("trl", values)}
                />
                <FacetMultiSelect
                  label={t("board.facetResearch")}
                  options={facetOptions.research}
                  selected={selectedFacets.research}
                  onChange={(values) => setFacet("research", values)}
                />
                {kpis.length > 0 && (
                  <FacetMultiSelect
                    label={t("board.facetGoal")}
                    options={facetOptions.kpi}
                    selected={selectedFacets.kpi}
                    onChange={(values) => setFacet("kpi", values)}
                  />
                )}
              </div>
            )}

            {facetChips.length > 0 && (
              <div className="mt-3 flex flex-wrap items-center gap-1.5">
                {facetChips.map((chip) => (
                  <Chip
                    key={`${chip.key}:${chip.value}`}
                    onClick={chip.remove}
                    aria-label={t("board.chipRemove", { value: chip.label })}
                    className="gap-1"
                  >
                    {chip.label}
                    <X className="size-3 text-muted-foreground" aria-hidden />
                  </Chip>
                ))}
              </div>
            )}

            {phase === "loading" && <CardGridSkeleton />}
            {phase === "error" && (
              <ErrorState message={t("board.loadError")} onRetry={() => void load()} />
            )}
            {phase === "ready" && filtered.length === 0 && (
              <EmptyState
                icon={Lightbulb}
                title={t(EMPTY_META[kind].title)}
                description={
                  filtersActive
                    ? t("board.emptyFiltered")
                    : activeKindRows.length === 0
                      ? t(EMPTY_META[kind].empty)
                      : t("board.emptyQueue")
                }
                action={
                  filtersActive ? (
                    <Button variant="outline" size="sm" onClick={resetFilters}>
                      {t("board.resetFilters")}
                    </Button>
                  ) : kind === "hypotheses" && activeKindRows.length === 0 ? (
                    <Button onClick={() => setGenOpen(true)}>
                      <Wand2 className="size-4" aria-hidden />
                      {t("actions.generate")}
                    </Button>
                  ) : undefined
                }
              />
            )}
            {phase === "ready" && filtered.length > 0 && (
              <>
                <p className="mt-4 text-sm text-muted-foreground">
                  {t("board.range", {
                    from: (safeBoardPage - 1) * BOARD_PAGE + 1,
                    to: (safeBoardPage - 1) * BOARD_PAGE + pagedRows.length,
                    total: sorted.length,
                  })}
                  {filtered.length < activeKindRows.length
                    ? t("board.rangeFiltered", { total: activeKindRows.length })
                    : ""}
                </p>
                {/* Заголовки карточек — третьего уровня: без раздела между ними и заголовком
                    страницы читалка видела разрыв в оглавлении. */}
                <h2 className="sr-only">{kindTabs.find((tab) => tab.id === kind)?.label}</h2>
                <div className="mt-3 grid grid-cols-[repeat(auto-fill,minmax(min(100%,21rem),1fr))] gap-4">
                  {pagedRows.map((h) => (
                    <HypothesisCard
                      key={h.id}
                      h={h}
                      owner={ownerName(h.owner_id)}
                      kpiTitle={h.kpi_id ? kpiById.get(h.kpi_id)?.title : undefined}
                      clusterTitle={clusterTitleOf(h)}
                      onOpen={() =>
                        navigate(
                          isResearchDirection(h) ? `/directions/${h.id}` : `/hypotheses/${h.id}`,
                        )
                      }
                    />
                  ))}
                </div>
                <Pagination
                  className="mt-5"
                  page={safeBoardPage}
                  pageCount={boardPageCount}
                  onPage={setBoardPage}
                />
              </>
            )}
          </>
        )}

        <DigestDialog
          open={digestOpen}
          onOpenChange={setDigestOpen}
          digest={digest}
          count={filtered.length}
        />
        {(idle || genOpen) && (
          <GoalPromptDialog
            open={genOpen}
            onOpenChange={setGenOpen}
            kpis={kpis}
            onCreated={() => {
              void listKPIs().then((next) => {
                if (mounted.current) setKpis(next);
              });
            }}
            onGenerated={onGenerated}
          />
        )}
      </TabPanel>
    </PageLayout>
  );
}

function JiraIcon() {
  return (
    <svg viewBox="0 0 24 24" fill="var(--integration-jira)" aria-hidden>
      <path d="M11.571 11.513H0a5.218 5.218 0 0 0 5.232 5.215h2.13v2.057A5.215 5.215 0 0 0 12.575 24V12.518a1.005 1.005 0 0 0-1.005-1.005zm5.723-5.756H5.736a5.215 5.215 0 0 0 5.215 5.214h2.129v2.058a5.218 5.218 0 0 0 5.215 5.214V6.758a1.001 1.001 0 0 0-1.001-1.001zM23.013 0H11.455a5.215 5.215 0 0 0 5.215 5.215h2.129v2.057A5.215 5.215 0 0 0 24 12.483V1.005A1.001 1.001 0 0 0 23.013 0z" />
    </svg>
  );
}
