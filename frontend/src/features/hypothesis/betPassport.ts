import type { TFunction } from "i18next";
import { i18n } from "@/shared/i18n";
import {
  formatDateShort,
  formatDateTime,
  formatNumber,
  formatPercent,
  upperFirst,
} from "@/shared/lib/format";
import type { ReportBlock, ReportDoc, ReportFact } from "@/shared/report/blocks";
import type { ApiEvidence, ApiHypothesis, ApiRevision } from "./api";
import { verdictMeta } from "./board/model";
import { displayHypothesisTitle, statusMeta } from "./model";
import {
  betIndicatorValue,
  radarBetBlocks,
  radarModelVerdict,
  type RadarBet,
  type RadarProvenance,
} from "./radarProvenance";

type T = TFunction<"hypothesis">;

const TREND_KEYS = [
  "passport.trend.0",
  "passport.trend.1",
  "passport.trend.2",
  "passport.trend.3",
] as const;

const SECTION_QUOTES = 2;
const QUOTE_CHARS = 160;

const DECISION_ACTIONS = new Set(["approved", "rejected", "status_changed"]);
const DECIDED = new Set<ApiHypothesis["status"]>(["approved", "under_review", "rejected"]);

export interface BetPassportInput {
  h: ApiHypothesis;
  radar: RadarProvenance;
  revisions: ApiRevision[];
}

/** Сигнал направления открывается в направлении: имя как запрос запустило бы новый поиск. */
export function radarSignalHref(radar: RadarProvenance, thesis?: string | null): string {
  const where = radar.topic ? ["topic", radar.topic] : ["q", radar.query];
  const params = new URLSearchParams([where, ["signal", radar.candidate_id]]);
  if (thesis) params.set("thesis", thesis);
  return `/trends?${params}`;
}

/** Последняя сверка знает больше оценки на момент переноса, а та — больше стадии прогноза. */
export function radarStageCode(radar: RadarProvenance): number {
  return radar.bet?.snapshot?.stage_code || radar.tz?.stage_code || radar.bet?.stage_code || 0;
}

/** Сервер начинает основание названием стадии, уже названной рядом: повтор читается как сбой. */
export function stageReason(basis: string | undefined) {
  const reason = basis
    ?.replace(/^стадия \d+ — [^:]+:\s*/i, "")
    .replace(/^утверждение карточки проверено по цитате:\s*/i, "");
  return upperFirst(reason ?? "");
}

export function tzStageLabel(code: number, t: T = i18n.getFixedT(null, "hypothesis")): string {
  return t(`radar.bet.stages.${code}`, { defaultValue: t("radar.bet.stages.0") });
}

function stageText(code: number, t: T): string {
  return code > 0
    ? t("passport.stageValue", { label: tzStageLabel(code, t) })
    : t("passport.stageUnknown");
}

/** Системные ревизии (архивация вместе с тезисом) решением не считаются. */
export function betDecision(
  h: Pick<ApiHypothesis, "status">,
  revisions: ApiRevision[],
): { label: string; revision?: ApiRevision } | null {
  if (!DECIDED.has(h.status)) return null;
  const revision = revisions
    .filter((r) => DECISION_ACTIONS.has(r.action) && r.editor_id && r.editor_id !== "system")
    .toSorted((a, b) => b.revision_no - a.revision_no)
    .at(0);
  return { label: statusMeta(h.status).label, revision };
}

function clip(text: string, max = QUOTE_CHARS): string {
  const flat = text.replace(/\s+/g, " ").trim();
  if (flat.length <= max) return flat;
  const cut = flat.slice(0, max);
  const end = Math.max(cut.lastIndexOf(". "), cut.lastIndexOf("! "), cut.lastIndexOf("? "));
  return end > max / 2 ? cut.slice(0, end + 1) : `${cut.trimEnd()}…`;
}

const kv = (rows: ReportFact[]): ReportBlock => ({
  kind: "kv",
  rows: rows.filter((r) => r.value.trim() !== ""),
});

const joined = (parts: (string | undefined)[], sep = " · ") => parts.filter(Boolean).join(sep);

