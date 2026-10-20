import type { KpiInput } from "./api";
import { researchDomain } from "@/shared/lib/researchDomain";
import { researchTaxonomy } from "@/shared/lib/researchTaxonomy";

/** Отрасль тезиса важнее технического метода в его описании или связанном прогнозе. */
export function kpiResearchDomain(
  kpi: KpiInput | undefined,
  ...relatedText: (string | null | undefined)[]
): string {
  const domain = researchDomain(kpi?.function_area);
  if (domain !== "unknown") return domain;
  const declared = kpi?.detail?.research_domain;
  if (
    typeof declared === "string" &&
    researchTaxonomy.categories.some((category) => category.id === declared)
  ) {
    return declared;
  }
  return researchDomain(...relatedText, kpi?.function_area, kpi?.title, kpi?.description);
}

export function inferDirection(
  title: string,
  baseline: number | null,
  target: number | null,
): string {
  if (baseline !== null && target !== null) {
    if (target > baseline) return "increase";
    if (target < baseline) return "decrease";
    return "maintain";
  }
  const text = title.toLowerCase();
  if (/(сниз|уменьш|сократ|минимиз|удешев|lower|reduce|decreas|minimi[sz]e)/i.test(text)) {
    return "decrease";
  }
  if (/(повыс|увелич|улучш|максимиз|рост|increase|improv|maximi[sz]e|raise)/i.test(text)) {
    return "increase";
  }
  return "maintain";
}
