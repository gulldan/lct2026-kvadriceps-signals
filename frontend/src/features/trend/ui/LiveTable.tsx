import { memo, useState } from "react";
import { useTranslation } from "react-i18next";
import type { TFunction } from "i18next";
import { ArrowDownRight, ArrowUpRight, ChevronDown, ChevronUp, Minus } from "lucide-react";
import { Badge } from "@/shared/ui/Badge";
import { Button } from "@/shared/ui/Button";
import { LegendItem } from "@/shared/ui/LegendItem";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/shared/ui/Table";
import { TimeSeriesChart } from "@/shared/ui/TimeSeriesChart";
import { cn } from "@/shared/lib/cn";
import { formatMonth } from "@/shared/lib/format";
import type { LiveTopic } from "../liveApi";
import { topicBranch } from "../liveBranches";
import { topicSeries } from "../series";
import { channelTotal, lastNewGroups } from "../liveMetrics";
import { stageTones } from "../stageTones";
import { groupTopics, SUBGROUP_ORDER } from "../liveGroups";

const COLUMNS = [
  "name",
  "delta",
  "unique",
  "citations",
  "papers",
  "groups",
  "repos",
  "hn",
  "since",
] as const;
type Column = (typeof COLUMNS)[number];

type RowProps = {
  topic: LiveTopic;
  complete: number;
  name: (topic: LiveTopic) => string;
  format: (n: number, digits?: number) => string;
  onOpen: (id: string) => void;
  t: TFunction<"trend">;
};

/** name и onOpen — useCallback в LiveOverview, format — константа: иначе memo бесполезен. */
const Row = memo(function Row({ topic, complete, name, format, onOpen, t }: RowProps) {
  const delta = topic.growth.delta_pp;
  const emerging = topic.stage === "emerging_signal";
  const Icon = delta === null || delta === 0 ? Minus : delta > 0 ? ArrowUpRight : ArrowDownRight;
  const repos = channelTotal(topic.repos, topic.series.length);
  const hn = channelTotal(topic.discussions, topic.series.length);
  return (
    <TableRow
      className={cn("cursor-pointer", emerging && "bg-brand-wash hover:bg-brand-wash")}
      onClick={() => onOpen(topic.id)}
    >
      <TableCell className="max-w-56 min-w-40">
        <Button
          variant="link"
          size="inline"
          className="h-auto justify-start text-left font-medium whitespace-normal"
          onClick={(event) => {
            event.stopPropagation();
            onOpen(topic.id);
          }}
        >
          {name(topic)}
        </Button>
        {topic.source === "discovered" && (
          <Badge variant="outline" className="ml-2 align-middle text-micro">
            arXiv
          </Badge>
        )}
        {topic.source === "search" && (
          <Badge variant="outline" className="ml-2 align-middle text-micro">
            {t("study.live.searchBadge")}
          </Badge>
        )}
      </TableCell>
      <TableCell className="whitespace-nowrap text-muted-foreground">
        {topicBranch(topic) ? t(`study.live.branches.${topicBranch(topic)!}`) : "—"}
      </TableCell>
      <TableCell className="whitespace-nowrap">
        <LegendItem marker={stageTones[topic.stage].marker} className="text-xs">
          {t(`study.live.table.stages.${topic.stage}`)}
        </LegendItem>
      </TableCell>
      <TableCell className="w-32 [&_svg]:h-6">
        <TimeSeriesChart
          compact
          series={[topicSeries(topic, name(topic), "share", t, format)]}
          label={t("study.miniChart", {
            name: name(topic),
            values: topic.series.map((row) => `${row.period}: ${row.count}`).join(", "),
          })}
        />
      </TableCell>
      <TableCell className="text-right font-mono text-xs whitespace-nowrap tabular-nums">
        <span className="inline-flex items-center gap-1">
          <Icon className="size-3" aria-hidden />
          {delta === null ? "—" : `${delta > 0 ? "+" : ""}${format(delta, 2)}`}
        </span>
      </TableCell>
      <TableCell className="text-right font-mono text-xs tabular-nums">
        {typeof topic.uniqueness !== "number" ? "—" : format(topic.uniqueness * 100, 0)}
      </TableCell>
      <TableCell className="text-right font-mono text-xs whitespace-nowrap tabular-nums">
        {typeof topic.citations?.normalized === "number"
          ? `${format(topic.citations.normalized, 1)}×`
          : "—"}
      </TableCell>
      <TableCell className="text-right font-mono text-xs tabular-nums">
        {topic.papers_count}
      </TableCell>
      <TableCell className="text-right font-mono text-xs whitespace-nowrap tabular-nums">
        {lastNewGroups(topic, complete)} / {topic.groups}
      </TableCell>
      <TableCell className="text-right font-mono text-xs tabular-nums">
        {repos === null ? "—" : repos}
      </TableCell>
      <TableCell className="text-right font-mono text-xs tabular-nums">
        {hn === null ? "—" : hn}
      </TableCell>
      <TableCell className="text-right font-mono text-xs whitespace-nowrap tabular-nums">
        {formatMonth(topic.first_year)}
      </TableCell>
    </TableRow>
  );
});

