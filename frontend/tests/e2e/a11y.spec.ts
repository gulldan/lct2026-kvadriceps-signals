import { AxeBuilder } from "@axe-core/playwright";
import { expect, test } from "./fixture";

const PAGES: { name: string; path: string }[] = [
  { name: "поиск (главная)", path: "/" },
  { name: "гипотезы", path: "/hypotheses" },
  { name: "темы", path: "/summary" },
  { name: "тезисы", path: "/kpi" },
  { name: "история", path: "/history" },
  { name: "документы", path: "/operator/documents" },
  { name: "тренды", path: "/trends" },
  { name: "каталог технологий", path: "/trends?entity=technologies" },
  { name: "обзор", path: "/admin" },
  { name: "метрики", path: "/admin/metrics" },
  { name: "настройки", path: "/admin/settings" },
];

test.describe("@a11y аудит страниц", () => {
  for (const { name, path } of PAGES) {
    test(`@a11y ${name}`, async ({ page }) => {
      await page.goto(path, { waitUntil: "load" });
      // networkidle не наступает: фон опрашивает /system/activity.
      await page.locator("main, [role=main]").first().waitFor({ timeout: 15_000 });
      await page.waitForTimeout(700);
      const results = await new AxeBuilder({ page }).withTags(["wcag2a", "wcag2aa"]).analyze();
      const serious = results.violations.filter(
        (v) => v.impact === "serious" || v.impact === "critical",
      );
      expect(serious.map((v) => `${v.id}: ${v.help} (${v.nodes.length} узлов)`)).toEqual([]);
    });
  }
});
