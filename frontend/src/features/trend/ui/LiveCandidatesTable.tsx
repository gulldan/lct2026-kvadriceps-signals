import { useState } from "react";
import { useTranslation } from "react-i18next";
import { ChevronDown, ChevronUp } from "lucide-react";
import { Badge } from "@/shared/ui/Badge";
import { LegendItem } from "@/shared/ui/LegendItem";
import { Button } from "@/shared/ui/Button";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/shared/ui/Table";
import { cn } from "@/shared/lib/cn";
import { formatMonth, formatNumber } from "@/shared/lib/format";
import type { LiveRejection, LiveTopic } from "../liveApi";
import { adoptionLabel, evidenceLevelLabel, marketLabel, momentumLabel } from "../signalReport";
import { tzSummary } from "../tzReport";
import { stageTones } from "../stageTones";
import { liveTopicName } from "./LiveOverview";
import { signalScore, thresholdLabelKey } from "../signalScore";

const COLUMNS = ["rank", "name", "confidence", "papers", "records", "since"] as const;
type Column = (typeof COLUMNS)[number];

export function LiveCandidatesTable({
  candidates,
  onOpen,
}: {
  candidates: LiveTopic[];
  onOpen: (candidate: LiveTopic) => void;
}) {
  const { t } = useTranslation("trend");
  const [sort, setSort] = useState<{ column: Column; desc: boolean }>({
    column: "rank",
    desc: false,
  });
  const rows = candidates.map((candidate, index) => ({ candidate, rank: index + 1 }));
  const value = (row: (typeof rows)[number]): number | string => {
    switch (sort.column) {
      case "name":
        return liveTopicName(row.candidate, t);
      case "confidence": {
        // «Мало данных» — не оценка: по справочному числу строка встала бы выше оценённых.
        const score = signalScore(row.candidate);
        return score && !score.gap ? score.percent : -1;
      }
      case "papers":
        return row.candidate.papers_count;
      case "records":
        return row.candidate.records_count ?? 0;
      case "since":
        return row.candidate.first_year || "";
      default:
        return row.rank;
    }
  };
  const sorted = rows.toSorted((a, b) => {
    const [first, second] = [value(a), value(b)];
    const order =
      typeof first === "string" || typeof second === "string"
        ? String(first).localeCompare(String(second))
        : first - second;
    return sort.desc ? -order : order;
  });
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
    <Table caption={t("signalList.tableTitle")} className="text-sm">
      <TableHeader>
        <TableRow>
          {header("rank", t("signalList.rankColumn"), "text-left")}
          {header("name", t("signalList.nameColumn"), "text-left")}
          <TableHead className="whitespace-nowrap">{t("tz.title")}</TableHead>
          <TableHead className="whitespace-nowrap">{t("report.stage")}</TableHead>
          <TableHead className="whitespace-nowrap">{t("report.momentum")}</TableHead>
          <TableHead className="whitespace-nowrap">{t("report.adoption")}</TableHead>
          <TableHead className="whitespace-nowrap">{t("report.market")}</TableHead>
          {header(
            "confidence",
            t(
              candidates.some((row) => row.model_score)
                ? "confidence.modelLabel"
                : "confidence.label",
            ),
          )}
          <TableHead className="whitespace-nowrap">{t("report.evidenceBar")}</TableHead>
          {header("papers", t("signalList.papers"))}
          {header("records", t("signalList.records"))}
          {header("since", t("signalList.firstSeen"))}
        </TableRow>
      </TableHeader>
      <TableBody>
        {sorted.map(({ candidate, rank }) => {
          const score = signalScore(candidate);
          const percent = score?.percent ?? null;
          return (
            <TableRow
              key={candidate.id}
              className="cursor-pointer"
              onClick={() => onOpen(candidate)}
            >
              <TableCell className="font-mono text-xs text-muted-foreground tabular-nums">
                {rank}
              </TableCell>
              <TableCell className="max-w-64 min-w-40">
                <Button
                  variant="link"
                  size="inline"
                  className="h-auto justify-start text-left font-medium whitespace-normal"
                  onClick={(event) => {
                    event.stopPropagation();
                    onOpen(candidate);
                  }}
                >
                  {liveTopicName(candidate, t)}
                </Button>
              </TableCell>
              <TableCell
                className="whitespace-nowrap"
                title={[candidate.tz?.stage_basis, candidate.tz?.trend_basis]
                  .filter(Boolean)
                  .join("\n")}
              >
                {tzSummary(candidate, t)}
              </TableCell>
              <TableCell className="whitespace-nowrap">
                <LegendItem marker={stageTones[candidate.stage].marker} className="text-xs">
                  {t(`study.stage.${candidate.stage}`)}
                </LegendItem>
              </TableCell>
              <TableCell className="whitespace-nowrap">{momentumLabel(candidate, t)}</TableCell>
              <TableCell>{adoptionLabel(candidate, t)}</TableCell>
              <TableCell>{marketLabel(candidate, t)}</TableCell>
              <TableCell className="text-right whitespace-nowrap">
                {score?.gap ? (
                  <>
                    <Badge variant="muted">{t("report.modelClass.insufficient_data")}</Badge>
                    <span className="mt-1 block text-micro text-muted-foreground">
                      {t(`report.dataGap.${score.gap}`)}
                    </span>
                    <span className="block text-micro text-muted-foreground">
                      {t("confidence.reference", { percent: score.percent })}
                    </span>
                  </>
                ) : percent === null ? (
                  "—"
                ) : (
                  <span className="font-mono text-xs tabular-nums">
                    {t("confidence.value", { percent })}
                    <span className="block text-micro text-muted-foreground">
                      {score &&
                        (score.model && !score.topUp && score.threshold !== undefined
                          ? t("confidence.modelThreshold", {
                              percent: Math.round(score.threshold * 100),
                            })
                          : t(thresholdLabelKey(score)))}
                    </span>
                  </span>
                )}
              </TableCell>
              <TableCell className="whitespace-nowrap">
                <Badge
                  variant={
                    candidate.shortfall ||
                    (candidate.evidence_level && candidate.evidence_level !== "strict")
                      ? "warn"
                      : "muted"
                  }
                >
                  {evidenceLevelLabel(candidate, t)}
                </Badge>
              </TableCell>
              <TableCell className="text-right font-mono text-xs tabular-nums">
                {formatNumber(candidate.papers_count)}
              </TableCell>
              <TableCell className="text-right font-mono text-xs tabular-nums">
                {formatNumber(candidate.records_count ?? 0)}
              </TableCell>
              <TableCell className="text-right font-mono text-xs whitespace-nowrap tabular-nums">
                {candidate.first_year ? formatMonth(candidate.first_year) : "—"}
              </TableCell>
            </TableRow>
          );
        })}
      </TableBody>
    </Table>
  );
}

