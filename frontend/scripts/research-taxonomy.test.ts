import { expect, test } from "bun:test";
import { readFileSync } from "node:fs";
import catalog from "../src/shared/lib/researchTaxonomy.json";
import { generatedResearchTaxonomy } from "./research-taxonomy";

test("shared catalog generates the deployed SQL and fallback labels without drift", () => {
  expect(
    catalog.categories.filter((category) => category.basis === "dealroom_industry"),
  ).toHaveLength(31);
  expect(catalog.categories.filter((category) => category.extension)).toHaveLength(6);
  for (const category of catalog.categories) {
    for (const parent of category.parent_ids) {
      const parentCategory = catalog.categories.find((candidate) => candidate.id === parent);
      expect(parentCategory).toBeDefined();
      expect(category.priority).toBeLessThan(parentCategory!.priority);
    }
  }
  for (const [path, content] of generatedResearchTaxonomy())
    expect(readFileSync(path, "utf8")).toBe(content);
});
