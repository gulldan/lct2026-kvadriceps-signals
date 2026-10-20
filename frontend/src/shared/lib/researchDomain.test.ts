import { expect, test } from "bun:test";
import { readFileSync } from "node:fs";
import { interleaveBy } from "./interleaveBy";
import { researchDomain } from "./researchDomain";

const cases: { text: string; want: string }[] = JSON.parse(
  readFileSync(
    new URL(
      "../../../../backend/db-service/internal/infrastructure/postgres/testdata/research_topics.json",
      import.meta.url,
    ),
    "utf8",
  ),
);

test("domain ordering groups distinct AI functions and prioritizes applications without inventing unknown fields", () => {
  const topics = [
    "Генеративное ранжирование ответов",
    "Анализ адаптеров больших языковых моделей",
    "Information retrieval algorithms",
    "Neural networks for cancer diagnosis",
    "Machine learning for crop yields",
    "Hydrogen storage using neural networks",
    "Пока неясная область",
    "Другая неуточнённая функция",
  ] as const;
  expect(topics.map((topic) => researchDomain(topic))).toEqual([
    "computing",
    "computing",
    "computing",
    "medicine",
    "agriculture",
    "energy",
    "unknown",
    "unknown",
  ]);
  expect(interleaveBy(topics, (topic) => researchDomain(topic))).toEqual([
    topics[0],
    topics[3],
    topics[4],
    topics[5],
    topics[6],
    topics[1],
    topics[7],
    topics[2],
  ]);
  expect(researchDomain(undefined, "", null)).toBe("unknown");
  expect(researchDomain("Алгоритмы", "", "Очистка сточных вод")).toBe("water");
  expect(researchDomain("Вычисления", "Gene editing using CRISPR")).toBe("biotechnology");
  expect(researchDomain("Reinforcement learning enhancement")).toBe("computing");
  expect(researchDomain("Cement composites")).toBe("materials");
  expect(researchDomain("Logistic regression algorithms for text classification")).toBe(
    "computing",
  );
  expect(researchDomain("Algorithms with additive Gaussian noise")).toBe("computing");
});

test("shared SQL and UI examples cover every category and ambiguous terms", () => {
  for (const entry of cases) expect(researchDomain(entry.text), entry.text).toBe(entry.want);
});
