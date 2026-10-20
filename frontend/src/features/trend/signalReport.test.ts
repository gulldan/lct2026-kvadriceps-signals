import { describe, expect, it } from "bun:test";
import { createInstance, type TFunction } from "i18next";
import ru from "@/shared/i18n/locales/ru/trend.json";
import {
  directionReportDoc,
  growthLabel,
  marketLabel,
  signalReportDoc,
  verdictReasons,
} from "./signalReport";
import { signalScore, signalVerdict, thresholdLabelKey } from "./signalScore";
import { readableReason, signalSources } from "./signalSources";
import type { LiveDetail, LiveTopic } from "./liveApi";

const t = ((key: string) => key) as unknown as TFunction<"trend">;

const candidate = {
  id: "ngram:anti-wetting",
  name: "Anti Wetting",
  label: "Мембраны против смачивания",
  stage: "emerging_signal",
  source: "candidate",
  evidence_level: "relaxed",
  papers_count: 9,
  records_count: 0,
  groups: 3,
  first_year: "2025-09",
  paper_ids: ["p1", "p2"],
  evidence_ids: [],
  external: [],
  limits: [],
  series: [],
  card: {
    summary: "что это",
    advantage: "что даёт",
    example: "где пробовали",
    report_assessment: "Оценка из аналитического отчёта с указанным автором",
    example_id: "p1",
    model: "m",
    verification_model: "verifier-v1",
    evidence: [
      {
        field: "advantage",
        source_id: "p1",
        quote: "Lower loss in laboratory conditions",
        verified: true,
      },
      {
        field: "report_assessment",
        source_id: "p1",
        quote: "The analyst report expects limited adoption in the near term",
        verified: true,
      },
    ],
  },
  confidence: {
    value: 6 / 7,
    passed: 6,
    total: 7,
    method: "rules",
    basis: "основание",
    threshold: 0.75,
    reasons: ["правило выполнено"],
    missing: ["правило не выполнено"],
    above_threshold: true,
    unmeasured: ["рыночная стадия не подтверждена"],
  },
  query_relevance: 0.42,
  momentum: "down",
  growth: { delta_pp: -0.4 },
  comparison: { prior: 16, current: 5 },
  adoption: { code: "pilot", rank: 3, documents: 9, matched: 2, basis: "признак стадии в 2 из 9" },
  market: {
    known: true,
    mature: false,
    basis: "набрано 0 признаков из 2",
    checks: ["издателей 2 при пороге 6"],
  },
} as unknown as LiveTopic;

const detail = {
  observed_at: "2026-09-16T00:00:00Z",
  method: "live-signals-v7",
  relevance_floor: 0.25,
  topic: { ...candidate, limits: [] },
  papers: [
    { id: "p1", title: "Older paper", date: "2025-09-13", url: "https://a" },
    { id: "p2", title: "Newer paper", date: "2026-04-21", url: "https://b" },
  ],
  passports: {
    p2: {
      publisher_type: "aggregator",
      genre: "news",
      source_name: "Example Research Journal",
      discovery_source: "openalex",
      language: "en",
      trust: "medium",
      trust_basis: "метаданные агрегатора",
      language_basis: "метаданные языка",
      digest: "Русская интерпретация",
      digest_model: "digest-v1",
    },
  },
  counters: { sources_processed: 1195, candidates: 90, above_threshold: 15 },
} as unknown as LiveDetail;

function scored(report: ReturnType<typeof signalReportDoc>, heading = "report.howScored") {
  const at = report.blocks.findIndex((b) => b.kind === "heading" && b.text === heading);
  const kv = report.blocks[at + 1];
  return kv?.kind === "kv" ? kv.rows : [];
}

