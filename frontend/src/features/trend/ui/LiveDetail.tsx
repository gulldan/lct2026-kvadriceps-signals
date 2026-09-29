import {
  useCallback,
  useEffect,
  useMemo,
  useRef,
  useState,
  type ComponentProps,
  type CSSProperties,
} from "react";
import { flushSync } from "react-dom";
import { useTranslation } from "react-i18next";
import { ArrowLeft, FileText, Presentation, Printer } from "lucide-react";
import { Button } from "@/shared/ui/Button";
import { Card } from "@/shared/ui/Card";
import { BackButton, PageToolbar } from "@/shared/ui/PageHeader";
import { InfoHint } from "@/shared/ui/InfoHint";
import { MetricTabs, type MetricTabItem } from "@/shared/ui/MetricTabs";
import { Notice } from "@/shared/ui/Notice";
import { ReadingContent } from "@/shared/ui/ReadingContent";
import { ReportBlocks } from "@/shared/report/ReportBlocks";
import { ReportExport, type ExportItem } from "@/shared/report/ReportExport";
import type { ReportDoc } from "@/shared/report/blocks";
import { WatchButton } from "@/features/watch";
import { Segmented } from "@/shared/ui/Segmented";
import { DIAL_ASPECT } from "@/shared/ui/SignalDial";
import { TabPanel } from "@/shared/ui/Tabs";
import { TimeSeriesChart } from "@/shared/ui/TimeSeriesChart";
import {
  formatDate,
  formatDateTime,
  formatMonth,
  formatNumber,
  formatPercent,
} from "@/shared/lib/format";
import { LiveCandidates, type CandidatesView } from "./LiveCandidates";
import { writeLiveExecutive, type LiveChannel, type LiveDetail, type LiveTopic } from "../liveApi";
import { candidatesReportDoc, executiveReportDoc } from "../candidatesReport";
import { topicBranch } from "../liveBranches";
import { mergeChannels, publicationSeries } from "../liveMetrics";
import { directionReportDoc, emergenceLabel } from "../signalReport";
import { topicSeries } from "../series";
import { StudyEventList } from "./StudyLadder";
import {
  ProfileDial,
  ProfileList,
  StudyDates,
  useProfileRows,
  type ProfileExtra,
} from "./StudyProfile";
import { ChannelBlock } from "./LiveAdditionalSources";
import { ExecutiveDialog } from "./ExecutiveDialog";
import { StudySources } from "./StudySources";
import { liveTopicName } from "./LiveOverview";
import { TopicHeader } from "./TopicHeader";

const level = (value: number | null, high: number, mid: number) =>
  value === null ? "unknown" : value >= high ? "high" : value >= mid ? "medium" : "low";

const format = (n: number) => formatNumber(n, { maximumFractionDigits: 1 });

type ChartMode = "sources" | "share" | "groups";

const CHANNEL_TABS = ["web", "registry", "corpus"] as const;

const channelCount = (channel: LiveChannel | null | undefined, extra = 0) =>
  `${channel?.partial && channel.total > 0 ? "≥" : ""}${formatNumber((channel?.total ?? 0) + extra)}`;

const noop = () => {};

/** Полотно графика берёт высоту колец: обе фигуры стоят в одной полосе. */
const DIAL_BAND = { "--dial-h": `${26 * DIAL_ASPECT}rem` } as CSSProperties;

