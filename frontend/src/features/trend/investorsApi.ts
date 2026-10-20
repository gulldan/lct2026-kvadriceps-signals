import { request } from "@/shared/api/client";
import type { MoneyRegion, MoneySummary } from "./moneyApi";

/** Справочник ранних инвесторов: вшитый снимок фондов, ангельских сетей и акселераторов. */
const INVESTOR_TYPES = [
  "vc_manager",
  "vc_fund",
  "seed_fund",
  "angel_fund",
  "angel_network",
  "accelerator",
  "cvc",
  "government",
  "family_office",
  "other",
] as const;
const INVESTOR_STAGES = ["angel", "pre_seed", "seed", "series_a", "growth"] as const;
const INVESTOR_SORTS = ["deals", "investments", "name"] as const;

type InvestorType = (typeof INVESTOR_TYPES)[number];
type InvestorStage = (typeof INVESTOR_STAGES)[number];

export interface InvestorFilter {
  region: MoneyRegion | "";
  country: string;
  type: InvestorType | "";
  stage: InvestorStage | "";
  rubric: string;
  q: string;
  sort: (typeof INVESTOR_SORTS)[number];
}

interface InvestorDeal {
  company?: string;
  date: string;
  stage: string;
  rubric?: string;
  url: string;
}

export interface Investor {
  name: string;
  type: InvestorType;
  country?: string;
  city?: string;
  stages: InvestorStage[];
  website?: string;
  n_investments?: number;
  sources: { source: string; url: string; license: string }[];
  region: MoneyRegion;
  rubrics: string[];
  /** Нет поля — доска ранних денег ещё не собрана, сделки неизвестны. */
  deals?: { total: number; latest: InvestorDeal[] };
}

type Facet = Record<string, number>;

export interface InvestorPage {
  status: "ready" | "failed";
  built_at: string;
  total: number;
  matched: number;
  money_status: MoneySummary["status"];
  money_from?: string;
  money_through?: string;
  facets: { regions: Facet; countries: Facet; types: Facet; stages: Facet; rubrics: Facet };
  items: Investor[];
  sources: { name: string; license: string; count: number }[];
}

function investorParams(filter: InvestorFilter, offset: number) {
  const params = new URLSearchParams();
  for (const [key, value] of Object.entries(filter)) if (value) params.set(key, value);
  if (offset) params.set("offset", String(offset));
  return params.toString();
}

export function getLiveInvestors(filter: InvestorFilter, offset: number, signal?: AbortSignal) {
  return request<InvestorPage>(`/trends/live/investors?${investorParams(filter, offset)}`, {
    signal,
  });
}
