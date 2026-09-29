import { badgeTones } from "@/shared/design-system/tones";
import { i18n } from "@/shared/i18n";

import type { ApiHypothesis, AssessmentScores, HypothesisStatus } from "./api";

export function successCriteriaLines(value: string | string[] | undefined): string[] {
  if (value === undefined) return [];
  const arr = Array.isArray(value) ? value : [value];
  return arr.map((s) => s.trim()).filter((s) => s !== "");
}

export function isResearchDirection(h: ApiHypothesis): boolean {
  const g = h.generation ?? {};
  return g.semantic_kind === "research_direction" || g.kind === "auto_cluster";
}

export function isFallbackDraft(h: ApiHypothesis): boolean {
  const g = h.generation ?? {};
  return (
    g.kind === "fallback_evidence" ||
    g.model === "cluster-fallback" ||
    typeof g.fallback_reason === "string"
  );
}

const GENERIC_INTERNAL_TITLES = new Set(["тематическая группа"]);
const GENERIC_METADATA_NOISE = /(<[^>]+>)|(https?:\/\/\S+)|(\bwww\.)/i;

export function hasInternalNoise(value: string): boolean {
  return GENERIC_METADATA_NOISE.test(value.trim());
}

export function cleanInternalTitle(title: string): string {
  const trimmed = title.trim();
  const normalized = trimmed.toLowerCase();
  if (trimmed === "" || GENERIC_INTERNAL_TITLES.has(normalized) || hasInternalNoise(trimmed)) {
    return "";
  }
  if (trimmed.includes(" / ")) {
    for (const part of trimmed.split(" / ")) {
      const clean = cleanInternalTitle(part);
      if (clean !== "") return clean;
    }
    return "";
  }
  return trimmed;
}

export function displayHypothesisTitle(h: Pick<ApiHypothesis, "title">): string {
  return cleanInternalTitle(h.title) || i18n.t("hypothesis:untitled");
}

export function directionMeta(): { label: string; className: string } {
  return {
    label: i18n.t("hypothesis:badges.direction"),
    className: badgeTones.brand,
  };
}

const SCHEMA_FIELD_KEYS: Record<
  string,
  | "hypothesis:schemaFields.intervention"
  | "hypothesis:schemaFields.mechanism"
  | "hypothesis:schemaFields.target"
  | "hypothesis:schemaFields.validationPlan"
> = {
  intervention: "hypothesis:schemaFields.intervention",
  mechanism: "hypothesis:schemaFields.mechanism",
  target: "hypothesis:schemaFields.target",
  validation_plan: "hypothesis:schemaFields.validationPlan",
};

/** Незнакомые ключи проходят слегка очищенными: ничего не теряется молча. */
export function schemaMissingLabels(missing: string[] | undefined): string[] {
  return (missing ?? [])
    .map((key) => {
      const dictKey = SCHEMA_FIELD_KEYS[key];
      return dictKey ? i18n.t(dictKey) : key.replace(/_/g, " ").trim();
    })
    .filter((v) => v !== "");
}

export const CONFIDENCE_FACETS = [
  {
    key: "belief_score",
    get label() {
      return i18n.t("hypothesis:confidenceFacets.belief.label");
    },
    get hint() {
      return i18n.t("hypothesis:confidenceFacets.belief.hint");
    },
  },
  {
    key: "verdict_confidence",
    get label() {
      return i18n.t("hypothesis:confidenceFacets.verdict.label");
    },
    get hint() {
      return i18n.t("hypothesis:confidenceFacets.verdict.hint");
    },
  },
  {
    key: "evidence_quality",
    get label() {
      return i18n.t("hypothesis:confidenceFacets.evidence.label");
    },
    get hint() {
      return i18n.t("hypothesis:confidenceFacets.evidence.hint");
    },
  },
] satisfies { key: keyof AssessmentScores; label: string; hint: string }[];

const RESEARCH_TAG_THEORETICAL = "теоретическое исследование";
const RESEARCH_TAG_PRACTICAL = "практическое";

export type ResearchTypeTag = typeof RESEARCH_TAG_THEORETICAL | typeof RESEARCH_TAG_PRACTICAL;

