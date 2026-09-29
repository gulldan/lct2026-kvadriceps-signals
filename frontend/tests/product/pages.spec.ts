import { AxeBuilder } from "@axe-core/playwright";
import { writeFile } from "node:fs/promises";
import { expect, test } from "./fixture";
import { inspectDesignContract } from "./design-contract";
import { analysisFixture, paperEvidence } from "./trends-fixture";

const pages = [
  ["search-home", "/search"],
  ["search-results", "/search?chat=chat-1"],
  ["hypotheses", "/hypotheses"],
  ["portfolio-report", "/hypotheses/report"],
  ["hypothesis-report", "/hypotheses/hypothesis-1/report"],
  ["hypothesis", "/hypotheses/hypothesis-1"],
  ["direction", "/directions/direction-1"],
  ["cluster", "/clusters/cluster-1"],
  ["summary", "/summary"],
  ["kpi", "/kpi"],
  ["history", "/history"],
  ["documents", "/operator/documents"],
  ["admin-overview", "/admin"],
  ["admin-users", "/admin/users"],
  ["admin-settings", "/admin/settings"],
  ["admin-metrics", "/admin/metrics"],
  ["not-found", "/nonexistent-page"],
  ["trend-live", "/trends"],
  ["trend-live-topic", "/trends?topic=mcp"],
  ["trend-study", "/trends?view=study"],
  ["radar-start", "/trends?view=analysis"],
  ["radar-results", "/trends?run=ui-run"],
  ["radar-topic", "/trends/ui-run/topics/ui-topic-0"],
  ["radar-method", "/trends/ui-run/method"],
  ["radar-guide", "/trends/method"],
  ["radar-example", "/trends/example"],
  ["source", "/sources/ui-paper?snapshot=ui-paper-v1"],
] as const;

test.beforeEach(async ({ page }) => {
  await page.route("**/api/v1/trends/analyses/ui-run", (route) =>
    route.fulfill({ json: analysisFixture() }),
  );
  await page.route("**/api/v1/trends/analyses/*/candidates/*/description", (route) =>
    route.fulfill({ status: 503, json: {} }),
  );
  await page.route("**/api/v1/documents/ui-paper/source*", (route) =>
    route.fulfill({ json: paperEvidence }),
  );
});

for (const width of [1440, 390]) {
  for (const [name, path] of pages) {
    test(`${name} is readable and accessible at ${width}px`, async ({ page }, info) => {
      await page.setViewportSize({ width, height: 1000 });
      await page.goto(path);
      // Фикстура синхронна: ни живого опроса, ни модели нет.
      await page.waitForLoadState("networkidle");
      await page.evaluate(() => document.fonts.ready);
      await page.emulateMedia({ reducedMotion: "reduce" });
      await page.screenshot({
        path: info.outputPath("page.png"),
        fullPage: true,
        animations: "disabled",
      });
      await page.screenshot({ path: info.outputPath("viewport.png"), animations: "disabled" });
      await writeFile(info.outputPath("controls.yml"), await page.locator("body").ariaSnapshot());
      const overflow = await page.evaluate(() => ({
        width: innerWidth,
        scrollWidth: document.documentElement.scrollWidth,
        elements: [...document.querySelectorAll("main *, header *")]
          .filter((element) => {
            const rect = element.getBoundingClientRect();
            return (
              rect.width > 0 &&
              rect.right > innerWidth + 1 &&
              getComputedStyle(element).position !== "fixed"
            );
          })
          .slice(0, 10)
          .map((element) => ({
            tag: element.tagName,
            class: element.className,
            text: element.textContent?.slice(0, 70),
          })),
      }));
      expect.soft(overflow.scrollWidth, JSON.stringify(overflow)).toBeLessThanOrEqual(width);
      const audit = await new AxeBuilder({ page })
        .withTags(["wcag2a", "wcag2aa", "wcag21aa"])
        .analyze();
      expect
        .soft(
          audit.violations.map(({ id, nodes }) => ({
            id,
            nodes: nodes.map(({ target, failureSummary }) => ({ target, failureSummary })),
          })),
        )
        .toEqual([]);
      await expect(page.locator("main")).toBeVisible();
      await inspectDesignContract(page);
    });
  }
}

test.describe("entry and access", () => {
  test.use({ roles: null });
  for (const width of [1440, 390]) {
    test(`login uses the shared kit at ${width}px`, async ({ page }, info) => {
      await page.setViewportSize({ width, height: 1000 });
      await page.goto("/login");
      await page.evaluate(() => document.fonts.ready);
      await page.screenshot({ path: info.outputPath("page.png"), fullPage: true });
      await inspectDesignContract(page);
      expect(
        (await new AxeBuilder({ page }).withTags(["wcag2a", "wcag2aa"]).analyze()).violations,
      ).toEqual([]);
    });
  }
  test("anonymous entry preserves the intended page and handles invalid credentials", async ({
    page,
    api,
  }) => {
    await page.goto("/admin/users");
    await expect(page).toHaveURL(/\/login$/);
    await page.getByLabel("Имя пользователя", { exact: true }).fill("review.admin");
    await page.getByLabel("Пароль", { exact: true }).fill("incorrect-password");
    await page.getByRole("button", { name: "Войти", exact: true }).click();
    await expect(page.getByRole("alert")).toBeVisible();
    expect(api.calls.filter((call) => call.path === "/auth/login")).toHaveLength(1);
    await page.getByLabel("Пароль", { exact: true }).fill("review-password");
    await page.getByRole("button", { name: "Войти", exact: true }).click();
    await expect(page).toHaveURL(/\/admin\/users$/);
    await expect(
      page.getByRole("heading", { name: "Пользователи и роли", exact: true }),
    ).toBeVisible();
  });
});

test.describe("role restrictions", () => {
  test.use({ roles: ["user"] });
  test("a researcher gets an accessible forbidden page on admin routes", async ({ page }) => {
    await page.goto("/admin/users");
    await page.waitForLoadState("networkidle");
    const audit = await new AxeBuilder({ page }).withTags(["wcag2a", "wcag2aa"]).analyze();
    expect(audit.violations.map(({ id }) => id)).toEqual([]);
    await expect(page.getByRole("heading", { name: "Этот сектор закрыт" })).toBeVisible();
    await inspectDesignContract(page);
  });
});
