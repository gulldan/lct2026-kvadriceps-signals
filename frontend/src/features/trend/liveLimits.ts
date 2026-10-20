/** Остальное (и незнакомый код) — границы метода: тревожный тон на них читался как поломка. */
const FAILURES = new Set([
  "analysis_unavailable",
  "arxiv_unavailable",
  "channel_unavailable",
  "citations_unavailable",
  "classifier_unavailable",
  "discovery_failed",
  "github_limited",
  "github_unavailable",
  "hn_unavailable",
  "openalex_budget",
  "openalex_exhausted",
  "patents_unavailable",
  "previous_snapshot",
  "pubmed_unavailable",
  "research_unavailable",
  "scholar_unavailable",
  "semantic_unavailable",
  "source_unavailable",
  "web_unavailable",
]);

export const liveLimitFailure = (code: string) => FAILURES.has(code);

export function liveLimitKind(limit: string) {
  const text = limit.toLowerCase();
  if (text.includes("patent_collector_url")) return "patents_unavailable";
  if (text.includes("web_collector_url")) return "web_unavailable";
  if (text.includes("latest refresh failed")) return "previous_snapshot";
  // Раньше общих: в тексте есть «budget» и «sample», и иначе они читались бы как отказ источника.
  if (text.startsWith("affiliations")) return "affiliations_budget";
  if (/directions? with fewer than/.test(text)) return "discovery_below_floor";
  if (text.startsWith("discovery:") && text.includes("sample of")) return "discovery_sample";
  if (text.startsWith("scholar:"))
    return text.includes("вне arxiv") ? "scholar_journals" : "scholar_capped";
  if (text.includes("github") && /limited|limit|403|429|budget/.test(text)) return "github_limited";
  if (text.includes("patent") && /grant|histor|sample|ambiguous/.test(text)) return "patent_sample";
  if (/rss|feed/.test(text)) return "web_sample";
  if (/enumerated|truncat|incomplete|budget|capped|partial|population sample/.test(text))
    return "partial";
  if (/arxiv/.test(text)) return "arxiv_unavailable";
  if (/semantic|embedding|f2llm/.test(text)) return "semantic_unavailable";
  // Незнакомая оговорка — не повод сказать, что источник лежит.
  if (/unavailable|unreachable|refused|failed|timed out|timeout|no response/.test(text))
    return "source_unavailable";
  return "other";
}