export function researchTypeTag(tags: string[]): ResearchTypeTag | null {
  for (const tag of tags) {
    const v = tag.trim().toLowerCase();
    if (v === RESEARCH_TAG_THEORETICAL) return RESEARCH_TAG_THEORETICAL;
    if (v === RESEARCH_TAG_PRACTICAL) return RESEARCH_TAG_PRACTICAL;
  }
  return null;
}

export function researchTypeMeta(tag: ResearchTypeTag | null): {
  label: string;
  className: string;
} | null {
  if (tag === RESEARCH_TAG_THEORETICAL) {
    return {
      label: i18n.t("hypothesis:researchTypes.theoretical"),
      className: badgeTones.brand,
    };
  }
  if (tag === RESEARCH_TAG_PRACTICAL) {
    return {
      label: i18n.t("hypothesis:researchTypes.practical"),
      className: badgeTones.ok,
    };
  }
  return null;
}

export function trlMeta(trl: number | null): { label: string; className: string } {
  if (trl === null) {
    return {
      label: i18n.t("hypothesis:trl.none"),
      className: badgeTones.muted,
    };
  }
  const label = i18n.t("hypothesis:trl.value", { trl });
  if (trl <= 3) {
    return { label, className: badgeTones.secondary };
  }
  if (trl <= 6) {
    return { label, className: badgeTones.warn };
  }
  return { label, className: badgeTones.ok };
}

const STATUS_META: Record<
  HypothesisStatus,
  {
    labelKey:
      | "hypothesis:statuses.draft"
      | "hypothesis:statuses.generated"
      | "hypothesis:statuses.underReview"
      | "hypothesis:statuses.approved"
      | "hypothesis:statuses.rejected"
      | "hypothesis:statuses.archived";
    className: string;
  }
> = {
  draft: {
    labelKey: "hypothesis:statuses.draft",
    className: badgeTones.secondary,
  },
  generated: {
    labelKey: "hypothesis:statuses.generated",
    className: badgeTones.brand,
  },
  under_review: {
    labelKey: "hypothesis:statuses.underReview",
    className: badgeTones.warn,
  },
  approved: {
    labelKey: "hypothesis:statuses.approved",
    className: badgeTones.ok,
  },
  rejected: {
    labelKey: "hypothesis:statuses.rejected",
    className: badgeTones.risk,
  },
  archived: {
    labelKey: "hypothesis:statuses.archived",
    className: badgeTones.muted,
  },
};

export function statusMeta(status: HypothesisStatus): { label: string; className: string } {
  const meta = STATUS_META[status] ?? STATUS_META.draft;
  return { label: i18n.t(meta.labelKey), className: meta.className };
}

const REVISION_ACTIONS = [
  "created",
  "edited",
  "status_changed",
  "score_override",
  "approved",
  "rejected",
  "commented",
  "verified",
  "enriched",
  "planned",
] as const;

export function revisionActionLabel(action: string): string {
  const key = REVISION_ACTIONS.find((item) => item === action);
  return key ? i18n.t(`hypothesis:revisionActions.${key}`) : action;
}

const PLAN_ESTIMATES = ["low", "medium", "high", "days", "weeks", "months"] as const;

export function planEstimateLabel(value: string | undefined): string {
  const v = (value ?? "").trim();
  const key = PLAN_ESTIMATES.find((item) => item === v.toLowerCase());
  return key ? i18n.t(`hypothesis:planEstimates.${key}`) : v;
}

const REVIEW_STATUSES = new Set<HypothesisStatus>([
  "under_review",
  "approved",
  "rejected",
  "archived",
]);

// Машинные состояния по умолчанию («generated», «draft») скрыты: они есть у каждой новой гипотезы.
export function reviewStatusMeta(
  status: HypothesisStatus,
): { label: string; className: string } | null {
  return REVIEW_STATUSES.has(status) ? statusMeta(status) : null;
}

export function pct(score: number | null | undefined): string {
  if (score === null || score === undefined) {
    return "—";
  }
  return `${Math.round(score * 100)}%`;
}

