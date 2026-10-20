import type { TFunction } from "i18next";
import { sourcePath } from "@/features/document";
import type { ReportDoc } from "@/shared/report/blocks";
import { formatDate } from "@/shared/lib/format";
import type { Mention, Technology, TechnologyBrief } from "./technologyApi";
import { ALIAS_BASES, VALIDATION_STATUSES, known } from "./technologyLabels";
import { groupBySource } from "./ui/TechnologySources";

type StatementRow = ["problem" | "mechanism" | "inputs" | "outputs", string];

export function distinctStatements(mentions: Mention[]) {
  const seen = new Set<string>();
  const out: { mention: Mention; rows: StatementRow[] }[] = [];
  for (const mention of mentions) {
    const rows: StatementRow[] = [];
    if (mention.problem) rows.push(["problem", mention.problem]);
    if (mention.mechanism) rows.push(["mechanism", mention.mechanism]);
    if (mention.inputs?.length) rows.push(["inputs", mention.inputs.join(", ")]);
    if (mention.outputs?.length) rows.push(["outputs", mention.outputs.join(", ")]);
    const key = rows.map(([label, value]) => `${label}=${value}`).join("\n");
    if (rows.length === 0 || seen.has(key)) continue;
    seen.add(key);
    out.push({ mention, rows });
  }
  return out;
}

/** Относительный путь Word не открывает, поэтому приписывается адрес стенда. */
const sourceLink = (id: string) =>
  typeof window === "undefined" ? undefined : window.location.origin + sourcePath(id);

interface TechnologyReportInput {
  technology: Technology;
  brief?: TechnologyBrief | null;
  t: TFunction<"trend">;
}

/** Пустое поле раздела не рождает: прочерк в файле читается как измеренный ноль. */
export function technologyReportDoc({ technology, brief, t }: TechnologyReportInput): ReportDoc {
  const status = known(VALIDATION_STATUSES, technology.validation_status);
  const groups = groupBySource(technology.mentions);
  const dates = groups.map((group) => group.date).filter(Boolean);
  const statements = distinctStatements(technology.mentions);
  const blocks: ReportDoc["blocks"] = [
    {
      kind: "doctitle",
      org: null,
      doctype: t("technology.report.docType"),
      title: technology.canonical_name,
      meta: [
        `${t("technology.report.status")}: ${
          status ? t(`technology.status.${status}`) : technology.validation_status
        }`,
        `${t("technology.report.exportedAt")}: ${formatDate(new Date().toISOString())}`,
      ],
      lines: [],
      lead: technology.description || undefined,
      about: brief?.advantage ? t("technology.brief.title") : undefined,
      text: brief?.advantage || undefined,
      metrics: [
        { value: String(groups.length), label: t("technology.report.sources") },
        { value: String(technology.mentions.length), label: t("technology.report.mentions") },
      ],
    },
  ];
  if (brief?.advantage) {
    blocks.push({
      kind: "paragraph",
      muted: true,
      italic: true,
      text:
        t("report.writtenBy", { model: brief.model }) + (brief.basis ? ` · ${brief.basis}` : ""),
    });
  }
  const fields = (["problem", "hypothesis", "grounds", "method", "criterion"] as const).filter(
    (field) => brief?.[field],
  );
  if (brief && fields.length) {
    blocks.push({ kind: "heading", text: t("technology.implementation.title") });
    for (const field of fields) {
      blocks.push({ kind: "subheading", text: t(`technology.implementation.fields.${field}`) });
      blocks.push({ kind: "paragraph", text: brief[field] });
    }
  }
  if (statements.length) {
    blocks.push({ kind: "heading", text: t("technology.statements.title") });
    for (const { mention, rows } of statements) {
      blocks.push({
        kind: "kv",
        rows: rows.map(([label, value]) => ({
          label: t(`technology.statements.${label}`),
          value,
        })),
      });
      blocks.push({
        kind: "paragraph",
        muted: true,
        text: t("technology.statements.from", {
          source: mention.source_title || t("technology.sources.untitled"),
        }),
      });
    }
  }
  if (technology.aliases.length) {
    blocks.push({ kind: "heading", text: t("technology.aliases.title") });
    blocks.push({
      kind: "list",
      items: technology.aliases.map((alias) => {
        const basis = known(ALIAS_BASES, alias.basis);
        const label = basis ? t(`technology.aliases.basis.${basis}`) : alias.basis;
        const language = t(`source.languages.${alias.language}`, {
          defaultValue: alias.language,
        });
        return `${alias.value} · ${language} · ${label}`;
      }),
    });
  }
  if (dates.length) {
    blocks.push({ kind: "heading", text: t("technology.dates.title") });
    blocks.push({
      kind: "kv",
      rows: [
        { label: t("technology.dates.first"), value: formatDate(dates.at(-1) ?? "") },
        { label: t("technology.dates.last"), value: formatDate(dates[0] ?? "") },
      ],
    });
  }
  blocks.push({ kind: "heading", text: t("report.sources", { count: groups.length }) });
  blocks.push({
    kind: "references",
    items: groups.map((group, index) => ({
      n: index + 1,
      url: sourceLink(group.id),
      text: `${group.title || t("technology.sources.untitled")} · ${
        group.date ? formatDate(group.date) : t("technology.sources.dateUnknown")
      }`,
    })),
  });
  for (const [index, group] of groups.entries()) {
    const source = `${t("report.sourceNumber", { n: index + 1 })} ${
      group.title || t("technology.sources.untitled")
    }`;
    for (const quote of group.mentions.flatMap((mention) => mention.evidence))
      blocks.push({ kind: "quote", source, text: quote.quote });
  }
  if (technology.restricted_revisions > 0)
    blocks.push({
      kind: "paragraph",
      muted: true,
      text: t("technology.restricted", { count: technology.restricted_revisions }),
    });
  return {
    fileName: `${technology.canonical_name.replaceAll(/[^\p{L}\p{N} _-]/gu, "").trim() || "technology"}.docx`,
    title: technology.canonical_name,
    blocks,
  };
}
