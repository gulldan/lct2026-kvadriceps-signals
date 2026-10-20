import { request } from "@/shared/api/client";
import type { MoneyKind } from "./moneyApi";

/** Карта мира: наука по странам аффилиаций OpenAlex для верха мирового топа радара, два полугодия. */
interface MapCountryCount {
  country: string;
  count: number;
}

export interface MapSignal {
  id: string;
  name: string;
  label?: string;
  /** «search» открывается своим сохранённым ответом, а не карточкой снимка. */
  source: "curated" | "discovered" | "search" | "candidate";
  stage: string;
  /** Технологий в посчитанной карточке направления; null — карточка ещё не считалась. */
  technologies: number | null;
  branch?: string;
  first_year?: string;
  terms: string[];
  radar_prior: number;
  radar_recent: number;
  groups: number;
  new_groups: number;
  share_ratio: number | null;
  science_prior: number;
  science_recent: number;
  /** Страны-лидеры науки по убыванию работ за текущее полугодие, код ISO alpha-2. */
  lead: MapCountryCount[];
  money: { total: number; kinds: Partial<Record<MoneyKind, number>> };
}

export interface MapSummary {
  status: "ready" | "loading" | "failed";
  built_at?: string;
  from?: string;
  mid?: string;
  to?: string;
  /** Сколько направлений опрошено: страны на карте — сумма только по ним. */
  topics: number;
  signals: MapSignal[];
  countries: Record<string, { prior: number; recent: number }>;
}

export function getLiveMap(signal?: AbortSignal) {
  return request<MapSummary>("/trends/live/map", { signal });
}
