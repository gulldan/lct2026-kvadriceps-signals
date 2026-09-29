import { expect, test } from "bun:test";
import { createInstance, type TFunction } from "i18next";
import { radarReportBlocks } from "@/features/hypothesis";
import ru from "@/shared/i18n/locales/ru/hypothesis.json";
import saved from "../../../tests/fixtures/live-search-v1.json";
import type { LiveDetail, LiveTopic } from "./liveApi";
import { signalHypothesisInput } from "./signalHypothesis";

const t = ((key: string) => key) as unknown as TFunction<"trend">;

test("перенос сохраняет доказательства и версии, но не присваивает новой гипотезе оценки радара", async () => {
  const savedDetail = saved as unknown as LiveDetail;
  const seed = savedDetail.candidates![0]!;
  const sourceId = seed.paper_ids[0]!;
  const detail: LiveDetail = {
    ...savedDetail,
    passports: {
      ...savedDetail.passports,
      [sourceId]: {
        ...savedDetail.passports?.[sourceId],
        genre: "press_release",
        source_name: "Example Research Journal",
        discovery_source: "openalex",
      },
    },
  };
  const candidate: LiveTopic = {
    ...seed,
    model_score: {
      value: 0.83,
      model: "classifier",
      version: "v2",
      label: "weak_signal",
      threshold: 0.75,
      basis: "контрольный пример",
      features: [
        { name: "Авторские группы", value: "3", contribution: 0.125 },
        { name: "Рыночные записи", value: "0", contribution: -0.05 },
        { name: "Рост доли", value: "не измерен", contribution: 0 },
      ],
    },
    card: {
      ...seed.card,
      evidence: [
        {
          field: "advantage",
          source_id: sourceId,
          quote: "Fixture evidence, not a measured product claim",
          verified: true,
        },
      ],
      verification_model: "verifier-v3",
    },
  };
  const input = signalHypothesisInput({
    detail,
    candidate,
    query: "контрольный запрос",
    form: {
      title: "  Проверяемая идея  ",
      problem: "",
      hypothesis: "  Собственная гипотеза аналитика  ",
      grounds: "",
      method: "",
      criterion: "",
    },
    kpiId: "goal-1",
    t,
  });
  expect(input.title).toBe("Проверяемая идея");
  expect(input.statement).toBe("Собственная гипотеза аналитика");
  expect(input.status).toBe("draft");
  expect(input.method).toBe("manual");
  expect(input.generation?.kind).toBe("radar");
  expect(input.kpi_id).toBe("goal-1");
  expect(input).not.toHaveProperty("confidence_score");
  expect(input).not.toHaveProperty("assessment");
  const provenance = input.detail?.radar;
  expect(provenance?.query).toBe("контрольный запрос");
  expect(provenance?.observed_at).toBe(detail.observed_at);
  expect(provenance?.method).toBe(detail.method);
  expect(provenance?.model_score?.version).toBe("v2");
  expect(provenance?.model_score).toEqual(candidate.model_score!);
  expect(provenance?.verification_model).toBe("verifier-v3");
  const source = provenance?.sources.find(({ id }) => id === sourceId);
  expect(source?.url).toBe(detail.papers.find(({ id }) => id === sourceId)?.url);
  expect(source?.genre).toBe("press_release");
  expect(source?.passport).toContain("source.genre.press_release");
  expect(source?.source_name).toBe("Example Research Journal");
  expect(source?.discovery_source).toBe("openalex");
  expect(source?.passport).toContain("source.sourceName");
  expect(source?.passport).toContain("source.discoveredVia");
  expect(source?.quotes).toContainEqual({
    field: "advantage",
    quote: "Fixture evidence, not a measured product claim",
    verified: true,
  });
  expect(seed.card).not.toHaveProperty("verification_model");
  const local = createInstance();
  await local.init({ lng: "ru", resources: { ru: { hypothesis: ru } }, defaultNS: "hypothesis" });
  const translate = local.getFixedT("ru", "hypothesis");
  const report = radarReportBlocks(provenance!, translate);
  expect(report).toContainEqual({
    kind: "kv",
    rows: [{ label: "Порог модели", value: "75 %" }],
  });
  expect(report).toContainEqual({ kind: "paragraph", text: "контрольный пример" });
  expect(report).toContainEqual({
    kind: "table",
    title: "Обученная модель",
    head: ["Фактор", "Значение", "Вклад"],
    rows: [
      ["Авторские группы", "3", "0,125"],
      ["Рыночные записи", "0", "-0,05"],
      ["Рост доли", "не измерен", "0"],
    ],
  });
  const withZero = radarReportBlocks(
    { ...provenance!, model_score: { ...candidate.model_score!, threshold: 0 } },
    translate,
  );
  expect(withZero).toContainEqual({
    kind: "kv",
    rows: [{ label: "Порог модели", value: "0 %" }],
  });
  for (const missing of [undefined, null]) {
    const old = radarReportBlocks(
      {
        ...provenance!,
        model_score: {
          ...candidate.model_score!,
          threshold: missing,
          basis: missing,
          features: missing,
        },
      },
      translate,
    );
    expect(old.some((block) => block.kind === "table")).toBe(false);
    const text = JSON.stringify(old);
    expect(text).not.toContain("Порог модели");
    expect(text).not.toContain("контрольный пример");
  }
  const snapshot = {
    observed_at: "2026-09-25",
    stage_code: 3,
    trend_code: 2,
    score: 5,
    events: { pilot: 1 },
  };
  const bet = radarReportBlocks(
    {
      ...provenance!,
      bet: {
        horizon: "1-3",
        stage_code: 2,
        next_stage_trigger: "первый пилот у банка",
        indicators: [{ kind: "stage", threshold: 3, label: "стадия не ниже 3" }],
        baseline: { ...snapshot, stage_code: 2, events: {} },
        snapshot,
        status: "confirming",
        history: [
          {
            at: "2026-09-25T03:00:00Z",
            kind: "event:pilot",
            text: "Появилось событие «пилот»",
            quote: "Bank X piloted",
            url: "https://example.test/pilot",
          },
        ],
      },
    },
    translate,
  );
  expect(bet).toContainEqual({
    kind: "table",
    title: "Опережающие индикаторы",
    head: ["Индикатор и порог", "Сейчас", "Выполнен"],
    rows: [["стадия не ниже 3", "3", "да"]],
  });
  expect(JSON.stringify(bet)).toContain("Появилось событие «пилот» · «Bank X piloted»");
});
