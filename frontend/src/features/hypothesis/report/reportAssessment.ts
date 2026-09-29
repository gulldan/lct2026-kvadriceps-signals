import type { ApiHypothesis } from "@/features/hypothesis/api";
import { formatItcIndex } from "@/features/hypothesis/itcProfile";
import { CONFIDENCE_FACETS, pct, priorityScore, trlMeta } from "@/features/hypothesis/model";
import type { ReportBlock } from "@/shared/report/blocks";
import { itcComponents } from "./reportData";
import { t, riskLevel } from "./reportText";

export function assessmentBlocks(h: ApiHypothesis): ReportBlock[] {
  const a = h.assessment;
  const blocks: ReportBlock[] = [];

  blocks.push({
    kind: "paragraph",
    text: t("hypothesis:hreport.disclaimer"),
    muted: true,
    italic: true,
  });
  const ranking = a.ranking;
  if (ranking && (ranking.factors?.length ?? 0) > 0) {
    blocks.push({
      kind: "table",
      title: t("hypothesis:hreport.rankingTableTitle"),
      head: [
        t("hypothesis:hreport.r.factor"),
        t("hypothesis:hreport.r.weight"),
        t("hypothesis:hreport.r.value"),
        t("hypothesis:hreport.r.contribution"),
        t("hypothesis:hreport.r.detail"),
      ],
      rows: ranking.factors.map((f) => [
        f.label,
        f.weight.toFixed(2),
        f.scored ? pct(f.value) : "—",
        f.contribution.toFixed(3),
        f.detail,
      ]),
    });
    const method = [ranking.method, ranking.formula, ranking.version].filter(Boolean).join(" · ");
    if (method) blocks.push({ kind: "paragraph", text: method, muted: true });
    if (priorityScore(h) !== null) {
      blocks.push({
        kind: "paragraph",
        text: t("hypothesis:hreport.priorityTotal", { score: priorityScore(h) }),
        bold: true,
      });
    }
  }
  const dims = [
    {
      label: t("hypothesis:hreport.p.novelty"),
      score: a.novelty?.score,
      why: a.novelty?.rationale,
    },
    {
      label: t("hypothesis:hreport.freshnessLabel"),
      score: a.topic_freshness?.score,
      why: a.topic_freshness?.rationale,
    },
    { label: t("hypothesis:hreport.p.value"), score: a.value?.score, why: a.value?.rationale },
  ];
  for (const dim of dims) {
    if (dim.score === undefined && !dim.why) continue;
    blocks.push({
      kind: "paragraph",
      text: `${dim.label}: ${pct(dim.score)}${dim.why ? ` — ${dim.why}` : ""}`,
    });
  }
  if (a.risk && (a.risk.score !== undefined || (a.risk.factors && a.risk.factors.length > 0))) {
    const lead = `${t("hypothesis:hreport.p.risk")}: ${pct(a.risk.score)}${
      a.risk.level ? ` (${riskLevel(a.risk.level)})` : ""
    }${a.risk.rationale ? ` — ${a.risk.rationale}` : ""}`;
    blocks.push({ kind: "paragraph", text: lead });
    if (a.risk.factors && a.risk.factors.length > 0) {
      blocks.push({ kind: "list", items: a.risk.factors });
    }
  }
  if (h.trl !== null || a.trl) {
    const trlLine = [
      t("hypothesis:hreport.trlLine", {
        level: h.trl ?? a.trl?.level ?? "—",
        name: a.trl?.name ?? trlMeta(h.trl).label,
      }),
      a.trl?.rationale ?? "",
      a.trl?.standard ? `(${a.trl.standard})` : "",
    ]
      .filter(Boolean)
      .join(" ");
    blocks.push({ kind: "paragraph", text: trlLine });
    const level = h.trl ?? a.trl?.level ?? 0;
    const levels = (a.trl?.levels ?? []).filter((l) => l.level <= level + 1);
    if (levels.length > 0) {
      blocks.push({
        kind: "list",
        items: levels.map(
          (l) =>
            `TRL ${l.level}: ${
              l.met ? t("hypothesis:hreport.trlMet") : t("hypothesis:hreport.trlNotMet")
            }${l.note ? ` — ${l.note}` : ""}`,
        ),
      });
    }
  }
  if (a.itc) {
    blocks.push({
      kind: "paragraph",
      text: `${t("hypothesis:hreport.p.itc")}: ${formatItcIndex(a.itc)}/100`,
    });
    const comps = itcComponents(a.itc);
    if (comps.length > 0) {
      blocks.push({
        kind: "kv",
        rows: comps.map((c) => ({
          label: c.name,
          value: `${Math.round(c.norm * 100)}${c.note ? ` — ${c.note}` : ""}`,
        })),
      });
    }
  }
  if (a.scores) {
    const rows = CONFIDENCE_FACETS.map((f) => ({
      label: f.label,
      value: a.scores?.[f.key] === undefined ? "" : pct(a.scores[f.key]),
    })).filter((r) => r.value !== "");
    if (rows.length > 0) {
      blocks.push({ kind: "subheading", text: t("hypothesis:hreport.confidenceSub") });
      blocks.push({ kind: "kv", rows });
    }
    if (a.scores.unverified) {
      blocks.push({ kind: "paragraph", text: t("hypothesis:hreport.unverifiedNote"), muted: true });
    }
  }
  if (a.learning_priority?.score !== undefined) {
    blocks.push({
      kind: "paragraph",
      text: t("hypothesis:hreport.learningLine", {
        score: pct(a.learning_priority.score),
        value: pct(a.learning_priority.value),
        uncertainty: pct(a.learning_priority.uncertainty),
      }),
    });
  }

  return blocks;
}
