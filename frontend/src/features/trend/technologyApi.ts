import { postJSON, request } from "@/shared/api/client";

type MentionRole = "primary" | "component" | "background" | "comparison" | (string & {});
type ExtractionStatus =
  | "complete"
  | "partial"
  | "needs_review"
  | "unresolved_mention"
  | (string & {});
type CoverageStatus =
  | "complete"
  | "partial"
  | "no_technology"
  | "failed"
  | "not_extracted"
  | "unsupported_language"
  | (string & {});

interface QuoteLocator {
  section: string;
  page?: number;
  start_char: number;
  end_char: number;
  element_path?: string;
  snapshot_sha256?: string;
}

interface Quote {
  evidence_id: string;
  locator: QuoteLocator;
  quote: string;
}

export interface Mention {
  mention_id: string;
  technology_id: string;
  name: string;
  description?: string;
  role: MentionRole;
  problem?: string;
  mechanism?: string;
  inputs?: string[];
  outputs?: string[];
  extraction_status: ExtractionStatus;
  source_revision_id?: string;
  source_title?: string;
  source_published_at?: string;
  evidence: Quote[];
  field_evidence?: Record<string, string[]>;
}

interface Coverage {
  status: CoverageStatus;
  reason_code?: string;
  processed_sections: number;
  total_sections: number;
  extracted_at?: string;
}

export interface RevisionTechnologies {
  source_revision_id: string;
  extractor_version: string;
  schema_version: string;
  coverage: Coverage;
  technologies: Mention[];
}

interface Alias {
  value: string;
  language: string;
  basis: string;
}

export interface Technology {
  technology_id: string;
  canonical_name: string;
  name_kind: "term" | "descriptive" | (string & {});
  description: string;
  validation_status: "proposed" | "verified" | "needs_review" | (string & {});
  method_version: string;
  aliases: Alias[];
  mentions: Mention[];
  readable_revisions: number;
  restricted_revisions: number;
}

type ReviewOperation =
  | "rename"
  | "describe"
  | "confirm"
  | "set_role"
  | "set_dependency"
  | "merge"
  | "split"
  | "revert";

export interface Review {
  id: string;
  technology_id: string;
  author_id: string;
  operation: ReviewOperation;
  reason: string;
  previous_value: Record<string, unknown>;
  new_value: Record<string, unknown>;
  reverts_id?: string;
  created_at: string;
}

export interface ReviewInput {
  operation: ReviewOperation;
  reason: string;
  previous_value: Record<string, unknown>;
  new_value: Record<string, unknown>;
  reverts_id?: string;
}

/** Значение отдельно от статуса и причин, чтобы «не измерено» никогда не читалось как ноль. */
interface MetricInterval {
  low: number;
  high: number;
  level?: number;
  method?: string;
}

export interface MetricEnvelope {
  value: number | null;
  unit?: string;
  status: string;
  reason_codes?: string[];
  window?: { from?: string; to?: string } | string;
  numerator?: number | null;
  denominator?: number | null;
  sample_size?: number | null;
  coverage_refs?: string[];
  method_version?: string;
  computed_at?: string;
  interval?: MetricInterval | null;
}

export interface SensitivityCheck {
  check: string;
  sign_changed?: boolean | null;
  stage_changed?: boolean | null;
  position_changed?: boolean | null;
  note?: string;
}

export interface TopicMetrics {
  analysis_id: string;
  analysis_run_id: string;
  schema_version: string;
  method_hash: string;
  topic: string;
  title: string;
  history_mode: string;
  cutoff: string;
  metrics: Record<string, unknown>;
  limitations: string[];
  sensitivity?: SensitivityCheck[];
}

export function isMetricEnvelope(value: unknown): value is MetricEnvelope {
  return (
    typeof value === "object" &&
    value !== null &&
    "status" in value &&
    "value" in value &&
    typeof (value as MetricEnvelope).status === "string"
  );
}

export interface TechnologyBrief {
  technology_id: string;
  title: string;
  advantage: string;
  problem: string;
  hypothesis: string;
  grounds: string;
  method: string;
  criterion: string;
  model: string;
  basis: string;
  sources: number;
  written_at: string;
}

export function getTechnology(id: string, signal?: AbortSignal) {
  return request<Technology>(`/technologies/${encodeURIComponent(id)}`, { signal });
}

export function getTechnologyBrief(id: string, signal?: AbortSignal) {
  return request<TechnologyBrief>(`/technologies/${encodeURIComponent(id)}/brief`, { signal });
}

export function writeTechnologyBrief(id: string) {
  return postJSON<TechnologyBrief>(`/technologies/${encodeURIComponent(id)}/brief`, {});
}

export function getDocumentTechnologies(documentID: string, signal?: AbortSignal) {
  return request<RevisionTechnologies>(
    `/documents/${encodeURIComponent(documentID)}/technologies`,
    { signal },
  );
}

export function getReviews(technologyID: string, signal?: AbortSignal) {
  return request<{ technology_id: string; reviews: Review[] }>(
    `/technologies/${encodeURIComponent(technologyID)}/reviews`,
    { signal },
  );
}

export function submitReview(technologyID: string, input: ReviewInput) {
  return postJSON<Review>(`/technologies/${encodeURIComponent(technologyID)}/reviews`, input);
}

export function getTopicMetrics(run: string, topic: string, signal?: AbortSignal) {
  return request<TopicMetrics>(
    `/trends/analyses/${encodeURIComponent(run)}/topics/${encodeURIComponent(topic)}/metrics`,
    { signal },
  );
}

export function technologyPath(id: string, returnTo?: string) {
  const params = new URLSearchParams();
  if (returnTo) params.set("return", returnTo);
  return `/technologies/${encodeURIComponent(id)}${params.size ? `?${params}` : ""}`;
}
