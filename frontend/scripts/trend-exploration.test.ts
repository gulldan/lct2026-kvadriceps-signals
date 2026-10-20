import { expect, test } from "bun:test";
import { exploreAnalysis } from "../src/features/trend/exploration";
import { analysisRequestParams } from "../src/features/trend/trendApi";
import { analysisFixture } from "../tests/product/trends-fixture";

test("search results survive an empty ranking; versions and overlapping memberships never multiply a paper", () => {
  const job = structuredClone(analysisFixture(0));
  job.sources[0]!.source.publication = { value: "2024-03-01", precision: "day", basis: "source" };
  job.sources.push({
    ...job.sources[0]!,
    document_id: "earlier",
    source: {
      ...job.sources[0]!.source,
      publication: { value: "2023-12", precision: "month", basis: "source" },
    },
  });
  job.request!.start = "2023-01-01";
  job.result!.works[0]!.document_ids.push("earlier");
  const found = exploreAnalysis(job);
  expect(found.candidates).toHaveLength(15);
  expect(found.works).toHaveLength(2);
  expect(found.years).toEqual([
    { year: 2023, count: 1 },
    { year: 2024, count: 0 },
  ]);
  expect(job.result!.ranking.ordered_ids).toEqual([]);
});

test("unmatched works, unknown publication dates and papers outside the shown years remain distinct", () => {
  const job = structuredClone(analysisFixture(0));
  job.result!.candidates.forEach((candidate) => {
    candidate.relevance.relevant = false;
  });
  expect(exploreAnalysis(job).works).toHaveLength(0);
  job.result!.candidates[0]!.relevance.relevant = true;
  job.sources[0]!.source.publication = null;
  expect(exploreAnalysis(job).undated).toBe(1);
  job.sources[0]!.source.publication = { value: "2020", precision: "year", basis: "source" };
  expect(exploreAnalysis(job).outside).toBe(1);
  expect(exploreAnalysis(job).years.every((year) => year.count === 0)).toBe(true);
});

test("demo and manual requests share fresh identities and preserve the previous result", () => {
  const value = {
    query: "RAG",
    start: "2023-01-01",
    cutoff: "2024-08-10",
    mode: "as_of" as const,
  };
  const a = analysisRequestParams(value, "previous-run");
  const b = analysisRequestParams(value);
  expect(a.get("request")).not.toBe(b.get("request"));
  expect(a.get("cutoff")).toBe("2024-08-10T23:59:59.999Z");
  expect(a.get("previous")).toBe("previous-run");
  expect(a.get("q")).toBe("RAG");
});

test("equally matching topics with source-backed explanations appear before empty briefs", () => {
  const job = structuredClone(analysisFixture(0));
  const empty = job.result!.candidates[0]!;
  empty.brief.problem = null;
  empty.brief.advantage = null;
  empty.brief.example = null;
  empty.relevance.semantic_similarity = 1;
  expect(exploreAnalysis(job).candidates[0]!.id).not.toBe(empty.id);
  expect(exploreAnalysis(job).candidates.at(-1)!.id).toBe(empty.id);
  expect(job.result!.ranking.ordered_ids).toEqual([]);
});