describe("signalReportDoc — выгрузка повторяет требования к выдаче", () => {
  const doc = signalReportDoc({
    detail,
    candidate,
    query: "мембраны для опреснения",
    rank: 1,
    total: 15,
    name: "Мембраны против смачивания",
    branchLabel: "Материалы и химия",
    t,
  });
  const headings = doc.blocks.filter((b) => b.kind === "heading").map((b) => b.text);
  const title = doc.blocks[0];
  const value = (label: string) => scored(doc).find((row) => row.label === label)?.value ?? "";

  it("открывается титулом доски: факты, итог, описание и четыре числа", () => {
    expect(title).toMatchObject({
      kind: "doctitle",
      title: "Мембраны против смачивания",
      about: "report.summary",
      text: "что это",
    });
    if (title?.kind !== "doctitle") throw new Error("нет титула");
    expect(title.facts?.map((fact) => fact.label)).toEqual([
      "report.factQuery",
      "report.factRank",
      "report.area",
      "report.originalName",
    ]);
    expect(title.metrics?.map((metric) => metric.label)).toEqual([
      "confidence.label",
      "tz.scoreTitle",
      "figures.recent",
      "figures.events",
    ]);
    expect(title.lead).toContain("report.verdictNoEvents");
  });

  it("содержит обязательные разделы карточки", () => {
    for (const key of [
      "report.howScored",
      "report.advantage",
      "report.example",
      "report.study_limitations",
      "report.report_assessment",
      "report.whySignal",
      "report.aboutMethod",
    ])
      expect(headings).toContain(key);
    expect(headings.some((h) => h.startsWith("report.sources"))).toBe(true);
    expect(doc.blocks).toContainEqual({
      kind: "paragraph",
      muted: true,
      text: "report.studyLimitationsMissing",
    });
  });

  it("ставит рядом с вердиктом числа, которыми его можно оспорить", () => {
    for (const key of ["report.scienceHistory", "report.adoption", "report.market"])
      expect(scored(doc).map((row) => row.label)).toContain(key);
    expect(value("report.adoption")).toContain("report.adoptionStage.pilot");
    expect(value("report.adoption")).toContain("признак стадии в 2 из 9");
    expect(value("report.market")).toContain("report.marketUnknown");
    expect(value("report.market")).toContain("набрано 0 признаков из 2");
    const early = { ...candidate.market!, status: "early" as const };
    const quote = { field: "market", source_id: "a", quote: "первые пилоты", verified: true };
    expect(marketLabel({ ...candidate, market: { ...early, evidence: [quote] } }, t)).toBe(
      "report.marketYoung",
    );
    // «Признаков рынка не найдено», а не утверждение, что ранняя стадия доказана.
    expect(marketLabel({ ...candidate, market: early }, t)).toBe("report.marketNoSigns");
    expect(value("report.relevance")).toBe("report.relevanceValue");
    expect(value("report.momentum")).toContain("report.momentumValue.down");
    expect(value("report.momentum")).toContain("report.momentumCompare");
  });

  it("не повторяет вердикт, которым сервер уже начал основание", () => {
    const echoed = signalReportDoc({
      detail,
      candidate: {
        ...candidate,
        market: { ...candidate.market!, basis: "report.marketUnknown: стандартов не найдено" },
      },
      query: "q",
      rank: 1,
      total: 1,
      name: "n",
      t,
    });
    expect(scored(echoed).find((row) => row.label === "report.market")?.value).toBe(
      "report.marketUnknown: стандартов не найдено",
    );
  });

  it("метод одной статьи с высокой оценкой модели — не слабый сигнал, и блок «почему» это объясняет", () => {
    const single = {
      ...candidate,
      papers_count: 1,
      groups: 1,
      confidence: { ...candidate.confidence!, reasons: [], above_threshold: false },
      growth: { prior: { count: 1 }, recent: { count: 0 } },
      model_score: {
        value: 0.96,
        threshold: 0.37,
        model: "weak-signal-logreg",
        version: "v3",
        basis: "оценка",
        label: "weak_signal",
        features: [
          { name: "История термина", value: "работ 2", contribution: 2.6 },
          { name: "Окно arXiv", value: "работ 1", contribution: -0.9 },
        ],
      },
    } as unknown as LiveTopic;
    expect(signalVerdict(single, t).text).toBe("tile.verdict.single");
    expect(growthLabel(single, t)).toBeNull();
    const why = verdictReasons(single, t);
    expect([why.heading, why.lead]).toEqual(["report.whyVerdict", "report.whyShort"]);
    expect(why.passed).toEqual(["История термина: работ 2"]);
    expect(why.against[0]).toBe("report.fewSources");
    const covered = {
      ...single,
      web: { records: [{ url: "https://www.reuters.com/a", source: "Reuters" }] },
    } as unknown as LiveTopic;
    expect(signalVerdict(covered, t).text).toBe("tile.verdict.weak");
    expect(verdictReasons(covered, t).heading).toBe("report.whySignal");
  });

  it("при «мало данных» вместо оценки называет причину, а число — только для справки", () => {
    const abstained = signalReportDoc({
      detail,
      candidate: {
        ...candidate,
        model_score: {
          value: 0.93,
          threshold: 0.5,
          model: "weak-signal-logreg",
          version: "v3",
          basis: "одна работа",
          label: "insufficient_data",
          data_gap: "too_early",
        },
      },
      query: "q",
      rank: 1,
      total: 1,
      name: "n",
      t,
    });
    expect(scored(abstained).find((r) => r.label === "confidence.modelLabel")?.value).toBe(
      "report.modelClass.insufficient_data: report.dataGap.too_early · confidence.reference",
    );
  });

  it("«не могу решить» внутри конформной полосы не выдаёт оценку выше порога за решение", () => {
    const undecided = {
      ...candidate,
      shortfall: undefined,
      model_score: {
        value: 0.27,
        threshold: 0.23,
        model: "weak-signal-logreg",
        version: "v3",
        basis: "полоса",
        label: "undecided",
      },
    } as LiveTopic;
    expect(signalVerdict(undecided, t).text).toBe("report.modelClass.undecided");
    expect(thresholdLabelKey(signalScore(undecided)!)).toBe("report.modelClass.undecided");
  });

  it("называет версию метода и порог в разделе о расчёте: без них выгрузку не повторить", () => {
    const at = doc.blocks.findIndex((b) => b.kind === "heading" && b.text === "report.aboutMethod");
    const about = doc.blocks[at + 1];
    const rows = about?.kind === "kv" ? about.rows : [];
    expect(rows.find((row) => row.label === "report.methodVersion")?.value).toBe("live-signals-v7");
    expect(rows.find((row) => row.label === "report.confidenceRules")?.value).toContain(
      "confidence.threshold",
    );
  });

  it("сохраняет рыночный источник из привязки цитаты, даже без paper_ids", () => {
    const quotedOnly = { ...candidate, paper_ids: [], evidence_ids: [] };
    expect(signalSources(detail, quotedOnly).map(({ paper }) => paper.id)).toEqual(["p1"]);
  });

  it("не нумерует одну работу дважды и снимает английскую присказку с пересказа", () => {
    const doubled = {
      ...detail,
      papers: [...detail.papers, detail.papers[0]!],
      passports: {
        p2: {
          ...detail.passports!.p2!,
          digest: "Based on the title and abstract, этот материал о мембранах",
        },
        p1: { ...detail.passports!.p2!, digest: "Mixture of Experts, архитектура" },
      },
    } as unknown as LiveDetail;
    const sources = signalSources(doubled, candidate);
    expect(sources.map(({ paper }) => paper.id)).toEqual(["p2", "p1"]);
    expect(sources.map(({ passport }) => passport?.digest)).toEqual([
      "Этот материал о мембранах",
      "Mixture of Experts, архитектура",
    ]);
  });

  it("убирает из причины проверки служебные метки подсказки модели", () => {
    expect(readableReason("Механизм: Механизм: учитель выбирает цели и соответствует scope.")).toBe(
      "Механизм: учитель выбирает цели и соответствует области запроса.",
    );
    expect(readableReason("Механизм конкретен и соответствует задаче b1.")).toBe(
      "Механизм конкретен и соответствует задаче запроса.",
    );
    expect(
      readableReason("Подтверждена аутентификация (b2), но банковский контекст (c1) не описан."),
    ).toBe("Подтверждена аутентификация, но банковский контекст не описан.");
    expect(
      readableReason("Метод отвечает b2; c1 не подтверждён, связь с b1 и c1 не описана."),
    ).toBe("Метод отвечает запросу; часть запроса не подтверждена, связь с запросом не описана.");
    expect(readableReason("Задача обнаружения C2 не привязана к отрасли.")).toBe(
      "Задача обнаружения C2 не привязана к отрасли.",
    );
  });

  it("находит кейс-пример на рыночной записи, а не только на статье", () => {
    const withRecord = signalReportDoc({
      detail: {
        ...detail,
        papers: [...detail.papers, { id: "r1", title: "Новость рынка", date: "2026-05-01" }],
      } as unknown as LiveDetail,
      candidate: { ...candidate, evidence_ids: ["r1"] } as unknown as LiveTopic,
      query: "q",
      rank: 1,
      total: 15,
      name: "n",
      t,
    });
    const refs = withRecord.blocks.find((b) => b.kind === "references");
    expect(refs?.items.map((item) => item.text)).toContainEqual(
      expect.stringContaining("Новость рынка"),
    );
  });

  it("перечисляет источники свежими сверху с площадкой, типом и пересказом", () => {
    const refs = doc.blocks.find((b) => b.kind === "references");
    expect(refs?.items.map((item) => item.n)).toEqual([1, 2]);
    expect(refs?.items[0]?.text).toContain("Newer paper");
    expect(refs?.items[0]?.text).toContain("Example Research Journal");
    expect(refs?.items[0]?.text).toContain("source.publisher.aggregator");
    expect(refs?.items[0]?.text).toContain("source.trust.medium — метаданные агрегатора");
    expect(refs?.items[0]?.text).toContain("source.language");
    // Паспорт без данных подписывает неизвестное, а не пропускает тип, язык и доверие.
    expect(refs?.items[1]?.text).toContain("source.trust.unknown");
    expect(refs?.items[0]?.note).toBe("Русская интерпретация (report.digestBy)");
    const text = JSON.stringify(doc.blocks);
    expect(text).toContain("Lower loss in laboratory conditions");
    expect(text).toContain("Оценка из аналитического отчёта с указанным автором");
    expect(doc.blocks).toContainEqual({
      kind: "quote",
      source: "report.sourceNumber Older paper",
      text: "The analyst report expects limited adoption in the near term",
      note: undefined,
    });
    expect(text).toContain("report.modelUnavailable");
  });

  it("подкрепляет кейс-пример номером работы, когда цитаты к нему нет", () => {
    const texts = doc.blocks.filter((b) => b.kind === "paragraph").map((b) => b.text);
    expect(texts.some((text) => text.includes("report.exampleSource"))).toBe(true);
    expect(texts.some((text) => text.includes("Older paper"))).toBe(true);
  });

  it("не прячет недобор обязательного поля", () => {
    const empty = signalReportDoc({
      detail,
      candidate: { ...candidate, card: undefined } as unknown as LiveTopic,
      query: "q",
      rank: 1,
      total: 15,
      name: "n",
      t,
    });
    expect(empty.blocks[0]).toMatchObject({ kind: "doctitle", text: "report.missingField" });
    const texts = empty.blocks.filter((b) => b.kind === "paragraph").map((b) => b.text);
    expect(texts.filter((text) => text === "report.missingField")).toHaveLength(2);
    expect(texts).toContain("report.reportAssessmentMissing");
  });

  it("выводит неизвестную стадию и переводит оговорки без технических ошибок", async () => {
    const local = createInstance();
    await local.init({ lng: "ru", resources: { ru: { trend: ru } }, defaultNS: "trend" });
    const report = signalReportDoc({
      detail,
      candidate: {
        ...candidate,
        adoption: { code: "unknown", rank: 0, documents: 0, matched: 0, basis: "нет данных" },
        limits: [
          { code: "analysis_unavailable", text: "analysis: HTTP 400: invalid analysis request" },
          { code: "relevance_unmeasured", text: "relevance: query similarity not measured" },
          { code: "scholar_capped", values: { taken: 12, found: 30 }, text: "scholar: capped" },
          { code: "scholar_journals", text: "scholar: missing counts" },
          { code: "unknown_failure", text: "service: internal error" },
          { code: "qualification_unavailable", text: "проверка кандидатов не завершена" },
        ],
      },
      query: "q",
      rank: 1,
      total: 1,
      name: "n",
      t: local.getFixedT("ru", "trend"),
    });
    const rows = scored(report, ru.report.howScored);
    expect(rows.find((row) => row.label === ru.report.adoption)?.value).toBe(
      `${ru.report.unknown} · нет данных`,
    );
    expect(report.blocks.at(-1)).toEqual({
      kind: "list",
      items: [
        ru.study.live.coverage.analysis_unavailable,
        ru.study.live.coverage.relevance_unmeasured,
        "В анализ взято 12 научных работ из 30 найденных.",
        ru.study.live.coverage.other,
        ru.study.live.coverage.qualification_unavailable,
      ],
    });
  });
});

