// Межфайловым границам нужно разрешение путей; структурные правила UI — в ast-grep.
import { readdirSync, readFileSync } from "node:fs";
import { dirname, normalize, join } from "node:path";
import { fileURLToPath } from "node:url";
import { parseSync, Visitor } from "oxc-parser";

export function validateImports(file: string, source: string): string[] {
  const errors: string[] = [];
  const shared = file.startsWith("src/shared/");
  const feature = /^src\/features\/([^/]+)\//.exec(file)?.[1];
  const check = (imported: string, position: number) => {
    const spec = imported.startsWith(".")
      ? normalize(join(dirname(file), imported)).replace(/^src\//, "@/")
      : imported;
    const line = source.slice(0, position).split("\n").length;
    const report = (rule: string, message: string) =>
      errors.push(`${file}:${line}: ${rule}: ${message}`);
    if (/^@\/app(?:\/|$)/.test(spec) && (shared || feature))
      report("no-import-app", "app — корень композиции; shared и features не зависят от него.");
    if (spec.startsWith("@/features/")) {
      const target = spec.split("/")[2];
      if (shared) report("shared-no-features", "shared не должен импортировать features.");
      else if (feature && feature !== target && spec !== `@/features/${target}`)
        report("cross-feature-via-barrel", `Используйте публичный модуль @/features/${target}.`);
    }
  };
  const parsed = parseSync(file, source);
  for (const error of parsed.errors) errors.push(`${file}: valid-syntax: ${error.message}`);
  new Visitor({
    ImportDeclaration: (node) => check(node.source.value, node.start),
    ExportNamedDeclaration: (node) => {
      if (node.source) check(node.source.value, node.start);
    },
    ExportAllDeclaration: (node) => check(node.source.value, node.start),
    ImportExpression: (node) => {
      if (node.source.type === "Literal" && typeof node.source.value === "string")
        check(node.source.value, node.start);
    },
    CallExpression: (node) => {
      const arg = node.arguments[0];
      if (
        node.callee.type === "Identifier" &&
        node.callee.name === "require" &&
        arg?.type === "Literal" &&
        typeof arg.value === "string"
      )
        check(arg.value, node.start);
    },
  }).visit(parsed.program);
  return errors;
}

if (import.meta.main) {
  const root = fileURLToPath(new URL("../", import.meta.url));
  const errors = readdirSync(`${root}src`, { recursive: true })
    .map(String)
    .filter((file) => /\.tsx?$/.test(file) && !file.endsWith(".d.ts"))
    .flatMap((file) => validateImports(`src/${file}`, readFileSync(`${root}src/${file}`, "utf8")));
  if (errors.length) {
    console.error(errors.join("\n"));
    process.exit(1);
  }
  console.log("✓ Границы shared / features / app соблюдены");
}
