import { ApiError, postJSON, putJSON, request } from "@/shared/api/client";
import { newRequestID } from "@/shared/lib/requestID";
import { hypothesisJobErrorMessage } from "./jobError";
import type { RadarProvenance } from "./radarProvenance";

interface ScoredDim {
  score?: number;
  rationale?: string;
}

interface LearningPriority {
  score?: number;
  value?: number;
  uncertainty?: number;
  evidence_quality?: number;
}

interface RiskDim {
  score?: number;
  level?: string;
  factors?: string[];
  rationale?: string;
}

export type Verdict = "supported" | "refuted" | "mixed" | "insufficient";

interface VerifyCheck {
  verdict?: Verdict;
  confidence?: number;
  rationale?: string;
  supporting?: string[];
  contradicting?: string[];
  model?: string;
  checked_at?: string;
}

interface TrlLevel {
  level: number;
  met: boolean;
  note?: string;
}

/** value — 0..100, norm — 0..1. */
export interface ItcComponent {
  key: string;
  name: string;
  value: number;
  norm: number;
  note?: string;
  weight?: number;
  contribution?: number;
}

export interface Itc {
  /** Шкала 1..10. */
  score: number;
  band?: { label: string; note?: string } | null;
  techscore: number;
  components?: {
    SM?: ItcComponent;
    NV?: ItcComponent;
    IP?: ItcComponent;
    HR?: ItcComponent;
  } | null;
  /** Перцентили внутри корпуса; нейтральная подстановка — не наблюдение. */
  axes: { momentum: number; novelty: number; impact: number; diffusion: number };
  signals?: {
    years?: number[];
    year_min?: number | null;
    year_max?: number | null;
    pub_count?: number;
    org_count?: number;
    orgs?: string[];
    evidence_citations?: number;
    novelty_distance?: number | null;
    document_count?: number;
    probe_count?: number;
    embedded_count?: number;
    freshness_available?: boolean;
    novelty_available?: boolean;
  };
  method?: string;
  scope?: string;
  computed_at?: string;
}

export interface AssessmentScores {
  belief_score?: number;
  verdict_confidence?: number;
  evidence_quality?: number;
  unverified?: boolean;
}

export interface AssessmentSchema {
  score?: number;
  complete?: boolean;
  missing?: string[];
}

interface Assessment {
  novelty?: ScoredDim;
  topic_freshness?: ScoredDim;
  risk?: RiskDim;
  value?: ScoredDim;
  confidence?: ScoredDim;
  scores?: AssessmentScores;
  learning_priority?: LearningPriority;
  schema?: AssessmentSchema;
  itc?: Itc;
  ranking?: Ranking;
  trl?: {
    level?: number;
    name?: string;
    rationale?: string;
    method?: string;
    standard?: string;
    signals?: string[];
    levels?: TrlLevel[];
  };
  check?: VerifyCheck;
}

interface RankingFactor {
  key: string;
  label: string;
  weight: number;
  value: number;
  contribution: number;
  detail: string;
  scored: boolean;
}

export interface Ranking {
  score: number;
  factors: RankingFactor[];
  method?: string;
  formula?: string;
  version?: string;
  expert?: { status: string; multiplier: number; label?: string };
}

interface QuantParam {
  name?: string;
  value?: number;
  unit?: string;
  role?: string;
}

interface Competitor {
  name?: string;
  approach?: string;
  strengths?: string[];
  weaknesses?: string[];
  maturity?: string;
  source?: string;
}

interface TaxRef {
  code: string;
  name: string;
}

