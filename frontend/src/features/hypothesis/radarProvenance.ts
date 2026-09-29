import type { TFunction } from "i18next";
import { i18n } from "@/shared/i18n";
import { formatDateTime, formatNumber, formatPercent } from "@/shared/lib/format";
import type { ReportBlock } from "@/shared/report/blocks";
import {
  dataGapLabel,
  type LiveAdoptionEvent,
  type LiveEventType,
  type LiveTopic,
  type LiveTZAssessment,
} from "@/features/trend";

/** Снимок на момент ручного переноса: повторный поиск не должен менять документы, по которым
 *  аналитик предложил гипотезу. */
export interface RadarProvenance {
  version: "radar-transfer-v1";
  query: string;
  topic?: string;
  candidate_id: string;
  name: string;
  observed_at: string;
  method: string;
  rules?: { method: string; value: number; passed: number; total: number; basis: string };
  model_score?: {
    value: number;
    model: string;
    version: string;
    label?: string;
    data_gap?: NonNullable<LiveTopic["model_score"]>["data_gap"];
    feature_schema?: string;
    threshold?: number | null;
    basis?: string | null;
    features?: { name: string; value: string; contribution?: number | null }[] | null;
  };
  market?: { status?: string; basis: string };
  sources: {
    id: string;
    title: string;
    url: string;
    date: string;
    passport?: string;
    language?: string;
    publisher_type?: string;
    genre?: string;
    source_name?: string;
    discovery_source?: string;
    trust?: string;
    trust_basis?: string;
    language_basis?: string;
    digest?: string;
    digest_model?: string;
    quotes: { field: string; quote: string; verified: boolean }[];
  }[];
  wording_model?: string;
  verification_model?: string;
  tz?: LiveTZAssessment | null;
  events?: LiveAdoptionEvent[] | null;
  bet?: RadarBet | null;
}

export interface RadarBet {
  horizon: "0-1" | "1-3" | "3+";
  /** 1–4, 0 — не установлена. */
  stage_code: number;
  next_stage_trigger: string;
  indicators: RadarBetIndicator[];
  baseline?: RadarBetSnapshot | null;
  snapshot?: RadarBetSnapshot | null;
  status?: RadarBetStatus;
  checked_at?: string;
  history?: RadarBetChange[];
}

export type RadarBetStatus = "confirming" | "unchanged" | "weakening";

interface RadarBetIndicator {
  kind: "event" | "downloads" | "stage" | "trend" | "model_score";
  event_type?: LiveEventType;
  threshold: number;
  label: string;
}

interface RadarBetSnapshot {
  observed_at: string;
  stage_code: number;
  trend_code: number;
  score: number;
  events: Partial<Record<LiveEventType, number>>;
  downloads?: number | null;
  model_score?: number | null;
}

interface RadarBetChange {
  at: string;
  kind: string;
  text: string;
  quote?: string;
  url?: string;
}

export function betIndicatorValue(indicator: RadarBetIndicator, snapshot: RadarBetSnapshot) {
  switch (indicator.kind) {
    case "stage":
      return snapshot.stage_code;
    case "trend":
      return snapshot.trend_code;
    case "event":
      return indicator.event_type ? (snapshot.events[indicator.event_type] ?? 0) : 0;
    case "downloads":
      return snapshot.downloads ?? 0;
    case "model_score":
      return snapshot.model_score ?? 0;
  }
}

export function radarBetBlocks(
  bet: RadarBet,
  t: TFunction<"hypothesis"> = i18n.getFixedT(null, "hypothesis"),
): ReportBlock[] {
  const now = bet.snapshot ?? bet.baseline;
  const blocks: ReportBlock[] = [
    { kind: "subheading", text: t("radar.bet.title") },
    {
      kind: "kv",
      rows: [
        {
          label: t("radar.bet.status"),
          value: t(`radar.bet.statuses.${bet.status ?? "unchanged"}`),
        },
        { label: t("radar.bet.horizon"), value: t(`radar.bet.horizons.${bet.horizon}`) },
        {
          label: t("radar.bet.stage"),
          value: t(`radar.bet.stages.${bet.stage_code}`, { defaultValue: t("radar.bet.stages.0") }),
        },
        { label: t("radar.bet.trigger"), value: bet.next_stage_trigger || "—" },
        {
          label: t("radar.bet.checked"),
          value: bet.checked_at ? formatDateTime(bet.checked_at) : t("radar.bet.notChecked"),
        },
      ],
    },
  ];
  if (bet.indicators.length) {
    blocks.push({
      kind: "table",
      title: t("radar.bet.indicators"),
      head: [t("radar.bet.indicator"), t("radar.bet.now"), t("radar.bet.met")],
      rows: bet.indicators.map((indicator) => {
        const value = now ? betIndicatorValue(indicator, now) : undefined;
        return [
          indicator.label,
          value === undefined
            ? "—"
            : indicator.kind === "model_score"
              ? formatPercent(value, 0)
              : formatNumber(value),
          t(value !== undefined && value >= indicator.threshold ? "radar.bet.yes" : "radar.bet.no"),
        ];
      }),
    });
  }
  blocks.push({ kind: "subheading", text: t("radar.bet.history") });
  if (bet.history?.length) {
    blocks.push({
      kind: "references",
      items: bet.history.toReversed().map((change, index) => ({
        n: index + 1,
        text: [formatDateTime(change.at), change.text, change.quote ? `«${change.quote}»` : ""]
          .filter(Boolean)
          .join(" · "),
        url: change.url,
      })),
    });
  } else {
    blocks.push({ kind: "paragraph", muted: true, text: t("radar.bet.noHistory") });
  }
  return blocks;
}

