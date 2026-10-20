import { expect, test } from "./fixture";
import { inspectSurface } from "./checks";
import app from "../../src/shared/i18n/locales/ru/app.json" with { type: "json" };
import trend from "../../src/shared/i18n/locales/ru/trend.json" with { type: "json" };
import money from "../fixtures/early-money.json" with { type: "json" };

const luminance = async (page: import("@playwright/test").Page) =>
  page.evaluate(() => {
    const [r, g, b] = getComputedStyle(document.body)
      .backgroundColor.match(/[\d.]+/g)!
      .slice(0, 3)
      .map(Number) as [number, number, number];
    return (0.2126 * r + 0.7152 * g + 0.0722 * b) / 255;
  });

test.describe("dark theme", () => {
  test.beforeEach(async ({ page }) => {
    await page.addInitScript(() => localStorage.setItem("rag.theme", "dark"));
  });

  for (const [name, path] of [
    ["radar", "/trends"],
    ["technology", "/technologies/tech-1"],
    ["hypotheses", "/hypotheses"],
    ["catalogue", "/trends?entity=technologies&all=true"],
  ] as const) {
    test(`${name} stays readable and accessible in the dark theme`, async ({ page }, info) => {
      await page.goto(path);
      await expect(page.locator("html")).toHaveAttribute("data-theme", "dark");
      expect(await luminance(page)).toBeLessThan(0.2);
      await inspectSurface(page, info, `dark-${name}`);
    });
  }

  test("early money stays readable with its industry table in the dark theme", async ({
    page,
  }, info) => {
    await page.goto("/trends?entity=money");
    await expect(page.getByRole("heading", { name: trend.money.title })).toBeVisible();
    await expect(page.getByRole("table", { name: trend.money.rubricsTitle })).toBeVisible();
    await expect(page.getByRole("link", { name: "Giraffe Bio" })).toBeVisible();
    const rubrics = new Set(money.cells.filter((cell) => cell.rubric).map((cell) => cell.rubric));
    await expect(page.locator("[data-money-rubric]")).toHaveCount(Math.min(rubrics.size, 12));
    await inspectSurface(page, info, "dark-money");
    await page.locator("#money-region").click();
    await page.getByRole("option", { name: trend.money.regions.latam, exact: true }).click();
    await page.locator("#money-kind").click();
    const filtered = page.waitForRequest((request) => {
      const url = new URL(request.url());
      return (
        url.pathname.endsWith("/trends/live/money") &&
        url.searchParams.get("region") === "latam" &&
        url.searchParams.get("kind") === "round"
      );
    });
    await page.getByRole("option", { name: trend.money.kinds.round, exact: true }).click();
    await filtered;
    await expect(page.getByRole("link", { name: "Giraffe Bio" })).toBeVisible();
    await page.locator("#money-region").click();
    await expect(
      page.getByRole("option", { name: trend.money.regions.americas, exact: true }),
    ).toBeVisible();
    await expect(
      page.getByRole("option", { name: trend.money.regions.asia, exact: true }),
    ).toBeVisible();
    await page.keyboard.press("Escape");
  });

  test("the header switch changes the theme without a reload and remembers it", async ({
    page,
  }) => {
    await page.goto("/trends");
    await page.getByRole("button", { name: app.theme.label }).click();
    await page.getByRole("menuitem", { name: app.theme.light }).click();
    await expect(page.locator("html")).toHaveAttribute("data-theme", "light");
    expect(await luminance(page)).toBeGreaterThan(0.8);
    expect(await page.evaluate(() => localStorage.getItem("rag.theme"))).toBe("light");
    await page.getByRole("button", { name: app.theme.label }).click();
    await page.getByRole("menuitem", { name: app.theme.system }).click();
    await expect(page.locator("html")).not.toHaveAttribute("data-theme", /./);
  });
});

test("the system preference is honoured when no theme is chosen", async ({ page }) => {
  await page.emulateMedia({ colorScheme: "dark" });
  await page.goto("/trends");
  await expect(page.locator("html")).not.toHaveAttribute("data-theme", /./);
  expect(await luminance(page)).toBeLessThan(0.2);
});