export interface HypothesisDetail {
  radar?: RadarProvenance;
  technology?: { id: string; name: string; sources?: number };
  verification?: {
    method?: string;
    metrics?: string[];
    success_criteria?: string;
    horizon?: string;
  };
  drivers?: string[];
  problem_addressed?: string;
  application_potential?: { objects?: string[]; conditions?: string; constraints?: string[] };
  quantitative_parameters?: QuantParam[];
  material_system?: string;
  composition_change?: string;
  process_change?: string;
  microstructure_mechanism?: string;
  target_property?: string;
  characterization_methods?: string[];
  test_methods?: string[];
  failure_modes?: string[];
  causal_chain?: { stage?: string; change?: string }[];
  /** Старые планы несут только прежние поля; success_criteria — строка или массив. */
  experiment_plan?: {
    experiment_type?:
      | "new_alloy"
      | "process_route"
      | "coating_corrosion"
      | "battery_material"
      | "generic"
      | string;
    sections?: { title?: string; purpose?: string; items?: string[] }[];
    materials?: string[];
    process_parameters?: { name?: string; range?: string }[];
    characterization_methods?: string[];
    test_methods?: string[];
    controls?: string[];
    success_criteria?: string | string[];
    estimated_cost?: string;
    estimated_time?: string;
    risks?: string[];
    parameters?: string[];
    methods?: string[];
    model?: string;
    planned_at?: string;
    /** Прежние поля: нужны старым планам и выгрузке дайджеста. */
    variables?: string[];
    horizon?: string;
  };
  feasibility?: { aspect?: string; level?: "low" | "medium" | "high" | ""; note?: string }[];
  competitors?: {
    summary?: string;
    items?: Competitor[];
    model?: string;
    analyzed_at?: string;
  };
  classification?: {
    research_type?: "теоретическое исследование" | "практическое";
    grnti?: TaxRef[];
    vak?: TaxRef[];
    asjc?: TaxRef[];
    model?: string;
    tagged_at?: string;
  };
}

export type HypothesisStatus =
  | "draft"
  | "generated"
  | "under_review"
  | "approved"
  | "rejected"
  | "archived";

export type EvidenceStance = "supports" | "contradicts" | "context";

export interface ApiEvidence {
  id: string;
  document_id?: string;
  chunk_id: string;
  filename: string;
  snippet: string;
  stance: EvidenceStance;
  score: number | null;
  ord: number;
  relation?: string;
  numeric_signals?: string[];
  /** Нумерация с 1; 0 или нет поля — неизвестно. */
  page_start?: number;
  page_end?: number;
  section_heading?: string;
  origin?: "input" | "knowledge" | "web";
}

export interface ApiHypothesis {
  id: string;
  version: number;
  owner_id: string;
  run_id: string;
  title: string;
  statement: string;
  rationale: string;
  method: string;
  status: HypothesisStatus;
  kpi_id?: string;
  primary_cluster_id?: string;
  trl: number | null;
  novelty_score: number | null;
  risk_score: number | null;
  value_score: number | null;
  confidence_score: number | null;
  composite_score: number | null;
  measurable: boolean;
  organization: string;
  function_area: string;
  source_type: string;
  location: string;
  tags: string[];
  assessment: Assessment;
  detail: HypothesisDetail;
  generation: Record<string, unknown>;
  created_at: string;
  updated_at: string;
  evidence_count?: number;
  evidence_document_count?: number;
  evidence_support_count?: number;
  evidence_contradict_count?: number;
  evidence: ApiEvidence[];
}

export interface ApiRevision {
  id: string;
  hypothesis_id: string;
  revision_no: number;
  editor_id: string;
  action: string;
  summary: string;
  patch: Record<string, unknown>;
  created_at: string;
}

export interface RevisionInput {
  action: string;
  summary?: string;
  editor_id?: string;
}

export interface HypothesisListParams {
  q?: string;
  queue?: string;
  status?: string;
  kpi_id?: string;
  cluster_id?: string;
  function_area?: string;
  source_type?: string;
  organization?: string;
  min_trl?: number;
  max_trl?: number;
  tags?: string;
  /** id документов через запятую. */
  document_ids?: string;
  order_by?: string;
  limit?: number;
  offset?: number;
  view?: "ref";
}

function queryString(params: HypothesisListParams = {}): string {
  const q = new URLSearchParams();
  for (const [key, value] of Object.entries(params)) {
    if (value === undefined || value === null || value === "" || value === "all") continue;
    q.set(key, String(value));
  }
  const s = q.toString();
  return s ? `?${s}` : "";
}

