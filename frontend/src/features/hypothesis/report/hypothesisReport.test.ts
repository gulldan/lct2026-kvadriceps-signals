import { beforeAll, expect, test } from "bun:test";
import { i18n, initI18n, loadNamespace } from "@/shared/i18n";
import type { ApiHypothesis } from "@/features/hypothesis/api";
import { createData } from "../../../../tests/product/data";
import { buildHypothesisReport } from "./hypothesisReport";
import { firstSentence, trimSnippet } from "./reportData";
import { radarReportBlocks, type RadarProvenance } from "../radarProvenance";
import { hypothesisClusterId, hypothesisItc } from "../model";
import { SORTS } from "../board/model";
import { formatItcIndex } from "../itcProfile";

beforeAll(async () => {
  const descriptor = Object.getOwnPropertyDescriptor(globalThis, "document");
  Object.defineProperty(globalThis, "document", {
    configurable: true,
    value: { documentElement: { lang: "" } },
  });
  try {
    await initI18n();
    await loadNamespace("hypothesis");
  } finally {
    if (descriptor) Object.defineProperty(globalThis, "document", descriptor);
    else Reflect.deleteProperty(globalThis, "document");
  }
});

test("аннотация сохраняет длинное предложение с числом, а сокращённая цитата явно помечена", () => {
  const sentence = `${"Условия сложной оценки детекции телефонного мошенничества требуют проверки. ".repeat(4).replaceAll(". ", "; ")}Macro-F1 снизился с 0.95 до 0.65–0.68.`;
  const next = "Следующее предложение содержит дополнительные подробности.";
  const problem = `${sentence} ${next}`;
  const base = createData().hypotheses[0]!;
  const h = { ...base, detail: { problem_addressed: problem }, assessment: {} };
  const { blocks } = buildHypothesisReport({ h, revisions: [] });
  expect(sentence.length).toBeGreaterThan(240);
  expect(firstSentence(problem)).toBe(sentence);
  expect(firstSentence("Short. Another sentence.")).toBe("Short.");
  const abstract = blocks.findIndex(
    (block) =>
      block.kind === "heading" && block.text === i18n.t("hypothesis:hreport.abstractTitle"),
  );
  expect(blocks[abstract + 1]).toMatchObject({
    kind: "paragraph",
    text: expect.stringContaining(sentence),
  });
  expect(blocks[abstract + 1]).toMatchObject({ text: expect.not.stringContaining(next) });
  const snippet = trimSnippet("Измерение ".repeat(60));
  expect(snippet).toEndWith("Измерение…");
  expect(snippet.length).toBeLessThanOrEqual(401);

  const radar: RadarProvenance = {
    version: "radar-transfer-v1",
    query: "fraud",
    candidate_id: "fraud",
    name: "Fraud benchmark",
    observed_at: "2026-09-27T00:00:00Z",
    method: "live-signals-v8",
    sources: [
      {
        id: "source",
        title: "Benchmark",
        url: "https://example.org/source",
        date: "2026-09-27",
        digest: "В тестах выявлены проблемы с коллапсом предсказаний и",
        quotes: [],
      },
    ],
  };
  const provenance = radarReportBlocks(radar);
  expect(provenance).toContainEqual({
    kind: "paragraph",
    text: "В тестах выявлены проблемы с коллапсом предсказаний и…",
  });
  expect(
    provenance.some(
      (block) =>
        block.kind === "paragraph" &&
        block.text.includes(i18n.t("hypothesis:radar.digestIncomplete")),
    ),
  ).toBe(true);
});

test("отчёт сохраняет нумерацию необязательных разделов и единые ссылки на цитируемый документ", () => {
  const base = createData().hypotheses[0]!;
  for (const includePlan of [false, true]) {
    const h: ApiHypothesis = {
      ...base,
      assessment: {},
      detail: {
        experiment_plan: includePlan ? {} : undefined,
        competitors: { items: [{ name: "Аналог" }] },
      },
      evidence: Array.from({ length: 8 }, (_, index) => ({
        ...base.evidence[0]!,
        id: `quote-${index}`,
        document_id: "same-document",
        filename: "evidence.pdf",
        stance: "supports",
        score: index / 10,
        snippet: `Цитата ${index}`,
      })),
    };
    const { blocks } = buildHypothesisReport({ h, revisions: [] });
    const numbered = blocks.filter((block) => block.kind === "heading" && /^\d+ /.test(block.text));
    const count = includePlan ? 5 : 4;
    expect(numbered).toHaveLength(count);
    expect(numbered.at(-1)).toEqual({
      kind: "heading",
      text: `${count} ${i18n.t("hypothesis:hreport.sec5Title")}`,
    });
    const quotes = blocks.filter((block) => block.kind === "quote");
    expect(quotes).toHaveLength(6);
    expect(quotes.map((quote) => quote.text)).toEqual(
      [7, 6, 5, 4, 3, 2].map((index) => `Цитата ${index}`),
    );
    expect(quotes.every((quote) => quote.source.startsWith("[1]"))).toBe(true);
    const references = blocks.find((block) => block.kind === "references");
    expect(references?.items).toHaveLength(1);
    expect(references?.items[0]?.n).toBe(1);
  }
});

test("проекция ближайшей темы не становится индексом гипотезы в сортировке и экспортируемом отчёте", () => {
  const data = createData();
  const kpi = data.kpis[0]!;
  for (const scope of [
    "hypothesis(theme)",
    "hypothesis(document_membership)",
    "hypothesis",
    undefined,
  ]) {
    const h = structuredClone(data.hypotheses[0]!);
    h.novelty_score = 0.11;
    const itc = h.assessment.itc!;
    itc.scope = scope;
    itc.techscore = 0.93;
    itc.axes.novelty = 0.97;
    const raw = structuredClone(h);
    const projected = scope === "hypothesis(theme)";
    const copiedIndex = projected || scope === "hypothesis(document_membership)";
    expect(hypothesisClusterId(h)).toBe(projected ? undefined : h.primary_cluster_id);
    expect(hypothesisItc(h)).toBe(copiedIndex ? undefined : itc);
    expect(SORTS.find((sort) => sort.key === "itc")!.value(h)).toBe(copiedIndex ? 0 : 0.93);
    expect(SORTS.find((sort) => sort.key === "novelty")!.value(h)).toBe(copiedIndex ? 0.11 : 0.97);
    const report = buildHypothesisReport({
      h,
      kpi,
      clusterTitle: "Проекционная флотация",
      revisions: [],
    });
    const exported = JSON.stringify(report.blocks);
    expect(exported.includes("Проекционная флотация")).toBe(!projected);
    expect(exported.includes(`${formatItcIndex(itc)}/100`)).toBe(!copiedIndex);
    expect(exported).toContain(kpi.title);
    expect(h).toEqual(raw);
  }
});
