import { researchTaxonomy } from "./researchTaxonomy";

// Проценты обозначают произвольный фрагмент, остальные символы — буквальный текст, как в SQL.
const matchers = researchTaxonomy.categories
  .toSorted((a, b) => a.priority - b.priority)
  .map((category) => ({
    id: category.id,
    pattern: new RegExp(
      category.patterns
        .map((pattern) => pattern.replace(/[.*+?^${}()|[\]\\]/g, "\\$&").replaceAll("%", ".*"))
        .join("|"),
      "is",
    ),
  }));

/** Одна предварительная категория: узкая область важнее общего метода, неизвестное не угадываем. */
export function researchDomain(...fields: (string | null | undefined)[]) {
  const text = ` ${fields.filter(Boolean).join(" ")} `;
  return matchers.find(({ pattern }) => pattern.test(text))?.id ?? "unknown";
}