function signalBlocks(radar: RadarProvenance, t: T): ReportBlock[] {
  const tz = radar.tz;
  const event = tz?.stage_event?.quote ? tz.stage_event : undefined;
  const stage = radarStageCode(radar);
  const model = radar.model_score;
  const blocks: ReportBlock[] = [
    kv([
      {
        label: t("passport.technology"),
        value: joined([
          radar.name,
          t("passport.observedOn", {
            date: formatDateShort(radar.observed_at),
            query: radar.query,
          }),
        ]),
      },
      {
        label: t("passport.tz"),
        value: joined(
          [
            tz && tz.score > 0
              ? t("passport.tzValue", {
                  label: tzStageLabel(stage, t),
                  trend: t(TREND_KEYS[tz.trend_code] ?? TREND_KEYS[0]),
                  score: tz.score,
                })
              : stageText(stage, t),
            event ? "" : stageReason(tz?.stage_basis),
          ],
          ". ",
        ),
      },
      {
        label: t("passport.scores"),
        value: joined(
          [
            model
              ? t("passport.modelValue", { value: radarModelVerdict(model, t) })
              : radar.rules
                ? t("passport.rulesValue", { passed: radar.rules.passed, total: radar.rules.total })
                : "",
          ],
          "; ",
        ),
      },
    ]),
  ];
  if (event?.quote) {
    blocks.push({
      kind: "quote",
      source: joined([
        event.type ? t(`passport.eventTypes.${event.type}`) : "",
        event.record.title,
      ]),
      text: clip(event.quote),
      note: event.record.url,
    });
  }
  // Одна работа приходит под двумя адресами с той же цитатой — повтор пропускается.
  const shown = new Set<string>();
  for (const [index, source] of radar.sources.entries()) {
    if (blocks.length - 1 >= SECTION_QUOTES) break;
    const quote = source.quotes
      .filter((q) => q.verified && q.quote.trim())
      .toSorted(
        (a, b) =>
          Number(a.field === "qualification") - Number(b.field === "qualification") ||
          a.quote.length - b.quote.length,
      )
      .at(0);
    if (!quote || shown.has(quote.quote)) continue;
    shown.add(quote.quote);
    blocks.push({
      kind: "quote",
      // Номер — тот же, которым проверка ниже называет работу радара.
      source: joined([
        `[${index + 1}] ${joined([source.source_name, source.date], ", ")}`.trim(),
        t(`radar.field.${quote.field}`, { defaultValue: t("radar.evidence") }),
      ]),
      text: clip(quote.quote),
      note: source.url,
    });
  }
  return blocks;
}

/** На листе — строками: таблица с отдельной историей вытесняла решение на второй лист. */
function compactBetBlocks(bet: RadarBet, t: T): ReportBlock[] {
  const now = bet.snapshot ?? bet.baseline;
  return [
    kv([
      {
        label: t("radar.bet.title"),
        value: joined([
          t(`radar.bet.statuses.${bet.status ?? "unchanged"}`),
          t(`radar.bet.horizons.${bet.horizon}`),
          bet.checked_at ? t("passport.checkedOn", { date: formatDateShort(bet.checked_at) }) : "",
        ]),
      },
      { label: t("passport.trigger"), value: bet.next_stage_trigger },
      {
        label: t("radar.bet.indicators"),
        value: bet.indicators
          .map((indicator) => {
            const value = now ? betIndicatorValue(indicator, now) : undefined;
            if (value === undefined) return indicator.label;
            return t("passport.indicatorNow", {
              label: indicator.label,
              value: indicator.kind === "model_score" ? formatPercent(value) : formatNumber(value),
              met: t(value >= indicator.threshold ? "passport.met" : "passport.notMet"),
            });
          })
          .join("; "),
      },
    ]),
    ...(bet.history?.length
      ? [
          {
            kind: "list",
            items: bet.history
              .toReversed()
              .slice(0, SECTION_QUOTES)
              .map((change) =>
                joined([
                  formatDateShort(change.at),
                  change.quote ? `${change.text}: «${clip(change.quote, 100)}»` : change.text,
                ]),
              ),
          } satisfies ReportBlock,
        ]
      : []),
  ];
}

function betIndicatorBlocks(radar: RadarProvenance, t: T, compact: boolean): ReportBlock[] {
  if (radar.bet) {
    return compact ? compactBetBlocks(radar.bet, t) : radarBetBlocks(radar.bet, t).slice(1);
  }
  const events = (radar.events ?? []).filter((e) => e.quote).slice(0, SECTION_QUOTES);
  if (events.length === 0) {
    return [{ kind: "paragraph", muted: true, text: t("passport.noIndicators") }];
  }
  return [
    { kind: "paragraph", muted: true, text: t("passport.events") },
    ...events.map((e): ReportBlock => ({
      kind: "quote",
      source: joined([
        e.type ? t(`passport.eventTypes.${e.type}`) : "",
        e.actor,
        e.amount,
        e.phase,
      ]),
      text: clip(e.quote ?? ""),
      note: e.record.url,
    })),
  ];
}

