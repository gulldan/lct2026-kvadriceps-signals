import { readFileSync, writeFileSync } from "node:fs";
import { fileURLToPath } from "node:url";
import catalog from "../src/shared/lib/researchTaxonomy.json";

const postgres = new URL(
  "../../backend/db-service/internal/infrastructure/postgres/",
  import.meta.url,
);
const application = new URL("../../backend/main-service/internal/application/", import.meta.url);
const quote = (value: string) => `'${value.replaceAll("'", "''")}'`;

/** Выпущенные миграции неизменны: следующая версия каталога должна получить новую миграцию. */
export function generatedResearchTaxonomy() {
  const categories = catalog.categories.toSorted((a, b) => a.priority - b.priority);
  if (
    new Set(categories.map((category) => category.id)).size !== categories.length ||
    new Set(categories.map((category) => category.priority)).size !== categories.length ||
    categories.some(
      (category) =>
        !category.patterns.length || category.patterns.some((term) => /[_\\]/.test(term)),
    )
  )
    throw new Error("Invalid taxonomy ids, priorities or LIKE patterns");
  const prior = readFileSync(new URL("migrations/00057_research_taxonomy.sql", postgres), "utf8")
    .split("-- +goose StatementBegin\n")[1]!
    .split("-- +goose StatementEnd")[0]!
    .replace("CREATE FUNCTION", "CREATE OR REPLACE FUNCTION");
  const cases = categories
    .map(
      (category) =>
        ` WHEN normalized LIKE ANY(ARRAY[\n  ${category.patterns.map((term) => quote(`%${term}%`)).join(",")}\n ]) THEN ${quote(category.id)}`,
    )
    .join("\n");
  const sql = `-- +goose Up
-- Создано из frontend/src/shared/lib/researchTaxonomy.json; порядок сохраняет одну категорию на запись.
-- Родительские категории получают только остаток после исследовательских подрубрик.
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION research_topic(input text) RETURNS text
LANGUAGE plpgsql IMMUTABLE PARALLEL SAFE AS $$
DECLARE normalized text := ' ' || lower(coalesce(input,'')) || ' ';
BEGIN
 RETURN CASE
${cases}
 ELSE '' END;
END;
$$;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
${prior}-- +goose StatementEnd
`;
  const labelWidth = Math.max(...catalog.categories.map((category) => category.id.length));
  const labels = catalog.categories
    .map(
      (category) =>
        `\t\t"${category.id}":${" ".repeat(labelWidth - category.id.length + 1)}"${category.labels.ru}",`,
    )
    .join("\n");
  const go = `package postgres

// Создано командой bun run taxonomy:generate; источник — researchTaxonomy.json.
const documentTopicMethod = "${catalog.method}"

func documentTopicLabel(id string) string {
\treturn map[string]string{
${labels}
\t}[id]
}
`;
  if (categories.some((category) => category.patterns.some((term) => term.includes("`"))))
    throw new Error("A backtick would end the Go raw string");
  const rubrics = categories
    .map((category) => {
      const head = `  {"id": ${JSON.stringify(category.id)}, "label": ${JSON.stringify(category.labels.ru)}, "patterns": [`;
      return `${head}\n${category.patterns.map((term) => `    ${JSON.stringify(term)}`).join(",\n")}\n  ]}`;
    })
    .join(",\n");
  const rubricsGo = `package application

// Создано командой bun run taxonomy:generate; источник — researchTaxonomy.json, порядок — приоритет.
// Каталог хранится одной строкой JSON: так шаблоны не размножают литералы по пакету.
const liveRubricMethod = "${catalog.method}"

const liveRubricCatalog = \`[
${rubrics}
]\`
`;
  return new Map([
    [fileURLToPath(new URL("migrations/00058_research_topic_homonyms.sql", postgres)), sql],
    [fileURLToPath(new URL("document_topic_labels.go", postgres)), go],
    [fileURLToPath(new URL("trend_live_rubrics.go", application)), rubricsGo],
  ]);
}

if (import.meta.main) {
  for (const [path, content] of generatedResearchTaxonomy()) writeFileSync(path, content);
  console.log(`Generated SQL and Go labels for ${catalog.categories.length} research categories`);
}
