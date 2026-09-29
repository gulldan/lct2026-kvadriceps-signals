import { AxeBuilder } from "@axe-core/playwright";
import { expect, test } from "@playwright/test";

for (const width of [390, 768, 1024, 1440]) {
  test(`TrendRadar source fixtures at ${width}px`, async ({ page }, info) => {
    await page.setViewportSize({ width, height: 1000 });
    await page.emulateMedia({ reducedMotion: "reduce" });
    const apiCalls: string[] = [];
    page.on("request", (request) => {
      if (new URL(request.url()).pathname.startsWith("/api/")) apiCalls.push(request.url());
    });
    await page.goto("/ui-kit.html#trend-fixtures");
    const section = page.locator("#trend-fixtures");
    const select = page.getByRole("combobox", { name: "Контрольная запись" });
    for (const [id, expected] of [
      ["year-only", "year"],
      ["unknown-date", "Неизвестна"],
      ["arxiv-v2", "2501.00001v2"],
      ["private-paper", "Только владелец"],
      ["repo-new-readme", "Исключена из среза"],
    ]) {
      await select.click();
      await page.getByRole("option", { name: id, exact: true }).click();
      await expect(section).toContainText(expected!);
      await expect(select).toBeFocused();
    }
    await section.getByText("Ожидаемые числа ранней темы", { exact: true }).click();
    await expect(section.getByRole("row", { name: "2025-Q4 0 Полный период" })).toBeVisible();
    await expect(
      section.getByRole("row", { name: "2026-Q1 Неизвестно Нет наблюдения" }),
    ).toBeVisible();
    expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(
      true,
    );
    const results = await new AxeBuilder({ page }).include("#trend-fixtures").analyze();
    expect(results.violations).toEqual([]);
    await section.evaluate((element) => element.scrollIntoView({ block: "start" }));
    await page.screenshot({ path: info.outputPath(`trend-fixtures-${width}.png`) });
    expect(apiCalls).toEqual([]);
  });
}
