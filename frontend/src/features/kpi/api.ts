import { postJSON, putJSON, request } from "@/shared/api/client";
import type { ApiDocument } from "@/features/document";

export interface GraphHypothesis {
  statement: string;
  title?: string;
  process?: string;
  property?: string;
  kpi?: string;
  rationale?: string;
  documents?: { id?: string; filename?: string }[];
  score?: number;
}

export interface ApiKPI {
  id: string;
  owner_id: string;
  title: string;
  description: string;
  metric: string;
  unit: string;
  direction: string;
  baseline: number | null;
  target: number | null;
  function_area: string;
  status: string;
  detail: Record<string, unknown>;
  created_at: string;
  updated_at: string;
}

export interface KpiInput {
  title: string;
  description?: string;
  metric?: string;
  unit?: string;
  /** increase | decrease | maintain */
  direction?: string;
  baseline?: number | null;
  target?: number | null;
  function_area?: string;
  /** active | archived */
  status?: string;
  detail?: Record<string, unknown>;
}

export interface KpiSuggestion {
  title: string;
  metric?: string;
  unit?: string;
  direction?: string;
  function_area?: string;
  rationale?: string;
}

export interface ParsedKpiPrompt {
  title: string;
  description?: string;
  metric?: string;
  unit?: string;
  direction?: string;
  function_area?: string;
  constraints?: string;
  baseline?: number | null;
  target?: number | null;
}

export interface KpiDocument extends ApiDocument {
  role: string;
  attached_at: string;
}

export function listKPIs(): Promise<ApiKPI[]> {
  return request<ApiKPI[]>("/kpis");
}

export function getKPI(id: string, signal?: AbortSignal): Promise<ApiKPI> {
  return request<ApiKPI>(`/kpis/${id}`, { signal });
}

/** PUT /kpis/{id} переписывает тезис целиком: всё, чего нет в теле, обнулится. */
export function kpiInput(k: ApiKPI, patch: Partial<KpiInput> = {}): KpiInput {
  return {
    title: k.title,
    description: k.description ?? "",
    metric: k.metric ?? "",
    unit: k.unit ?? "",
    direction: k.direction ?? "",
    baseline: k.baseline ?? null,
    target: k.target ?? null,
    function_area: k.function_area ?? "",
    status: k.status,
    detail: k.detail ?? {},
    ...patch,
  };
}

export interface ThesisSearch {
  query: string;
  at: string;
}

const THESIS_SEARCHES = 10;

export function thesisQuery(k: ApiKPI): string {
  const query = k.detail?.radar_query;
  return typeof query === "string" && query.trim() ? query.trim() : k.title;
}

export function thesisSearches(k: ApiKPI): ThesisSearch[] {
  const raw = k.detail?.radar_searches;
  return Array.isArray(raw)
    ? raw.filter((s): s is ThesisSearch => typeof s?.query === "string" && s.query !== "")
    : [];
}

export function thesisSignalsPath(k: ApiKPI): string {
  return `/trends?${new URLSearchParams({ q: thesisQuery(k), thesis: k.id })}`;
}

/** Уже верхний запрос не пишется повторно: перезагрузка выдачи не должна переписывать тезис. */
export async function linkThesisSearch(id: string, query: string): Promise<ApiKPI> {
  const kpi = await getKPI(id);
  const key = query.trim().toLowerCase();
  const searches = thesisSearches(kpi);
  if (searches[0]?.query.trim().toLowerCase() === key) return kpi;
  const next = [
    { query: query.trim(), at: new Date().toISOString() },
    ...searches.filter((s) => s.query.trim().toLowerCase() !== key),
  ].slice(0, THESIS_SEARCHES);
  return updateKPI(id, kpiInput(kpi, { detail: { ...kpi.detail, radar_searches: next } }));
}

export async function parseKpiPrompt(prompt: string): Promise<ParsedKpiPrompt> {
  const raw = await postJSON<{ kpi?: ParsedKpiPrompt }>("/kpis/parse", { prompt });
  return raw?.kpi ?? { title: "" };
}

export async function listKpiDocuments(kpiId: string): Promise<KpiDocument[]> {
  const raw = await request<{ documents?: KpiDocument[] }>(`/kpis/${kpiId}/documents`);
  return raw?.documents ?? [];
}

export function attachKpiDocuments(kpiId: string, documentIds: string[]): Promise<void> {
  return postJSON<void>(`/kpis/${kpiId}/documents`, { document_ids: documentIds });
}

export function detachKpiDocument(kpiId: string, documentId: string): Promise<void> {
  return request<void>(`/kpis/${kpiId}/documents/${documentId}`, { method: "DELETE" });
}

export function createKPI(body: KpiInput): Promise<ApiKPI> {
  return postJSON<ApiKPI>("/kpis", body);
}

export function updateKPI(id: string, body: KpiInput): Promise<ApiKPI> {
  return putJSON<ApiKPI>(`/kpis/${id}`, body);
}

export function deleteKPI(id: string): Promise<void> {
  return request<void>(`/kpis/${id}`, { method: "DELETE" });
}

export async function suggestKPIs(): Promise<KpiSuggestion[]> {
  const raw = await postJSON<unknown>("/kpis/suggest", {});
  if (Array.isArray(raw)) {
    return raw as KpiSuggestion[];
  }
  const obj = (raw ?? {}) as { suggestions?: unknown; items?: unknown };
  const list = Array.isArray(obj.suggestions)
    ? obj.suggestions
    : Array.isArray(obj.items)
      ? obj.items
      : [];
  return list as KpiSuggestion[];
}

export async function graphHypotheses(kpiId: string): Promise<GraphHypothesis[]> {
  const raw = await postJSON<unknown>(`/kpis/${kpiId}/graph-hypotheses`, {});
  if (Array.isArray(raw)) {
    return raw as GraphHypothesis[];
  }
  const obj = (raw ?? {}) as { candidates?: unknown; items?: unknown };
  const list = Array.isArray(obj.candidates)
    ? obj.candidates
    : Array.isArray(obj.items)
      ? obj.items
      : [];
  return list as GraphHypothesis[];
}

export function saveDirectionAsHypothesis(
  kpiId: string,
  item: GraphHypothesis,
): Promise<{ id: string }> {
  const statement = (item.statement ?? "").trim() || (item.title ?? "").trim();
  return postJSON<{ id: string }>("/hypotheses", {
    title: (item.title ?? "").trim() || statement.slice(0, 120),
    statement,
    rationale: (item.rationale ?? "").trim(),
    method: "manual",
    status: "draft",
    kpi_id: kpiId,
    measurable: false,
    tags: ["graph_direction"],
    generation: {
      kind: "graph_direction",
      process: item.process ?? "",
      property: item.property ?? "",
      kpi: item.kpi ?? "",
      documents: (item.documents ?? []).map((d) => d.id).filter(Boolean),
    },
  });
}
