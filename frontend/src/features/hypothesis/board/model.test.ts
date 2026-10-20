import { expect, test } from "bun:test";
import { createData } from "../../../../tests/product/data";
import { kpiResearchDomain } from "@/features/kpi";
import { loadHypothesisRuntimeSettings } from "@/shared/appSettings";
import { interleaveBy } from "@/shared/lib/interleaveBy";
import { matchesQueue } from "./model";
import { hypothesisResearchDomain } from "./researchDomain";

test("очередь гипотез не скрывает непроверенные направления и черновики", () => {
  const hypothesis = createData().hypotheses[0]!;
  const rows = [
    hypothesis,
    { ...hypothesis, id: "pending", status: "generated" as const, assessment: {} },
    { ...hypothesis, id: "closed", status: "archived" as const, assessment: {} },
  ];
  const settings = loadHypothesisRuntimeSettings();
  expect(rows.filter((row) => matchesQueue(row, "ready", settings, "hypotheses"))).toEqual([
    hypothesis,
  ]);
  for (const kind of ["directions", "drafts"] as const) {
    expect(rows.filter((row) => matchesQueue(row, "ready", settings, kind))).toEqual(rows);
    expect(rows.filter((row) => matchesQueue(row, "risk", settings, kind))).toEqual(rows);
  }
});

test("отрасль связанного тезиса разделяет прогнозы с одинаковой технической лексикой", () => {
  const data = createData();
  const hypothesis = {
    ...data.hypotheses[0]!,
    function_area: "",
    title: "Нейросетевой анализ обратной связи",
    statement: "Модель машинного обучения снижает ошибку оценки.",
  };
  const kpis = ["Музыка", "Кадры и подбор персонала", "Недвижимость"].map((function_area, index) =>
    Object.assign({}, data.kpis[0]!, {
      id: `context-${index}`,
      function_area,
      detail: {},
    }),
  );
  const byId = new Map(kpis.map((kpi) => [kpi.id, kpi]));
  const rows = kpis.flatMap((kpi) =>
    [1, 2].map((rank) =>
      Object.assign({}, hypothesis, { id: `${kpi.id}-${rank}`, kpi_id: kpi.id }),
    ),
  );
  const before = structuredClone(rows);
  const domain = (h: (typeof rows)[number]) => hypothesisResearchDomain(h, byId.get(h.kpi_id));
  expect(rows.map(domain)).toEqual([
    "music",
    "music",
    "jobs_recruitment",
    "jobs_recruitment",
    "real_estate",
    "real_estate",
  ]);
  expect(interleaveBy(rows, domain).map((h) => h.id)).toEqual([
    "context-0-1",
    "context-1-1",
    "context-2-1",
    "context-0-2",
    "context-1-2",
    "context-2-2",
  ]);
  expect(rows).toEqual(before);
  expect(hypothesisResearchDomain(hypothesis)).toBe("computing");
  const unknown = { ...hypothesis, title: "Новая методика", statement: "Пока неизвестно" };
  expect(hypothesisResearchDomain(unknown)).toBe("unknown");
  const context = { ...kpis[0]!, function_area: "", title: "", description: "" };
  expect(
    hypothesisResearchDomain(hypothesis, { ...context, detail: { research_domain: "music" } }),
  ).toBe("music");
  expect(
    hypothesisResearchDomain(unknown, { ...context, detail: { research_domain: "made_up" } }),
  ).toBe("unknown");
});

test("тезис о переработке текстиля и связанный прогноз сохраняют отрасль моды", () => {
  const data = createData();
  const kpi = {
    ...data.kpis[0]!,
    title: "Повысить выход пригодного вторичного полиэфирного волокна при переработке текстиля",
    description: "Экологический эффект переработки текстиля оценивается отдельно.",
    function_area: "Мода и текстиль",
    detail: { research_domain: "fashion" },
  };
  const hypothesis = {
    ...data.hypotheses[0]!,
    function_area: "",
    title: "Модель переработки вторичного сырья",
    statement: "Переработка текстиля повышает выход годного волокна.",
  };
  expect(kpiResearchDomain(kpi)).toBe("fashion");
  expect(hypothesisResearchDomain(hypothesis, kpi)).toBe("fashion");
  expect(kpiResearchDomain({ ...kpi, function_area: "" })).toBe("fashion");
  expect(kpiResearchDomain({ ...kpi, detail: { research_domain: "environment" } })).toBe("fashion");
});
