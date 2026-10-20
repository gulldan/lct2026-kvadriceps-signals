import { expect, test } from "bun:test";
import {
  researchCategoryLabel,
  researchTaxonomy,
  sortedResearchCategories,
} from "./researchTaxonomy";

const labels = (language: string) =>
  sortedResearchCategories(researchTaxonomy.categories, language).map((category) =>
    researchCategoryLabel(category, language),
  );

test("industries follow the alphabet of the interface language, not rule priority", () => {
  expect(labels("ru").slice(0, 2)).toEqual(["Биотехнологии", "Благополучие и красота"]);
  expect(labels("en").slice(0, 2)).toEqual(["Agriculture", "Biotechnology"]);
  expect(labels("ru")).toHaveLength(researchTaxonomy.categories.length);
});
