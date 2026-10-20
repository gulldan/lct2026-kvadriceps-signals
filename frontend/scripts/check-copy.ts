import { readdirSync, readFileSync } from "node:fs";
import { fileURLToPath } from "node:url";

const root = fileURLToPath(new URL("../src/shared/i18n/locales/ru/", import.meta.url));

// Латиница внутри русской фразы: названия источников, продуктов и клавиш, устоявшиеся аббревиатуры;
// венчурные стадии владелец велел оставить по-английски — переводы «посевной» читались хуже.
const ALLOWED_LATIN = [
  "Seed",
  "NEW",
  "arXiv",
  "Gartner",
  "Dealroom",
  "Michael Mullany",
  "Cosmos",
  "CC0",
  "GitHub",
  "Hacker News",
  "TAdviser",
  "CNews",
  "Bing",
  "GraphRAG",
  "RAG",
  "MCP",
  "LLM",
  "RL",
  "GUI",
  "KV",
  "DOI",
  "PDF",
  "CSV",
  "JSON",
  "API",
  "ID",
  "UTC",
  "ACL",
  "USPTO",
  "EPO",
  "HHI",
  "OAI",
  "TRL",
  "KPI",
  "DOCX",
  "Word",
  "Excel",
  "Jira",
  "OpenAlex",
  "PubMed",
  "DataCite",
  "OpenAIRE",
  "Crossref",
  "Scopus",
  "ASJC",
  "Prometheus",
  "Python",
  "README",
  "HTTP",
  "SHA",
  "HyDE",
  "Query2doc",
  "Enter",
  "Home",
  "End",
  "R&D",
  "°C",
  "Ni",
  "Cu",
  "p50",
  "p95",
  ".md",
].toSorted((a, b) => b.length - a.length);

// Ключи, технические по природе (названия тем и статей, имена файлов, поисковые запросы, формулы):
// правило латиницы к ним не применяется. Суффикс «.» — префикс поддерева.
const IGNORE_LATIN = [
  "trend:study.live.topics.",
  "trend:study.live.quickStarts.",
  "trend:study.events.",
  "trend:study.live.searchPlaceholder",
  "trend:study.footnote",
  "trend:study.explanation.population",
  "trend:study.explanation.classification",
  "trend:study.explanation.scienceIntro",
  "trend:demo.hyde",
  "trend:demo.query2doc",
  "trend:demo.useQueryHint",
  "trend:metrics.explain.numerator",
  "trend:metrics.explain.denominator",
  "hypothesis:hreport.conclLearning",
  "hypothesis:hreport.g.fallback",
  "document:upload.noEtag",
  "admin:metrics.trace.stages.",
];
const FILE_NAME_KEY = /fileName$/i;

const escape = (s: string) => s.replace(/[.*+?^${}()|[\]\\]/g, "\\$&");
const allowed = ALLOWED_LATIN.map((a) => new RegExp(`(?<![A-Za-z])${escape(a)}(?![A-Za-z])`, "g"));
const plural = /_(zero|one|two|few|many|other)$/;

function flatten(value: unknown, path = "", out = new Map<string, string>()): Map<string, string> {
  if (typeof value === "string") out.set(path, value);
  else if (value && typeof value === "object")
    for (const [k, v] of Object.entries(value)) flatten(v, path ? `${path}.${k}` : k, out);
  return out;
}

const ignoredLatin = (id: string) =>
  FILE_NAME_KEY.test(id) ||
  IGNORE_LATIN.some((i) => (i.endsWith(".") ? id.startsWith(i) : id === i));

export function checkCopy(ns: string, flat: Map<string, string>): string[] {
  const errors: string[] = [];
  const report = (key: string, rule: string, text: string) =>
    errors.push(`${ns}:${key}\t${rule}\t${JSON.stringify(text)}`);
  const groups = new Map<string, boolean[]>();
  for (const [key, text] of flat) {
    const id = `${ns}:${key}`;
    const russian = /[А-Яа-яЁё]/.test(text);
    if (russian && !ignoredLatin(id)) {
      let rest = text.replace(/\{\{\w+\}\}/g, " ");
      for (const re of allowed) rest = rest.replace(re, " ");
      const latin = rest.match(/[A-Za-z]+/g);
      if (latin) report(key, `latin: ${[...new Set(latin)].join(" ")}`, text);
    }
    if (/ {2}/.test(text) || /\s$/.test(text)) report(key, "spaces", text);
    if (text.includes("...")) report(key, "ellipsis: use …", text);
    const noGost = text.replace(/ГОСТ Р \d+-\d+/g, "");
    if (/\d-\d/.test(noGost)) report(key, "range: use – between digits", text);
    if (/\s–\s/.test(text)) report(key, "range: – without spaces (— between words)", text);
    if (/(\d|\}\}) ?%/.test(text)) report(key, "percent: NBSP before %", text);
    if (russian && text.includes('"')) report(key, "quotes: use «»", text);
    if (plural.test(key)) {
      const base = key.replace(plural, "");
      groups.set(base, [...(groups.get(base) ?? []), text.endsWith(".")]);
    }
  }
  for (const [base, dots] of groups)
    if (dots.some(Boolean) && !dots.every(Boolean))
      report(base, "plural forms: trailing period differs", base);
  return errors;
}

if (import.meta.main) {
  const errors = readdirSync(root)
    .filter((f) => f.endsWith(".json"))
    .flatMap((f) =>
      checkCopy(f.slice(0, -5), flatten(JSON.parse(readFileSync(`${root}${f}`, "utf8")))),
    );
  if (errors.length) {
    console.error(errors.join("\n"));
    process.exit(1);
  }
  console.log("✓ Русские тексты: латиница, пробелы, тире, проценты, кавычки, многоточие");
}
