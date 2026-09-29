import type { SetURLSearchParams } from "react-router-dom";
import { useTranslation } from "react-i18next";
import { ArrowDownRight, ArrowUpRight, Minus } from "lucide-react";
import { Button } from "@/shared/ui/Button";
import { Card } from "@/shared/ui/Card";
import { Badge } from "@/shared/ui/Badge";
import { PageTitle } from "@/shared/ui/PageHeader";
import { Notice } from "@/shared/ui/Notice";
import { Segmented } from "@/shared/ui/Segmented";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/shared/ui/Select";
import { TimeSeriesChart, type TimeSeries } from "@/shared/ui/TimeSeriesChart";
import { Tabs, TabPanel } from "@/shared/ui/Tabs";
import { StatStrip } from "@/shared/ui/Fields";
import { cn } from "@/shared/lib/cn";
import { formatNumber } from "@/shared/lib/format";
import type { StudyTopic, TrendStudy } from "../studyApi";
import { parseMetric, topicSeries } from "../series";
import { stageTones } from "../stageTones";
import { StudySources } from "./StudySources";
import { StudyExplanation } from "./StudyExplanation";
import { StudyProfile } from "./StudyProfile";
import { StudyLadder } from "./StudyLadder";

const format = (n: number) => formatNumber(n, { maximumFractionDigits: 1 });

