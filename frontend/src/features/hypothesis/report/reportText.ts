import { i18n } from "@/shared/i18n";

export const t = i18n.t.bind(i18n);

export function riskLevel(level: string): string {
  switch (level) {
    case "low":
    case "medium":
    case "high":
      return t(`hypothesis:hreport.level.${level}`);
    default:
      return level;
  }
}
