import type { Itc } from "@/features/hypothesis";
import { loadAuth, request } from "@/shared/api/client";

interface ClusterRep {
  document_id?: string;
  filename?: string;
  snippet?: string;
}

export interface ApiCluster {
  id: string;
  owner_id: string;
  label: string;
  /** В списке пустое ради веса ответа; описание отдаёт getCluster. */
  summary: string;
  keywords: string[];
  method: string;
  chunk_count: number;
  document_count: number;
  representatives: ClusterRep[];
  /** Целиком — только в getCluster; в списке — проекция ради веса ответа. */
  params?: { itc?: Itc } & Record<string, unknown>;
  status: string;
  created_at: string;
  updated_at: string;
}

// Кэш привязан к объекту сессии: он стабилен до нового входа или обновления токена.
const CLUSTERS_TTL_MS = 120_000;

let clustersCache: {
  session: ReturnType<typeof loadAuth>;
  at: number;
  promise: Promise<ApiCluster[]>;
} | null = null;

export function listClusters(): Promise<ApiCluster[]> {
  const session = loadAuth();
  if (
    clustersCache !== null &&
    clustersCache.session === session &&
    Date.now() - clustersCache.at < CLUSTERS_TTL_MS
  ) {
    return clustersCache.promise;
  }
  const entry: NonNullable<typeof clustersCache> = {
    session,
    at: Date.now(),
    promise: request<ApiCluster[]>("/clusters").catch((err: unknown) => {
      // Отказ не кэшируется: следующий вызов повторит запрос.
      if (clustersCache === entry) clustersCache = null;
      throw err;
    }),
  };
  clustersCache = entry;
  return entry.promise;
}

export function listTopClusters(limit: number): Promise<ApiCluster[]> {
  return request<ApiCluster[]>(`/clusters?limit=${limit}`);
}

export function invalidateClusters(): void {
  clustersCache = null;
}

export function getCluster(id: string): Promise<ApiCluster> {
  return request<ApiCluster>(`/clusters/${id}`);
}