export function StudyExplorer({
  study,
  params,
  setParams,
  busy,
}: {
  study: TrendStudy;
  params: URLSearchParams;
  setParams: SetURLSearchParams;
  busy: boolean;
}) {
  const { t } = useTranslation("trend");
  const filter = params.get("filter") === "emerging" ? "emerging" : "all";
  const metric = parseMetric(params.get("metric"));
  const share = metric === "share";
  const periods = study.topics.flatMap((topic) => topic.series.map((row) => row.period)).toSorted();
  const topics = study.topics.filter(
    (topic) =>
      filter !== "emerging" || topic.stage === "emerging_signal" || topic.stage === "early_signal",
  );
  const selected = topics.find((topic) => topic.id === params.get("topic")) ?? topics[0];
  const comparison = study.topics.find(
    (topic) => topic.id === params.get("compare") && topic.id !== selected?.id,
  );
  const tab = ["basis", "sources"].includes(params.get("detail") ?? "")
    ? params.get("detail")!
    : "development";
  const year = selected?.series.some((row) => row.period === params.get("year"))
    ? params.get("year")!
    : undefined;
  const change = (values: Record<string, string>) =>
    setParams((previous) => {
      const next = new URLSearchParams(previous);
      for (const [key, value] of Object.entries(values)) {
        if (value) next.set(key, value);
        else next.delete(key);
      }
      return next;
    });
  const values = (topic: StudyTopic, fresh = false): TimeSeries =>
    topicSeries(topic, t(`study.topics.${topic.id}.name`), metric, t, format, fresh);
  const chartSeries = (topic: StudyTopic) =>
    metric === "groups" ? [values(topic), values(topic, true)] : [values(topic)];
  const complete = study.coverage === "complete_bounded_population";
  const growth = selected?.growth;
  return (
    <div className="space-y-5" aria-busy={busy}>
      <div className="flex flex-wrap items-center justify-between gap-3 text-sm">
        <div className="flex flex-wrap items-center gap-2">
          <Badge variant={complete ? "ok" : "warn"}>
            {t(complete ? "study.coverageComplete" : "study.coveragePartial", {
              count: study.available_papers,
              total: study.expected_papers,
            })}
          </Badge>
          {periods.length > 0 && (
            <span className="text-muted-foreground">
              {t("study.corpus", { from: periods[0], to: periods.at(-1) })}
            </span>
          )}
        </div>
        <Segmented
          aria-label={t("study.metric")}
          value={metric}
          onChange={(value) => change({ metric: value })}
          options={[
            { value: "share", label: t("study.share") },
            { value: "count", label: t("study.count") },
            { value: "groups", label: t("study.groupsMetric") },
          ]}
        />
      </div>
      {!complete && (
        <Notice tone="warn">
          {t("study.partial", { count: study.available_papers, total: study.expected_papers })}
        </Notice>
      )}
      <div className="flex flex-wrap items-center justify-between gap-3">
        <Segmented
          aria-label={t("study.filter")}
          value={filter}
          onChange={(value) => change({ filter: value, year: "" })}
          options={[
            { value: "all", label: t("study.all") },
            { value: "emerging", label: t("study.emerging") },
          ]}
        />
        <span className="hidden text-xs text-muted-foreground lg:inline">
          {t("study.railHint")}
        </span>
      </div>
      {!selected ? (
        <Notice>{t("study.emptyFilter")}</Notice>
      ) : (
        <Card className="overflow-hidden">
          <div className="grid lg:grid-cols-[280px_minmax(0,1fr)]">
            <aside
              className="min-w-0 border-b p-3 lg:border-r lg:border-b-0"
              aria-label={t("study.directions")}
            >
              <div className="lg:hidden">
                <Select
                  value={selected.id}
                  onValueChange={(topic) => change({ topic, year: "", compare: "" })}
                >
                  <SelectTrigger aria-label={t("study.selectDirection")}>
                    <SelectValue />
                  </SelectTrigger>
                  <SelectContent>
                    {topics.map((topic) => (
                      <SelectItem key={topic.id} value={topic.id}>
                        {t(`study.topics.${topic.id}.name`)}
                      </SelectItem>
                    ))}
                  </SelectContent>
                </Select>
              </div>
              <div className="hidden space-y-1 lg:block">
                {topics.map((topic) => {
                  const trend = topic.growth.delta_pp;
                  const Icon =
                    topic.momentum === "up"
                      ? ArrowUpRight
                      : topic.momentum === "down"
                        ? ArrowDownRight
                        : Minus;
                  return (
                    <Button
                      key={topic.id}
                      variant={selected.id === topic.id ? "secondary" : "ghost"}
                      size="content"
                      aria-pressed={selected.id === topic.id}
                      aria-label={t(`study.topics.${topic.id}.name`)}
                      className="w-full p-3 text-left"
                      onClick={() => change({ topic: topic.id, year: "", compare: "" })}
                    >
                      <span className="block w-full min-w-0 space-y-2">
                        <span className="flex items-start justify-between gap-2">
                          <span className="font-medium">{t(`study.topics.${topic.id}.name`)}</span>
                          <span className="shrink-0 font-mono text-xs text-muted-foreground">
                            {topic.paper_ids.length}
                          </span>
                        </span>
                        <TimeSeriesChart
                          compact
                          series={[values(topic)]}
                          label={t("study.miniChart", {
                            name: t(`study.topics.${topic.id}.name`),
                            values: values(topic)
                              .points.map((p) => `${p.period}: ${p.display}`)
                              .join(", "),
                          })}
                        />
                        <span className="flex flex-wrap items-center justify-between gap-x-2 gap-y-1 text-xs">
                          <span className={stageTones[topic.stage].text}>
                            {t(`study.stage.${topic.stage}`)}
                          </span>
                          <span className="inline-flex items-center gap-1 tabular-nums">
                            <Icon className="size-3" aria-hidden />
                            {trend === null
                              ? "—"
                              : `${trend > 0 ? "+" : ""}${format(trend)} ${t("study.pp")}`}
                          </span>
                        </span>
                        <span className="flex flex-wrap justify-between gap-x-2 text-xs text-muted-foreground tabular-nums">
                          <span>{t("study.railGroups", { count: topic.groups })}</span>
                          <span>
                            {t("study.railNew", {
                              count: topic.series.at(-1)?.new_groups ?? 0,
                              year: topic.series.at(-1)?.period,
                            })}
                          </span>
                        </span>
                      </span>
                    </Button>
                  );
                })}
              </div>
            </aside>
            <section
              className="min-w-0 space-y-5 p-4 sm:p-6"
              aria-label={t("study.selectedDirection")}
            >
              <div className="space-y-2">
                <div className="flex flex-wrap items-center gap-3">
                  <PageTitle as="h2" variant="object">
                    {t(`study.topics.${selected.id}.name`)}
                  </PageTitle>
                  <Badge variant={stageTones[selected.stage].badge}>
                    {t(`study.stage.${selected.stage}`)}
                  </Badge>
                </div>
                <p className="max-w-2xl text-sm leading-6 text-muted-foreground">
                  {t(`study.topics.${selected.id}.summary`)}
                </p>
              </div>
              <Tabs
                id="study-detail"
                className="flex-wrap gap-x-3 gap-y-1 overflow-visible"
                active={tab}
                onChange={(detail) => change({ detail })}
                tabs={[
                  { id: "development", label: t("study.development") },
                  { id: "basis", label: t("study.basis") },
                  { id: "sources", label: t("study.sources"), badge: selected.paper_ids.length },
                ]}
              />
              <TabPanel tabsId="study-detail" active={tab} className="space-y-5">
                {tab === "basis" ? (
                  <StudyExplanation study={study} topic={selected} />
                ) : tab === "sources" ? (
                  <StudySources
                    key={selected.id + year}
                    study={study}
                    topic={selected}
                    year={year}
                    onClearYear={() => change({ year: "" })}
                  />
                ) : (
                  <>
                    <StudyProfile study={study} topic={selected} />
                    <TimeSeriesChart
                      series={[
                        ...chartSeries(selected),
                        ...(comparison ? [values(comparison)] : []),
                      ]}
                      label={t(
                        share
                          ? "study.chartShare"
                          : metric === "groups"
                            ? "study.chartGroups"
                            : "study.chartCount",
                      )}
                      formatValue={(value) => (share ? `${format(value)}%` : format(value))}
                      selected={year}
                      onSelect={(period) => change({ year: period === year ? "" : period })}
                    />
                    <StatStrip
                      items={[
                        { label: t("study.papers"), value: String(selected.paper_ids.length) },
                        {
                          label: t("study.change", {
                            prior: growth?.prior.period,
                            recent: growth?.recent.period,
                          }),
                          value:
                            !growth || growth.delta_pp === null
                              ? "—"
                              : `${growth.delta_pp > 0 ? "+" : ""}${format(growth.delta_pp)} ${t("study.pp")}`,
                        },
                        {
                          label: t("study.groupsLabel"),
                          value: t("study.groupsValue", {
                            count: selected.groups,
                            years: selected.active_years,
                          }),
                        },
                      ]}
                    />

                    <div className="flex flex-wrap items-center justify-between gap-3">
                      <p className="max-w-md text-xs leading-5 text-muted-foreground">
                        {t("study.chartHint")}
                      </p>
                      <Select
                        value={comparison?.id ?? "none"}
                        onValueChange={(compare) =>
                          change({ compare: compare === "none" ? "" : compare })
                        }
                      >
                        <SelectTrigger
                          aria-label={t("study.compare")}
                          className="w-full sm:w-56"
                          size="sm"
                        >
                          <SelectValue />
                        </SelectTrigger>
                        <SelectContent>
                          <SelectItem value="none">{t("study.compare")}</SelectItem>
                          {study.topics
                            .filter((topic) => topic.id !== selected.id)
                            .map((topic) => (
                              <SelectItem key={topic.id} value={topic.id}>
                                {t(`study.topics.${topic.id}.name`)}
                              </SelectItem>
                            ))}
                        </SelectContent>
                      </Select>
                    </div>
                    {(comparison || metric === "groups") && (
                      <p className="flex flex-wrap gap-4 text-xs">
                        {[
                          ...chartSeries(selected),
                          ...(comparison ? [values(comparison)] : []),
                        ].map((series, n) => (
                          <span key={series.id} className="inline-flex items-center gap-2">
                            <span
                              className={cn(
                                "size-2 rounded-full",
                                ["bg-chart-1", "bg-chart-2", "bg-chart-3"][n],
                              )}
                            />
                            {series.label}
                          </span>
                        ))}
                      </p>
                    )}
                    <StudyLadder study={study} topic={selected} />
                    {year ? (
                      <StudySources
                        key={selected.id + year}
                        study={study}
                        topic={selected}
                        year={year}
                        onClearYear={() => change({ year: "" })}
                      />
                    ) : (
                      <StudySources key={selected.id} study={study} topic={selected} milestones />
                    )}
                  </>
                )}
              </TabPanel>
            </section>
          </div>
        </Card>
      )}
      <p className="text-xs leading-5 text-muted-foreground">{t("study.footnote")}</p>
    </div>
  );
}
