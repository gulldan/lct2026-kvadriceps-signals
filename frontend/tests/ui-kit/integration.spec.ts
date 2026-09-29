import { expect, test } from "@playwright/test";

test("updated PDF renderer and its bundled worker render text and a page", async ({ page }) => {
  const errors: string[] = [];
  page.on("pageerror", (error) => errors.push(error.message));
  await page.goto("/tests/fixtures/pdf-viewer.html");
  await expect(page.locator(".react-pdf__Page__textContent")).toContainText("UI kit PDF smoke");
  await expect(page.locator(".react-pdf__Page__canvas")).toBeVisible();
  expect(errors).toEqual([]);
});

test("product entry and router still render the login form", async ({ page }) => {
  const errors: string[] = [];
  page.on("pageerror", (error) => errors.push(error.message));
  await page.goto("/login");
  await expect(page.locator('input[type="password"]')).toBeVisible();
  await expect(page.locator('button[type="submit"]')).toBeVisible();
  expect(errors).toEqual([]);
});

test("scientific text and the catalog PDF use the shared lazy renderers", async ({ page }) => {
  const errors: string[] = [];
  page.on("pageerror", (error) => errors.push(error.message));
  await page.goto("/ui-kit.html#content");
  const scientificText = page.locator("#content");
  await expect(scientificText.locator(".katex")).toHaveCount(2);
  await expect(scientificText.locator(".katex-error")).toHaveCount(0);
  await expect(scientificText.locator("math").last()).toContainText("H");
  await page.getByRole("button", { name: "Открыть пример PDF" }).click();
  await expect(page.locator(".react-pdf__Page__textContent")).toContainText("UI kit PDF smoke");
  await page.getByRole("button", { name: "Увеличить" }).click();
  await expect(page.locator("#kit-pdf")).toContainText("120%");
  await page.getByRole("button", { name: "Закрыть пример PDF" }).click();
  await expect(page.locator("#kit-pdf")).toBeHidden();
  expect(errors).toEqual([]);
});