export function listHypotheses(params?: HypothesisListParams): Promise<ApiHypothesis[]> {
  return request<ApiHypothesis[]>(`/hypotheses${queryString(params)}`);
}

export const RADAR_BETS_KEY = "radar-bets";
export const listRadarBets = () =>
  listHypotheses({ source_type: "Технологический радар", limit: 500 });

/** Постранично: без предела сервер отдаёт только первые сто. */
export async function listAllHypotheses(params?: HypothesisListParams): Promise<ApiHypothesis[]> {
  const page = 500;
  const all: ApiHypothesis[] = [];
  for (let offset = 0; ; offset += page) {
    const items = await listHypotheses({ ...params, limit: page, offset });
    all.push(...items);
    if (items.length < page) return all;
  }
}

export interface HypothesisBoardResponse {
  items: ApiHypothesis[];
  total: number;
  limit: number;
  offset: number;
  queue_counts: Record<string, number>;
  facets: Record<string, string[]>;
}

export function getHypothesisBoard(
  params?: HypothesisListParams,
): Promise<HypothesisBoardResponse> {
  return request<HypothesisBoardResponse>(`/hypotheses/board${queryString(params)}`);
}

export function getHypothesis(id: string): Promise<ApiHypothesis> {
  return request<ApiHypothesis>(`/hypotheses/${id}`);
}

export function checkRadarBet(id: string) {
  return postJSON<{ checked: number; changed: { id: string; changes: unknown[] }[] }>(
    `/trends/live/bets/check?${new URLSearchParams({ hypothesis: id })}`,
    {},
  );
}

export type CreateHypothesisInput = Pick<ApiHypothesis, "title" | "statement"> &
  Partial<
    Pick<
      ApiHypothesis,
      | "rationale"
      | "method"
      | "status"
      | "kpi_id"
      | "measurable"
      | "source_type"
      | "generation"
      | "detail"
    >
  >;

export function createHypothesis(body: CreateHypothesisInput): Promise<ApiHypothesis> {
  return postJSON<ApiHypothesis>("/hypotheses", body);
}

/** Сервер перезаписывает гипотезу целиком, поэтому уходит полный объект. */
export function updateHypothesis(
  id: string,
  body: Partial<ApiHypothesis> & Pick<ApiHypothesis, "version"> & { revision?: RevisionInput },
): Promise<ApiHypothesis> {
  return putJSON<ApiHypothesis>(`/hypotheses/${id}`, body);
}

export function addHypothesisRevision(id: string, body: RevisionInput): Promise<ApiRevision> {
  return postJSON<ApiRevision>(`/hypotheses/${id}/revisions`, body);
}

type HypothesisJobKind =
  | "generate"
  | "verify"
  | "enrich"
  | "assess_trl"
  | "competitors"
  | "refine"
  | "tag";

type HypothesisJobStatus = "queued" | "running" | "succeeded" | "failed";

interface HypothesisJobInput {
  hypothesis_id?: string;
  kpi_id?: string;
  kpi_title?: string;
  kpi_metric?: string;
  kpi_description?: string;
  constraints?: string;
  count?: number;
  document_ids?: string[];
}

export interface ApiHypothesisJob {
  id: string;
  owner_id: string;
  kind: HypothesisJobKind;
  status: HypothesisJobStatus;
  input: HypothesisJobInput;
  result_ids: string[];
  error?: string;
  created_at: string;
  started_at?: string;
  heartbeat_at?: string;
  finished_at?: string;
}

function createHypothesisJob(
  kind: HypothesisJobKind,
  input: HypothesisJobInput,
): Promise<ApiHypothesisJob> {
  return postJSON<ApiHypothesisJob>("/hypothesis-jobs", {
    request_id: newRequestID(),
    kind,
    ...input,
  });
}

function getHypothesisJob(id: string): Promise<ApiHypothesisJob> {
  return request<ApiHypothesisJob>(`/hypothesis-jobs/${id}`);
}

