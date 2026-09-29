import type { ApiSource } from "@/features/search";
import { i18n } from "@/shared/i18n";
import { formatDate } from "@/shared/lib/format";
import type { DocFragment, FileType, KbDoc } from "@/shared/types";
import type { ApiChunk, ApiDocument } from "./api";

export function fileTypeOf(filename: string, mime?: string): FileType {
  // На рантайме filename может не прийти.
  const ext = (filename ?? "").toLowerCase().split(".").pop() ?? "";
  switch (ext) {
    case "pdf":
      return "pdf";
    case "doc":
    case "docx":
      return "docx";
    case "eml":
    case "msg":
      return "eml";
    case "xls":
    case "xlsx":
      return "xlsx";
    case "ppt":
    case "pptx":
      return "pptx";
    case "txt":
    case "md":
      return "txt";
    case "png":
    case "jpg":
    case "jpeg":
    case "webp":
    case "tiff":
      return "image";
    case "db":
    case "sqlite":
    case "sqlite3":
      return "db";
    default:
      break;
  }
  const mt = (mime ?? "").toLowerCase();
  if (mt.includes("pdf")) return "pdf";
  if (mt.startsWith("image/")) return "image";
  if (mt.includes("wordprocessingml") || mt.includes("msword")) return "docx";
  if (mt.includes("spreadsheetml") || mt.includes("excel")) return "xlsx";
  if (mt.includes("presentationml") || mt.includes("powerpoint")) return "pptx";
  if (mt === "message/rfc822") return "eml";
  if (mt.includes("sqlite")) return "db";
  if (mt.startsWith("text/")) return "txt";
  return "other";
}

export function titleOf(filename: string): string {
  const name = filename ?? "";
  const dot = name.lastIndexOf(".");
  return dot > 0 ? name.slice(0, dot) : name;
}

export function documentTitle(d: Pick<ApiDocument, "title" | "filename">): string {
  return d.title?.trim() || titleOf(d.filename);
}

export function publishedLabel(raw?: string): string {
  const value = raw?.trim() ?? "";
  if (!value) return "";
  const formatted = formatDate(value);
  return formatted === "—" ? value : formatted;
}

function pageLabel(s: ApiSource): string {
  const start = s.page_start ?? 0;
  if (start <= 0) return "";
  const end = s.page_end ?? 0;
  return end > start
    ? i18n.t("document:adapter.pageRangeN", { start, end })
    : i18n.t("document:adapter.pageN", { page: start });
}

export function sourceLocation(s: ApiSource): string {
  const heading = s.section_heading?.trim() ?? "";
  const page = pageLabel(s);
  const parts = [page, heading].filter((p) => p.length > 0);
  return parts.length > 0 ? parts.join(" · ") : i18n.t("document:adapter.foundFragment");
}

function fragmentOfChunk(c: ApiChunk): DocFragment {
  return {
    id: c.id,
    text: c.text,
    location: i18n.t("document:adapter.fragmentN", { index: c.index + 1 }),
  };
}

function docFromApi(d: ApiDocument): KbDoc {
  return {
    id: d.id,
    real: true,
    hasProvenance: Boolean(d.source_metadata),
    title: documentTitle(d),
    snippet: "",
    section: i18n.t("document:adapter.uploadedSection"),
    sourceId: "backend",
    sourceName: i18n.t("document:adapter.corpus"),
    updatedAt: d.updated_at,
    tags: [],
    fileType: fileTypeOf(d.filename, d.mime_type),
    fileName: d.filename,
    author: d.author?.trim() || undefined,
    publishedAt: d.published_at?.trim() || undefined,
    fragments: [],
  };
}

export function docWithChunks(d: ApiDocument, chunks: ApiChunk[], snapshot?: string): KbDoc {
  return {
    ...docFromApi(d),
    sourceSnapshot: snapshot,
    snippet: chunks[0]?.text.slice(0, 180) ?? "",
    fragments: chunks.map(fragmentOfChunk),
  };
}

function docFromSource(s: ApiSource): KbDoc {
  return {
    id: s.document_id,
    real: true,
    sourceSnapshot: s.source_index_snapshot,
    hasProvenance: Boolean(s.source_index_snapshot),
    title: titleOf(s.filename),
    snippet: s.snippet,
    section: i18n.t("document:adapter.foundInCorpus"),
    sourceId: "backend",
    sourceName: i18n.t("document:adapter.corpus"),
    updatedAt: "",
    tags: [],
    fileType: fileTypeOf(s.filename),
    fileName: s.filename,
    fragments: [
      {
        id: s.chunk_id,
        text: s.snippet,
        location: sourceLocation(s),
        page: s.page_start && s.page_start > 0 ? s.page_start : undefined,
        heading: s.section_heading?.trim() || undefined,
      },
    ],
  };
}

export interface ScoredSourceDoc {
  doc: KbDoc;
  fragmentId: string;
  /** Проценты 0–100 без нормировок; нет поля — скор не откалиброван. */
  relevance?: number;
  score: number;
  page: string;
  section: string;
}

function isBibliographySource(s: ApiSource): boolean {
  const heading = (s.section_heading ?? "").trim().toLowerCase();
  return /^(references?|bibliography|works cited|литература|список литературы)$/.test(heading);
}

/** Процент — калиброванный скор реранкера; вне шкалы 0..1 он не показывается. */
export function dedupeSources(sources: ApiSource[]): ScoredSourceDoc[] {
  const byDoc = new Map<string, ApiSource>();
  for (const s of sources) {
    const prev = byDoc.get(s.document_id);
    if (
      !prev ||
      (isBibliographySource(prev) && !isBibliographySource(s)) ||
      (isBibliographySource(prev) === isBibliographySource(s) && s.score > prev.score)
    ) {
      byDoc.set(s.document_id, s);
    }
  }
  const list = [...byDoc.values()].toSorted((a, b) => b.score - a.score);
  const nonBibliography = list.filter((s) => !isBibliographySource(s));
  const displayList = nonBibliography.length >= 3 ? nonBibliography : list;
  return displayList.map((s) => ({
    doc: docFromSource(s),
    fragmentId: s.chunk_id,
    relevance: s.score > 0 && s.score <= 1 ? Math.round(s.score * 100) : undefined,
    score: s.score,
    page: pageLabel(s),
    section: s.section_heading?.trim() ?? "",
  }));
}