export function visibleMeta(value: string | null | undefined): string | null {
  const trimmed = (value ?? "").trim();
  const normalized = trimmed.toLowerCase();
  if (trimmed === "" || trimmed === "—" || trimmed === "-" || normalized === "n/a") {
    return null;
  }
  return cleanInternalTitle(trimmed) || null;
}

export function sourceTypeLabel(value: string | null | undefined): string | null {
  const source = visibleMeta(value);
  return source
    ? i18n.t(`hypothesis:sourceTypes.${source.toLowerCase()}`, { defaultValue: source })
    : null;
}

export function priorityScore(h: ApiHypothesis): number | null {
  const s = h.assessment?.ranking?.score ?? h.composite_score ?? null;
  return s === null ? null : Math.round(s * 100);
}

function csvCell(value: string): string {
  return /[";\n]/.test(value) ? `"${value.replace(/"/g, '""')}"` : value;
}

/** CSV для Excel: BOM UTF-8 и разделитель «;» для русской локали. */
export function hypothesesToCSV(rows: ApiHypothesis[]): string {
  const head = [
    i18n.t("hypothesis:export.csv.title"),
    i18n.t("hypothesis:export.csv.statement"),
    i18n.t("hypothesis:export.csv.priority"),
    i18n.t("hypothesis:export.csv.trl"),
    i18n.t("hypothesis:export.csv.novelty"),
    i18n.t("hypothesis:export.csv.risk"),
    i18n.t("hypothesis:export.csv.value"),
    i18n.t("hypothesis:export.csv.confidence"),
    i18n.t("hypothesis:export.csv.organization"),
    i18n.t("hypothesis:export.csv.method"),
    i18n.t("hypothesis:export.csv.source"),
    i18n.t("hypothesis:export.csv.location"),
    i18n.t("hypothesis:export.csv.status"),
    i18n.t("hypothesis:export.csv.tags"),
  ];
  const lines = [head.join(";")];
  for (const h of rows) {
    const prio = priorityScore(h);
    const cells = [
      displayHypothesisTitle(h),
      h.statement,
      prio === null ? "" : String(prio),
      h.trl === null ? "" : String(h.trl),
      pct(h.novelty_score),
      pct(h.risk_score),
      pct(h.value_score),
      pct(h.confidence_score),
      h.organization,
      h.function_area,
      h.source_type,
      h.location,
      statusMeta(h.status).label,
      h.tags.join(", "),
    ];
    lines.push(cells.map((c) => csvCell(c)).join(";"));
  }
  return `﻿${lines.join("\r\n")}`;
}

export function hypothesesToJSON(rows: ApiHypothesis[]): string {
  const ordered = [...rows].toSorted((a, b) => (priorityScore(b) ?? -1) - (priorityScore(a) ?? -1));
  const items = ordered.map((h) => ({
    id: h.id,
    title: displayHypothesisTitle(h),
    statement: h.statement,
    rationale: h.rationale,
    priority: priorityScore(h),
    trl: h.trl,
    novelty: h.novelty_score,
    risk: h.risk_score,
    value: h.value_score,
    confidence: h.confidence_score,
    status: h.status,
    kpi_id: h.kpi_id,
    method: h.method,
    organization: h.organization || undefined,
    function_area: h.function_area || undefined,
    source_type: h.source_type || undefined,
    tags: h.tags,
    created_at: h.created_at,
  }));
  return JSON.stringify(
    { generated_at: new Date().toISOString(), count: items.length, items },
    null,
    2,
  );
}

function taskPriority(h: ApiHypothesis): "Highest" | "High" | "Medium" | "Low" {
  const p = priorityScore(h);
  if (p === null) return "Medium";
  if (p >= 80) return "Highest";
  if (p >= 60) return "High";
  if (p >= 35) return "Medium";
  return "Low";
}

function jiraLabel(tag: string): string {
  return tag.trim().replace(/\s+/g, "-");
}

function taskDescription(h: ApiHypothesis): string {
  const parts = [h.statement];
  if (h.rationale) {
    parts.push("", `${i18n.t("hypothesis:digest.rationale")}: ${h.rationale}`);
  }
  const criteria = successCriteriaLines(h.detail?.experiment_plan?.success_criteria);
  if (criteria.length > 0) {
    parts.push("", `${i18n.t("hypothesis:digest.successCriteria")}: ${criteria.join("; ")}`);
  }
  return parts.join("\n");
}

function jiraCell(value: string): string {
  return /[",\n]/.test(value) ? `"${value.replace(/"/g, '""')}"` : value;
}

export function hypothesesToJiraCSV(rows: ApiHypothesis[]): string {
  const ordered = [...rows].toSorted((a, b) => (priorityScore(b) ?? -1) - (priorityScore(a) ?? -1));
  const lines = ["Summary,Description,Issue Type,Priority,Labels,Status"];
  for (const h of ordered) {
    const labels = [...h.tags.map(jiraLabel), "hypothesis-factory"].filter(Boolean).join(" ");
    const cells = [
      displayHypothesisTitle(h),
      taskDescription(h),
      "Task",
      taskPriority(h),
      labels,
      statusMeta(h.status).label,
    ];
    lines.push(cells.map((c) => jiraCell(c)).join(","));
  }
  return `﻿${lines.join("\r\n")}`;
}

export function hypothesesToTasksJSON(rows: ApiHypothesis[]): string {
  const ordered = [...rows].toSorted((a, b) => (priorityScore(b) ?? -1) - (priorityScore(a) ?? -1));
  const tasks = ordered.map((h) => ({
    summary: displayHypothesisTitle(h),
    description: taskDescription(h),
    type: "Task",
    priority: taskPriority(h),
    labels: [...h.tags.map(jiraLabel), "hypothesis-factory"],
    status: h.status,
    external_id: h.id,
    kpi_id: h.kpi_id ?? undefined,
    trl: h.trl ?? undefined,
    rank: priorityScore(h) ?? undefined,
  }));
  return JSON.stringify(
    { generated_at: new Date().toISOString(), count: tasks.length, tasks },
    null,
    2,
  );
}

export function hypothesesToDigest(rows: ApiHypothesis[]): string {
  const ordered = [...rows].toSorted((a, b) => (priorityScore(b) ?? -1) - (priorityScore(a) ?? -1));
  const out = [`# ${i18n.t("hypothesis:digest.heading", { n: rows.length })}`, ""];
  for (const h of ordered) {
    const prio = priorityScore(h);
    out.push(
      `## ${displayHypothesisTitle(h)} · ${trlMeta(h.trl).label}${
        prio !== null ? ` · ${i18n.t("hypothesis:digest.priority", { n: prio })}` : ""
      }`,
    );
    const meta = [statusMeta(h.status).label, h.organization, h.function_area].filter(Boolean);
    out.push(`*${meta.join(" · ")}*`, "", h.statement, "");
    if (h.rationale) {
      out.push(`**${i18n.t("hypothesis:digest.rationale")}:** ${h.rationale}`, "");
    }
    const chain = (h.detail?.causal_chain ?? [])
      .map((s) => s.stage)
      .filter(Boolean)
      .join(" → ");
    if (chain) {
      out.push(`**${i18n.t("hypothesis:digest.mechanism")}:** ${chain}`, "");
    }
    const criteria = successCriteriaLines(h.detail?.experiment_plan?.success_criteria);
    if (criteria.length > 0) {
      out.push(`**${i18n.t("hypothesis:digest.successCriteria")}:** ${criteria.join("; ")}`, "");
    }
    const scores = [
      h.novelty_score === null
        ? ""
        : i18n.t("hypothesis:digest.novelty", { value: pct(h.novelty_score) }),
      h.risk_score === null ? "" : i18n.t("hypothesis:digest.risk", { value: pct(h.risk_score) }),
      h.value_score === null
        ? ""
        : i18n.t("hypothesis:digest.value", { value: pct(h.value_score) }),
      h.confidence_score === null
        ? ""
        : i18n.t("hypothesis:digest.confidence", { value: pct(h.confidence_score) }),
    ].filter(Boolean);
    if (scores.length > 0) {
      out.push(`**${i18n.t("hypothesis:digest.scores")}:** ${scores.join(", ")}`, "");
    }
    out.push("---", "");
  }
  return out.join("\n");
}
