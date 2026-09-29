export const ROLES = ["primary", "component", "background", "comparison"] as const;
export const EXTRACTION_STATUSES = [
  "complete",
  "partial",
  "needs_review",
  "unresolved_mention",
] as const;
export const VALIDATION_STATUSES = ["proposed", "verified", "needs_review"] as const;
export const NAME_KINDS = ["term", "descriptive"] as const;
export const ALIAS_BASES = ["expert_review", "extraction", "alias"] as const;
export const COVERAGE_STATUSES = [
  "complete",
  "partial",
  "no_technology",
  "failed",
  "not_extracted",
  "unsupported_language",
] as const;

/** Неизвестное значение с бэкенда не роняет экран, а сводится к явному «неизвестно». */
export function known<const T extends readonly string[]>(list: T, value: string | undefined) {
  return list.find((item) => item === value) as T[number] | undefined;
}
