import type { TFunction } from "i18next";
import type { ReportBlock, ReportDoc } from "@/shared/report/blocks";
import { formatDate, formatNumber, upperFirst } from "@/shared/lib/format";
import type {
  LiveDetail,
  LiveExecutive,
  LiveExecutiveItem,
  LiveRejection,
  LiveTopic,
} from "./liveApi";
import {
  adoptionLabel,
  coverageRows,
  marketLabel,
  signalReportDoc,
  thresholdPercent,
} from "./signalReport";
import { isConfirmedSignal, isUnchecked, scoreSummary, signalScore } from "./signalScore";
import { spellAcronyms } from "./signalSources";
import { tzSummary } from "./tzReport";

/** Обязательный размер выдачи по ТЗ; одинаков для экрана и отчёта. */
export const LIVE_TOP_CANDIDATES = 15;

const REJECT_ORDER = [
  "off_topic",
  "not_technology",
  "incoherent_evidence",
  "single_source",
  "qualification_unavailable",
  "qualification_budget",
  "incomplete_card",
  "incomplete_interpretation",
  "mature",
  "market_formed",
  "hype",
  "long_history",
  "well_known",
  "one_group",
  "duplicate",
  "thin_evidence",
  "unassessed",
  "below_top",
];

/** Пятнадцать, где каждая строка — решение модели или помеченный добор, — цель выполнена, а не сорвана:
 *  предупреждение о недоборе противоречило бы экрану. Иначе топ не сформирован. */
export function topStatus(candidates: LiveTopic[]) {
  // То же правило, что у плашки «Слабый сигнал»: решение модели и второе независимое подтверждение.
  const confirmed = candidates.filter((row) => !row.shortfall && isConfirmedSignal(row)).length;
  const full = candidates.length >= LIVE_TOP_CANDIDATES;
  return {
    confirmed,
    incomplete: candidates.length > 0 && !full,
    toppedUp: full && confirmed < LIVE_TOP_CANDIDATES,
  };
}

/** Старые снимки хранили усечённые списки; пересчёт поверх ответа скрыл бы потерю причин отбора. */
export function candidateCountsAgree(detail: LiveDetail) {
  const counters = detail.counters;
  if (!counters) return true;
  const candidates = detail.candidates ?? [];
  const rejected = detail.rejected ?? [];
  return (
    counters.candidates === candidates.length + rejected.length &&
    (counters.selected === undefined || counters.selected === candidates.length) &&
    (counters.rejected === undefined || counters.rejected === rejected.length) &&
    // Добранная до пятнадцати технология в число прошедших порог не входит.
    counters.above_threshold ===
      candidates.filter((row) => !row.shortfall && row.confidence?.above_threshold).length
  );
}

export function rejectionGroups(rows: LiveRejection[]) {
  const codes = [...REJECT_ORDER, ...rows.map((row) => row.code)];
  return [...new Set(codes)]
    .map((code) => ({ code, items: rows.filter((row) => row.code === code) }))
    .filter((group) => group.items.length > 0);
}

const queryTitle = (query: string) => upperFirst(spellAcronyms(query));

function confidencePercent(topic: LiveTopic, t: TFunction<"trend">) {
  const score = signalScore(topic);
  if (!score) return "—";
  return score.gap ? scoreSummary(score, t) : t("confidence.value", { percent: score.percent });
}

interface CandidatesReportInput {
  detail: LiveDetail;
  query: string;
  name: (topic: LiveTopic) => string;
  t: TFunction<"trend">;
}

