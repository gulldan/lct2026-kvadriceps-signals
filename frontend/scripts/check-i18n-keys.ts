import { existsSync, readdirSync, readFileSync } from "node:fs";
import { fileURLToPath } from "node:url";

const root = fileURLToPath(new URL("../src/shared/i18n/locales/", import.meta.url));
const languages = ["en", "zh"];
const plural = /_(zero|one|two|few|many|other)$/;

function flatten(value: unknown, path = "", out = new Map<string, string>()): Map<string, string> {
  if (typeof value === "string") out.set(path, value);
  else if (value && typeof value === "object")
    for (const [k, v] of Object.entries(value)) flatten(v, path ? `${path}.${k}` : k, out);
  return out;
}

function read(lang: string, ns: string): Map<string, string> | null {
  const file = `${root}${lang}/${ns}.json`;
  return existsSync(file) ? flatten(JSON.parse(readFileSync(file, "utf8"))) : null;
}

const placeholders = (s: string) => new Set([...s.matchAll(/\{\{(\w+)\}\}/g)].map((m) => m[1]!));

function collapse(
  flat: Map<string, string>,
): Map<string, { texts: string[]; placeholders: Set<string> }> {
  const out = new Map<string, { texts: string[]; placeholders: Set<string> }>();
  for (const [key, text] of flat) {
    const base = key.replace(plural, "");
    const entry = out.get(base) ?? { texts: [], placeholders: new Set<string>() };
    entry.texts.push(text);
    for (const p of placeholders(text)) entry.placeholders.add(p);
    out.set(base, entry);
  }
  return out;
}

const errors: string[] = [];
for (const file of readdirSync(`${root}ru`).filter((f) => f.endsWith(".json"))) {
  const ns = file.slice(0, -5);
  const ru = read("ru", ns)!;
  for (const [key, text] of ru)
    if (text.trim() === "") errors.push(`ru/${ns}:${key}: empty string`);
  const ruBase = collapse(ru);
  for (const lang of languages) {
    const flat = read(lang, ns);
    if (!flat) {
      errors.push(`${lang}/${ns}.json: missing namespace file`);
      continue;
    }
    for (const [key, text] of flat)
      if (text.trim() === "") errors.push(`${lang}/${ns}:${key}: empty string`);
    const base = collapse(flat);
    for (const key of ruBase.keys())
      if (!base.has(key)) errors.push(`${lang}/${ns}:${key}: missing key`);
    for (const key of base.keys())
      if (!ruBase.has(key)) errors.push(`${lang}/${ns}:${key}: extra key (absent in ru)`);
    for (const [key, entry] of ruBase) {
      const other = base.get(key);
      if (!other) continue;
      for (const p of entry.placeholders)
        if (!other.placeholders.has(p))
          errors.push(`${lang}/${ns}:${key}: placeholder {{${p}}} missing`);
    }
  }
}

if (errors.length) {
  console.error(errors.join("\n"));
  process.exit(1);
}
console.log("✓ Ключи и плейсхолдеры словарей совпадают в ru / en / zh");
