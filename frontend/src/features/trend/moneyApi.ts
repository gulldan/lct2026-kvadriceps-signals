import { request } from "@/shared/api/client";

/** Слой ранних денег: раунды pre-seed, seed и Series A, гранты стартапам и отборы акселераторов. */
export type MoneyKind = "round" | "grant" | "accelerator" | "development" | "research";
export type MoneyRegion =
  | "americas"
  | "europe"
  | "latam"
  | "mena"
  | "africa"
  | "asia"
  | "cis"
  | "unknown";
type MoneyStage =
  | "pre_seed"
  | "seed"
  | "series_a"
  | "unknown"
  | "grant"
  | "accelerator"
  | "development"
  | "research";

export interface MoneyFilter {
  region: MoneyRegion | "all";
  kind: MoneyKind | "all";
}

export interface MoneyEvent {
  id: string;
  kind: MoneyKind;
  stage: MoneyStage;
  company?: string;
  region: MoneyRegion;
  country?: string;
  date: string;
  amount?: string;
  /** Сумма полным числом в валюте `currency` (ISO 4217); без валюты экран показывает исходную `amount`. */
  amount_value?: number;
  currency?: string;
  investors?: string;
  rubric: string;
  rubric_basis?: "text" | "name";
  title: string;
  url: string;
  source: string;
  license: string;
  quote?: string;
}

export interface MoneyCell {
  rubric: string;
  region: MoneyRegion;
  kind: MoneyKind;
  stage: MoneyStage;
  month: string;
  count: number;
}

interface MoneyScience {
  rubric: string;
  topics: number;
  prior: number;
  recent: number;
  prior_population: number;
  recent_population: number;
}

interface MoneySource {
  name: string;
  kind: "dataset" | "registry" | "feed";
  region: MoneyRegion;
  license: string;
  url: string;
  records: number;
  events: number;
  error?: string;
}

export interface MoneySummary {
  status: "ready" | "loading" | "failed";
  built_at?: string;
  from?: string;
  through?: string;
  method?: string;
  months: string[];
  cells: MoneyCell[];
  science: MoneyScience[];
  /** Окна роста науки: «2025-10–2026-03». */
  science_prior?: string;
  science_recent?: string;
  latest: MoneyEvent[];
  sources: MoneySource[];
  total: number;
  unsorted: number;
}

export interface MoneyMatch {
  status: MoneySummary["status"];
  total: number;
  kinds: Partial<Record<MoneyKind, number>>;
  events: MoneyEvent[];
}

export function getLiveMoney(filter: MoneyFilter, signal?: AbortSignal) {
  const params = new URLSearchParams();
  if (filter.region !== "all") params.set("region", filter.region);
  if (filter.kind !== "all") params.set("kind", filter.kind);
  return request<MoneySummary>(`/trends/live/money?${params}`, { signal });
}

export function getLiveMoneyMatch(terms: string[], signal?: AbortSignal) {
  const params = new URLSearchParams();
  for (const term of terms.slice(0, 12)) params.append("term", term);
  return request<MoneyMatch>(`/trends/live/money/match?${params}`, { signal });
}
