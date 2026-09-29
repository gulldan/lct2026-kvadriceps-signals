import { request } from "@/shared/api/client";

interface StudyPeriod {
  period: string;
  count: number;
  population: number;
  share: number | null;
  groups: number;
  new_groups: number;
  retained_groups: number;
}

export interface StudySignals {
  novelty: "new" | "recent" | "earlier" | "unknown";
  momentum: "up" | "down" | "level" | "unknown";
  coherence: "screened" | "titles" | "unscreened" | "query";
  spread: "broad" | "several" | "single" | "none" | "unknown";
  application: "releases" | "implementations" | "none_observed";
  concentration: "low" | "medium" | "high" | "unknown";
}

export interface StudyEvent {
  topic: string;
  kind: "implementation" | "release" | "preprint" | "vendor_material" | "discussion" | "patent";
  community:
    | "academia"
    | "open_source"
    | "vendor"
    | "startup"
    | "organization"
    | "discussion"
    | "patent";
  date: string;
  title: string;
  org: string;
  url: string;
  stars?: number;
}

export interface StudyTopic {
  id: "graph" | "adaptive" | "multimodal" | "agentic" | "memory";
  paper_ids: string[];
  series: StudyPeriod[];
  stage: "emerging_signal" | "early_signal" | "established" | "unassessed";
  momentum: "up" | "down" | "level" | "unknown";
  reviewed: boolean;
  screening: "abstracts" | "titles" | "none" | "query";
  evidence_ids: string[];
  first_year: string;
  growth_start: string;
  active_years: number;
  groups: number;
  top_group_share: number | null;
  signals: StudySignals;
  external: StudyEvent[];
  earliest_external: string;
  growth: {
    prior: StudyPeriod;
    recent: StudyPeriod;
    delta_pp: number | null;
    share_ratio: number | null;
    count_ratio: number | null;
  };
}

export interface StudyPaper {
  id: string;
  title: string;
  date: string;
  url: string;
  authors: string[];
  document_id: string;
  snapshot_id: string;
}

export interface TrendStudy {
  version: string;
  method: string;
  revision: string;
  observed_at: string;
  external_observed_at: string;
  title_query: string;
  enumerated_papers: number;
  expected_papers: number;
  available_papers: number;
  coverage: string;
  topics: StudyTopic[];
  papers: StudyPaper[];
}

export async function getTrendStudy(signal?: AbortSignal) {
  const study = await request<TrendStudy>("/trends/study", { signal });
  if (
    study.version !== "acl-rag-study-v1" ||
    !["bibliographic-signals-v2", "bibliographic-signals-v3-screened"].includes(study.method) ||
    study.available_papers !== study.papers.length
  )
    throw new Error("inconsistent research study");
  const ids = new Set(study.papers.map((p) => p.id));
  if (
    ids.size !== study.papers.length ||
    study.topics.some((topic) => topic.paper_ids.some((id) => !ids.has(id)))
  )
    throw new Error("inconsistent research membership");
  return study;
}
