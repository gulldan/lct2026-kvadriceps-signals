import type { ApiHypothesis } from "../api";
import { kpiResearchDomain, type ApiKPI } from "@/features/kpi";

export function hypothesisResearchDomain(h: ApiHypothesis, kpi?: ApiKPI): string {
  return kpiResearchDomain(kpi, h.function_area, h.title, h.statement);
}
