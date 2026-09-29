import type { AnalysisJob, TrendCandidate } from "../../src/features/trend/trendApi";
import type { SourceMetadata } from "../../src/features/document/sourceApi";
import corpus from "../fixtures/signalgraph-corpus-v1.json" with { type: "json" };

const paper = corpus.records.find((record) => record.id === "arxiv-v1")!.source as SourceMetadata;
const repository = corpus.records.find((record) => record.id === "repo-original")!
  .source as SourceMetadata;
const quoteText =
  "😀 A problem remains unresolved. Our method is 10% faster in the benchmark. Tested on dataset X; no production deployment is claimed.";
export const paperEvidence = {
  document_id: "ui-paper",
  snapshot_id: "ui-paper-v1",
  title: "UI-M · Scientific source",
  filename: "paper.txt",
  mime_type: "text/plain",
  source: paper,
  text_hash: "a".repeat(64),
  parser_revision: "fixture",
  pipeline_revision: "fixture",
  parser_metadata: {},
  chunks: [
    {
      id: "paragraph-1",
      index: 0,
      text: quoteText,
      char_start: 0,
      char_end: Array.from(quoteText).length,
      page_start: 0,
      page_end: 0,
      locator_kind: "text_span",
      section: "",
    },
  ],
};
export const repoEvidence = {
  ...paperEvidence,
  document_id: "ui-repo",
  snapshot_id: "ui-repo-v1",
  title: "UI-M · Reference implementation",
  source: repository,
  text_hash: "b".repeat(64),
  chunks: [{ ...paperEvidence.chunks[0]!, text: "https://arxiv.org/abs/2401.12345", char_end: 30 }],
};

const fields = {
  problem: { text: "A problem remains unresolved.", start: 2, end: 31 },
  advantage: { text: "Our method is 10% faster in the benchmark.", start: 32, end: 74 },
  example: {
    text: "Tested on dataset X; no production deployment is claimed.",
    start: 75,
    end: 132,
  },
} as const;

function candidate(index: number): TrendCandidate {
  const id = `ui-topic-${index}`;
  const evidence = (Object.keys(fields) as (keyof typeof fields)[]).map((field) => ({
    id: `${id}-${field}`,
    field,
    work_id: "paper:ui",
    document_id: "ui-paper",
    snapshot_id: "ui-paper-v1",
    source_revision: paper.revision,
    text_sha256: "a".repeat(64),
    start: fields[field].start,
    end: fields[field].end,
    quote: fields[field].text,
    claim_type: "research_claim" as const,
  }));
  const briefField = (field: keyof typeof fields) => ({
    text: fields[field].text,
    evidence_ids: [`${id}-${field}`],
  });
  return {
    id,
    title: `UI-M · ${index + 1}. Исследование методов доказательного поиска — Evidence retrieval and 中文技术研究`,
    work_ids: ["paper:ui", "repository:ui"],
    complete_for_brief: true,
    missing_fields: [],
    brief: {
      problem: briefField("problem"),
      advantage: briefField("advantage"),
      example: briefField("example"),
      description_status: "template",
      evidence_sha256: "c".repeat(64),
    },
    evidence,
    measurements: {
      earliest: {
        date: { value: "2024-03", precision: "month", basis: "fixture" },
        document_id: "ui-paper",
        snapshot_id: "ui-paper-v1",
        source_revision: paper.revision,
        work_id: "paper:ui",
      },
      earliest_scientific: null,
      history_kind: "retrospective_distribution_of_current_membership",
      stars_history: "unknown_before_first_observation",
      unallocated: [],
      units: ["scientific_works"],
      series: [
        {
          period: "2024-Q1",
          start: "2024-01-01",
          end_exclusive: "2024-04-01",
          complete_period: true,
          coverage: "complete",
          comparable_exposure: 100,
          comparable_scientific_works: 0,
          normalized_activity: 0,
          found: {
            scientific_works: 0,
            independent_repositories: 0,
            published_releases: 0,
            patent_publications: 0,
            company_materials: 0,
            analyses: 0,
          },
        },
        {
          period: "2024-Q2",
          start: "2024-04-01",
          end_exclusive: "2024-07-01",
          complete_period: true,
          coverage: "unknown_or_partial",
          comparable_exposure: null,
          comparable_scientific_works: null,
          normalized_activity: null,
          found: {
            scientific_works: 2,
            independent_repositories: 1,
            published_releases: 1,
            patent_publications: 0,
            company_materials: 0,
            analyses: 0,
          },
        },
        {
          period: "2024-Q3",
          start: "2024-07-01",
          end_exclusive: "2024-10-01",
          complete_period: false,
          coverage: "unknown_or_partial",
          comparable_exposure: null,
          comparable_scientific_works: null,
          normalized_activity: null,
          found: {
            scientific_works: 1,
            independent_repositories: 0,
            published_releases: 0,
            patent_publications: 1,
            company_materials: 0,
            analyses: 0,
          },
        },
      ],
    },
    assessment: {
      version: "ui-method",
      eligible: true,
      reasons: [],
      features: {
        dated_scientific_works: 3,
        active_scientific_quarters: 2,
        log_rate_ratio: null,
        recent_rate: null,
      },
      parameters: {},
      order: [1, 0.01, 2, 3],
    },
    relevance: {
      relevant: true,
      mode: "semantic_and_lexical",
      semantic_similarity: 0.8,
      lexical_overlap: 1,
    },
  };
}