export function LiveDetailView({
  detail,
  scope = "direction",
  onBack,
  query,
  candidatesView,
  onCandidatesView,
  onOpenCandidate,
  watch,
  backLabel,
}: {
  detail: LiveDetail;
  scope?: "direction" | "signal";
  onBack: () => void;
  backLabel?: string;
  query?: string;
  candidatesView?: CandidatesView;
  onCandidatesView?: (view: CandidatesView) => void;
  onOpenCandidate?: (candidate: LiveTopic) => void;
  watch?: ComponentProps<typeof WatchButton>;
}) {
  const { t } = useTranslation("trend");
  const [mode, setMode] = useState<ChartMode>("sources");
  const [period, setPeriod] = useState<string>();
  const [tab, setTab] = useState<{ topic: string; id: string }>();
  const [ray, setRay] = useState<string>();
  const panel = useRef<HTMLDivElement>(null);
  const [reserve, setReserve] = useState(0);
  const topic = detail.topic;
  const uniqueness = topic.uniqueness ?? null;
  const impact = topic.citations?.normalized ?? null;
  const relevance = topic.relevance ?? null;
  const name = liveTopicName(topic, t);
  const study = {
    papers: detail.papers,
    observed_at: detail.observed_at,
    external_observed_at: detail.observed_at,
  };
  const patents = useMemo(() => mergeChannels(topic.patents, topic.patents_cn), [topic]);
  // Без памяти пять рядов пересобирались на каждый клик по вкладке.
  const series = useMemo(
    () =>
      mode === "sources"
        ? publicationSeries(topic, t, patents)
        : [
            topicSeries(topic, name, mode, t, format),
            ...(mode === "groups" ? [topicSeries(topic, name, mode, t, format, true)] : []),
          ],
    [mode, topic, t, patents, name],
  );
  const selected = detail.periods.find((row) => row.period === period);
  const lastPeriod = detail.periods.at(-1);
  const events = topic.external;
  const repoEvents = useMemo(
    () =>
      events
        .map((event) =>
          event.community === "organization"
            ? { ...event, community: "open_source" as const }
            : event,
        )
        .filter((event) => event.community === "open_source"),
    [events],
  );
  const hnEvents = useMemo(
    () => events.filter((event) => event.community === "discussion"),
    [events],
  );
  const patentEvents = useMemo(() => events.filter((event) => event.kind === "patent"), [events]);
  // Сотни записей фильтровались и при закрытой вкладке, а отбор зависит только от месяца.
  const papersShown = useMemo(
    () =>
      selected
        ? detail.papers.filter(
            (paper) => paper.date >= selected.start && paper.date < selected.end_exclusive,
          )
        : detail.papers,
    [detail.papers, selected],
  );
  const candidates = detail.candidates ?? [];
  // Без версии выдачи направление в очереди: вкладка пишет «считается», а не пустоту.
  const counted = topic.source === "search" || !!detail.search_contract;
  // Отбор относится ко всей выдаче; у отдельной технологии есть только её источники и измерения.
  const sourceTabs: MetricTabItem[] = useMemo(
    () => [
      ...(scope === "direction"
        ? [
            {
              id: "candidates",
              label: t("study.live.types.found"),
              value: counted ? formatNumber(candidates.length) : "…",
            },
          ]
        : []),
      {
        id: "papers",
        label: t("study.live.types.papers"),
        value: formatNumber(papersShown.length),
      },
      { id: "repos", label: t("study.live.types.repos"), value: formatNumber(repoEvents.length) },
      { id: "hn", label: t("study.live.types.hn"), value: formatNumber(hnEvents.length) },
      {
        id: "patents",
        label: t("study.live.types.patents"),
        value: patents || patentEvents.length ? channelCount(patents, patentEvents.length) : "—",
      },
      ...CHANNEL_TABS.map((key) => ({
        id: key,
        label: t(`study.live.types.${key}`),
        value: topic[key] ? channelCount(topic[key]) : "—",
      })),
    ],
    [
      t,
      scope,
      counted,
      candidates.length,
      papersShown.length,
      repoEvents.length,
      hnEvents.length,
      patents,
      patentEvents.length,
      topic,
    ],
  );
  // Выбор вкладки не переносится между направлением и измерениями отдельной технологии.
  const active =
    (tab?.topic === topic.id && sourceTabs.find((item) => item.id === tab.id)?.id) ||
    sourceTabs[0]!.id;
  // Короткая вкладка после длинной укорачивала документ, и браузер утягивал читателя вверх: панель
  // держит столько высоты, чтобы низ окна остался внутри документа.
  const need = () => {
    const element = panel.current;
    if (!element) return 0;
    const rest = document.documentElement.scrollHeight - element.offsetHeight;
    return Math.max(0, window.scrollY + window.innerHeight - rest);
  };
  const hold = () => setReserve(need());
  const switchTab = (id: string) => {
    hold();
    setTab({ topic: topic.id, id });
  };
  // Резерв тает с прокруткой вверх и не растёт вниз: сброс разом отбросил бы читателя.
  const reserved = reserve > 0;
  useEffect(() => {
    if (!reserved) return;
    const shrink = () => setReserve((current) => Math.min(current, need()));
    window.addEventListener("scroll", shrink, { passive: true });
    return () => window.removeEventListener("scroll", shrink);
  }, [reserved]);
  const channelTab = CHANNEL_TABS.find((key) => key === active);
  // Два десятка переводов с подстановкой — самое дорогое на карточке, а от вкладки они не зависят.
  const extra: ProfileExtra[] = useMemo(
    () => [
      {
        key: "semantic",
        label: t("study.live.semantic.label"),
        value: topic.semantic
          ? t("study.live.semantic.score", { value: format(topic.semantic.distance * 100) })
          : "—",
        level: topic.semantic
          ? t("study.live.semantic.nearest", {
              name: liveTopicName(
                { id: topic.semantic.nearest_id, name: topic.semantic.nearest_name },
                t,
              ),
            })
          : t("study.live.semantic.unavailable"),
        tone: "muted",
        basis: topic.semantic
          ? t("study.live.semantic.basis", {
              count: topic.semantic.compared_topics,
              papers: topic.semantic.sample_paper_ids.length,
              model: topic.semantic.model,
              revision: topic.semantic.revision,
            })
          : t("study.live.semantic.missingBasis"),
      },
      ...(topic.semantic?.query_similarity !== null &&
      topic.semantic?.query_similarity !== undefined
        ? [
            {
              key: "semantic-relevance",
              label: t("study.live.semantic.relevance"),
              value: format(topic.semantic.query_similarity),
              level: t("study.live.semantic.cosine"),
              tone: "muted" as const,
              basis: t("study.live.semantic.relevanceBasis"),
            },
          ]
        : []),
      {
        key: "uniqueness",
        label: t("study.live.profile.uniqueness.label"),
        value: uniqueness === null ? "—" : formatPercent(uniqueness, 1),
        level: t(`study.live.profile.uniqueness.${level(uniqueness, 0.7, 0.4)}`),
        tone: uniqueness === null ? "muted" : uniqueness >= 0.7 ? "ok" : "warn",
        basis: t("study.live.profile.uniqueness.basis"),
      },
      {
        key: "impact",
        label: t("study.live.profile.impact.label"),
        value: impact === null ? "—" : `${formatNumber(impact, { maximumFractionDigits: 1 })}×`,
        level: t(`study.live.profile.impact.${level(impact, 1.5, 0.8)}`),
        tone: impact === null ? "muted" : impact >= 1.5 ? "ok" : impact >= 0.8 ? "muted" : "warn",
        basis: topic.citations
          ? t("study.live.profile.impact.basisKnown", {
              total: formatNumber(topic.citations.total),
              papers: formatNumber(topic.citations.papers),
              missing: formatNumber(topic.citations.missing),
              date: formatDate(topic.citations.observed_at),
            })
          : t("study.live.profile.impact.basis"),
      },
      {
        key: "relevance",
        label: t("study.live.profile.relevance.label"),
        value: relevance === null ? "—" : formatPercent(relevance, 1),
        level: t(`study.live.profile.relevance.${level(relevance, 0.6, 0.3)}`),
        tone: relevance === null ? "muted" : relevance >= 0.6 ? "ok" : "warn",
        basis: t("study.live.profile.relevance.basis"),
      },
      {
        key: "emergence",
        label: t("report.emergence"),
        value:
          typeof topic.emergence?.dov_growth === "number" &&
          typeof topic.emergence.dod_growth === "number"
            ? `${formatPercent(topic.emergence.dov_growth)} · ${formatPercent(topic.emergence.dod_growth)}`
            : "—",
        level: emergenceLabel(topic, t),
        tone:
          topic.emergence?.class === "weak_signal" || topic.emergence?.class === "strong_signal"
            ? "ok"
            : "muted",
        basis: topic.emergence?.basis ?? t("report.emergenceMissing"),
      },
    ],
    [t, topic, uniqueness, impact, relevance],
  );
  const rows = useProfileRows({
    study,
    topic,
    compact: true,
    extra,
    complete: detail.periods.filter((row) => row.complete).length,
  });
  const branch = topicBranch(topic);
  // Новые стрелки на каждый рендер обесценивали память графика.
  const formatValue = useCallback(
    (value: number) => (mode === "share" ? `${format(value)}%` : format(value)),
    [mode],
  );
  const chartLabel = t(mode === "sources" ? "study.live.typesChart" : `study.live.charts.${mode}`);
  const periodLabel = useCallback(
    (value: string) =>
      value === lastPeriod?.period && !lastPeriod.complete
        ? t("study.live.partialQuarter", { period: formatMonth(value) })
        : formatMonth(value),
    [lastPeriod, t],
  );
  const selectPeriod = useCallback(
    (value: string) => setPeriod((current) => (current === value ? undefined : value)),
    [],
  );
  // Выбор месяца или вкладки печатную версию не меняет.
  const report = useMemo(
    () =>
      detail.candidates?.length
        ? candidatesReportDoc({
            detail,
            query: query ?? name,
            name: (row) => liveTopicName(row, t),
            t,
          })
        : directionReportDoc({
            detail,
            name,
            branchLabel: branch ? t(`study.live.branches.${branch}`) : undefined,
            t,
          }),
    [detail, query, name, branch, t],
  );
  // Справка на время подменяет печатный отчёт; пишется моделью только по выбору в меню.
  const [printed, setPrinted] = useState<ReportDoc>();
  const [executiveOpen, setExecutiveOpen] = useState(false);
  const executive = async () =>
    executiveReportDoc({
      brief: await writeLiveExecutive(query ? { q: query } : { topic: topic.id }),
      name: (item) => liveTopicName(item, t),
      t,
    });
  const executiveItems: ExportItem[] = detail.candidates?.length
    ? [
        {
          label: t("executive.menuWord"),
          icon: <FileText className="text-integration-word" aria-hidden />,
          run: async () => {
            const { downloadBlocksDocx } = await import("@/shared/report/docx");
            await downloadBlocksDocx(await executive());
          },
        },
        {
          label: t("executive.menuPrint"),
          icon: <Printer className="text-muted-foreground" aria-hidden />,
          run: async () => {
            const doc = await executive();
            flushSync(() => setPrinted(doc));
            window.print();
            setPrinted(undefined);
          },
        },
      ]
    : [];
  return (
    <>
      {/* Правило «не разрывать» здесь было бы глобальным и выталкивало бы целые разделы. */}
      <style>{`@media print { @page { size: A4; margin: 16mm; } }`}</style>
      <ReadingContent className="hidden print:block print:max-w-none print:text-sm">
        <ReportBlocks blocks={(printed ?? report).blocks} />
      </ReadingContent>
      <div className="space-y-4 print:hidden">
        <PageToolbar>
          <BackButton onClick={onBack}>
            <ArrowLeft aria-hidden />
            {backLabel ?? t("study.live.back")}
          </BackButton>
          <div className="flex flex-wrap items-center gap-2">
            {watch && <WatchButton {...watch} />}
            {executiveItems.length > 0 && (
              <Button variant="outline" onClick={() => setExecutiveOpen(true)}>
                <Presentation aria-hidden />
                {t("executive.menuScreen")}
              </Button>
            )}
            <ReportExport doc={() => report} items={executiveItems} />
          </div>
        </PageToolbar>
        {executiveItems.length > 0 && (
          <ExecutiveDialog
            open={executiveOpen}
            onOpenChange={setExecutiveOpen}
            detail={detail}
            query={query}
          />
        )}
        {/* Ширину меряет карточка, а не окно: при открытом меню узкая колонка сжимала подписи. */}
        <Card className="@container space-y-5 p-4 sm:p-6">
          <TopicHeader
            topic={topic}
            description={topic.description || topic.card?.summary || null}
          />
          {query && (
            <p className="text-caption text-muted-foreground">
              {t("signalList.collectedAt", { date: formatDateTime(detail.observed_at) })}{" "}
              {t("signalList.savedResult")}
            </p>
          )}
          {detail.rescoring && (
            <output className="block text-caption text-muted-foreground">
              {t("signalList.rescoring")}
            </output>
          )}
          <StudyDates study={study} topic={topic} />
          <div
            className="grid gap-x-10 gap-y-3 @4xl:grid-cols-[minmax(0,1fr)_26rem]"
            style={DIAL_BAND}
          >
            <div className="flex min-w-0 flex-wrap items-center justify-between gap-x-4 gap-y-2">
              <h3 className="flex min-h-control-sm items-center text-sm font-medium">
                {t("study.live.dynamics")}
              </h3>
              <Segmented
                aria-label={t("study.metric")}
                value={mode}
                onChange={setMode}
                size="sm"
                options={[
                  { value: "sources", label: t("study.sourcesMetric") },
                  { value: "share", label: t("study.share") },
                  { value: "groups", label: t("study.groupsMetric") },
                ]}
              />
            </div>
            {/* Без self-start фигура тянулась бы до низа колец, отрывая подпись от оси месяцев. */}
            <TimeSeriesChart
              className="self-start"
              captionBelow
              plotClassName="@4xl:h-(--dial-h) @4xl:flex-none"
              series={series}
              label={chartLabel}
              formatValue={formatValue}
              selected={period}
              onSelect={selectPeriod}
              log={mode === "sources"}
              periodLabel={periodLabel}
              note={
                <>
                  {mode === "sources" ? t("study.live.typesNote") : chartLabel}
                  <InfoHint label={chartLabel} className="ml-1 align-middle">
                    {mode === "sources" ? `${t("study.live.typesCumulative")} ` : ""}
                    {t("study.live.typesHint")}
                  </InfoHint>
                </>
              }
            />
            {/* На телефоне двенадцать подписей по окружности слипаются, поэтому там список. */}
            <h3 className="mt-3 hidden min-h-control-sm items-center self-start text-sm font-medium sm:flex @4xl:col-start-2 @4xl:row-start-1 @4xl:mt-0">
              {t("study.profile.title")}
            </h3>
            <ProfileDial
              rows={rows}
              active={ray}
              onActive={setRay}
              className="hidden sm:flex @4xl:col-start-2 @4xl:row-start-2"
            />
          </div>
          <section aria-labelledby="live-profile-measures" className="space-y-3 pt-2">
            <h3 id="live-profile-measures" className="text-sm font-medium">
              {t("study.profile.measures")}
            </h3>
            <ProfileList
              rows={rows}
              active={ray}
              onActive={setRay}
              className="grid gap-x-8 @5xl:grid-cols-2"
            />
          </section>
          <div className="space-y-2">
            <div className="flex items-center gap-1">
              <h3 id="live-detail-sources" className="text-sm font-medium">
                {t("sources")}
              </h3>
              <InfoHint label={t("sources")}>{t("study.live.tabsLegend")}</InfoHint>
            </div>
            <MetricTabs
              id="live-detail"
              aria-labelledby="live-detail-sources"
              active={active}
              onChange={switchTab}
              items={sourceTabs}
            />
          </div>
          <TabPanel
            ref={panel}
            tabsId="live-detail"
            active={active}
            className="space-y-4"
            style={reserve ? { minHeight: reserve } : undefined}
          >
            {active === "candidates" ? (
              counted ? (
                <LiveCandidates
                  detail={detail}
                  view={candidatesView ?? "cards"}
                  onView={(next) => {
                    // Таблица короче карточек — та же просадка, что у вкладок.
                    hold();
                    onCandidatesView?.(next);
                  }}
                  onOpen={onOpenCandidate ?? noop}
                />
              ) : (
                <Notice tone="muted">{t("study.live.technologies.pending")}</Notice>
              )
            ) : active === "papers" ? (
              <StudySources
                key={topic.id + (period ?? "")}
                study={study}
                topic={topic}
                passports={detail.passports}
                year={period}
                window={`${formatMonth(detail.periods[0]?.period ?? "")}–${formatMonth(detail.periods.at(-1)?.period ?? "")}`}
                match={
                  selected
                    ? (date) => date >= selected.start && date < selected.end_exclusive
                    : undefined
                }
                onClearYear={() => setPeriod(undefined)}
              />
            ) : active === "repos" || active === "hn" ? (
              <StudyEventList
                study={study}
                events={active === "repos" ? repoEvents : hnEvents}
                corpusLane="arxiv"
                limit={6}
              />
            ) : channelTab ? (
              <ChannelBlock source={topic[channelTab]} />
            ) : (
              <>
                <ChannelBlock source={patents} />
                {patentEvents.length > 0 && (
                  <StudyEventList study={study} events={patentEvents} corpusLane="arxiv" />
                )}
              </>
            )}
          </TabPanel>
        </Card>
      </div>
    </>
  );
}
