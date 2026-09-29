import { describe, expect, it } from "bun:test";
import type { TFunction } from "i18next";
import { technologyReportDoc } from "./technologyReport";
import type { Technology, TechnologyBrief } from "./technologyApi";

const t = ((key: string) => key) as unknown as TFunction<"trend">;

const technology = {
  technology_id: "tech-1",
  canonical_name: "Твердооксидный электролизёр",
  name_kind: "term",
  description: "Ячейка, разлагающая водяной пар на водород и кислород.",
  validation_status: "proposed",
  method_version: "identity-v1",
  aliases: [{ value: "SOEC", language: "en", basis: "extraction" }],
  mentions: [
    {
      mention_id: "m-1",
      technology_id: "tech-1",
      name: "Твердооксидный электролизёр",
      role: "primary",
      problem: "Снизить расход электроэнергии.",
      mechanism: "Часть энергии подводится теплом.",
      extraction_status: "complete",
      source_revision_id: "doc-1",
      source_title: "Высокотемпературный электролиз",
      source_published_at: "2026-03-10",
      evidence: [
        {
          evidence_id: "e-1",
          locator: { section: "abstract", start_char: 12, end_char: 96 },
          quote: "Расход электроэнергии ниже на четверть.",
        },
      ],
    },
  ],
  readable_revisions: 1,
  restricted_revisions: 0,
} satisfies Technology;

const brief = {
  technology_id: "tech-1",
  title: "Электролиз пара",
  advantage: "Ниже расход электроэнергии, чем у щелочного электролизёра.",
  problem: "Водород дорог из-за электроэнергии.",
  hypothesis: "Если подвести тепло, расход снизится.",
  grounds: "Два источника из трёх.",
  method: "Пилот на стенде.",
  criterion: "Снижение на 15 %.",
  model: "corp/model",
  basis: "Написано по двум источникам.",
  sources: 2,
  written_at: "2026-09-22T10:00:00Z",
} satisfies TechnologyBrief;

const doc = technologyReportDoc({ technology, brief, t });
const kinds = (blocks: typeof doc.blocks) => blocks.map((block) => block.kind);
const texts = (blocks: typeof doc.blocks) =>
  blocks.flatMap((block) => ("text" in block ? [block.text] : []));

describe("technologyReportDoc", () => {
  it("заполняет титул и имя файла", () => {
    const title = doc.blocks.find((block) => block.kind === "doctitle");
    expect(doc.blocks[0]?.kind).toBe("doctitle");
    expect(title?.title).toBe(technology.canonical_name);
    expect(title?.meta).toHaveLength(2);
    expect(doc.fileName).toBe("Твердооксидный электролизёр.docx");
  });

  it("выносит описание и преимущество в титул, гипотезу — блоками", () => {
    expect(doc.blocks[0]?.kind === "doctitle" && doc.blocks[0].lead).toBe(technology.description);
    expect(texts(doc.blocks)).toContain(brief.advantage);
    expect(texts(doc.blocks)).toContain(brief.hypothesis);
  });

  it("без брифа не выдумывает преимущество и гипотезу", () => {
    const bare = technologyReportDoc({ technology, t });
    expect(texts(bare.blocks)).not.toContain(brief.advantage);
    expect(texts(bare.blocks)).not.toContain(brief.hypothesis);
    expect(kinds(bare.blocks)).toContain("references");
  });

  it("доносит цитаты источников до списка источников", () => {
    const references = doc.blocks.find((block) => block.kind === "references");
    expect(references?.items[0]?.text).toContain("Высокотемпературный электролиз");
    expect(texts(doc.blocks)).toContain(technology.mentions[0]!.evidence[0]!.quote);
  });
});
