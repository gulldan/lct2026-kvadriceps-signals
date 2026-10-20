import { expect, test } from "bun:test";
import { analysisFixture } from "../tests/product/trends-fixture";
import { technologyMap, mapLayout, visibleWorks } from "../src/features/trend/mapData";

test("a shared canonical work is one graph node even when it belongs to fifteen topics", () => {
  const job = structuredClone(analysisFixture(0));
  const before = JSON.stringify(job);
  const map = technologyMap(job);
  expect(map.topics).toHaveLength(15);
  expect(map.works).toHaveLength(2);
  const nodes = mapLayout(map.topics);
  expect(nodes).toHaveLength(17);
  expect(new Set(nodes.map((node) => node.id)).size).toBe(17);
  expect(nodes.every((node) => Number.isFinite(node.x) && Number.isFinite(node.y))).toBe(true);
  expect(JSON.stringify(job)).toBe(before);
});

test("time filters use the earliest documented publication, excluding undated works from past years", () => {
  const job = structuredClone(analysisFixture(0));
  job.sources[0]!.source.publication = { value: "2024-03", precision: "month", basis: "UI-M" };
  job.sources[1]!.source.publication = null;
  job.sources.push({
    ...structuredClone(job.sources[0]!),
    document_id: "paper-earlier",
    source: {
      ...job.sources[0]!.source,
      publication: { value: "2023", precision: "year", basis: "UI-M" },
    },
  });
  job.result!.works[0]!.document_ids.push("paper-earlier");
  const map = technologyMap(job);
  const topic = map.topics[0]!;
  expect(visibleWorks(topic, 2022)).toHaveLength(0);
  expect(visibleWorks(topic, 2023)).toHaveLength(1);
  expect(visibleWorks(topic, 2024)).toHaveLength(1);
  expect(visibleWorks(topic, null)).toHaveLength(2);
  expect(map.works.find((work) => work.kind === "paper")!.source.document_id).toBe("paper-earlier");
  expect(map.years).toContain(2023);
});
