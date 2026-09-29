import { expect, test } from "./fixture";
import { choose, chooseRadio, inspectSurface } from "./checks";
import { paperEvidence } from "./trends-fixture";
import type { TrendStudy } from "../../src/features/trend/studyApi";
import study from "../fixtures/trend-study.json" with { type: "json" };
import ru from "../../src/shared/i18n/locales/ru/trend.json" with { type: "json" };
import doc from "../../src/shared/i18n/locales/ru/document.json" with { type: "json" };
import en from "../../src/shared/i18n/locales/en/trend.json" with { type: "json" };
import zh from "../../src/shared/i18n/locales/zh/trend.json" with { type: "json" };

for (const [width, language, copy] of [
  [320, "ru", ru],
  [390, "ru", ru],
  [768, "en", en],
  [1440, "ru", ru],
  [1440, "zh", zh],
] as const) {
  test(`measured study, comparison and explanation at ${width}px ${language}`, async ({
    page,
  }, info) => {
    test.setTimeout(90_000);
    await page.setViewportSize({ width, height: 1000 });
    await page.addInitScript((lang) => localStorage.setItem("rag.lang", lang), language);
    await page.emulateMedia({ reducedMotion: "reduce" });
    await page.goto("/trends?view=study");
    const selected = page.getByRole("region", { name: copy.study.selectedDirection });
    const chart = page.getByRole("figure", { name: copy.study.chartShare, exact: true });
    await expect(chart).toBeVisible();
    await expect(selected.getByRole("heading", { name: "GraphRAG", exact: true })).toBeVisible();
    await expect(chart).toContainText("29/318");
    await inspectSurface(page, info, `study-${width}-${language}`);
    await choose(page, copy.study.compare, copy.study.topics.adaptive.name);
    await expect(chart).toContainText(copy.study.topics.adaptive.name);
    await chooseRadio(page, copy.study.count);
    await expect(page.getByRole("figure", { name: copy.study.chartCount })).toContainText("29");
    await page.getByRole("button", { name: "2020", exact: true }).click();
    await expect(selected.getByText(copy.study.noPapers)).toBeVisible();
    await page.getByRole("button", { name: "2024", exact: true }).click();
    await expect(
      selected.getByRole("region", { name: copy.study.sources }).getByRole("link"),
    ).toHaveCount(4);
    await page.getByRole("tab", { name: copy.study.basis, exact: true }).click();
    await expect(selected.getByText("4 / 104", { exact: true })).toBeVisible();
    await expect(selected.getByText("29 / 318", { exact: true })).toBeVisible();
    await selected.getByText(copy.study.explanation.science, { exact: true }).click();
    await expect(selected.getByRole("link", { name: /Rotolo/ })).toHaveAttribute(
      "href",
      "https://arxiv.org/abs/1503.00673",
    );
    await inspectSurface(page, info, `basis-${width}-${language}`);
    if (width === 390) {
      await page.evaluate(() => (document.documentElement.style.fontSize = "200%"));
      await inspectSurface(page, info, "basis-text-200");
      await page.getByRole("tab", { name: copy.study.development, exact: true }).click();
      await page.getByRole("button", { name: "2025", exact: true }).click();
      await expect(page.getByRole("button", { name: "2025", exact: true })).toBeInViewport();
      await inspectSurface(page, info, "development-text-200");
    }
  });
}

