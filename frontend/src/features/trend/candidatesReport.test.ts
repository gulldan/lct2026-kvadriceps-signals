import { describe, expect, it } from "bun:test";
import { createInstance, type TFunction } from "i18next";
import ru from "@/shared/i18n/locales/ru/trend.json";
import { candidateCountsAgree, candidatesReportDoc } from "./candidatesReport";
import { isConfirmedSignal } from "./signalScore";
import type { LiveDetail, LiveRejection, LiveTopic } from "./liveApi";

const t = ((key: string) => key) as unknown as TFunction<"trend">;

const candidate = (id: string, papers: number): LiveTopic =>
  ({
    id,
    name: id,
    stage: "emerging_signal",
    momentum: "down",
    evidence_level: "strict",
    papers_count: papers,
    records_count: 2,
    first_year: "2025-09",
    paper_ids: ["p1"],
    evidence_ids: [],
    limits: [],
    confidence: { value: 0.857, above_threshold: true },
    card: {
      summary: "что это",
      advantage: "что даёт",
      example: "где проверяли",
      evidence: [
        {
          field: "advantage",
          source_id: "p1",
          quote: "Measured gain under laboratory conditions",
          verified: true,
        },
      ],
    },
  }) as unknown as LiveTopic;

const detail = {
  observed_at: "2026-09-17T06:00:00Z",
  topic: { limits: [] },
  papers: [
    { id: "p1", title: "Первичный источник", date: "2026-03-01", url: "https://example.org/paper" },
  ],
  candidates: [candidate("a", 9), candidate("b", 4)],
  rejected: [
    { name: "Зрелая штука", code: "mature", reason: "рынок сформирован", stage: "established" },
  ],
  counters: { sources_processed: 1195, candidates: 90, above_threshold: 15 },
} as unknown as LiveDetail;

describe("candidatesReportDoc — вся выдача одним документом", () => {
  const doc = candidatesReportDoc({
    detail,
    query: "мембраны для опреснения",
    name: (topic) => `имя:${topic.id}`,
    t,
  });

  it("сводная таблица содержит строку на каждую технологию", () => {
    const table = doc.blocks.find((block) => block.kind === "table");
    expect(table?.rows).toHaveLength(2);
    expect(table?.rows[0]).toEqual([
      "1",
      "имя:a",
      "confidence.value",
      "—",
      "report.unknown",
      "report.marketUnknown",
    ]);
  });

  it("топ сформирован только пятнадцатью строками, а подтверждённые считаются по правилу плашки", async () => {
    const local = createInstance();
    await local.init({ lng: "ru", resources: { ru: { trend: ru } }, defaultNS: "trend" });
    const translate = local.getFixedT("ru", "trend");
    const model: NonNullable<LiveTopic["model_score"]> = {
      value: 0.9,
      threshold: 0.75,
      label: "weak_signal",
      model: "fixture",
      version: "1",
      basis: "Проверка выдачи",
    };
    const paragraphs = (candidates: LiveTopic[]) =>
      candidatesReportDoc({
        detail: { ...detail, candidates },
        query: "проверка",
        name: (topic) => topic.name,
        t: translate,
      }).blocks.flatMap((block) => (block.kind === "paragraph" ? [block.text] : []));
    const rows = Array.from({ length: 15 }, (_, index) => ({
      ...candidate(`technology-${index}`, 1),
      model_score: index < 6 ? model : undefined,
      shortfall: index < 6 ? null : { code: "qualification_budget", reason: "не проверено" },
    }));
    const confirmed = rows.filter((row) => !row.shortfall && isConfirmedSignal(row)).length;
    const title = translate("signalList.incompleteTitle", { limit: 15 });
    // Пятнадцать с помеченным добором — цель выполнена: вместо предупреждения спокойная строка.
    const filled = paragraphs(rows);
    expect(filled).toContain(translate("signalList.toppedUp", { count: confirmed, limit: 15 }));
    expect(filled.some((text) => text.startsWith(title))).toBe(false);
    // Четырнадцать — топ не сформирован, как бы ни были уверены оценки.
    expect(paragraphs(rows.slice(0, 14)).some((text) => text.startsWith(title))).toBe(true);
  });
  it("исключённые с причинами попадают в выгрузку", async () => {
    const items = doc.blocks.flatMap((block) => (block.kind === "list" ? block.items : []));
    expect(items).toContain("Зрелая штука — рынок сформирован");
    const local = createInstance();
    await local.init({ lng: "ru", resources: { ru: { trend: ru } }, defaultNS: "trend" });
    const rejection: LiveRejection = {
      name: "Проверенный кандидат",
      code: "classifier_rejected",
      reason: "Оценка обученной модели ниже её порога",
      stage: "unassessed",
      papers: 3,
      confidence: 57,
    };
    const unchecked: LiveRejection = {
      ...rejection,
      name: "Фрагмент",
      code: "qualification_budget",
      reason: "не проверено: исчерпан общий срок",
    };
    const report = candidatesReportDoc({
      detail: { ...detail, rejected: [rejection, unchecked] },
      query: "проверка",
      name: (topic) => topic.name,
      t: local.getFixedT("ru", "trend"),
    });
    expect(report.blocks).toContainEqual({
      kind: "subheading",
      text: "Отклонено обученной моделью · 1",
    });
    // Непроверенный кандидат не прибавляется к исключённым: 1 + 1, а не «исключено 2».
    const headings = report.blocks.flatMap((block) =>
      block.kind === "heading" ? [block.text] : [],
    );
    expect(headings).toContain("Исключены по существу: 1");
    expect(headings).toContain("Не успели проверить: 1");
    const coverage = report.blocks.flatMap((block) => (block.kind === "kv" ? block.rows : []));
    expect(coverage).toContainEqual({ label: "Исключено по существу", value: "1" });
    expect(coverage).toContainEqual({ label: "Не успели проверить", value: "1" });
    expect(report.blocks).toContainEqual({
      kind: "list",
      items: [`${rejection.name} — ${rejection.reason}`],
    });
    expect(JSON.stringify(report.blocks)).not.toContain("classifier_rejected");
  });

  it("полные карточки сохраняют ссылки и цитаты, а не только выводы", () => {
    const sources = doc.blocks.flatMap((block) => (block.kind === "references" ? block.items : []));
    expect(sources.some((source) => source.url === "https://example.org/paper")).toBe(true);
    const quotes = doc.blocks.flatMap((block) => (block.kind === "quote" ? [block.text] : []));
    expect(quotes).toContain("Measured gain under laboratory conditions");
    const texts = doc.blocks.flatMap((block) => (block.kind === "paragraph" ? [block.text] : []));
    expect(texts).toContain("report.modelUnavailable");
    expect(texts).toContain("signalList.countMismatch");
    expect(candidateCountsAgree(detail)).toBe(false);
    expect(
      candidateCountsAgree({
        ...detail,
        counters: { ...detail.counters!, candidates: 3, above_threshold: 2 },
      }),
    ).toBe(true);
  });
});