export function listHypothesisJobs(limit = 30): Promise<ApiHypothesisJob[]> {
  return request<ApiHypothesisJob[]>(`/hypothesis-jobs?limit=${limit}`);
}

function sleep(ms: number): Promise<void> {
  return new Promise((resolve) => window.setTimeout(resolve, ms));
}

async function waitHypothesisJob(job: ApiHypothesisJob): Promise<ApiHypothesisJob> {
  let current = job;
  for (;;) {
    if (current.status === "succeeded") return current;
    if (current.status === "failed")
      throw new ApiError(
        current.error === "insufficient_context" ? 422 : 500,
        hypothesisJobErrorMessage(current.error),
      );
    await sleep(1500);
    current = await getHypothesisJob(job.id);
  }
}

async function runHypothesisJob(
  kind: HypothesisJobKind,
  input: HypothesisJobInput,
  onJobCreated?: (jobID: string) => void,
): Promise<ApiHypothesisJob> {
  const job = await createHypothesisJob(kind, input);
  onJobCreated?.(job.id);
  return waitHypothesisJob(job);
}

export async function analyzeCompetitors(
  id: string,
  onJobCreated?: (jobID: string) => void,
): Promise<ApiHypothesis> {
  const job = await runHypothesisJob("competitors", { hypothesis_id: id }, onJobCreated);
  return getHypothesis(job.result_ids[0] ?? id);
}

export async function refineHypothesis(
  id: string,
  onJobCreated?: (jobID: string) => void,
): Promise<ApiHypothesis> {
  const job = await runHypothesisJob("refine", { hypothesis_id: id }, onJobCreated);
  return getHypothesis(job.result_ids[0] ?? id);
}

export async function tagHypothesis(
  id: string,
  onJobCreated?: (jobID: string) => void,
): Promise<ApiHypothesis> {
  const job = await runHypothesisJob("tag", { hypothesis_id: id }, onJobCreated);
  return getHypothesis(job.result_ids[0] ?? id);
}

export function planExperiment(id: string): Promise<ApiHypothesis> {
  return postJSON<ApiHypothesis>(`/hypotheses/${id}/experiment`, {});
}

export function listHypothesisRevisions(id: string): Promise<ApiRevision[]> {
  return request<ApiRevision[]>(`/hypotheses/${id}/revisions`);
}

type GraphNodeKind = "hypothesis" | "document";

export interface GraphNode {
  id: string;
  kind: GraphNodeKind;
  label: string;
  status?: string;
  trl?: number | null;
  verdict?: Verdict | "";
  degree?: number;
  kpi_id?: string;
}

export interface GraphEdge {
  source: string;
  target: string;
  class: EvidenceStance;
  score?: number | null;
  chunk_id?: string;
}

export interface HypothesisGraphData {
  nodes: GraphNode[];
  edges: GraphEdge[];
}

export function getHypothesisGraph(): Promise<HypothesisGraphData> {
  return request<HypothesisGraphData>("/hypotheses/graph");
}

/** Сервер проверяет (≥1 положительный, каждый в [0,1]) и нормирует сумму к 1. */
export interface ScoringWeights {
  kpi_fit: number;
  evidence: number;
  novelty: number;
  value: number;
  risk_inv: number;
  trl_fit: number;
}

export function getScoringWeights(): Promise<ScoringWeights> {
  return request<ScoringWeights>("/hypotheses/scoring-weights");
}

export function saveScoringWeights(weights: ScoringWeights): Promise<ScoringWeights> {
  return putJSON<ScoringWeights>("/hypotheses/scoring-weights", weights);
}

export async function generateHypotheses(
  body: {
    kpi_id?: string;
    kpi_title?: string;
    kpi_metric?: string;
    kpi_description?: string;
    constraints?: string;
    count?: number;
    document_ids?: string[];
  },
  onJobCreated?: (jobID: string) => void,
): Promise<ApiHypothesis[]> {
  const job = await runHypothesisJob("generate", body, onJobCreated);
  return Promise.all(job.result_ids.map((id) => getHypothesis(id)));
}
