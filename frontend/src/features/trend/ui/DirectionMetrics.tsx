import { useEffect, useState } from "react";
import { useTranslation } from "react-i18next";
import { ApiError } from "@/shared/api/client";
import { Disclosure } from "@/shared/ui/Disclosure";
import { InfoHint } from "@/shared/ui/InfoHint";
import { Notice } from "@/shared/ui/Notice";
import { Spinner } from "@/shared/ui/Spinner";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/shared/ui/Table";
import { formatDate, formatDateTime, formatNumber, formatPercent } from "@/shared/lib/format";
import { metricReasons, metricState } from "../metricState";
import {
  getTopicMetrics,
  isMetricEnvelope,
  type MetricEnvelope,
  type SensitivityCheck,
  type TopicMetrics,
} from "../technologyApi";
import { known } from "../technologyLabels";
import { MetricValue } from "./MetricValue";

const GROUPS = {
  growth: [
    "volume",
    "growth",
    "velocity",
    "acceleration",
    "monotonicity",
    "stability",
    "spike_flag",
    "momentum_index",
  ],
  novelty: ["novelty"],
  spread: ["hhi", "concentration", "spread", "participants"],
  impact: ["impact", "citations"],
  application: ["applied_events"],
  attention: ["attention"],
} as const;
type Group = keyof typeof GROUPS | "other";
const NAMES = [
  "volume",
  "growth",
  "velocity",
  "acceleration",
  "monotonicity",
  "stability",
  "spike_flag",
  "momentum_index",
  "hhi",
  "concentration",
  "spread",
  "participants",
  "impact",
  "citations",
  "applied_events",
  "attention",
] as const;
const CHECKS = ["largest_participant", "largest_peak", "single_provider"] as const;
const MODES = ["as_of", "retrospective_current_membership"] as const;

function groupOf(name: string): Group {
  for (const [group, names] of Object.entries(GROUPS) as [
    keyof typeof GROUPS,
    readonly string[],
  ][]) {
    if (names.some((prefix) => name === prefix || name.startsWith(prefix + "_"))) return group;
  }
  return "other";
}

const format = (value: number) => formatNumber(value, { maximumFractionDigits: 3 });

export function DirectionMetrics({
  run,
  topic,
  formula,
}: {
  run: string;
  topic: string;
  formula?: string;
}) {
  const { t } = useTranslation("trend");
  const [data, setData] = useState<TopicMetrics | null>(null);
  const [state, setState] = useState<"loading" | "ready" | "empty" | "error">("loading");
  useEffect(() => {
    const controller = new AbortController();
    setState("loading");
    getTopicMetrics(run, topic, controller.signal)
      .then((result) => {
        if (controller.signal.aborted) return;
        setData(result);
        setState("ready");
      })
      .catch((failure: unknown) => {
        if (controller.signal.aborted) return;
        setState(
          failure instanceof ApiError && (failure.status === 404 || failure.status === 409)
            ? "empty"
            : "error",
        );
      });
    return () => controller.abort();
  }, [run, topic]);
  if (state === "loading")
    return (
      <output className="flex items-center gap-2 text-sm text-muted-foreground">
        <span aria-hidden="true">
          <Spinner />
        </span>
        {t("loading")}
      </output>
    );
  if (state === "error") return <Notice tone="risk">{t("loadError")}</Notice>;
  const entries = Object.entries(data?.metrics ?? {}).filter(
    (entry): entry is [string, MetricEnvelope] => isMetricEnvelope(entry[1]),
  );
  if (state === "empty" || entries.length === 0) return <Notice>{t("metrics.empty")}</Notice>;
  const grouped = new Map<Group, [string, MetricEnvelope][]>();
  for (const entry of entries) {
    const group = groupOf(entry[0]);
    grouped.set(group, [...(grouped.get(group) ?? []), entry]);
  }
  const order: Group[] = [
    "growth",
    "novelty",
    "spread",
    "impact",
    "application",
    "attention",
    "other",
  ];
  const mode = known(MODES, data?.history_mode);
  return (
    <div className="space-y-6">
      {data && (
        <p className="text-xs text-muted-foreground">
          {mode ? t(`metrics.mode.${mode}`) : data.history_mode}
          {data.cutoff ? ` · ${t("cutoffAt", { date: data.cutoff.slice(0, 10) })}` : ""}
        </p>
      )}
      {order
        .filter((group) => grouped.has(group))
        .map((group) => (
          <section key={group} aria-label={t(`metrics.groups.${group}`)} className="space-y-2">
            <h3 className="text-sm font-medium">{t(`metrics.groups.${group}`)}</h3>
            <ul className="divide-y rounded-lg border">
              {grouped.get(group)!.map(([name, metric]) => (
                <li key={name} className="space-y-1 px-3 py-2">
                  <div className="flex flex-wrap items-baseline justify-between gap-x-4 gap-y-1 text-sm">
                    <span>{metricLabel(name, t)}</span>
                    <MetricValue metric={metric} format={format} />
                  </div>
                  <Disclosure title={t("metrics.explain.title")}>
                    <Explanation metric={metric} formula={formula} />
                  </Disclosure>
                </li>
              ))}
            </ul>
          </section>
        ))}
      <section aria-label={t("metrics.sensitivity.title")} className="space-y-2">
        <h3 className="text-sm font-medium">{t("metrics.sensitivity.title")}</h3>
        <p className="text-xs text-muted-foreground">{t("metrics.sensitivity.intro")}</p>
        <Sensitivity checks={data?.sensitivity ?? []} />
      </section>
      {data && data.limitations.length > 0 && (
        <section aria-label={t("metrics.limitations")} className="space-y-1">
          <h3 className="text-sm font-medium">{t("metrics.limitations")}</h3>
          <ul className="list-disc space-y-1 pl-5 text-sm text-muted-foreground">
            {data.limitations.map((limitation) => (
              <li key={limitation}>{limitation}</li>
            ))}
          </ul>
        </section>
      )}
    </div>
  );
}

