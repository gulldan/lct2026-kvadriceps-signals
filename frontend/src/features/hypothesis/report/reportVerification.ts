import type { ApiHypothesis } from "@/features/hypothesis/api";
import { planEstimateLabel, successCriteriaLines } from "@/features/hypothesis/model";
import type { ReportBlock } from "@/shared/report/blocks";
import { formatDate } from "@/shared/lib/format";
import { t, riskLevel } from "./reportText";

const STAGE_ITEMS_MAX = 6;

// Пустой план сохраняет раздел в документе; отсутствие раздела обозначается отдельно.
export function verificationBlocks(d: ApiHypothesis["detail"]): ReportBlock[] | null {
  const blocks: ReportBlock[] = [];
  const plan = d.experiment_plan;
  const hasVerification =
    (d.verification?.method ?? "") !== "" ||
    (d.verification?.metrics && d.verification.metrics.length > 0) ||
    successCriteriaLines(d.verification?.success_criteria).length > 0;
  if (!hasVerification && !plan && !d.feasibility?.length) return null;
  if (d.verification?.method) {
    blocks.push({ kind: "paragraph", text: d.verification.method });
  }
  if (d.verification?.metrics && d.verification.metrics.length > 0) {
    blocks.push({
      kind: "paragraph",
      text: t("hypothesis:hreport.metricsLead"),
      bold: true,
    });
    blocks.push({ kind: "list", items: d.verification.metrics });
  }
  const baseCriteria = successCriteriaLines(d.verification?.success_criteria);
  const planCriteria = successCriteriaLines(plan?.success_criteria);
  const criteria = [...new Set([...baseCriteria, ...planCriteria])];
  if (criteria.length > 0) {
    blocks.push({ kind: "paragraph", text: t("hypothesis:hreport.criteriaLead"), bold: true });
    blocks.push({ kind: "list", items: criteria, ordered: true });
  }
  if (d.verification?.horizon) {
    blocks.push({
      kind: "paragraph",
      text: t("hypothesis:hreport.horizonLine", { horizon: d.verification.horizon }),
    });
  }
  if (plan) {
    const stages = (plan.sections ?? []).filter((s) => s.title || (s.items && s.items.length > 0));
    if (stages.length > 0) {
      blocks.push({ kind: "subheading", text: t("hypothesis:hreport.roadmapSub") });
      stages.forEach((s, i) => {
        blocks.push({
          kind: "paragraph",
          text: `${i + 1}. ${s.title ?? ""}${s.purpose ? ` — ${s.purpose}` : ""}`,
          bold: true,
        });
        if (s.items && s.items.length > 0) {
          blocks.push({ kind: "list", items: s.items.slice(0, STAGE_ITEMS_MAX) });
        }
      });
    }
    const params = (plan.process_parameters ?? []).filter((p) => p.name);
    if (params.length > 0) {
      blocks.push({
        kind: "table",
        title: t("hypothesis:hreport.processTableTitle"),
        head: [t("hypothesis:hreport.q.param"), t("hypothesis:hreport.q.range")],
        rows: params.map((p) => [p.name ?? "", p.range ?? ""]),
      });
    }
    const methodLines = [
      { label: t("hypothesis:hreport.pl.materials"), items: plan.materials },
      {
        label: t("hypothesis:hreport.pl.characterization"),
        items: [
          ...new Set([
            ...(plan.characterization_methods ?? []),
            ...(d.characterization_methods ?? []),
          ]),
        ],
      },
      {
        label: t("hypothesis:hreport.pl.tests"),
        items: [...new Set([...(plan.test_methods ?? []), ...(d.test_methods ?? [])])],
      },
      { label: t("hypothesis:hreport.pl.controls"), items: plan.controls },
    ].filter((r) => (r.items?.length ?? 0) > 0);
    if (methodLines.length > 0) {
      blocks.push({
        kind: "kv",
        rows: methodLines.map((r) => ({
          label: r.label,
          value: (r.items ?? []).join(", "),
        })),
      });
    }
    if (plan.estimated_cost || plan.estimated_time) {
      blocks.push({
        kind: "paragraph",
        text: t("hypothesis:hreport.resourcesLine", {
          cost: planEstimateLabel(plan.estimated_cost) || "—",
          time: planEstimateLabel(plan.estimated_time) || "—",
        }),
      });
    }
    const risks = [...new Set([...(plan.risks ?? []), ...(d.failure_modes ?? [])])];
    if (risks.length > 0) {
      blocks.push({ kind: "paragraph", text: t("hypothesis:hreport.risksLead"), bold: true });
      blocks.push({ kind: "list", items: risks });
    }
    const planMeta = [
      plan.model ? t("hypothesis:hreport.planModel", { model: plan.model }) : "",
      plan.planned_at ? formatDate(plan.planned_at) : "",
    ]
      .filter(Boolean)
      .join(", ");
    if (planMeta) blocks.push({ kind: "paragraph", text: planMeta, muted: true });
  }
  const feasibility = (d.feasibility ?? []).filter((f) => f.aspect);
  if (feasibility.length > 0) {
    blocks.push({
      kind: "table",
      title: t("hypothesis:hreport.feasibilityTableTitle"),
      head: [
        t("hypothesis:hreport.f.aspect"),
        t("hypothesis:hreport.f.level"),
        t("hypothesis:hreport.f.note"),
      ],
      rows: feasibility.map((f) => [
        f.aspect ?? "",
        f.level ? riskLevel(f.level) : "—",
        f.note ?? "",
      ]),
    });
  }

  return blocks;
}
