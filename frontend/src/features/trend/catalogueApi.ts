import { request } from "@/shared/api/client";

type MatchReason = "name" | "alias" | "description" | "mechanism" | "problem" | "application";

interface TechnologyHit {
  technology_id: string;
  canonical_name: string;
  description: string;
  validation_status: string;
  match_reasons: MatchReason[];
  matched_text?: string;
}

type CatalogueReason = "query_required" | "below_relevance_floor" | "catalogue_empty";

export interface TechnologyPage {
  technologies: TechnologyHit[];
  next_cursor?: string;
  catalogue_version: string;
  reason?: CatalogueReason;
  schema_version: string;
}

export interface CatalogueQuery {
  q: string;
  languages?: string[];
  roles?: string[];
  limit?: number;
  cursor?: string;
  all?: boolean;
}

function catalogueParams(query: CatalogueQuery): URLSearchParams {
  const params = new URLSearchParams();
  if (query.q) params.set("q", query.q);
  for (const language of query.languages ?? []) params.append("language", language);
  for (const role of query.roles ?? []) params.append("role", role);
  if (query.limit) params.set("limit", String(query.limit));
  if (query.cursor) params.set("cursor", query.cursor);
  if (query.all) params.set("all", "true");
  return params;
}

export function searchTechnologies(
  query: CatalogueQuery,
  signal?: AbortSignal,
): Promise<TechnologyPage> {
  return request<TechnologyPage>(`/technologies?${catalogueParams(query)}`, { signal });
}
