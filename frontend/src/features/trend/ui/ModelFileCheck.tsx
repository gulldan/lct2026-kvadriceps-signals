import { useEffect, useRef, useState } from "react";
import { useTranslation } from "react-i18next";
import { FileUp } from "lucide-react";
import { ApiError } from "@/shared/api/client";
import { saveFile } from "@/shared/lib/download";
import { formatMonth, formatNumber } from "@/shared/lib/format";
import { Badge } from "@/shared/ui/Badge";
import { Button } from "@/shared/ui/Button";
import { CounterStrip } from "@/shared/ui/CounterStrip";
import { Disclosure } from "@/shared/ui/Disclosure";
import { Input } from "@/shared/ui/Input";
import { Notice } from "@/shared/ui/Notice";
import { Section } from "@/shared/ui/Section";
import { Spinner } from "@/shared/ui/Spinner";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/shared/ui/Table";
import {
  getLiveModelCheck,
  startLiveModelCheck,
  type ModelFileReport,
  type ModelFileRow,
} from "../liveApi";

// Обозначения идут в русские строки подстановкой: проверка текстов латиницу не пропускает.
const F1 = "F1";
const LATIN = {
  name: "technology, name",
  label: "label, is_weak_signal",
  phrases: "phrases",
};
const MAX_ROWS = 300;
// Книга Excel организаторов принимается как есть: сервер сам найдёт шапку под строкой-титулом.
const ACCEPT = [
  ".csv",
  ".json",
  ".xlsx",
  "text/csv",
  "application/json",
  "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet",
].join(",");
const POLL_MS = 2000;

type Check =
  | { status: "idle" }
  | { status: "running"; file: string }
  | { status: "error"; message: string }
  | { status: "done"; file: string; report: ModelFileReport };

const isWeak = (row: ModelFileRow, threshold: number) =>
  row.score === null ? null : row.score >= threshold;

const signed = (value: number) =>
  `${value >= 0 ? "+" : "−"}${formatNumber(Math.abs(value), { minimumFractionDigits: 2, maximumFractionDigits: 2 })}`;

