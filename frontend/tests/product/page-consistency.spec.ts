import { expect, test } from "./fixture";
import ru from "../../src/shared/i18n/locales/ru/trend.json" with { type: "json" };

const pages = [
  "/trends",
  "/kpi",
  "/hypotheses",
  "/summary",
  "/history",
  "/operator/documents",
  "/admin/metrics",
  "/admin/users",
  "/admin",
];
const segmentedPages = new Set(["/trends", "/kpi", "/history"]);

for (const [width, height] of [
  [1280, 720],
  [1024, 768],
] as const) {
  test(`page titles and primary switches share the desktop geometry at ${width}px`, async ({
    page,
  }) => {
    await page.setViewportSize({ width, height });
    for (const path of pages) {
      await test.step(path, async () => {
        await page.goto(path);
        const title = page.getByRole("heading", { level: 1 });
        await expect(title).toBeVisible();
        await page.evaluate(() => document.fonts.ready);
        await expect(title).toHaveCSS("font-size", "30px");
        await expect(title).toHaveCSS("line-height", "36px");
        expect((await title.boundingBox())!.y).toBeCloseTo(89, 1);

        if (!segmentedPages.has(path)) return;
        const segmented = page.locator('[data-slot="segmented"]').first();
        await expect(segmented).toHaveAttribute("data-size", "md");
        const bounds = (await segmented.boundingBox())!;
        expect(bounds.height).toBe(40);
        const segments = await segmented.locator(":scope > label > span").evaluateAll((labels) =>
          labels.map((label) => {
            const { y, height: labelHeight } = label.getBoundingClientRect();
            return { y, height: labelHeight };
          }),
        );
        expect(segments.length).toBeGreaterThanOrEqual(2);
        for (const segment of segments) {
          expect(segment.height).toBeGreaterThanOrEqual(34);
          expect(segment.y + segment.height / 2).toBeCloseTo(bounds.y + bounds.height / 2, 1);
        }
        if (path === "/history") {
          const search = (await page
            .locator('[data-slot="filter-bar"]')
            .getByRole("textbox")
            .boundingBox())!;
          expect(bounds.y).toBeCloseTo(search.y, 1);
          expect(bounds.height).toBe(search.height);
          for (const segment of segments)
            expect(segment.y + segment.height / 2).toBeCloseTo(search.y + search.height / 2, 1);
          await page.evaluate(() => {
            document.documentElement.style.fontSize = "200%";
          });
          const enlarged = await segmented.evaluate((element) => {
            const { y, height: groupHeight } = element.getBoundingClientRect();
            return {
              centre: y + groupHeight / 2,
              segments: [...element.querySelectorAll(":scope > label > span")].map((label) => {
                const { y: top, height: labelHeight } = label.getBoundingClientRect();
                return top + labelHeight / 2;
              }),
            };
          });
          for (const centre of enlarged.segments) expect(centre).toBeCloseTo(enlarged.centre, 1);
          expect(await page.evaluate(() => document.documentElement.scrollWidth)).toBe(width);
        }
      });
    }
  });
}

test("model introduction and report tables use the available page width", async ({ page }) => {
  await page.goto("/trends/model");
  const layout = page.locator('[data-slot="page-layout"]');
  const introduction = page.getByText(ru.modelCheck.intro, { exact: true });
  const report = page.locator('article[data-slot="reading-content"]');
  const table = page.getByRole("table", { name: ru.modelCheck.trainingTitle, exact: true });
  await expect(table).toBeVisible();
  await page.evaluate(() => document.fonts.ready);
  await expect(report).toHaveCSS("max-width", "none");
  await expect(introduction).toHaveCSS("max-width", "none");
  for (const width of [1024, 1280, 1920]) {
    await page.setViewportSize({ width, height: 900 });
    const contentWidth = await layout.evaluate((element) => {
      const style = getComputedStyle(element);
      return (
        element.getBoundingClientRect().width -
        Number.parseFloat(style.paddingLeft) -
        Number.parseFloat(style.paddingRight)
      );
    });
    if (width === 1920) expect(contentWidth).toBe(1232);
    for (const content of [introduction, report, table])
      expect((await content.boundingBox())!.width).toBeCloseTo(contentWidth, 1);
  }
});
