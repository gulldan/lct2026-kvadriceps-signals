import { expect, test } from "./fixture";
import { choose, inspectSurface } from "./checks";
import live from "../fixtures/trend-live.json" with { type: "json" };
import ru from "../../src/shared/i18n/locales/ru/trend.json" with { type: "json" };

test("the radar table groups directions by stage and names its subgroups", async ({
  page,
}, info) => {
  const overview = structuredClone(live) as typeof live & { topics: Record<string, unknown>[] };
  const [first, second] = overview.topics as unknown as [
    Record<string, unknown>,
    Record<string, unknown>,
  ];
  first.comparison = {
    prior: 0,
    current: 5,
    fresh: 2,
    days: 184,
    prior_start: "2025-09-08",
    recent_start: "2026-03-11",
    end_exclusive: "2026-09-11",
  };
  first.stage = "established";
  second.growth = { ...(second.growth as Record<string, unknown>), delta_pp: null };
  second.stage = "established";
  await page.route("**/api/v1/trends/live", (route) => route.fulfill({ json: overview }));
  await page.goto("/trends");
  await choose(page, ru.study.live.filterLabel, `${ru.study.live.all} · 3`);
  await page.locator("summary", { hasText: ru.study.live.tableTitle.split(" ·")[0]! }).click();
  const table = page.getByRole("table", { name: ru.study.live.table.aria });
  await expect(table.getByText(ru.study.live.groups.basis.established)).toBeVisible();
  await expect(
    table.getByText(ru.study.live.groups.sub.new_from_zero, { exact: true }),
  ).toBeVisible();
  await expect(
    table.getByText(ru.study.live.groups.sub.delta_unknown, { exact: true }),
  ).toBeVisible();
  await expect(page.getByText(ru.study.live.groups.nonAdditive)).toBeVisible();
  const headers = table.locator("tbody > tr:first-child");
  expect(await headers.count()).toBeGreaterThanOrEqual(2);
  await inspectSurface(page, info, "live-groups-1440");
});
