import type { ApiEvidence, ApiHypothesis } from "@/features/hypothesis/api";
import { verdictMeta } from "@/features/hypothesis/board/model";
import { pct } from "@/features/hypothesis/model";
import type { GenerationInfo } from "@/features/hypothesis/provenance";
import type { ReportBlock } from "@/shared/report/blocks";
import { formatDateTime } from "@/shared/lib/format";
import { trimSnippet } from "./reportData";
import { t } from "./reportText";

const QUOTES_FOR_MAX = 6;

function pagesLabel(ev: ApiEvidence): string {
  if (!ev.page_start) return "";
  const end = ev.page_end && ev.page_end !== ev.page_start ? `–${ev.page_end}` : "";
  return t("hypothesis:hreport.pages", { pages: `${ev.page_start}${end}` });
}

export interface CitationRegistry {
  refOfDoc: Map<string, number>;
  entries: { n: number; text: string; url?: string }[];
}

export function buildCitations(h: ApiHypothesis, gen: GenerationInfo): CitationRegistry {
  const refOfDoc = new Map<string, number>();
  const entries: { n: number; text: string; url?: string }[] = [];

  const docs = new Map<string, ApiEvidence[]>();
  for (const ev of h.evidence) {
    const key = ev.document_id || ev.filename;
    if (!key) continue;
    const list = docs.get(key);
    if (list) {
      list.push(ev);
    } else {
      docs.set(key, [ev]);
    }
  }
  for (const [key, group] of docs) {
    const n = entries.length + 1;
    refOfDoc.set(key, n);
    const first = group[0];
    if (!first) continue;
    const sections = [
      ...new Set(group.map((e) => (e.section_heading ?? "").trim()).filter(Boolean)),
    ].slice(0, 3);
    const pageSpans = [...new Set(group.map((e) => pagesLabel(e)).filter(Boolean))].slice(0, 4);
    const parts = [t("hypothesis:hreport.internalDoc", { filename: first.filename })];
    if (sections.length > 0) {
      parts.push(t("hypothesis:hreport.internalSections", { sections: sections.join("; ") }));
    }
    if (pageSpans.length > 0) parts.push(pageSpans.join("; "));
    entries.push({ n, text: parts.join(" — ") });
  }

  for (const w of gen.externalWorks) {
    const n = entries.length + 1;
    refOfDoc.set(`ext:${w.doi ?? w.title}`, n);
    const venue = w.venue ? ` // ${w.venue}` : "";
    const year = w.year ? `. — ${w.year}` : "";
    const doi = w.doi ? `. — DOI: ${w.doi}` : w.source ? `. — ${w.source}` : "";
    entries.push({
      n,
      text: `${w.title}${venue}${year}${doi}`,
      url: w.doi ? `https://doi.org/${w.doi}` : undefined,
    });
  }

  return { refOfDoc, entries };
}

export function evidenceCounts(h: ApiHypothesis): {
  forCount: number;
  againstCount: number;
  documentCount: number;
} {
  const check = h.assessment.check;
  const evFor = h.evidence.filter((e) => e.stance === "supports").length;
  const evAgainst = h.evidence.filter((e) => e.stance === "contradicts").length;
  return {
    documentCount:
      h.evidence_document_count ?? new Set(h.evidence.map((e) => e.document_id || e.filename)).size,
    forCount: check?.supporting?.length ?? evFor,
    againstCount: check?.contradicting?.length ?? evAgainst,
  };
}