function metricLabel(name: string, t: (key: `metrics.names.${(typeof NAMES)[number]}`) => string) {
  const key = known(NAMES, name);
  return key ? t(`metrics.names.${key}`) : name.replaceAll("_", " ");
}

function Explanation({ metric, formula }: { metric: MetricEnvelope; formula?: string }) {
  const { t } = useTranslation("trend");
  const measured = metricState(metric) === "measured";
  const number = (value: number | null | undefined) =>
    typeof value === "number" && Number.isFinite(value) ? format(value) : "—";
  const window =
    typeof metric.window === "string"
      ? metric.window
      : metric.window
        ? [metric.window.from, metric.window.to]
            .filter((day): day is string => !!day)
            .map(formatDate)
            .join(" — ")
        : "—";
  const rows: [string, string][] = [
    [
      t("metrics.explain.formula"),
      measured ? formula || "—" : t("metrics.explain.unavailableReason"),
    ],
    [t("metrics.explain.numerator"), number(metric.numerator)],
    [t("metrics.explain.denominator"), number(metric.denominator)],
    [t("metrics.explain.sample"), number(metric.sample_size)],
    [t("metrics.explain.window"), window],
    [
      t("metrics.explain.interval"),
      metric.interval
        ? `${format(metric.interval.low)} — ${format(metric.interval.high)}${metric.interval.level ? ` (${formatPercent(metric.interval.level)})` : ""}`
        : "—",
    ],
    [
      t("metrics.explain.coverage"),
      metric.coverage_refs?.length ? metric.coverage_refs.join(", ") : "—",
    ],
    [t("metrics.explain.method"), metric.method_version || "—"],
    [t("metrics.explain.computed"), metric.computed_at ? formatDateTime(metric.computed_at) : "—"],
  ];
  const reasons = measured ? [] : metricReasons(metric);
  return (
    <div className="space-y-2 text-xs">
      <dl className="grid gap-x-4 gap-y-1 sm:grid-cols-[12rem_minmax(0,1fr)]">
        {rows.map(([label, value]) => (
          <div key={label} className="contents">
            <dt className="text-muted-foreground">
              {label}
              {label === t("metrics.explain.interval") && metric.interval && (
                <InfoHint className="ml-1 align-middle">
                  {t("metrics.explain.intervalHint")}
                </InfoHint>
              )}
            </dt>
            <dd className="font-mono wrap-anywhere">{value}</dd>
          </div>
        ))}
      </dl>
      {reasons.length > 0 && (
        <ul className="list-disc pl-4 text-muted-foreground">
          {reasons.map((code) => (
            <li key={code}>
              {t(`metrics.reasons.${code}` as "metrics.reasons.unavailable", {
                defaultValue: code,
              })}
            </li>
          ))}
        </ul>
      )}
    </div>
  );
}

function Sensitivity({ checks }: { checks: SensitivityCheck[] }) {
  const { t } = useTranslation("trend");
  if (checks.length === 0)
    return <p className="text-sm text-muted-foreground">{t("metrics.sensitivity.empty")}</p>;
  const cell = (value: boolean | null | undefined) =>
    value === true
      ? t("metrics.sensitivity.changed")
      : value === false
        ? t("metrics.sensitivity.same")
        : t("metrics.sensitivity.unknown");
  return (
    <Table className="text-sm">
      <TableHeader>
        <TableRow>
          <TableHead>{t("metrics.sensitivity.title")}</TableHead>
          <TableHead>{t("metrics.sensitivity.sign")}</TableHead>
          <TableHead>{t("metrics.sensitivity.stage")}</TableHead>
          <TableHead>{t("metrics.sensitivity.position")}</TableHead>
        </TableRow>
      </TableHeader>
      <TableBody>
        {checks.map((check) => {
          const key = known(CHECKS, check.check);
          return (
            <TableRow key={check.check}>
              <TableCell>{key ? t(`metrics.sensitivity.checks.${key}`) : check.check}</TableCell>
              <TableCell>{cell(check.sign_changed)}</TableCell>
              <TableCell>{cell(check.stage_changed)}</TableCell>
              <TableCell>{cell(check.position_changed)}</TableCell>
            </TableRow>
          );
        })}
      </TableBody>
    </Table>
  );
}
