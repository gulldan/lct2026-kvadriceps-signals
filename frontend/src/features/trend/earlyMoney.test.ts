import { expect, test } from "bun:test";
import {
  moneyCells,
  moneyMonths,
  moneyQuadrant,
  moneyRubrics,
  moneyTotals,
  moneyWindows,
  monthRange,
} from "./earlyMoney";
import type { MoneyCell, MoneySummary } from "./moneyApi";

const cell = (rubric: string, month: string, count: number, kind: MoneyCell["kind"] = "round") =>
  ({ rubric, region: "europe", kind, stage: "seed", month, count }) satisfies MoneyCell;

const summary: MoneySummary = {
  status: "ready",
  months: ["2026-04", "2026-05", "2026-06", "2026-07", "2026-08", "2026-09", "2026-10"],
  cells: [
    // Медицина: деньги стабильны, наука растёт — окно для ангела.
    cell("medicine", "2026-04", 10),
    cell("medicine", "2026-08", 10),
    // Финтех: деньги растут вчетверо при тех же работах — деньги впереди науки.
    cell("fintech", "2026-05", 2),
    cell("fintech", "2026-09", 8),
    // Неполный текущий месяц и событие без рубрики в квадрант не входят.
    cell("fintech", "2026-10", 50),
    cell("", "2026-07", 5, "grant"),
    // Мало событий — точка не строится; науки нет — рубрика уходит в «не измерено».
    cell("space", "2026-06", 2),
    cell("food", "2026-04", 4),
    cell("food", "2026-07", 4),
  ],
  science: [
    {
      rubric: "medicine",
      topics: 3,
      prior: 40,
      recent: 120,
      prior_population: 1000,
      recent_population: 1000,
    },
    {
      rubric: "fintech",
      topics: 2,
      prior: 30,
      recent: 30,
      prior_population: 1000,
      recent_population: 1000,
    },
  ],
  latest: [],
  sources: [],
  total: 95,
  unsorted: 5,
};

test("quadrant compares shares of complete months and keeps thin rubrics out", () => {
  const { points, unmeasured } = moneyQuadrant(summary, summary.cells);
  const byRubric = Object.fromEntries(points.map((point) => [point.rubric, point]));
  expect(Object.keys(byRubric).toSorted()).toEqual(["fintech", "medicine"]);
  expect(byRubric.medicine!.quadrant).toBe("angel_window");
  expect(byRubric.fintech!.quadrant).toBe("money_first");
  expect(unmeasured).toEqual(["food"]);
});

test("filters, totals and rubric ranking agree on the same cells", () => {
  const grants = moneyCells(summary, { region: "all", kind: "grant" });
  expect(moneyTotals(grants)).toEqual({
    total: 5,
    classified: 0,
    kinds: { round: 0, grant: 5, accelerator: 0, development: 0, research: 0 },
  });
  expect(moneyRubrics(summary.cells)[0]).toEqual({ rubric: "fintech", count: 60 });
});

test("growth windows are dated: money by quarters, science by its own half-years", () => {
  const windows = moneyWindows(summary.months);
  expect(windows).toEqual({
    prior: ["2026-04", "2026-05", "2026-06"],
    recent: ["2026-07", "2026-08", "2026-09"],
    current: "2026-10",
  });
  expect(moneyMonths(summary.cells, summary.months).map((item) => item.count)).toEqual([
    14, 2, 2, 9, 10, 8, 50,
  ]);
  expect(monthRange(windows.recent, "en")).toBe("Jul – Sep 2026");
  expect(monthRange("2025-10–2026-03", "en")).toBe("Oct 2025 – Mar 2026");
  expect(monthRange(undefined, "en")).toBeNull();
});
