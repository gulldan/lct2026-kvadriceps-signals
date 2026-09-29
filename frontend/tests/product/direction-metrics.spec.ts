import { expect, test } from "./fixture";
import { inspectSurface } from "./checks";
import ru from "../../src/shared/i18n/locales/ru/trend.json" with { type: "json" };
import { analysisFixture, paperEvidence, repoEvidence } from "./trends-fixture";

test.beforeEach(async ({ page }) => {
  const job = analysisFixture();
  await page.route("**/api/v1/trends/analyses/ui-run", (route) => route.fulfill({ json: job }));
  await page.route("**/api/v1/trends/analyses/*/candidates/*/description", (route) =>
    route.fulfill({ status: 404, json: { error: "no description" } }),
  );
  await page.route("**/api/v1/documents/ui-paper/source*", (route) =>
    route.fulfill({ json: paperEvidence }),
  );
  await page.route("**/api/v1/documents/ui-repo/source*", (route) =>
    route.fulfill({ json: repoEvidence }),
  );
});

for (const width of [390, 1440]) {
  test(`direction metrics keep four value states apart and explain each number at ${width}px`, async ({
    page,
  }, info) => {
    await page.setViewportSize({ width, height: 900 });
    const candidate = analysisFixture().result!.candidates[0]!.id;
    await page.goto(`/trends/ui-run/topics/${candidate}`);
    await page.locator("summary", { hasText: ru.metrics.title }).click();
    const panel = page.getByRole("region", { name: ru.metrics.title });
    await expect(panel.getByText(ru.metrics.groups.growth, { exact: true })).toBeVisible();
    await expect(panel.getByText("128", { exact: true }).first()).toBeVisible();
    await expect(panel.getByText(ru.metrics.state.unknown, { exact: true })).toHaveCount(2);
    await expect(panel.getByText(ru.metrics.state.not_observed, { exact: true })).toBeVisible();
    await expect(panel.getByRole("button", { name: ru.metrics.state.zeroHint })).toBeVisible();
    const why = panel.getByRole("button", { name: ru.metrics.state.whyLabel }).first();
    await why.hover();
    await expect(
      page.getByRole("tooltip").getByText(ru.metrics.reasons.insufficient_quarters),
    ).toBeVisible();
    const growth = panel.locator("li", { hasText: ru.metrics.names.growth }).first();
    await growth.locator("summary").click();
    await expect(growth.getByText(ru.metrics.explain.numerator, { exact: true })).toBeVisible();
    await expect(growth.getByText("0,1 — 0,7 (90 %)")).toBeVisible();
    await expect(panel.getByText(ru.metrics.sensitivity.checks.largest_participant)).toBeVisible();
    await expect(panel.getByText(ru.metrics.sensitivity.changed, { exact: true })).toHaveCount(1);
    await expect(panel.getByText(ru.metrics.sensitivity.unknown, { exact: true })).toHaveCount(3);
    await inspectSurface(page, info, `direction-metrics-${width}`);
  });
}

test("a direction without envelope metrics says so instead of showing zeros", async ({ page }) => {
  const candidate = analysisFixture().result!.candidates[0]!.id;
  await page.route("**/api/v1/trends/analyses/ui-run/topics/*/metrics", (route) =>
    route.fulfill({
      json: {
        analysis_id: "ui-run",
        analysis_run_id: "ui-run",
        schema_version: "analysis-result-v1",
        method_hash: "m".repeat(64),
        topic: candidate,
        title: "Направление",
        history_mode: "as_of",
        cutoff: "2026-09-01",
        metrics: { series: [], found: {} },
        limitations: [],
      },
    }),
  );
  await page.goto(`/trends/ui-run/topics/${candidate}`);
  await page.locator("summary", { hasText: ru.metrics.title }).click();
  const panel = page.getByRole("region", { name: ru.metrics.title });
  await expect(panel.getByText(ru.metrics.empty)).toBeVisible();
  await expect(panel.getByText("0", { exact: true })).toHaveCount(0);
});