function checkBlocks(h: ApiHypothesis, radar: RadarProvenance, t: T): ReportBlock[] {
  const check = h.assessment.check;
  const verdict = verdictMeta(h);
  const count = (stance: ApiEvidence["stance"]) =>
    h.evidence.filter((e) => e.stance === stance).length;
  const blocks: ReportBlock[] = [
    kv([
      {
        label: t("passport.verdict"),
        value: verdict
          ? joined([
              verdict.label,
              t("passport.forAgainst", {
                for: check?.supporting?.length ?? count("supports"),
                against: check?.contradicting?.length ?? count("contradicts"),
              }),
              check?.checked_at ? formatDateShort(check.checked_at) : "",
            ])
          : t("passport.notChecked"),
      },
      {
        label: t("passport.readiness"),
        value: joined(
          [
            h.trl === null ? t("passport.trlPending") : t("passport.trlValue", { trl: h.trl }),
            stageText(radarStageCode(radar), t),
          ],
          "; ",
        ),
      },
    ]),
  ];
  if (check?.rationale) blocks.push({ kind: "paragraph", text: clip(check.rationale, 200) });
  const radarIndex = new Map(radar.sources.map((s, i) => [s.title, i]));
  const seen = new Set<string>();
  const rows = h.evidence
    .filter((e) => {
      const key = e.snippet.trim();
      if (!key || seen.has(key)) return false;
      seen.add(key);
      return true;
    })
    .toSorted(
      (a, b) =>
        Number(radarIndex.has(b.filename)) - Number(radarIndex.has(a.filename)) ||
        (b.score ?? 0) - (a.score ?? 0),
    );
  const against = rows.filter((e) => e.stance === "contradicts").slice(0, 1);
  const supports = rows
    .filter((e) => e.stance === "supports")
    .slice(0, SECTION_QUOTES - against.length);
  for (const e of [...supports, ...against]) {
    const index = radarIndex.get(e.filename);
    blocks.push({
      kind: "quote",
      source: joined([
        t(e.stance === "supports" ? "passport.for" : "passport.against"),
        index === undefined ? e.filename : t("passport.radarWork", { n: index + 1 }),
      ]),
      text: clip(e.snippet),
      note: index === undefined ? undefined : radar.sources[index]?.url,
    });
  }
  return blocks;
}

function decisionBlocks(h: ApiHypothesis, revisions: ApiRevision[], t: T): ReportBlock[] {
  const decision = betDecision(h, revisions);
  const r = decision?.revision;
  return [
    kv([
      {
        label: t("passport.decisionValue"),
        value: !decision
          ? t("passport.decisionNone")
          : r
            ? t("passport.decidedBy", {
                decision: decision.label,
                author: r.editor_id,
                date: formatDateTime(r.created_at),
              })
            : t("passport.decidedUnknown", { decision: decision.label }),
      },
      { label: t("passport.reason"), value: r?.summary ?? "" },
    ]),
  ];
}

export function betPassportBlocks(
  { h, radar, revisions }: BetPassportInput,
  t: T = i18n.getFixedT(null, "hypothesis"),
  compact = false,
): ReportBlock[] {
  return [
    { kind: "subheading", text: t("passport.signal") },
    ...signalBlocks(radar, t),
    { kind: "subheading", text: t("passport.indicators") },
    ...betIndicatorBlocks(radar, t, compact),
    { kind: "subheading", text: t("passport.check") },
    ...checkBlocks(h, radar, t),
    { kind: "subheading", text: t("passport.decision") },
    ...decisionBlocks(h, revisions, t),
  ];
}

export function betPassportDoc(input: BetPassportInput): ReportDoc {
  const t = i18n.getFixedT(null, "hypothesis");
  const { h } = input;
  const title = displayHypothesisTitle(h);
  const id8 = h.id.slice(0, 8);
  return {
    fileName: t("passport.fileName", { id: id8 }),
    title,
    dense: true,
    blocks: [
      {
        kind: "paragraph",
        muted: true,
        text: t("passport.doctype", { id: id8, date: formatDateShort(h.updated_at) }),
      },
      { kind: "heading", text: title },
      { kind: "paragraph", text: clip(h.statement.replace(/\*+/g, ""), 360) },
      ...betPassportBlocks(input, t, true),
    ],
  };
}
