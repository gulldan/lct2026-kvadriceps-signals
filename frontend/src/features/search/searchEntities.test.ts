import { expect, test } from "bun:test";
import { catalogueHits, matchTopics } from "./searchEntities";

test("связанные технологии сохраняют полный вопрос и не обходят порог релевантности", async () => {
  const question =
    "Какие ограничения выявил бенчмарк TeleAntiFraud 2.0 при обнаружении мошенничества?";
  const queries: string[] = [];
  const originalFetch = globalThis.fetch;
  globalThis.fetch = (async (input: RequestInfo | URL) => {
    const url = new URL(String(input), "https://app.example");
    queries.push(url.searchParams.get("q") ?? "");
    return Response.json({ reason: "below_relevance_floor", technologies: [] });
  }) as typeof fetch;
  try {
    expect(await catalogueHits(question, new AbortController().signal)).toEqual([]);
    expect(queries).toEqual([question]);
  } finally {
    globalThis.fetch = originalFetch;
  }
  const topic = { source: "curated" as const, name: "Смысловая память агентов" };
  const name = () => topic.name;
  expect(matchTopics([topic], "Ограничения памяти устройства", name)).toEqual([]);
  expect(matchTopics([topic], "Как работает смысловая память агентов?", name)).toEqual([topic]);
});