export function radarModelVerdict(
  model: NonNullable<RadarProvenance["model_score"]>,
  t: TFunction<"hypothesis"> = i18n.getFixedT(null, "hypothesis"),
): string {
  const tt = i18n.getFixedT(null, "trend");
  if (model.label === "insufficient_data") {
    return [
      model.data_gap ? dataGapLabel(model.data_gap, tt) : tt("report.modelClass.insufficient_data"),
      tt("confidence.reference", { percent: Math.round(model.value * 100) }),
    ].join(" · ");
  }
  return [
    formatPercent(model.value),
    model.label ? t(`radar.modelClass.${model.label}`, { defaultValue: model.label }) : "",
  ]
    .filter(Boolean)
    .join(" · ");
}

export function radarReportBlocks(
  radar: RadarProvenance,
  t: TFunction<"hypothesis"> = i18n.getFixedT(null, "hypothesis"),
): ReportBlock[] {
  const blocks: ReportBlock[] = [
    { kind: "paragraph", text: t("radar.transferred") },
    ...(radar.bet ? radarBetBlocks(radar.bet, t) : []),
    {
      kind: "kv",
      rows: [
        { label: t("radar.query"), value: radar.query },
        { label: t("radar.candidate"), value: radar.name },
        { label: t("radar.observed"), value: formatDateTime(radar.observed_at) },
        { label: t("radar.method"), value: radar.method },
        {
          label: t("radar.rules"),
          value: radar.rules
            ? `${radar.rules.passed}/${radar.rules.total} · ${radar.rules.method} · ${radar.rules.basis}`
            : "—",
        },
        {
          label: t("radar.model"),
          value: radar.model_score
            ? [
                radarModelVerdict(radar.model_score, t),
                `${radar.model_score.model} ${radar.model_score.version}`,
                radar.model_score.feature_schema,
              ]
                .filter(Boolean)
                .join(" · ")
            : t("radar.modelMissing"),
        },
        { label: t("radar.market"), value: radar.market?.basis || t("radar.marketUnknown") },
        { label: t("radar.wording"), value: radar.wording_model || "—" },
        { label: t("radar.verification"), value: radar.verification_model || "—" },
      ],
    },
  ];
  const model = radar.model_score;
  if (typeof model?.threshold === "number" && Number.isFinite(model.threshold)) {
    blocks.push({
      kind: "kv",
      rows: [{ label: t("radar.modelThreshold"), value: formatPercent(model.threshold, 1) }],
    });
  }
  if (model?.basis?.trim()) blocks.push({ kind: "paragraph", text: model.basis });
  if (model?.features?.length) {
    blocks.push({
      kind: "table",
      title: t("radar.model"),
      head: [t("hreport.r.factor"), t("hreport.r.value"), t("hreport.r.contribution")],
      rows: model.features.map((feature) => [
        feature.name,
        feature.value,
        typeof feature.contribution === "number"
          ? formatNumber(feature.contribution, { maximumFractionDigits: 3 })
          : "—",
      ]),
    });
  }
  for (const [index, source] of radar.sources.entries()) {
    blocks.push({
      kind: "references",
      items: [
        {
          n: index + 1,
          text: [
            source.title,
            source.date,
            source.passport,
            source.trust_basis,
            source.language_basis,
          ]
            .filter(Boolean)
            .join(" · "),
          url: source.url,
        },
      ],
    });
    if (source.digest) {
      const digest = source.digest.trim();
      const complete = /[.!?…。！？][»”'"\])]*$/.test(digest);
      blocks.push({ kind: "paragraph", text: complete ? digest : `${digest}…` });
      blocks.push({
        kind: "paragraph",
        muted: true,
        text: [
          t("radar.digestBy", { model: source.digest_model || "—" }),
          complete ? "" : t("radar.digestIncomplete"),
        ]
          .filter(Boolean)
          .join(" "),
      });
    }
    for (const quote of source.quotes) {
      blocks.push({
        kind: "quote",
        source: t(`radar.field.${quote.field}`, { defaultValue: t("radar.evidence") }),
        text: quote.quote,
        note: t(quote.verified ? "radar.checked" : "radar.unchecked"),
      });
    }
  }
  return blocks;
}
