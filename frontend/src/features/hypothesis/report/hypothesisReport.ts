import { formatItcIndex } from "@/features/hypothesis/itcProfile";
import { type ApiHypothesis, type ApiRevision } from "@/features/hypothesis/api";
import { verdictMeta } from "@/features/hypothesis/board/model";
import {
  displayHypothesisTitle,
  pct,
  priorityScore,
  revisionActionLabel,
  schemaMissingLabels,
  statusMeta,
  visibleMeta,
} from "@/features/hypothesis/model";
import { parseGeneration, type GenerationInfo } from "@/features/hypothesis/provenance";
import { radarReportBlocks } from "@/features/hypothesis/radarProvenance";
import {
  firstSentence,
  hypothesisCard,
  kpiTargetLabel,
  plainText,
} from "@/features/hypothesis/report/reportData";
import { type ApiKPI } from "@/features/kpi";
import type { ReportBlock, ReportDoc } from "@/shared/report/blocks";
import { formatDate, formatDateShort, formatDateTime } from "@/shared/lib/format";

import { assessmentBlocks } from "./reportAssessment";
import { buildCitations, evidenceBlocks, evidenceCounts } from "./reportEvidence";
import { verificationBlocks } from "./reportVerification";
import { t, riskLevel } from "./reportText";

export interface HypothesisReportInput {
  h: ApiHypothesis;
  kpi?: ApiKPI;
  clusterTitle?: string;
  ownerName?: string | null;
  revisions: ApiRevision[];
}

export type HypothesisReportDoc = ReportDoc;

const SERVICE_TAGS = new Set([
  "auto_bridge",
  "auto_cluster",
  "discovery",
  "graph_direction",
  "on_demand",
  "fallback_evidence",
]);
const COMPETITORS_MAX = 5;
const REVISIONS_MAX = 5;

function joinSentences(parts: string[]): string {
  return parts
    .map((p) => p.trim())
    .filter(Boolean)
    .map((p) => (/[.!?…]$/.test(p) ? p : `${p}.`))
    .join(" ");
}

function generationKindLabel(gen: GenerationInfo): string {
  switch (gen.kind) {
    case "auto_bridge": {
      const themes = gen.themeA && gen.themeB ? ` (${gen.themeA} × ${gen.themeB})` : "";
      return t("hypothesis:hreport.genKindBridge") + themes;
    }
    case "auto_cluster":
      return (
        t("hypothesis:hreport.genKindCluster") + (gen.clusterLabel ? ` (${gen.clusterLabel})` : "")
      );
    case "radar":
      return t("hypothesis:radar.title");
    case "on_demand":
      return t("hypothesis:hreport.genKindOnDemand");
    case "fallback":
      return t("hypothesis:hreport.genKindFallback");
    default:
      return t("hypothesis:hreport.genKindUnknown");
  }
}

function recommendation(h: ApiHypothesis): string {
  const verdict = h.assessment.check?.verdict;
  if (h.status === "approved" || verdict === "supported") {
    return t("hypothesis:hreport.recApprove");
  }
  if (h.status === "rejected" || verdict === "refuted") return t("hypothesis:hreport.recReject");
  if (h.status === "under_review" || verdict === "mixed" || verdict === "insufficient") {
    const missing = schemaMissingLabels(h.assessment.schema?.missing);
    return missing.length > 0
      ? t("hypothesis:hreport.recReviewMissing", { missing: missing.join(", ") })
      : t("hypothesis:hreport.recReview");
  }
  return t("hypothesis:hreport.recDecide");
}

function targetEffect(h: ApiHypothesis, kpi?: ApiKPI): string {
  const targets = (h.detail.quantitative_parameters ?? []).filter(
    (p) => (p.role ?? "").toLowerCase() === "target" && p.name,
  );
  if (targets.length > 0) {
    return targets
      .map((p) => `${p.name}: ${p.value ?? "—"}${p.unit ? ` ${p.unit}` : ""}`)
      .join("; ");
  }
  return kpi ? `${kpi.title}: ${kpiTargetLabel(kpi)}` : "";
}