export function evidenceBlocks(
  h: ApiHypothesis,
  gen: GenerationInfo,
  citations: CitationRegistry,
): ReportBlock[] {
  const a = h.assessment;
  const verdict = verdictMeta(h);
  const { forCount, againstCount, documentCount } = evidenceCounts(h);
  const blocks: ReportBlock[] = [];
  const refOf = (ev: ApiEvidence): number | undefined =>
    citations.refOfDoc.get(ev.document_id || ev.filename);
  const extRef = (key: string): number | undefined => citations.refOfDoc.get(`ext:${key}`);

  const check = a.check;
  if (!check && h.evidence.length === 0) {
    blocks.push({ kind: "paragraph", text: t("hypothesis:hreport.noEvidence") });
  } else {
    if (check?.verdict && verdict) {
      blocks.push({
        kind: "paragraph",
        text: `${verdict.label}${
          check.confidence === undefined
            ? ""
            : ` (${t("hypothesis:hreport.checkConfidence", { value: pct(check.confidence) })})`
        }`,
        bold: true,
      });
      if (check.rationale) blocks.push({ kind: "paragraph", text: check.rationale });
      const meta = [
        check.model ? t("hypothesis:hreport.checkModel", { model: check.model }) : "",
        check.checked_at
          ? t("hypothesis:hreport.checkedAt", { date: formatDateTime(check.checked_at) })
          : "",
      ]
        .filter(Boolean)
        .join(" · ");
      if (meta) blocks.push({ kind: "paragraph", text: meta, muted: true });
    } else {
      blocks.push({ kind: "paragraph", text: t("hypothesis:hreport.checkPending"), muted: true });
    }
    const contextCount = h.evidence.filter((e) => e.stance === "context").length;
    blocks.push({
      kind: "table",
      title: t("hypothesis:hreport.balanceTableTitle"),
      head: [
        t("hypothesis:hreport.b.total"),
        t("hypothesis:hreport.b.docs"),
        t("hypothesis:hreport.b.for"),
        t("hypothesis:hreport.b.against"),
        t("hypothesis:hreport.b.context"),
      ],
      rows: [
        [
          String(h.evidence_count ?? h.evidence.length),
          String(documentCount),
          String(forCount),
          String(againstCount),
          String(contextCount),
        ],
      ],
    });

    const quoteBlock = (ev: ApiEvidence): ReportBlock => {
      const n = refOf(ev);
      const src = [
        n === undefined ? "" : `[${n}]`,
        ev.filename,
        ev.section_heading ?? "",
        pagesLabel(ev),
      ]
        .filter(Boolean)
        .join(", ");
      const note = [
        ev.relation ?? "",
        ev.numeric_signals && ev.numeric_signals.length > 0
          ? `(${ev.numeric_signals.join("; ")})`
          : "",
      ]
        .filter(Boolean)
        .join(" ");
      return { kind: "quote", source: src, text: trimSnippet(ev.snippet), note: note || undefined };
    };

    const supports = h.evidence
      .filter((e) => e.stance === "supports")
      .toSorted((x, y) => (y.score ?? 0) - (x.score ?? 0));
    if (supports.length > 0) {
      blocks.push({ kind: "subheading", text: t("hypothesis:hreport.forSub") });
      for (const ev of supports.slice(0, QUOTES_FOR_MAX)) blocks.push(quoteBlock(ev));
      if (supports.length > QUOTES_FOR_MAX) {
        blocks.push({
          kind: "paragraph",
          text: t("hypothesis:hreport.moreQuotes", { count: supports.length - QUOTES_FOR_MAX }),
          muted: true,
        });
      }
    }
    blocks.push({ kind: "subheading", text: t("hypothesis:hreport.againstSub") });
    const contradicts = h.evidence.filter((e) => e.stance === "contradicts");
    if (contradicts.length > 0) {
      for (const ev of contradicts) blocks.push(quoteBlock(ev));
    } else {
      blocks.push({ kind: "paragraph", text: t("hypothesis:hreport.noAgainst") });
    }
    if (contextCount > 0) {
      blocks.push({
        kind: "paragraph",
        text: t("hypothesis:hreport.contextCount", { count: contextCount }),
        muted: true,
      });
    }
    if (gen.externalWorks.length > 0) {
      blocks.push({ kind: "subheading", text: t("hypothesis:hreport.externalSub") });
      blocks.push({
        kind: "list",
        items: gen.externalWorks.map((w) => {
          const n = extRef(w.doi ?? w.title);
          return `${n === undefined ? "" : `[${n}] `}${w.title}${w.venue ? `, ${w.venue}` : ""}${
            w.year ? ` (${w.year})` : ""
          }`;
        }),
      });
    }
  }

  return blocks;
}
