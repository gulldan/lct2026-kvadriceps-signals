import { expect, test } from "./fixture";
import { inspectSurface } from "./checks";

test("desktop menu preserves collapse, focus, home links and the shared search layout", async ({
  page,
}, info) => {
  await page.setViewportSize({ width: 1440, height: 950 });
  // Корень ведёт на радар, а домашний экран вопросов по документам — /search.
  await page.goto("/search");
  const nav = page.getByRole("navigation", { name: "Радар", exact: true });
  const collapse = page.getByRole("button", { name: "Свернуть меню", exact: true });
  const expand = page.getByRole("button", { name: "Развернуть меню", exact: true });
  const brand = page.getByRole("link", { name: "Технологический радар", exact: true });
  const aligned = async () => {
    const main = await page.locator("main").boundingBox();
    const home = await page.locator('[data-slot="page-layout"]').boundingBox();
    expect(Math.abs(home!.x + home!.width / 2 - (main!.x + main!.width / 2))).toBeLessThan(1);
    expect(home!.width).toBeLessThanOrEqual(1280);
    await expect(page.getByRole("heading", { level: 1 })).toHaveCSS("text-align", "start");
  };
  await expect(nav).toBeVisible();
  await aligned();
  await inspectSurface(page, info, "home-expanded");
  await page.getByRole("textbox", { name: "Поисковый запрос" }).fill("Черновик запроса");
  await collapse.focus();
  await page.keyboard.press("Enter");
  await expect(expand).toBeFocused();
  await expect(expand).toHaveAttribute("aria-expanded", "false");
  await expect(nav).toBeHidden();
  await expect.poll(async () => (await page.locator("main").boundingBox())!.x).toBe(0);
  await aligned();
  await expect(page.getByRole("textbox", { name: "Поисковый запрос" })).toHaveValue(
    "Черновик запроса",
  );
  await expect(brand).toHaveAttribute("href", "/");
  await inspectSurface(page, info, "home-collapsed");
  await page.reload();
  await expect(expand).toHaveAttribute("aria-expanded", "false");
  await page.goto("/history");
  await expect(expand).toBeVisible();
  await brand.click();
  await expect(page).toHaveURL(/\/trends$/);
  await expand.click();
  await expect(collapse).toBeFocused();
  await expect(nav).toBeVisible();
  await nav.getByRole("link", { name: "История", exact: true }).click();
  await brand.click();
  await expect(page).toHaveURL(/\/trends$/);
});

test("mobile menu closes on navigation, Escape, outside click and desktop resize", async ({
  page,
}, info) => {
  await page.setViewportSize({ width: 390, height: 844 });
  await page.goto("/history");
  const expand = page.getByRole("button", { name: "Развернуть меню", exact: true });
  const menu = page.getByRole("dialog", { name: "Главное меню", exact: true });
  await expand.click();
  await expect(menu).toBeVisible();
  await expect(menu.getByRole("link", { name: "Документы", exact: true })).toBeVisible();
  await inspectSurface(page, info, "mobile-menu");
  await page.keyboard.press("Escape");
  await expect(menu).toBeHidden();
  await expect(expand).toBeFocused();
  await expand.click();
  await menu.getByRole("link", { name: "Технологический радар", exact: true }).click();
  await expect(page).toHaveURL(/\/trends$/);
  await expect(menu).toBeHidden();
  await expand.click();
  await menu.getByRole("link", { name: "Поиск по документам", exact: true }).click();
  await expect(menu).toBeHidden();
  // Поле забирает фокус, когда модуль догрузится: до этого меню запомнит не ту кнопку.
  await expect(page.getByRole("textbox", { name: "Поисковый запрос" })).toBeFocused();
  await expand.click();
  await expect(menu).toBeVisible();
  await page.locator('[data-state="open"].bg-overlay').click({ position: { x: 375, y: 500 } });
  await expect(menu).toBeHidden();
  await expect(expand).toBeFocused();
  await expand.click();
  await page.setViewportSize({ width: 1024, height: 844 });
  await expect(menu).toBeHidden();
  await expect(page.getByRole("button", { name: "Свернуть меню", exact: true })).toBeVisible();
  await page.setViewportSize({ width: 390, height: 844 });
  await expect(menu).toBeHidden();
  await page.goto("/history");
  await page.getByRole("link", { name: "Технологический радар", exact: true }).click();
  await expect(page).toHaveURL(/\/trends$/);
  await page.goto("/search");
  await expect(page.getByRole("heading", { level: 1 })).toHaveCSS("text-align", "start");
  await inspectSurface(page, info, "mobile-home");
  await page.evaluate(() => {
    document.documentElement.style.fontSize = "200%";
  });
  const brand = page.getByRole("link", { name: "Технологический радар", exact: true });
  const brandBox = await brand.boundingBox();
  expect(brandBox!.width).toBeGreaterThan(100);
  await brand.focus();
  await expect(brand).toBeFocused();
  await inspectSurface(page, info, "mobile-home-text200");
});