export function LiveTable({
  topics,
  complete,
  name,
  format,
  onOpen,
}: {
  topics: LiveTopic[];
  complete: number;
  name: (topic: LiveTopic) => string;
  format: (n: number, digits?: number) => string;
  onOpen: (id: string) => void;
}) {
  const { t } = useTranslation("trend");
  const [sort, setSort] = useState<{ column: Column; desc: boolean }>({
    column: "delta",
    desc: true,
  });
  const value = (topic: LiveTopic): number | string => {
    switch (sort.column) {
      case "name":
        return name(topic);
      case "delta":
        return topic.growth.delta_pp ?? -Infinity;
      case "unique":
        return topic.uniqueness ?? -1;
      case "citations":
        return topic.citations?.normalized ?? -1;
      case "papers":
        return topic.papers_count;
      case "groups":
        return lastNewGroups(topic, complete);
      case "repos":
        return channelTotal(topic.repos, topic.series.length) ?? -1;
      case "hn":
        return channelTotal(topic.discussions, topic.series.length) ?? -1;
      default:
        return topic.first_year;
    }
  };
  const sorted = (list: LiveTopic[]) =>
    list.toSorted((a, b) => {
      const [va, vb] = [value(a), value(b)];
      const order =
        typeof va === "string" || typeof vb === "string"
          ? String(va).localeCompare(String(vb))
          : va - vb;
      return sort.desc ? -order : order;
    });
  const groups = groupTopics(topics);
  const header = (column: Column, label: string, align = "text-right") => {
    const active = sort.column === column;
    const Icon = active && !sort.desc ? ChevronUp : ChevronDown;
    return (
      <TableHead
        aria-sort={active ? (sort.desc ? "descending" : "ascending") : "none"}
        className={cn("whitespace-nowrap", align)}
      >
        <Button
          variant="ghost"
          size="sm"
          aria-pressed={active}
          className="gap-1 px-1 whitespace-nowrap"
          onClick={() =>
            setSort((current) => ({
              column,
              desc: current.column === column ? !current.desc : column !== "name",
            }))
          }
        >
          {label}
          <Icon className={cn("size-3", !active && "opacity-40")} aria-hidden />
        </Button>
      </TableHead>
    );
  };
  return (
    <>
      <Table aria-label={t("study.live.table.aria")} className="text-sm">
        <TableHeader>
          <TableRow>
            {header("name", t("study.live.table.name"), "text-left")}
            <TableHead className="whitespace-nowrap">{t("study.live.table.branch")}</TableHead>
            <TableHead className="whitespace-nowrap">{t("study.live.table.stage")}</TableHead>
            <TableHead className="w-32">{t("study.live.table.trend")}</TableHead>
            {header("delta", t("study.live.table.delta"))}
            {header("unique", t("study.live.table.unique"))}
            {header("citations", t("study.live.table.citations"))}
            {header("papers", t("study.live.table.papers"))}
            {header("groups", t("study.live.table.groups"))}
            {header("repos", t("study.live.table.repos"))}
            {header("hn", t("study.live.table.hn"))}
            {header("since", t("study.live.table.since"))}
          </TableRow>
        </TableHeader>
        {groups.map((group) => (
          <TableBody key={group.stage}>
            <TableRow className="bg-muted hover:bg-muted">
              <TableCell colSpan={12} className="py-2">
                <span className="inline-flex flex-wrap items-baseline gap-x-2 gap-y-1 text-sm">
                  <LegendItem marker={stageTones[group.stage].marker} className="font-medium">
                    {t(`study.live.table.stages.${group.stage}`)}
                  </LegendItem>
                  <span className="font-mono text-xs text-muted-foreground tabular-nums">
                    {SUBGROUP_ORDER.reduce((sum, key) => sum + group.subgroups[key].length, 0)}
                  </span>
                  <span className="text-xs text-muted-foreground">
                    {t(`study.live.groups.basis.${group.stage}`)}
                  </span>
                </span>
              </TableCell>
            </TableRow>
            {SUBGROUP_ORDER.flatMap((key) => {
              const members = sorted(group.subgroups[key]);
              if (members.length === 0) return [];
              const rows = members.map((topic) => (
                <Row
                  key={topic.id}
                  topic={topic}
                  complete={complete}
                  name={name}
                  format={format}
                  onOpen={onOpen}
                  t={t}
                />
              ));
              if (key === "measured") return rows;
              return [
                <TableRow key={`${group.stage}-${key}`} className="hover:bg-transparent">
                  <TableCell colSpan={10} className="py-1.5 text-xs text-muted-foreground">
                    <span className="font-medium text-foreground">
                      {t(`study.live.groups.sub.${key}`)}
                    </span>
                    {" — "}
                    {t(`study.live.groups.subHint.${key}`)}
                  </TableCell>
                </TableRow>,
              ].concat(rows);
            })}
          </TableBody>
        ))}
      </Table>
      <p className="mt-2 text-xs text-muted-foreground">{t("study.live.groups.nonAdditive")}</p>
    </>
  );
}