export function analysisFixture(count = 15, id = "ui-run", query = "RAG"): AnalysisJob {
  const candidates = Array.from({ length: 15 }, (_, index) => candidate(index));
  for (const item of candidates.slice(count)) {
    item.complete_for_brief = false;
    item.assessment.eligible = false;
    item.assessment.reasons = ["comparable_coverage_missing"];
    item.assessment.order = null;
  }
  return {
    id,
    input_run_id: "ui-input",
    request: {
      request_id: "ui-request",
      query,
      start: "2024-01-01",
      cutoff: "2024-08-10T00:00:00Z",
      mode: "retrospective_current_membership",
    },
    method_sha256: "d".repeat(64),
    result_sha256: "e".repeat(64),
    status: "published",
    failure_code: "",
    created_at: "2025-01-01T00:00:00Z",
    updated_at: "2025-01-01T00:00:02Z",
    active: true,
    sources: [
      {
        document_id: "ui-paper",
        snapshot_id: "ui-paper-v1",
        title: paperEvidence.title,
        source: paper,
        text_sha256: "a".repeat(64),
      },
      {
        document_id: "ui-repo",
        snapshot_id: "ui-repo-v1",
        title: repoEvidence.title,
        source: repository,
        text_sha256: "b".repeat(64),
      },
    ],
    method: {
      version: "ui-method-v1",
      quality_status: "not_frozen",
      code_sha256: "f".repeat(64),
      parameters: {
        emergence: {
          recent_quarters: 4,
          prior_quarters: 4,
          max_first_age_quarters: 12,
          min_scientific_works: 2,
          min_active_quarters: 2,
          min_recent_works: 1,
          smoothing: 0.5,
        },
      },
      explanation: {
        title: "UI-M методика",
        status: "UI-M",
        definition: "UI-M: несколько разных научных работ.",
        counts: "Версии не увеличивают число работ.",
        earliest: "Самое раннее найденное упоминание.",
        history: "Ретроспективный ряд.",
        formula: "ln(recent / prior)",
        order: "Научное основание, затем стабильный ID.",
        brief: "Цитаты источников.",
        limitations: ["UI-M: синтетические данные для проверки интерфейса."],
      },
    },
    result: {
      version: "analysis-result-v1",
      job_id: id,
      input_run_id: "ui-input",
      query,
      cutoff: "2024-08-10T00:00:00Z",
      method_sha256: "d".repeat(64),
      candidates,
      ranking: {
        ordered_ids: candidates.slice(0, count).map((item) => item.id),
        completeness: count === 15 ? "complete" : "insufficient_coverage",
        reasons: count === 15 ? [] : ["fewer_than_15_relevant_complete_temporal_candidates"],
      },
      works: [
        { id: "paper:ui", kind: "paper", document_ids: ["ui-paper"], implementation_root: null },
        {
          id: "repository:ui",
          kind: "software_repository",
          document_ids: ["ui-repo"],
          implementation_root: "github:1",
        },
      ],
      relations: [
        {
          id: "ui-relation",
          paper_id: "paper:ui",
          repository_id: "repository:ui",
          repository_root: "github:1",
          relation: "explicit_reference",
          official: false,
          evidence: [
            {
              document_id: "ui-repo",
              snapshot_id: "ui-repo-v1",
              text_sha256: "b".repeat(64),
              char_start: 0,
              char_end: 30,
              quote: repoEvidence.chunks[0]!.text,
              available_at: "2024-01-01T00:00:00Z",
            },
          ],
        },
      ],
      search: { mode: "semantic_and_lexical", applied_query: query, reason: null },
      limitations: ["UI-M only"],
    },
  };
}
