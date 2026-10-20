import catalog from "./researchTaxonomy.json";

export const researchTaxonomy = catalog;
export type ResearchCategory = (typeof catalog.categories)[number];

export function researchCategoryLabelById(id: string | undefined, language: string) {
  const category = catalog.categories.find((item) => item.id === id);
  return category ? researchCategoryLabel(category, language) : undefined;
}

export function researchCategoryLabel(category: ResearchCategory, language: string) {
  return category.labels[language === "en" || language === "zh" ? language : "ru"];
}

/** Список отраслей ищут глазами: порядок — алфавит языка интерфейса, а не приоритет правил. */
export function sortedResearchCategories(
  categories: readonly ResearchCategory[],
  language: string,
) {
  return categories.toSorted((a, b) =>
    researchCategoryLabel(a, language).localeCompare(researchCategoryLabel(b, language), language),
  );
}
