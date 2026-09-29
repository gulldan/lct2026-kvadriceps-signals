import { request } from "@/shared/api/client";
import { newRequestID } from "@/shared/lib/requestID";
import type { SourceMetadata } from "@/features/document";

export interface AnalysisRequest {
  request_id: string;
  query: string;
  start: string;
  cutoff: string;
  mode: "as_of" | "retrospective_current_membership";
  collection_scope_ids?: string[];
  search_terms?: string[];
}

interface EvidenceClaim {
  id: string;
  field: "problem" | "advantage" | "example";
  work_id: string;
  document_id: string;
  snapshot_id: string;
  source_revision: string;
  text_sha256: string;
  start: number;
  end: number;
  quote: string;
  claim_type: "research_claim" | "company_claim";
}

interface BriefField {
  text: string;
  evidence_ids: string[];
}

interface PeriodMeasurement {
  period: string;
  start: string;
  end_exclusive: string;
  complete_period: boolean;
  coverage: string;
  comparable_exposure: number | null;
  comparable_scientific_works: number | null;
  normalized_activity: number | null;
  found: {
    scientific_works: number;
    independent_repositories: number;
    published_releases: number;
    patent_publications: number;
    company_materials: number;
    analyses: number;
  };
}

interface FirstMention {
  date: NonNullable<SourceMetadata["publication"]>;
  document_id: string;
  snapshot_id: string;
  source_revision: string;
  work_id: string;
}

export interface TrendCandidate {
  id: string;
  title: string;
  work_ids: string[];
  complete_for_brief: boolean;
  missing_fields: string[];
  brief: {
    problem: BriefField | null;
    advantage: BriefField | null;
    example: BriefField | null;
    description_status: "template" | "validated";
    evidence_sha256: string;
  };
  evidence: EvidenceClaim[];
  measurements: {
    earliest: FirstMention | null;
    earliest_scientific: FirstMention | null;
    history_kind: string;
    series: PeriodMeasurement[];
    stars_history: string;
    unallocated: unknown[];
    units: string[];
  };
  assessment: {
    version: string;
    eligible: boolean;
    basis?: "normalized_rates" | "query_corpus_share" | "observed_counts";
    reasons: string[];
    features: Record<string, number | null>;
    parameters: Record<string, number>;
    order: number[] | null;
  };
  relevance: {
    relevant: boolean;
    mode: string;
    semantic_similarity: number | null;
    lexical_overlap: number;
  };
}

interface AnalysisSource {
  document_id: string;
  snapshot_id: string;
  title: string;
  source: SourceMetadata;
  text_sha256: string;
}

interface CanonicalWork {
  id: string;
  kind: SourceMetadata["source_kind"];
  document_ids: string[];
  implementation_root: string | null;
}

interface ImplementationLink {
  id: string;
  paper_id: string;
  repository_id: string;
  repository_root: string | null;
  relation: string;
  official: boolean;
  evidence: {
    document_id: string;
    snapshot_id: string;
    text_sha256: string;
    char_start: number;
    char_end: number;
    quote: string;
    available_at: string;
  }[];
}

interface AnalysisResult {
  version: string;
  job_id: string;
  input_run_id: string;
  query: string;
  cutoff: string;
  method_sha256: string;
  candidates: TrendCandidate[];
  works: CanonicalWork[];
  relations: ImplementationLink[];
  ranking: { ordered_ids: string[]; completeness: string; reasons: string[] };
  search: { mode: string; applied_query: string; reason: string | null };
  limitations: string[];
}

export interface AnalysisJob {
  request: AnalysisRequest | null;
  id: string;
  input_run_id: string;
  method_sha256: string;
  result_sha256: string;
  status: "prepared" | "validated" | "published" | "failed";
  failure_code: string;
  created_at: string;
  updated_at: string;
  active: boolean;
  result: AnalysisResult | null;
  sources: AnalysisSource[];
  method: {
    version: string;
    quality_status: string;
    code_sha256: string;
    parameters: { emergence: Record<string, number>; [key: string]: unknown };
    explanation: {
      title: string;
      status: string;
      definition: string;
      counts: string;
      earliest: string;
      history: string;
      formula: string;
      order: string;
      brief: string;
      limitations: string[];
    };
  };
}

/** Недостающее поле показывается как не найденное, а не подменяется. */
export const MIN_BRIEF_FIELDS = 2;

export function briefFields(candidate: TrendCandidate): number {
  const { problem, advantage, example } = candidate.brief;
  return [problem, advantage, example].filter(Boolean).length;
}

function checkedAnalysis(job: AnalysisJob): AnalysisJob {
  if (!["prepared", "validated", "published", "failed"].includes(job.status))
    throw new Error("unknown analysis state");
  if (job.status !== "published") {
    if (job.result !== null) throw new Error("partial analysis response");
    return job;
  }
  const result = job.result;
  if (!result || result.job_id !== job.id || result.method_sha256 !== job.method_sha256)
    throw new Error("mixed analysis response");
  const ids = result.ranking.ordered_ids;
  if (ids.length > 15 || new Set(ids).size !== ids.length)
    throw new Error("invalid ranked membership");
  if (new Set(result.candidates.map((candidate) => candidate.id)).size !== result.candidates.length)
    throw new Error("duplicate candidate");
  for (const id of ids) {
    const candidate = result.candidates.find((item) => item.id === id);
    if (
      !candidate?.assessment.eligible ||
      !candidate.relevance.relevant ||
      briefFields(candidate) < MIN_BRIEF_FIELDS ||
      !candidate.evidence.length
    )
      throw new Error("incomplete ranked brief");
  }
  if ((result.ranking.completeness === "complete") !== (ids.length === 15))
    throw new Error("false completeness");
  return { ...job, sources: job.sources ?? [] };
}

export async function getAnalysis(id: string, signal?: AbortSignal) {
  const job = await request<AnalysisJob>(`/trends/analyses/${encodeURIComponent(id)}`, { signal });
  if (job.id !== id) throw new Error("wrong analysis identity");
  return checkedAnalysis(job);
}

export async function submitAnalysis(input: AnalysisRequest, signal?: AbortSignal) {
  return checkedAnalysis(
    await request<AnalysisJob>("/trends/analyses", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify(input),
      signal,
    }),
  );
}

export function analysisPath(id: string, query = "") {
  const params = new URLSearchParams({ run: id });
  if (query) params.set("q", query);
  return `/trends?${params}`;
}

export function analysisRequestParams(value: Omit<AnalysisRequest, "request_id">, previous = "") {
  const end = new Date(`${value.cutoff.slice(0, 10)}T23:59:59.999Z`);
  const params = new URLSearchParams({
    request: newRequestID(),
    q: value.query,
    start: value.start,
    cutoff: new Date(Math.min(end.getTime(), Date.now())).toISOString(),
    mode: value.mode,
  });
  if (previous) params.set("previous", previous);
  for (const scope of value.collection_scope_ids ?? []) params.append("scope", scope);
  return params;
}

export function trendPath(run: string, candidate: string, returnTo?: string) {
  const path = `/trends/${encodeURIComponent(run)}/topics/${encodeURIComponent(candidate)}`;
  return returnTo ? `${path}?${new URLSearchParams({ return: returnTo })}` : path;
}
