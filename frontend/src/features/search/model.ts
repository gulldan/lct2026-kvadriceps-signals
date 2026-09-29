import type { AnswerChip, SmartAnswer, SmartAnswerSource } from "@/shared/types";
import type { ApiSource } from "@/features/search/api";
import { dedupeSources } from "@/features/document";

const MARKER = /\[\s*(S\d+(?:\s*,\s*S\d+)*)\s*\]/g;

/** Номер сноски — номер карточки источника: цитаты одного документа делят номер, но не фрагмент. */
export function buildSmartAnswer(content: string, raw: ApiSource[]): SmartAnswer | null {
  const text = content.trim();
  if (!text) return null;
  const citedByDoc = new Map<string, ApiSource>();
  for (const match of text.matchAll(MARKER)) {
    for (const marker of (match[1] ?? "").split(",")) {
      const source = raw[Number(marker.trim().slice(1)) - 1];
      if (source && !citedByDoc.has(source.document_id)) citedByDoc.set(source.document_id, source);
    }
  }
  // Номер и фрагмент карточки задаёт первая цитата, а не позиция документа в поисковой выдаче.
  const scored = [
    ...[...citedByDoc.values()].flatMap((source) => dedupeSources([source])),
    ...dedupeSources(raw).filter((source) => !citedByDoc.has(source.doc.id)),
  ];
  const sources: SmartAnswerSource[] = scored.map((s) => ({
    doc: s.doc,
    fragmentId: s.fragmentId,
    relevance: s.relevance,
    page: s.page,
    section: s.section,
  }));
  const numberByDoc = new Map<string, number>(sources.map((s, i) => [s.doc.id, i + 1]));

  const chipFor = (marker: string): AnswerChip | null => {
    const n = Number(marker.slice(1));
    const src = raw[n - 1];
    if (!src) return null;
    const label = numberByDoc.get(src.document_id);
    if (label === undefined) return null;
    return { label, docId: src.document_id, fragmentId: src.chunk_id };
  };

  const chips: AnswerChip[] = [];
  let markdown = "";
  let last = 0;
  // Дедупликация внутри «пачки» подряд идущих маркеров: [S1][S1, S2] → 1, 2.
  let runFragments = new Set<string>();
  for (const m of text.matchAll(MARKER)) {
    const before = text
      .slice(last, m.index)
      .replace(/[ \t]+([,.;:!?])/g, "$1")
      .replace(/[ \t]+$/g, "");
    if (before !== "") runFragments = new Set();
    markdown += before;
    // Группа 1 обязательна в MARKER; «??» — только для типов.
    for (const part of (m[1] ?? "").split(",")) {
      const chip = chipFor(part.trim());
      if (!chip) continue;
      const fragmentKey = JSON.stringify([chip.docId, chip.fragmentId]);
      if (runFragments.has(fragmentKey)) continue;
      runFragments.add(fragmentKey);
      markdown += `[${chip.label}](#cite-${chips.length})`;
      chips.push(chip);
    }
    last = (m.index ?? 0) + m[0].length;
  }
  markdown += text.slice(last).replace(/[ \t]+([,.;:!?])/g, "$1");

  const plain = text
    .replace(MARKER, "")
    .replace(/[ \t]+([,.;:!?])/g, "$1")
    .trim();
  return { markdown, plain, chips, sources };
}