const csvCell = (value: string) =>
  /[";\r\n]/.test(value) ? `"${value.replace(/"/g, '""')}"` : value;

export function ModelFileCheck() {
  const { t } = useTranslation("trend");
  const input = useRef<HTMLInputElement>(null);
  const abort = useRef<AbortController | null>(null);
  const [check, setCheck] = useState<Check>({ status: "idle" });
  useEffect(() => () => abort.current?.abort(), []);

  const run = async (file: File) => {
    abort.current?.abort();
    const controller = new AbortController();
    abort.current = controller;
    setCheck({ status: "running", file: file.name });
    try {
      let state = await startLiveModelCheck(file, controller.signal);
      while (!state.report) {
        await new Promise((resolve) => setTimeout(resolve, POLL_MS));
        if (controller.signal.aborted) return;
        state = await getLiveModelCheck(state.id, controller.signal);
      }
      setCheck({ status: "done", file: file.name, report: state.report });
    } catch (error) {
      if (controller.signal.aborted) return;
      setCheck({ status: "error", message: failureText(error) });
    }
  };
  const failureText = (error: unknown) => {
    const status = error instanceof ApiError ? error.status : 0;
    if (status === 413) return t("modelCheck.file.errors.tooLarge");
    if (status === 429) return t("modelCheck.file.errors.busy");
    if (status === 503) return t("modelCheck.file.errors.notReady");
    if (status === 400 && error instanceof Error)
      return t("modelCheck.file.errors.read", { detail: error.message });
    return t("modelCheck.file.errors.failed");
  };

  return (
    <Section reading title={t("modelCheck.file.title")}>
      <p>{t("modelCheck.file.intro", { metric: F1 })}</p>
      <ul className="list-disc space-y-1 pl-5">
        <li>{t("modelCheck.file.columnName", { latin: LATIN.name })}</li>
        <li>{t("modelCheck.file.columnLabel", { latin: LATIN.label })}</li>
        <li>{t("modelCheck.file.columnPhrases", { latin: LATIN.phrases })}</li>
      </ul>
      <p>{t("modelCheck.file.formats", { rows: formatNumber(MAX_ROWS) })}</p>
      <p className="text-muted-foreground">{t("modelCheck.file.cost")}</p>
      <div className="flex flex-wrap items-center gap-3">
        <Button
          type="button"
          variant="outline"
          disabled={check.status === "running"}
          onClick={() => input.current?.click()}
        >
          <FileUp aria-hidden />
          {t("modelCheck.file.pick")}
        </Button>
        <Input
          ref={input}
          type="file"
          accept={ACCEPT}
          className="hidden"
          aria-label={t("modelCheck.file.pick")}
          onChange={(event) => {
            const file = event.target.files?.[0];
            event.target.value = "";
            if (file) void run(file);
          }}
        />
        {check.status === "running" && <Running file={check.file} />}
      </div>
      {check.status === "error" && <Notice tone="risk">{check.message}</Notice>}
      {check.status === "done" && <FileReport file={check.file} report={check.report} />}
    </Section>
  );
}

function Running({ file }: { file: string }) {
  const { t } = useTranslation("trend");
  const [seconds, setSeconds] = useState(0);
  useEffect(() => {
    const timer = setInterval(() => setSeconds((value) => value + 1), 1000);
    return () => clearInterval(timer);
  }, []);
  return (
    <output className="flex items-center gap-2 text-sm text-muted-foreground">
      <Spinner size="sm" />
      {t("modelCheck.file.running", { name: file, seconds: formatNumber(seconds) })}
    </output>
  );
}

function FileReport({ file, report }: { file: string; report: ModelFileReport }) {
  const { t } = useTranslation("trend");
  const percent = (share: number, digits = 0) =>
    t("modelCheck.percent", {
      percent: formatNumber(share * 100, { maximumFractionDigits: digits }),
    });
  const { rows, metrics, threshold } = report;
  const scored = rows.filter((row) => row.score !== null);
  const withoutHistory = rows.filter((row) => row.note === "no_history").length;
  const labeled = rows.some((row) => row.label !== null || row.label_text);
  const decision = (weak: boolean | null) =>
    weak === null ? "—" : t(`modelCheck.file.decisions.${weak ? "weak" : "other"}`);
  const match = (row: ModelFileRow) => {
    const weak = isWeak(row, threshold);
    return row.label === null || weak === null ? null : weak === row.label;
  };
  const note = (row: ModelFileRow) => (row.note ? t(`modelCheck.file.notes.${row.note}`) : "");
  const label = (row: ModelFileRow) =>
    row.label_text
      ? t("modelCheck.file.unknownLabel", { label: row.label_text })
      : decision(row.label);
  const download = () => {
    const yesNo = (value: boolean | null) =>
      value === null ? "" : t(`modelCheck.file.${value ? "yes" : "no"}`);
    const head = [
      "number",
      "technology",
      "phrases",
      "phrasesSource",
      "score",
      "decision",
      "label",
      "match",
      "note",
      "drivers",
    ] as const;
    const lines = [head.map((key) => t(`modelCheck.file.${key}`)).join(";")];
    for (const row of rows) {
      lines.push(
        [
          String(row.number),
          row.name,
          (row.phrases ?? []).join(", "),
          row.phrases_from ? t(`modelCheck.file.phrasesFrom.${row.phrases_from}`) : "",
          row.score === null ? "" : formatNumber(row.score, { maximumFractionDigits: 3 }),
          decision(isWeak(row, threshold)),
          row.label === null && !row.label_text ? "" : label(row),
          yesNo(match(row)),
          note(row),
          (row.features ?? [])
            .map((feature) => `${feature.name} ${signed(feature.contribution)}`)
            .join("; "),
        ]
          .map(csvCell)
          .join(";"),
      );
    }
    // BOM и точка с запятой: Excel в русской локали иначе склеивает столбцы и кириллицу.
    saveFile(
      t("modelCheck.file.fileName", { name: file.replace(/\.\w+$/, "") }),
      `﻿${lines.join("\r\n")}`,
      "text/csv;charset=utf-8",
    );
  };

  return (
    <div className="space-y-4">
      <p className="font-medium">
        {t("modelCheck.file.summary", {
          version: report.version,
          month: formatMonth(report.month),
          threshold: formatNumber(threshold * 100, { maximumFractionDigits: 0 }),
          scored: formatNumber(scored.length),
          total: formatNumber(rows.length),
          weak: formatNumber(scored.filter((row) => isWeak(row, threshold)).length),
        })}
      </p>
      {scored.length < rows.length && (
        <Notice tone="warn">
          {t("modelCheck.file.unscored", { count: rows.length - scored.length })}
        </Notice>
      )}
      {withoutHistory > 0 && (
        <Notice tone="warn">{t("modelCheck.file.noHistory", { count: withoutHistory })}</Notice>
      )}
      {metrics && (metrics.unknown_labels ?? 0) > 0 && (
        <Notice tone="warn">
          {t("modelCheck.file.unknownLabels", { count: metrics.unknown_labels })}
        </Notice>
      )}
      {metrics && (
        <>
          <CounterStrip
            items={[
              { label: t("modelCheck.precision"), value: percent(metrics.precision, 1) },
              { label: t("modelCheck.recall"), value: percent(metrics.recall, 1) },
              { label: t("modelCheck.f1", { metric: F1 }), value: percent(metrics.f1, 1) },
              { label: t("modelCheck.file.accuracy"), value: percent(metrics.accuracy, 1) },
            ]}
          />
          <p>
            {t("modelCheck.file.metricsNote", {
              scored: formatNumber(metrics.scored),
              labeled: formatNumber(metrics.labeled),
              tp: formatNumber(metrics.tp),
              fp: formatNumber(metrics.fp),
              fn: formatNumber(metrics.fn),
              tn: formatNumber(metrics.tn),
            })}
          </p>
          {(metrics.abstained ?? 0) > 0 && (
            <p>
              {t("modelCheck.file.abstained", {
                count: metrics.abstained,
                weak: formatNumber(metrics.abstained_weak),
                recall: percent(
                  metrics.tp / Math.max(metrics.tp + metrics.fn + metrics.abstained_weak, 1),
                  1,
                ),
              })}
            </p>
          )}
        </>
      )}
      <Button type="button" variant="outline" onClick={download}>
        {t("modelCheck.file.download")}
      </Button>
      <Table caption={t("modelCheck.file.title")} className="text-sm">
        <TableHeader>
          <TableRow>
            <TableHead className="text-right">{t("modelCheck.file.number")}</TableHead>
            <TableHead>{t("modelCheck.file.technology")}</TableHead>
            <TableHead className="text-right">{t("modelCheck.file.score")}</TableHead>
            <TableHead>{t("modelCheck.file.decision")}</TableHead>
            {labeled && <TableHead>{t("modelCheck.file.label")}</TableHead>}
            {labeled && <TableHead>{t("modelCheck.file.match")}</TableHead>}
          </TableRow>
        </TableHeader>
        <TableBody>
          {rows.map((row) => {
            const weak = isWeak(row, threshold);
            const matched = match(row);
            return (
              <TableRow key={row.number}>
                <TableCell className="text-right font-mono text-xs text-muted-foreground tabular-nums">
                  {row.number}
                </TableCell>
                <TableCell className="max-w-md min-w-56">
                  <span lang="ru">{row.name || "—"}</span>
                  {row.phrases && row.phrases.length > 0 && (
                    <span className="block text-xs text-muted-foreground">
                      <span lang="en">{row.phrases.join(", ")}</span>
                      {row.phrases_from &&
                        ` · ${t(`modelCheck.file.phrasesFrom.${row.phrases_from}`)}`}
                    </span>
                  )}
                  {row.note && <span className="block text-xs text-warn">{note(row)}</span>}
                  {row.features && row.features.length > 0 && (
                    <Disclosure
                      lazy
                      title={t("modelCheck.file.why")}
                      className="mt-1 border-t-0"
                      summaryClassName="py-1 text-xs text-muted-foreground"
                    >
                      <p className="mb-1 text-xs text-muted-foreground">
                        {t("modelCheck.file.whyHint")}
                      </p>
                      <ul className="space-y-1 text-xs">
                        {row.features.map((feature) => (
                          <li key={feature.name} className="flex justify-between gap-3">
                            <span className="min-w-0">
                              <span className="font-medium">{feature.name}</span>
                              <span className="block text-muted-foreground">{feature.value}</span>
                            </span>
                            <span className="shrink-0 font-mono tabular-nums">
                              {signed(feature.contribution)}
                            </span>
                          </li>
                        ))}
                      </ul>
                    </Disclosure>
                  )}
                </TableCell>
                <TableCell className="text-right font-mono tabular-nums">
                  {row.score === null ? "—" : percent(row.score)}
                </TableCell>
                <TableCell>
                  {weak !== null && (
                    <Badge variant={weak ? "ok" : "muted"} className="whitespace-nowrap">
                      {decision(weak)}
                    </Badge>
                  )}
                </TableCell>
                {labeled && <TableCell className="whitespace-nowrap">{label(row)}</TableCell>}
                {labeled && (
                  <TableCell>
                    {matched !== null && (
                      <Badge variant={matched ? "ok" : "risk"}>
                        {t(`modelCheck.file.${matched ? "yes" : "no"}`)}
                      </Badge>
                    )}
                  </TableCell>
                )}
              </TableRow>
            );
          })}
        </TableBody>
      </Table>
    </div>
  );
}