describe("directionReportDoc — у карточки направления появилась выгрузка", () => {
  const direction = {
    ...detail,
    limits: [],
    topic: {
      ...detail.topic,
      id: "digital-twins",
      description: "Что это за направление и в чём новизна.",
      repos: [1, 2],
      discussions: null,
    },
  } as unknown as LiveDetail;
  const doc = directionReportDoc({ detail: direction, name: "Цифровые двойники", t });

  it("документ подписан направлением, а файл назван его идентификатором", () => {
    expect(doc.blocks[0]).toMatchObject({
      kind: "doctitle",
      doctype: "report.directionDocType",
      title: "Цифровые двойники",
    });
    expect(doc.fileName).toBe("digital-twins.docx");
  });

  it("описание стоит в шапке, а несобранный канал в таблицу не попадает", () => {
    expect(doc.blocks[0]).toMatchObject({ lead: "Что это за направление и в чём новизна." });
    const table = doc.blocks.find((block) => block.kind === "table");
    expect(table?.rows.map(([label]) => label)).toEqual([
      "study.live.channels.papers",
      "study.live.channels.repos",
    ]);
  });

  it("пустое описание в шапку не попадает", () => {
    const bare = directionReportDoc({
      detail: { ...direction, topic: { ...direction.topic, description: "" } },
      name: "Цифровые двойники",
      t,
    });
    expect(bare.blocks[0]).toMatchObject({ lead: undefined });
  });
});