export function buildHypothesisReport(input: HypothesisReportInput): HypothesisReportDoc {
  const { h, kpi, clusterTitle, ownerName, revisions } = input;
  const a = h.assessment;
  const d = h.detail;
  const gen = parseGeneration(h);
  const title = displayHypothesisTitle(h);
  const verdict = verdictMeta(h);
  const { forCount, againstCount, documentCount } = evidenceCounts(h);
  const citations = buildCitations(h, gen);
  const blocks: ReportBlock[] = [];
  const id8 = h.id.slice(0, 8);

  const provenanceLines = [
    gen.kind === "radar"
      ? t("hypothesis:radar.transferred")
      : t("hypothesis:hreport.generatedBy", {
          model: gen.model ?? "LLM",
          kind: generationKindLabel(gen),
          date: formatDateShort(h.created_at),
        }),
  ];
  if (ownerName) provenanceLines.push(t("hypothesis:hreport.ownerLine", { name: ownerName }));
  provenanceLines.push(
    ["approved", "rejected", "under_review"].includes(h.status)
      ? t("hypothesis:hreport.expertVerified")
      : t("hypothesis:hreport.expertNotVerified"),
  );
  if (!h.measurable) provenanceLines.push(t("hypothesis:hreport.scientificNote"));

  const card = hypothesisCard(h, kpi, clusterTitle);
  blocks.push({
    kind: "doctitle",
    org: visibleMeta(h.organization),
    doctype: t("hypothesis:hreport.doctype"),
    title,
    meta: [
      id8,
      statusMeta(h.status).label,
      verdict?.label ?? "",
      formatDateShort(h.updated_at),
      t("hypothesis:hreport.generatedAt", { date: formatDateTime(new Date().toISOString()) }),
      t("hypothesis:hreport.version", { n: revisions.length + 1 }),
    ].filter(Boolean),
    lines: provenanceLines,
    facts: card.facts,
    lead: card.lead,
    about: h.statement ? t("hypothesis:report.pilotAbout") : undefined,
    text: h.statement || undefined,
    metrics: card.metrics,
  });

  const scienceTags = h.tags.filter((tag) => !SERVICE_TAGS.has(tag));
  const missing = schemaMissingLabels(a.schema?.missing);
  const passport: { label: string; value: string }[] = [
    {
      label: t("hypothesis:hreport.p.verdict"),
      value: verdict
        ? `${verdict.label} (${t("hypothesis:hreport.forAgainst", { forCount, againstCount })})`
        : "",
    },
    { label: t("hypothesis:hreport.p.novelty"), value: pct(h.novelty_score) },
    { label: t("hypothesis:hreport.p.value"), value: pct(h.value_score) },
    {
      label: t("hypothesis:hreport.p.risk"),
      value: pct(h.risk_score) + (a.risk?.level ? ` (${riskLevel(a.risk.level)})` : ""),
    },
    { label: t("hypothesis:hreport.p.confidence"), value: pct(h.confidence_score) },
    {
      label: t("hypothesis:hreport.p.itc"),
      value: a.itc ? `${formatItcIndex(a.itc)}/100` : "",
    },
    {
      label: t("hypothesis:hreport.p.trl"),
      value:
        h.trl === null
          ? t("hypothesis:hreport.trlPending")
          : `TRL ${h.trl}/9${a.trl?.name ? ` — ${a.trl.name}` : ""}`,
    },
    {
      label: t("hypothesis:hreport.p.evidence"),
      value:
        h.evidence.length > 0 || forCount + againstCount > 0
          ? t("hypothesis:hreport.evidenceLine", {
              docs: documentCount,
              forCount,
              againstCount,
            })
          : "",
    },
    { label: t("hypothesis:hreport.p.specialties"), value: scienceTags.join(", ") },
    {
      label: t("hypothesis:hreport.p.completeness"),
      value: a.schema?.score
        ? pct(a.schema.score) +
          (missing.length > 0
            ? ` (${t("hypothesis:hreport.missing", { missing: missing.join(", ") })})`
            : "")
        : "",
    },
  ].filter((r) => r.value !== "" && r.value !== "—");
  blocks.push({ kind: "kv", rows: passport });
  blocks.push({
    kind: "paragraph",
    text: t("hypothesis:hreport.disclaimer"),
    muted: true,
    italic: true,
  });

  blocks.push({ kind: "heading", text: t("hypothesis:hreport.abstractTitle") });
  const objectText =
    [d.material_system, d.target_property].filter(Boolean).join("; ") || firstSentence(h.statement);
  const abstractParts = [
    t("hypothesis:hreport.a.object", { object: objectText }),
    kpi
      ? t("hypothesis:hreport.a.goal", { title: kpi.title, target: kpiTargetLabel(kpi) })
      : d.problem_addressed
        ? t("hypothesis:hreport.a.goalProblem", { problem: firstSentence(d.problem_addressed) })
        : "",
    t("hypothesis:hreport.a.data", {
      docs: documentCount,
      external: gen.externalWorks.length,
    }),
    verdict
      ? t("hypothesis:hreport.a.result", { verdict: verdict.label, forCount, againstCount })
      : t("hypothesis:hreport.a.resultPending"),
    t("hypothesis:hreport.a.recommendation", { rec: recommendation(h) }),
    targetEffect(h, kpi) ? t("hypothesis:hreport.a.effect", { effect: targetEffect(h, kpi) }) : "",
  ].filter(Boolean);
  blocks.push({ kind: "paragraph", text: joinSentences(abstractParts) });
  const keywords = [
    ...scienceTags,
    ...(d.classification?.grnti ?? []).slice(0, 3).map((g) => g.name),
  ];
  const keywordsFallback = [d.target_property, d.material_system].filter((value): value is string =>
    Boolean(value),
  );
  const kw = (keywords.length > 0 ? keywords : keywordsFallback).slice(0, 8);
  if (kw.length > 0) {
    blocks.push({
      kind: "paragraph",
      text: t("hypothesis:hreport.keywords", { words: kw.join(", ").toUpperCase() }),
    });
  }

  blocks.push({ kind: "heading", text: t("hypothesis:hreport.introTitle") });
  blocks.push({
    kind: "paragraph",
    text: plainText(d.problem_addressed || h.rationale || firstSentence(h.statement)),
  });
  if (kpi) {
    blocks.push({
      kind: "paragraph",
      text: t("hypothesis:hreport.introGoal", { title: kpi.title, target: kpiTargetLabel(kpi) }),
    });
  }
  if (d.drivers && d.drivers.length > 0) {
    blocks.push({ kind: "paragraph", text: t("hypothesis:hreport.driversLead"), bold: true });
    blocks.push({ kind: "list", items: d.drivers });
  }
  const freshness = a.topic_freshness;
  const relevance = [
    freshness?.rationale
      ? t("hypothesis:hreport.freshnessNote", {
          value: pct(freshness.score),
          rationale: freshness.rationale,
        })
      : "",
    a.novelty?.rationale ?? "",
  ]
    .filter(Boolean)
    .join(" ");
  if (relevance) blocks.push({ kind: "paragraph", text: relevance });
  blocks.push({ kind: "paragraph", text: t("hypothesis:hreport.abbreviations"), muted: true });

  let sec = 0;

  blocks.push({
    kind: "heading",
    text: `${++sec} ${t("hypothesis:hreport.sec1Title")}`,
  });
  blocks.push({ kind: "paragraph", text: plainText(h.statement) });
  if (h.rationale.trim() !== "") {
    blocks.push({ kind: "subheading", text: t("hypothesis:hreport.rationaleSub") });
    blocks.push({ kind: "paragraph", text: plainText(h.rationale) });
  }
  if (gen.constraints) {
    blocks.push({
      kind: "paragraph",
      text: t("hypothesis:hreport.constraintsLead", { constraints: gen.constraints }),
    });
  }
  const quant = (d.quantitative_parameters ?? []).filter((p) => p.name);
  if (quant.length > 0) {
    blocks.push({
      kind: "table",
      title: t("hypothesis:hreport.quantTableTitle"),
      head: [
        t("hypothesis:hreport.q.param"),
        t("hypothesis:hreport.q.value"),
        t("hypothesis:hreport.q.unit"),
        t("hypothesis:hreport.q.role"),
      ],
      rows: quant.map((p) => [
        p.name ?? "",
        p.value === undefined ? "—" : String(p.value),
        p.unit ?? "",
        p.role ?? "",
      ]),
    });
  }
  const materialRows = [
    { label: t("hypothesis:hreport.m.system"), value: d.material_system ?? "" },
    { label: t("hypothesis:hreport.m.composition"), value: d.composition_change ?? "" },
    { label: t("hypothesis:hreport.m.process"), value: d.process_change ?? "" },
    { label: t("hypothesis:hreport.m.microstructure"), value: d.microstructure_mechanism ?? "" },
    { label: t("hypothesis:hreport.m.property"), value: d.target_property ?? "" },
  ].filter((r) => r.value.trim() !== "");
  if (materialRows.length > 0) {
    blocks.push({ kind: "subheading", text: t("hypothesis:hreport.materialsSub") });
    blocks.push({ kind: "kv", rows: materialRows });
  }
  const ap = d.application_potential;
  if (ap && ((ap.objects && ap.objects.length > 0) || (ap.conditions ?? "") !== "")) {
    const text = [
      ap.objects && ap.objects.length > 0
        ? t("hypothesis:hreport.applicationObjects", {
            objects: ap.objects.join(", "),
          })
        : "",
      ap.conditions
        ? t("hypothesis:hreport.applicationConditions", { conditions: ap.conditions })
        : "",
    ]
      .filter(Boolean)
      .join(" ");
    blocks.push({ kind: "paragraph", text });
  }
  const chain = (d.causal_chain ?? []).filter((s) => s.stage || s.change);
  if (chain.length > 0) {
    blocks.push({ kind: "subheading", text: `${sec}.1 ${t("hypothesis:hreport.mechanismSub")}` });
    blocks.push({
      kind: "table",
      title: t("hypothesis:hreport.causalTableTitle"),
      head: [t("hypothesis:hreport.c.stage"), t("hypothesis:hreport.c.change")],
      rows: chain.map((s) => [s.stage ?? "", s.change ?? ""]),
    });
  }

  blocks.push({ kind: "heading", text: `${++sec} ${t("hypothesis:hreport.sec2Title")}` });
  blocks.push(...assessmentBlocks(h));

  blocks.push({ kind: "heading", text: `${++sec} ${t("hypothesis:hreport.sec3Title")}` });
  blocks.push(...evidenceBlocks(h, gen, citations));

  const verification = verificationBlocks(d);
  if (verification !== null) {
    blocks.push(
      { kind: "heading", text: `${++sec} ${t("hypothesis:hreport.sec4Title")}` },
      ...verification,
    );
  }

  const competitors = d.competitors;
  const constraintsList = ap?.constraints ?? [];
  if ((competitors?.items?.length ?? 0) > 0 || competitors?.summary || constraintsList.length > 0) {
    blocks.push({ kind: "heading", text: `${++sec} ${t("hypothesis:hreport.sec5Title")}` });
    if (competitors?.summary) blocks.push({ kind: "paragraph", text: competitors.summary });
    if ((competitors?.items?.length ?? 0) > 0) {
      blocks.push({
        kind: "table",
        title: t("hypothesis:hreport.competitorsTableTitle"),
        head: [
          t("hypothesis:hreport.co.name"),
          t("hypothesis:hreport.co.approach"),
          t("hypothesis:hreport.co.maturity"),
          t("hypothesis:hreport.co.source"),
        ],
        rows: (competitors?.items ?? [])
          .slice(0, COMPETITORS_MAX)
          .map((c) => [
            c.name ?? "",
            [c.approach, (c.strengths ?? []).join("; "), (c.weaknesses ?? []).join("; ")]
              .filter(Boolean)
              .join(". "),
            c.maturity ?? "",
            c.source ?? "",
          ]),
      });
      const compMeta = [
        competitors?.model ? t("hypothesis:hreport.planModel", { model: competitors.model }) : "",
        competitors?.analyzed_at ? formatDate(competitors.analyzed_at) : "",
      ]
        .filter(Boolean)
        .join(", ");
      if (compMeta) blocks.push({ kind: "paragraph", text: compMeta, muted: true });
    }
    if (constraintsList.length > 0) {
      blocks.push({ kind: "paragraph", text: t("hypothesis:hreport.limitationsLead"), bold: true });
      blocks.push({ kind: "list", items: constraintsList });
    }
  }

  blocks.push({ kind: "heading", text: t("hypothesis:hreport.conclusionTitle") });
  const conclusions: string[] = [];
  conclusions.push(
    verdict
      ? t("hypothesis:hreport.conclVerdict", {
          verdict: verdict.label,
          forCount,
          againstCount,
        })
      : t("hypothesis:hreport.conclVerdictPending"),
  );
  if (h.trl !== null || a.itc) {
    const trlPart = h.trl === null ? "—" : `${h.trl}${a.trl?.name ? ` — ${a.trl.name}` : ""}`;
    conclusions.push(
      a.itc
        ? t("hypothesis:hreport.conclReadiness", {
            trl: trlPart,
            itc: `${formatItcIndex(a.itc)}/100`,
          })
        : t("hypothesis:hreport.conclReadinessTrlOnly", { trl: trlPart }),
    );
  }
  const topFactor = (a.ranking?.factors ?? []).toSorted(
    (x, y) => y.contribution - x.contribution,
  )[0];
  if (priorityScore(h) !== null) {
    conclusions.push(
      t("hypothesis:hreport.conclPriority", {
        score: priorityScore(h),
        factor: topFactor ? `${topFactor.label} (+${topFactor.contribution.toFixed(2)})` : "—",
      }),
    );
  }
  const mainRisk = a.risk?.factors?.[0] ?? d.experiment_plan?.risks?.[0];
  if (mainRisk) {
    conclusions.push(t("hypothesis:hreport.conclRisk", { risk: mainRisk }));
  }
  if (a.learning_priority?.score !== undefined) {
    conclusions.push(
      t("hypothesis:hreport.conclLearning", { score: pct(a.learning_priority.score) }),
    );
  }
  blocks.push({ kind: "list", items: conclusions, ordered: true });
  blocks.push({
    kind: "paragraph",
    text: t("hypothesis:hreport.recommendationLead", { rec: recommendation(h) }),
    bold: true,
  });
  if (targetEffect(h, kpi)) {
    blocks.push({
      kind: "paragraph",
      text: t("hypothesis:hreport.effectLine", { effect: targetEffect(h, kpi) }),
    });
  }

  if (citations.entries.length > 0) {
    blocks.push({ kind: "heading", text: t("hypothesis:hreport.referencesTitle") });
    blocks.push({ kind: "references", items: citations.entries });
  }

  if (h.detail.radar) {
    blocks.push(
      { kind: "heading", text: t("hypothesis:radar.title") },
      ...radarReportBlocks(h.detail.radar),
    );
  }

  const genRows = [
    { label: t("hypothesis:hreport.g.kind"), value: generationKindLabel(gen) },
    {
      label: t("hypothesis:hreport.g.model"),
      value: [gen.model, gen.promptVersion].filter(Boolean).join(", "),
    },
    {
      label: t("hypothesis:hreport.g.mediators"),
      value: gen.mediators.length > 0 ? String(gen.mediators.length) : "",
    },
    {
      label: t("hypothesis:hreport.g.scores"),
      value: gen.scores
        ? [
            gen.scores.composite !== undefined ? `composite ${pct(gen.scores.composite)}` : "",
            gen.scores.convergence !== undefined
              ? `convergence ${pct(gen.scores.convergence)}`
              : "",
            gen.scores.affinity !== undefined ? `affinity ${pct(gen.scores.affinity)}` : "",
          ]
            .filter(Boolean)
            .join(" · ")
        : "",
    },
    {
      label: t("hypothesis:hreport.g.noveltyGate"),
      value: gen.gates?.novelty
        ? [
            gen.gates.novelty.topSim !== undefined
              ? `top-sim ${pct(gen.gates.novelty.topSim)}`
              : "",
            gen.gates.novelty.nearestFilename
              ? t("hypothesis:hreport.g.nearest", { file: gen.gates.novelty.nearestFilename })
              : "",
          ]
            .filter(Boolean)
            .join(" · ")
        : "",
    },
    { label: t("hypothesis:hreport.g.fallback"), value: gen.fallbackReason ?? "" },
  ].filter((r) => r.value !== "");
  const recentRevisions = revisions.slice(0, REVISIONS_MAX);
  if (genRows.length > 0 || recentRevisions.length > 0) {
    blocks.push({ kind: "heading", text: t("hypothesis:hreport.appendixTitle") });
    if (genRows.length > 0) blocks.push({ kind: "kv", rows: genRows });
    if (recentRevisions.length > 0) {
      blocks.push({
        kind: "table",
        title: t("hypothesis:hreport.historyTableTitle"),
        head: [
          t("hypothesis:hreport.h.no"),
          t("hypothesis:hreport.h.date"),
          t("hypothesis:hreport.h.action"),
          t("hypothesis:hreport.h.comment"),
        ],
        rows: recentRevisions.map((r) => [
          String(r.revision_no),
          formatDateTime(r.created_at),
          revisionActionLabel(r.action),
          r.summary,
        ]),
      });
    }
    blocks.push({
      kind: "paragraph",
      text: t("hypothesis:hreport.systemLink", {
        path: `${globalThis.location?.origin ?? ""}/hypotheses/${h.id}`,
      }),
      muted: true,
    });
  }

  return {
    fileName: t("hypothesis:hreport.fileName", { id: id8 }),
    title,
    blocks,
  };
}
