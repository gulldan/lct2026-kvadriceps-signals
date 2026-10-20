import { readdirSync, readFileSync } from "node:fs";
import { fileURLToPath } from "node:url";
import { tokenCycles, validateDesignSource } from "./design-rules";

const root = fileURLToPath(new URL("../", import.meta.url));
const files = readdirSync(`${root}src`, { recursive: true })
  .map(String)
  .filter((file) => /\.(tsx?|css)$/.test(file) && !/\.(test|d)\.ts$/.test(file));
const sources = new Map(
  files.map((file) => [`src/${file}`, readFileSync(`${root}src/${file}`, "utf8")]),
);
const css = [...sources]
  .filter(([file]) => file.endsWith(".css"))
  .map(([, source]) => source)
  .join("\n");
const tokens = new Set([...css.matchAll(/(--[\w-]+)\s*:/g)].map((match) => match[1]!));
const errors: string[] = [];
for (const [file, source] of sources) {
  for (const item of validateDesignSource(file, source, tokens))
    errors.push(`${file}:${item.line}: ${item.rule}: ${item.message}`);
}
const tokenSource = sources.get("src/shared/design-system/tokens.css")!;
for (const cycle of tokenCycles(tokenSource)) errors.push(`tokens.css: token-cycle: ${cycle}`);

const catalog = [...sources]
  .filter(([file]) => file.startsWith("src/ui-kit/"))
  .map(([, source]) => source)
  .join("\n");
for (const file of sources.keys()) {
  if (!/^src\/shared\/ui\/[^/]+\.tsx$/.test(file)) continue;
  const module = file.slice("src/".length, -4);
  // Ленивые реализации покрыты через свои публичные компоненты.
  if (/\/(RichText|PdfViewer)Inner$/.test(module)) continue;
  if (!catalog.includes(`@/${module}"`))
    errors.push(`${file}: catalog-coverage: Добавьте живой пример компонента в src/ui-kit.`);
}
if (errors.length) {
  console.error(errors.join("\n"));
  process.exit(1);
}
console.log(
  `✓ UI kit: цвета, ссылки токенов, ${tokens.size} токенов, границы компонентов и покрытие каталога`,
);
