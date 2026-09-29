import type { ApiDocument } from "@/features/document";
import { putJSON, request } from "@/shared/api/client";
import type { FileType } from "@/shared/types";

export interface ApiAccount {
  id: string;
  username: string;
  roles: string[];
  created_at: string;
}

export function adminListUsers(): Promise<ApiAccount[]> {
  return request<ApiAccount[]>("/admin/users");
}

export interface AdminDocumentsParams {
  limit?: number;
  offset?: number;
  q?: string;
  status?: string;
  type?: string;
}

export interface AdminDocumentList {
  items: ApiDocument[];
  total: number;
  limit: number;
  offset: number;
}

export function adminListDocuments(params: AdminDocumentsParams = {}): Promise<AdminDocumentList> {
  const qs = new URLSearchParams();
  if (params.limit !== undefined) qs.set("limit", String(params.limit));
  if (params.offset !== undefined) qs.set("offset", String(params.offset));
  if (params.q) qs.set("q", params.q);
  if (params.status) qs.set("status", params.status);
  if (params.type) qs.set("type", params.type);
  const suffix = qs.size > 0 ? `?${qs.toString()}` : "";
  return request<AdminDocumentList>(`/admin/documents${suffix}`);
}

export interface AdminDocumentsStats {
  total: number;
  by_status: Record<string, number>;
  by_file_type: Partial<Record<FileType, number>>;
  total_size_bytes: number;
  total_chunks: number;
  /** Последние 14 календарных дней (UTC), дни без загрузок — нули. */
  uploads_by_day: { day: string; count: number }[];
  top_by_chunks: { id: string; filename: string; title?: string; chunk_count: number }[];
  indexed_pct: number;
}

export function adminDocumentsStats(): Promise<AdminDocumentsStats> {
  return request<AdminDocumentsStats>("/admin/documents/stats");
}

/** Секунды; любого значения может не быть — нет замеров. */
interface StageLatency {
  p50?: number;
  p95?: number;
}

interface HypothesisJobStat {
  kind: string;
  count: number;
  p50_sec: number;
  max_sec: number;
}

/** hypothesis_jobs — длительность задач гипотез за последние 24 ч. */
export interface PerfResponse {
  pipeline: Record<string, StageLatency>;
  ingestion: Record<string, StageLatency>;
  hypothesis_jobs: HypothesisJobStat[];
}

export function getPerf(): Promise<PerfResponse> {
  return request<PerfResponse>("/admin/perf");
}

interface LlmUsageRow {
  requests: number;
  prompt_tokens: number;
  completion_tokens: number;
  total_tokens: number;
  cost_usd: number;
}

/** kind: реальные деньги, условные (ключ организатора) или бесплатно (локальная модель). */
type LlmProvider = LlmUsageRow & {
  key: string;
  label: string;
  currency: string;
  kind: "real" | "notional" | "free";
  cost_estimated: boolean;
  cost_rub: number;
};

export interface LlmUsage {
  range_days: number;
  from: string;
  to: string;
  provider: string;
  currency: string;
  cost_estimated: boolean;
  total_cost_rub: number;
  rub_per_usd: number;
  totals: LlmUsageRow;
  /** Каждый в своей валюте — стоимости не суммируются. */
  providers: LlmProvider[];
  by_day: (LlmUsageRow & { day: string })[];
  by_model: (LlmUsageRow & { model: string })[];
  by_operation: (LlmUsageRow & { operation: string })[];
  per_hypothesis: {
    hypotheses: number;
    requests: number;
    total_tokens: number;
    cost_usd: number;
    cost_rub: number;
    operations: string[];
  };
  budget: { credits_total: number; credits_used: number; credits_remaining: number };
  quota: {
    today_requests: number;
    daily_limit: number;
    per_min_used: number;
    per_min_limit: number;
  };
}

export function getLlmUsage(days = 7): Promise<LlmUsage> {
  return request<LlmUsage>(`/admin/llm-usage?days=${days}`);
}

export interface AppSettingView {
  key: string;
  kind: "string" | "bool" | "number" | "secret";
  group: "llm" | "pubsearch" | "generation";
  default: string;
  override: string;
  hasOverride: boolean;
  envValue: string;
  envSet: boolean;
  envOnly: boolean;
  source: "db" | "env" | "default";
}

export function adminGetAppSettings(): Promise<{ settings: AppSettingView[]; revision: number }> {
  return request<{ settings: AppSettingView[]; revision: number }>("/admin/settings");
}

/** null или "" сбрасывает оверрайд (возврат к конфигурации развёртывания). */
export function adminPutAppSettings(
  values: Record<string, string | null>,
  revision: number,
): Promise<{ settings: AppSettingView[]; revision: number }> {
  return putJSON<{ settings: AppSettingView[]; revision: number }>("/admin/settings", {
    values,
    revision,
  });
}

/** known=false — провайдер с запуска сервиса ещё не отвечал, и бесплатная проверка не удалась. */
export interface OpenAlexBudget {
  credential: "key" | "address";
  known: boolean;
  limit?: number;
  remaining?: number;
  spent?: number;
  reserve?: number;
  reset_at?: string;
  seen_at?: string;
}

export function getOpenAlexBudgets(signal?: AbortSignal): Promise<{ budgets: OpenAlexBudget[] }> {
  return request<{ budgets: OpenAlexBudget[] }>("/admin/openalex", { signal });
}