test("source revision and context survive show-more, return and reload", async ({ page }) => {
  await page.setViewportSize({ width: 1440, height: 1000 });
  const topic = study.topics.find((item) => item.id === "graph")!;
  const paper = study.papers.filter((item) => topic.paper_ids.includes(item.id)).toReversed()[12]!;
  await page.route(`**/api/v1/documents/${paper.document_id}/source*`, async (route) => {
    expect(new URL(route.request().url()).searchParams.get("snapshot")).toBe(paper.snapshot_id);
    await route.fulfill({
      json: {
        ...paperEvidence,
        document_id: paper.document_id,
        snapshot_id: paper.snapshot_id,
        title: paper.title,
      },
    });
  });
  await page.goto("/trends?view=study&detail=sources&filter=emerging");
  await page.getByRole("button", { name: /Показать ещё/ }).click();
  const id = `study-paper-${paper.id.replaceAll(".", "_")}`;
  const link = page.locator(`[id="${id}"]`);
  await link.click();
  await expect(page).toHaveURL(
    new RegExp(`/sources/${paper.document_id}\\?snapshot=${paper.snapshot_id}`),
  );
  await expect(page.getByRole("heading", { name: paper.title, exact: true })).toBeVisible();
  await page.getByRole("link", { name: doc.source.backRadar, exact: true }).click();
  await expect(page).toHaveURL(/detail=sources&filter=emerging/);
  await expect(link).toBeFocused();
  await page.reload();
  await expect(link).toBeFocused();
  await expect(page.getByRole("radio", { name: ru.study.emerging, exact: true })).toBeChecked();
  await expect(
    page.getByRole("button", { name: ru.study.topics.adaptive.name, exact: true }),
  ).toHaveCount(0);
});

test("unreviewed families have measurements without an emergence claim", async ({ page }) => {
  await page.goto("/trends?view=study&topic=memory&detail=basis");
  const selected = page.getByRole("region", { name: ru.study.selectedDirection });
  await expect(selected).toContainText(ru.study.stage.unassessed);
  await expect(selected).toContainText(ru.study.explanation.lexical);
  await expect(selected).not.toContainText(ru.study.stage.emerging_signal);
  await expect(selected.getByText("5 / 104", { exact: true })).toBeVisible();
  await expect(selected.getByText("3 / 318", { exact: true })).toBeVisible();
});

test("partial coverage suppresses growth and emergence without hiding available sources", async ({
  page,
}) => {
  const partial = structuredClone(study) as TrendStudy;
  partial.coverage = "partial";
  const removed = partial.papers.pop()!;
  partial.available_papers--;
  for (const topic of partial.topics) {
    topic.paper_ids = topic.paper_ids.filter((id) => id !== removed.id);
    topic.evidence_ids = topic.evidence_ids.filter((id) => id !== removed.id);
    topic.stage = "unassessed";
    topic.momentum = "unknown";
    topic.series.forEach((period) => (period.share = null));
    Object.assign(topic.growth, { delta_pp: null, share_ratio: null, count_ratio: null });
    topic.growth.prior.share = null;
    topic.growth.recent.share = null;
  }
  await page.route("**/api/v1/trends/study", (route) => route.fulfill({ json: partial }));
  await page.goto("/trends?view=study&detail=basis");
  const selected = page.getByRole("region", { name: ru.study.selectedDirection });
  await expect(selected.getByText(ru.study.explanation.growthUnconfirmedTitle)).toBeVisible();
  await expect(selected.getByText(ru.study.explanation.growthTitle, { exact: true })).toHaveCount(
    0,
  );
  await expect(selected.getByText(ru.study.stage.emerging_signal, { exact: true })).toHaveCount(0);
  await chooseRadio(page, ru.study.emerging);
  await expect(page.getByText(ru.study.emptyFilter)).toBeVisible();
  await chooseRadio(page, ru.study.all);
  await page.getByRole("tab", { name: ru.study.development, exact: true }).click();
  await chooseRadio(page, ru.study.count);
  await expect(page.getByRole("figure", { name: ru.study.chartCount })).toBeVisible();
});

test("API failure and refresh failure remain actionable", async ({ page, api }) => {
  api.failures.set("GET /trends/study", 503);
  await page.goto("/trends?view=study");
  await expect(page.getByRole("alert")).toContainText(ru.study.error);
  await expect(page.getByRole("figure", { name: ru.study.chartShare })).toHaveCount(0);
  await page.getByRole("button", { name: ru.retry, exact: true }).click();
  await expect(page.getByRole("figure", { name: ru.study.chartShare })).toBeVisible();
  api.failures.set("GET /trends/study", 503);
  await page.getByRole("button", { name: ru.study.refresh, exact: true }).click();
  await expect(page.getByRole("alert")).toBeVisible();
  await expect(page.getByRole("figure", { name: ru.study.chartShare })).toBeVisible();
  expect(api.calls.filter((call) => call.path === "/trends/study")).toHaveLength(3);
});