export function candidatesReportDoc({ detail, query, name, t }: CandidatesReportInput): ReportDoc {
  const candidates = detail.candidates ?? [];
  const top = topStatus(candidates);
  const rejected = detail.rejected ?? [];
  const confidence = candidates[0]?.confidence;
  const percent = thresholdPercent(confidence);
  const blocks: ReportBlock[] = [
    {
      kind: "doctitle",
      org: null,
      doctype: t("signalList.docType"),
      title: queryTitle(query),
      meta: [`${t("report.observedAt")}: ${formatDate(detail.observed_at)}`],
      lines: [t("signalList.summary", { shown: candidates.length, limit: LIVE_TOP_CANDIDATES })],
    },
  ];
  if (top.incomplete) {
    blocks.push({
      kind: "paragraph",
      text: `${t("signalList.incompleteTitle", { limit: LIVE_TOP_CANDIDATES })}. ${t("signalList.incompleteReason", { shown: candidates.length, count: top.confirmed, limit: LIVE_TOP_CANDIDATES })}`,
    });
  }
  if (top.toppedUp) {
    blocks.push({
      kind: "paragraph",
      muted: true,
      text: t("signalList.toppedUp", { count: top.confirmed, limit: LIVE_TOP_CANDIDATES }),
    });
  }
  if (!candidateCountsAgree(detail)) {
    blocks.push({ kind: "paragraph", text: t("signalList.countMismatch") });
  }
  blocks.push({
    kind: "table",
    title: t("signalList.tableTitle"),
    head: [
      t("signalList.rankColumn"),
      t("signalList.nameColumn"),
      t(candidates.some((row) => row.model_score) ? "confidence.modelLabel" : "confidence.label"),
      t("tz.title"),
      t("report.adoption"),
      t("report.market"),
    ],
    rows: candidates.map((candidate, index) => [
      String(index + 1),
      name(candidate),
      confidencePercent(candidate, t),
      tzSummary(candidate, t),
      adoptionLabel(candidate, t),
      marketLabel(candidate, t),
    ]),
  });
  blocks.push({ kind: "paragraph", muted: true, text: t("signalList.confidenceNote") });
  if (candidates.length > 0) {
    blocks.push({ kind: "heading", text: t("signalList.cardsTitle") });
    for (const [index, candidate] of candidates.entries()) {
      const [title, ...rest] = signalReportDoc({
        detail,
        candidate,
        query,
        rank: index + 1,
        total: candidates.length,
        name: name(candidate),
        t,
      }).blocks;
      if (title?.kind === "doctitle")
        blocks.push({
          kind: "card",
          label: t("signalList.place", { rank: index + 1 }),
          title: title.title,
          facts: title.facts ?? [],
          lead: title.lead,
          about: title.about,
          text: title.text,
          metrics: title.metrics ?? [],
        });
      const method = rest.findIndex(
        (block) => block.kind === "heading" && block.text === t("report.aboutMethod"),
      );
      blocks.push(...(method < 0 ? rest : rest.toSpliced(method, 2)));
    }
  }
  const judged = rejected.filter((row) => !isUnchecked(row));
  const unchecked = rejected.filter(isUnchecked);
  if (judged.length > 0) {
    blocks.push({ kind: "heading", text: t("signalList.rejectedTitle", { count: judged.length }) });
    blocks.push({ kind: "paragraph", text: t("signalList.rejectedIntro") });
    blocks.push(...rejectionBlocks(judged, t));
  }
  if (unchecked.length > 0) {
    blocks.push({
      kind: "heading",
      text: t("signalList.uncheckedTitle", { count: unchecked.length }),
    });
    blocks.push({ kind: "paragraph", text: t("signalList.uncheckedHint") });
    blocks.push(...rejectionBlocks(unchecked, t));
  }
  blocks.push({ kind: "heading", text: t("report.aboutMethod") });
  blocks.push({
    kind: "kv",
    rows: [
      { label: t("report.methodVersion"), value: detail.method || "—" },
      ...(confidence
        ? [
            {
              label: t("report.confidenceRules"),
              value: [confidence.method, percent !== null && t("confidence.threshold", { percent })]
                .filter(Boolean)
                .join(" · "),
            },
          ]
        : []),
      ...coverageRows(detail, confidence, t),
    ],
  });
  return {
    fileName: `${fileStem(query)}.docx`,
    title: queryTitle(query),
    blocks,
  };
}

function rejectionBlocks(rows: LiveRejection[], t: TFunction<"trend">): ReportBlock[] {
  return rejectionGroups(rows).flatMap((group): ReportBlock[] => [
    {
      kind: "subheading",
      text: `${t(`signalList.reject.${group.code}`, { defaultValue: group.code })} · ${formatNumber(group.items.length)}`,
    },
    {
      kind: "list",
      items: group.items.map(
        (row) =>
          `${row.name}${row.original_name ? ` (${t("signalList.originalName", { name: row.original_name })})` : ""} — ${row.reason}`,
      ),
    },
  ]);
}

const fileStem = (query: string) => query.replaceAll(/[^\p{L}\p{N} _-]/gu, "").trim() || "search";

const EXECUTIVE_STEPS = ["pilot", "watch", "reject"] as const;

export function executiveReportDoc({
  brief,
  name,
  t,
}: {
  brief: LiveExecutive;
  name: (item: LiveExecutiveItem) => string;
  t: TFunction<"trend">;
}): ReportDoc {
  const missing = t("executive.noData");
  const blocks: ReportBlock[] = [
    {
      kind: "doctitle",
      org: null,
      doctype: t("executive.docType"),
      title: queryTitle(brief.query),
      meta: [
        t("signalList.collectedAt", { date: formatDate(brief.observed_at) }),
        t("executive.coverage", {
          sources: formatNumber(brief.sources_processed),
          candidates: formatNumber(brief.candidates),
          selected: formatNumber(brief.selected),
        }),
      ],
      lines: [],
      about: t("executive.decisions"),
      text: brief.items.length
        ? t("executive.lead", { shown: brief.items.length, selected: brief.selected })
        : t("executive.none"),
      metrics: brief.items.length
        ? EXECUTIVE_STEPS.map((step) => ({
            value: String(brief.items.filter((item) => item.step === step).length),
            label: t(`executive.steps.${step}`),
          }))
        : [],
    },
  ];
  for (const step of [...EXECUTIVE_STEPS, undefined]) {
    const items = brief.items.filter((item) => item.step === step);
    if (items.length === 0) continue;
    const key = step ?? "none";
    blocks.push({ kind: "heading", text: t(`executive.steps.${key}`) });
    blocks.push({
      kind: "paragraph",
      muted: true,
      text: upperFirst(t(`executive.stepHints.${key}`)),
    });
    for (const item of items) {
      blocks.push({
        kind: "card",
        label: [
          t(`executive.steps.${key}`),
          item.horizon && `${t("executive.horizon")} ${t(`executive.horizons.${item.horizon}`)}`,
        ]
          .filter(Boolean)
          .join(" · "),
        title: name(item),
        facts: [
          { label: t("executive.benefit"), value: item.benefit || missing },
          { label: t("executive.reason"), value: item.reason || missing },
        ],
        lead: item.summary || missing,
        metrics: [],
      });
    }
  }
  if (brief.model) {
    blocks.push({
      kind: "paragraph",
      muted: true,
      text: t("executive.basis", { model: brief.model }),
    });
  }
  return {
    fileName: `${fileStem(`${t("executive.docType")} ${brief.query}`)}.docx`,
    title: queryTitle(brief.query),
    blocks,
  };
}
