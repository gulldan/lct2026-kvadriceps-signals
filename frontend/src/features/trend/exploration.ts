import type { AnalysisJob, TrendCandidate } from "./trendApi";

const supportedFields = (candidate: TrendCandidate) =>
  [candidate.brief.problem, candidate.brief.advantage, candidate.brief.example].filter(Boolean)
    .length;

export function exploreAnalysis(job: AnalysisJob) {
  const result = job.result!;
  const candidates = result.candidates
    .filter((candidate) => candidate.relevance.relevant)
    .toSorted(
      (a, b) =>
        b.relevance.lexical_overlap - a.relevance.lexical_overlap ||
        supportedFields(b) - supportedFields(a) ||
        (b.relevance.semantic_similarity ?? -1) - (a.relevance.semantic_similarity ?? -1) ||
        a.id.localeCompare(b.id),
    );
  const ids = new Set(candidates.flatMap((candidate) => candidate.work_ids));
  const works = result.works.filter((work) => ids.has(work.id));
  const documents = new Set(works.flatMap((work) => work.document_ids));
  const sources = job.sources.filter((source) => documents.has(source.document_id));
  const publications = new Map(
    sources.map((source) => [source.document_id, source.source.publication?.value]),
  );
  const start = Number((job.request?.start ?? result.cutoff).slice(0, 4));
  const end = Number(result.cutoff.slice(0, 4));
  const years = new Map(Array.from({ length: end - start + 1 }, (_, index) => [start + index, 0]));
  let undated = 0;
  let outside = 0;
  for (const work of works.filter((item) => item.kind === "paper")) {
    // Каноническая работа считается один раз — по самой ранней задокументированной публикации.
    const publication = work.document_ids
      .flatMap((id) => (publications.get(id) ? [publications.get(id)!] : []))
      .toSorted()[0];
    if (!publication) {
      undated++;
      continue;
    }
    const year = Number(publication.slice(0, 4));
    if (!years.has(year)) {
      outside++;
      continue;
    }
    years.set(year, years.get(year)! + 1);
  }
  return {
    candidates,
    works,
    sources,
    years: [...years].map(([year, count]) => ({ year, count })),
    undated,
    outside,
  };
}