export function RejectedTable({
  caption,
  rows,
}: {
  caption: string;
  rows: { row: LiveRejection; name: string }[];
}) {
  const { t } = useTranslation("trend");
  const model = rows.some(({ row }) => row.model_value !== undefined);
  return (
    <Table caption={caption} className="text-sm">
      <TableHeader>
        <TableRow>
          <TableHead>{t("signalList.nameColumn")}</TableHead>
          <TableHead className="whitespace-nowrap">{t("report.stage")}</TableHead>
          {model && (
            <TableHead className="text-right whitespace-nowrap">
              {t("confidence.modelLabel")}
            </TableHead>
          )}
          <TableHead className="text-right whitespace-nowrap">{t("confidence.label")}</TableHead>
          <TableHead className="text-right whitespace-nowrap">{t("signalList.papers")}</TableHead>
          <TableHead>{t("signalList.reasonColumn")}</TableHead>
        </TableRow>
      </TableHeader>
      <TableBody>
        {rows.map(({ row, name }) => (
          <TableRow key={`${row.code}:${row.original_name ?? row.name}`}>
            <TableCell className="max-w-64 min-w-40 font-medium">{name}</TableCell>
            <TableCell className="whitespace-nowrap">
              <LegendItem marker={stageTones[row.stage].marker} className="text-xs">
                {t(`study.stage.${row.stage}`)}
              </LegendItem>
            </TableCell>
            {model && (
              <TableCell className="text-right font-mono text-xs tabular-nums">
                {row.model_value !== undefined
                  ? t("confidence.value", { percent: Math.round(row.model_value * 100) })
                  : "—"}
              </TableCell>
            )}
            <TableCell className="text-right font-mono text-xs tabular-nums">
              {row.confidence ? t("confidence.value", { percent: row.confidence }) : "—"}
            </TableCell>
            <TableCell className="text-right font-mono text-xs tabular-nums">
              {formatNumber(row.papers)}
            </TableCell>
            <TableCell className="min-w-72">
              <Badge variant="muted">
                {t(`signalList.reject.${row.code}`, { defaultValue: row.code })}
              </Badge>
              <span className="mt-1 block max-w-[72ch] text-muted-foreground">{row.reason}</span>
            </TableCell>
          </TableRow>
        ))}
      </TableBody>
    </Table>
  );
}
