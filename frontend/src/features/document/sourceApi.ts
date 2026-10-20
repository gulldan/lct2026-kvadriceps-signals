import { request } from "@/shared/api/client";

interface SourceDate {
  value: string;
  precision: "year" | "month" | "day";
  basis: string;
}

export interface SourceMetadata {
  version: number;
  genre?: "investment_thesis" | "investor_commentary";
  text_status?:
    | "metadata_only"
    | "article_text"
    | "representative_claim"
    | "title_only"
    | "title_and_abstract"
    | "pdf"
    | "repository_metadata_only"
    | "pinned_readme";
  source_kind:
    | "paper"
    | "patent"
    | "analysis"
    | "company_material"
    | "software_repository"
    | "hypothesis";
  provider: string;
  native_id: string;
  canonical_id: string;
  url: string;
  revision: string;
  content_hash: string;
  hash_algorithm: string;
  publication: SourceDate | null;
  priority: SourceDate | null;
  filing: SourceDate | null;
  observed_at: string;
  available_at: string | null;
  available_basis?: string;
  access_scope: "public" | "shared" | "owner";
  arxiv: null | {
    base_id: string;
    version: number;
    first_published: string;
    version_updated: string;
    doi?: string;
    journal_ref?: string;
  };
  github: null | {
    repository_id: number;
    commit_sha: string;
    blob_sha?: string;
    readme_path?: string;
    fork: boolean;
    root_id: number | null;
    archived: boolean;
    license?: string;
    created_at: string;
    updated_at: string;
    pushed_at: string | null;
    release_id: number | null;
    released_at: string | null;
    stars: number | null;
    forks: number | null;
  };
}

interface SourceFragment {
  id: string;
  index: number;
  text: string;
  char_start: number;
  char_end: number;
  page_start: number;
  page_end: number;
  locator_kind: string;
  section: string;
}

export interface SourceEvidence {
  document_id: string;
  snapshot_id: string;
  title: string;
  filename: string;
  mime_type: string;
  source: SourceMetadata;
  text_hash: string;
  parser_revision: string;
  pipeline_revision: string;
  parser_metadata: Record<string, string>;
  chunks: SourceFragment[];
}

export interface SourceAnchor {
  start: number;
  end: number;
  textHash: string;
}

export function sourcePath(
  id: string,
  snapshot?: string,
  fragment?: string,
  context?: { anchor?: SourceAnchor; analysis?: string; returnTo?: string },
): string {
  const params = new URLSearchParams();
  if (snapshot) params.set("snapshot", snapshot);
  if (fragment) params.set("fragment", fragment);
  if (context?.anchor) {
    params.set("start", String(context.anchor.start));
    params.set("end", String(context.anchor.end));
    params.set("textHash", context.anchor.textHash);
  }
  if (context?.analysis) params.set("analysis", context.analysis);
  if (context?.returnTo) params.set("return", context.returnTo);
  return `/sources/${encodeURIComponent(id)}${params.size ? `?${params}` : ""}`;
}

export function sourceReturnPath(returnTo: string, analysis = ""): string {
  return /^\/(?:operator\/documents|search|kpi|hypotheses|trends|(?:technologies|hypotheses|directions|clusters)\/[\w-]+)(?:\?[^#]*)?(?:#[\w-]+)?$/.test(
    returnTo,
  ) || /^\/trends\/[\w-]+\/(?:topics\/[\w-]+|method)(?:\?[^#]*)?(?:#[\w-]+)?$/.test(returnTo)
    ? returnTo
    : analysis
      ? `/trends?run=${encodeURIComponent(analysis)}`
      : "/operator/documents";
}

/** Смещения — в кодовых точках Unicode, как в индексе источников на Python и Go. */
export function locateSourceQuote(evidence: SourceEvidence, anchor: SourceAnchor) {
  if (
    anchor.textHash !== evidence.text_hash ||
    !Number.isSafeInteger(anchor.start) ||
    !Number.isSafeInteger(anchor.end) ||
    anchor.start < 0 ||
    anchor.end <= anchor.start ||
    anchor.end - anchor.start > 16000
  )
    return null;
  let position = anchor.start;
  let quote = "";
  let fragment = "";
  for (const chunk of evidence.chunks.toSorted((a, b) => a.char_start - b.char_start)) {
    if (chunk.char_end <= position) continue;
    if (chunk.char_start > position) return null;
    const chars = Array.from(chunk.text);
    if (chars.length !== chunk.char_end - chunk.char_start) return null;
    if (!fragment) fragment = chunk.id;
    const end = Math.min(anchor.end, chunk.char_end);
    quote += chars.slice(position - chunk.char_start, end - chunk.char_start).join("");
    position = end;
    if (position === anchor.end) return { quote, fragment };
  }
  return null;
}

export function getSourceEvidence(id: string, snapshot?: string, signal?: AbortSignal) {
  const query = snapshot ? `?snapshot=${encodeURIComponent(snapshot)}` : "";
  return request<SourceEvidence>(`/documents/${encodeURIComponent(id)}/source${query}`, { signal });
}

/** Ссылка на GitHub закреплена на том же коммите, что и сохранённый README. */
export function sourceContentURL(source: SourceMetadata): string {
  const url = new URL(source.url);
  if (source.github && url.hostname === "github.com") {
    const repo = url.pathname.split("/").filter(Boolean).slice(0, 2).join("/");
    const readme = source.github.readme_path;
    const suffix = readme ? "/" + readme.split("/").map(encodeURIComponent).join("/") : "";
    url.pathname = `/${repo}/${readme ? "blob" : "commit"}/${source.github.commit_sha}${suffix}`;
    url.search = "";
    url.hash = "";
  }
  return url.href;
}
