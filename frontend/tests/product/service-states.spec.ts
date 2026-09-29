import { expect, test } from "./fixture";
import { inspectSurface } from "./checks";
import type { LiveSearchState } from "../../src/features/trend/liveApi";
import result from "../fixtures/trend-live-topic.json" with { type: "json" };

// Варианты экрана создают графические сцены; программный рендерер проверяет их по очереди.
test.describe.configure({ mode: "default" });

for (const theme of ["light", "dark"]) {
  for (const width of [1440, 1280, 1063, 390]) {
    test(`service pages and long search are accessible at ${width}px in ${theme}`, async ({
      page,
    }, info) => {
      await page.setViewportSize({ width, height: width === 1280 ? 720 : 900 });
      await page.addInitScript((value) => localStorage.setItem("rag.theme", value), theme);
      for (const [code, title] of [
        [403, "Этот сектор закрыт"],
        [404, "За пределами радара"],
        [500, "Система сбилась с курса"],
        [503, "Радар на паузе"],
      ] as const) {
        await page.goto(`/${code}`);
        await expect(page.getByRole("heading", { level: 1 })).toHaveText(title);
        await inspectSurface(page, info, `error-${code}-${theme}-${width}`);
      }
      await page.route("**/api/v1/trends/live/search?**", (route) =>
        route.fulfill({
          json: {
            query: "Новые технологии хранения энергии",
            status: "running",
            step: "analysis",
            step_index: 9,
            step_total: 10,
            elapsed_seconds: 214,
            typical_seconds: 320,
            found: ["Твердотельные аккумуляторы", "Натрий-ионные батареи"],
          },
        }),
      );
      await page.goto("/trends?q=Новые%20технологии%20хранения%20энергии");
      await expect(page.getByText("Обычно 12–13 минут", { exact: true })).toBeVisible();
      await expect(page.getByRole("switch", { name: "Анимация", exact: true })).toHaveCount(0);
      await page.emulateMedia({ reducedMotion: "reduce" });
      await expect(page.locator(".research-library")).toHaveAttribute(
        "data-reduced-motion",
        "true",
      );
      await expect(page.locator(".research-library canvas")).toHaveAttribute(
        "data-animating",
        "false",
      );
      await inspectSurface(page, info, `progress-${theme}-${width}`);
      expect(
        await page
          .locator('[aria-current="step"] .animate-spin')
          .evaluate((element) => getComputedStyle(element).animationName),
      ).not.toBe("spin");
      await expect(
        page.getByRole("button", { name: "К обзору направлений", exact: true }),
      ).toHaveCount(0);
      await expect(page.getByRole("textbox", { name: "Поиск направления по запросу" })).toHaveCount(
        0,
      );
    });
  }
}

test("the decorative 404 radar responds to the pointer and respects reduced motion", async ({
  page,
}) => {
  await page.goto("/404");
  const artwork = page.locator('[data-slot="status-artwork"]');
  const bounds = await artwork.boundingBox();
  expect(bounds).not.toBeNull();
  await page.mouse.move(bounds!.x + bounds!.width * 0.8, bounds!.y + bounds!.height * 0.2);
  await expect(artwork).toHaveAttribute("style", /--status-bearing:/);
  const bearing = await artwork.getAttribute("style");
  await page.emulateMedia({ reducedMotion: "reduce" });
  await page.mouse.move(bounds!.x + bounds!.width * 0.3, bounds!.y + bounds!.height * 0.8);
  await expect(artwork).toHaveAttribute("style", bearing!);
  expect(
    await page.locator(".status-needle").evaluate((node) => getComputedStyle(node).animationName),
  ).toBe("none");
  await expect(artwork).toHaveAttribute("aria-hidden", "true");
  await page.getByRole("link", { name: "На главную", exact: true }).focus();
  await page.keyboard.press("Enter");
  await expect(page).not.toHaveURL(/\/404$/);
});

test("server error recovery reloads the current page or returns home", async ({ page }) => {
  for (const code of [500, 503]) {
    await page.goto(`/${code}`);
    await Promise.all([
      page.waitForEvent("load"),
      page.getByRole("button", { name: "Повторить", exact: true }).click(),
    ]);
    await expect(page).toHaveURL(new RegExp(`/${code}$`));
    await page.getByRole("link", { name: "На главную", exact: true }).click();
    await expect(page).not.toHaveURL(new RegExp(`/${code}$`));
  }
});

test("a live search keeps honest time and findings through reconnection and completion", async ({
  page,
}) => {
  await page.clock.install({ time: new Date("2026-09-27T00:00:00Z") });
  await page.clock.pauseAt(new Date("2026-09-27T00:00:01Z"));
  await page.emulateMedia({ reducedMotion: "reduce" });
  let disconnected = false;
  let state: LiveSearchState = {
    query: "энергия",
    status: "running",
    step: "analysis",
    step_index: 9,
    elapsed_seconds: 120,
    found: ["Твердотельные аккумуляторы"],
  };
  await page.route("**/api/v1/trends/live/search?**", (route) =>
    route.fulfill({ status: disconnected ? 503 : 200, json: disconnected ? {} : state }),
  );
  await page.goto("/trends?q=энергия");
  const timer = page.locator("time[datetime^='PT']");
  await expect(timer).toHaveText("2:00");
  await page.clock.runFor(1000);
  await expect(timer).toHaveText("2:01");
  state = { ...state, elapsed_seconds: 122 };
  await page.clock.runFor(1200);
  await expect(timer).toHaveText("2:02");
  state = { ...state, elapsed_seconds: 860, step: "cards", step_index: 10 };
  await page.clock.runFor(2200);
  await expect(page.getByText(/Поиск идёт дольше 14 минут/)).toBeVisible();
  await expect(page.getByRole("listitem").filter({ hasText: "Текущий этап." })).toContainText(
    "Написание описаний",
  );
  disconnected = true;
  await page.clock.runFor(2200);
  await expect(page.getByText("Восстанавливаем соединение", { exact: true })).toBeVisible();
  await expect(page.locator(".research-library canvas")).toHaveAttribute("data-found", "1");
  await expect(page.getByRole("heading", { name: "Радар на паузе" })).toBeHidden();
  disconnected = false;
  state = {
    ...state,
    status: "ready",
    detail: result as unknown as NonNullable<LiveSearchState["detail"]>,
  };
  await page.clock.runFor(4500);
  await expect(page.getByText("Восстанавливаем соединение", { exact: true })).toBeHidden();
  await expect(timer).toBeHidden();
  await expect(page.getByRole("heading", { name: "Система сбилась с курса" })).toBeHidden();
  await expect(page.getByRole("tablist").first()).toBeVisible();
});

test("saved results skip the collection plan and a 503 retries the same query", async ({
  page,
}) => {
  let ready = false;
  await page.route("**/api/v1/trends/live/search?**", (route) =>
    route.fulfill({
      status: ready ? 200 : 503,
      json: ready
        ? {
            query: "энергия",
            status: "running",
            reopening: true,
            step: "rescore",
            elapsed_seconds: 2,
          }
        : {},
    }),
  );
  await page.goto("/trends?q=энергия");
  await expect(page.getByRole("heading", { name: "Радар на паузе" })).toBeVisible();
  ready = true;
  await page.getByRole("button", { name: "Повторить", exact: true }).click();
  await expect(page.getByRole("heading", { name: /Открываем сохранённый ответ/ })).toBeVisible();
  await expect(page.getByRole("region", { name: "Этапы исследования" })).toBeHidden();
  await expect(page.getByText("Обычно 12–13 минут", { exact: true })).toBeHidden();
  await expect(page).toHaveURL(/q=/);
});

test.describe("compact sign-in", () => {
  test.use({ roles: null });
  for (const theme of ["light", "dark"]) {
    for (const width of [1440, 1063, 768, 390]) {
      test(`login at ${width}px in ${theme}`, async ({ page }, info) => {
        await page.setViewportSize({ width, height: width === 1063 ? 908 : 900 });
        await page.addInitScript((value) => localStorage.setItem("rag.theme", value), theme);
        await page.goto("/login");
        await expect(page.getByLabel("Имя пользователя", { exact: true })).toBeFocused();
        await expect(page.getByRole("heading", { name: "Вход в систему" })).toBeVisible();
        await inspectSurface(page, info, `login-${theme}-${width}`);
        await page.getByText("Доступ к стенду", { exact: true }).click();
        await expect(
          page.getByText("Логин и пароль выдаёт организатор демо.", { exact: false }),
        ).toBeVisible();
        await expect(page.getByRole("heading", { name: "Возможности разных ролей" })).toBeVisible();
      });
    }
  }
  test("password visibility and pending sign-in preserve the draft after a service error", async ({
    page,
  }) => {
    let release!: () => void;
    const pending = new Promise<void>((resolve) => {
      release = resolve;
    });
    await page.route("**/api/v1/auth/login", async (route) => {
      await pending;
      await route.fulfill({ status: 503, json: {} });
    });
    await page.goto("/login");
    await page.getByLabel("Имя пользователя", { exact: true }).fill("review.admin");
    const password = page.getByLabel("Пароль", { exact: true });
    await password.fill("test-only-password");
    await page.getByRole("button", { name: "Показать пароль" }).click();
    await expect(password).toHaveAttribute("type", "text");
    await page.getByRole("button", { name: "Скрыть пароль" }).press("Space");
    await expect(password).toHaveAttribute("type", "password");
    await password.press("Enter");
    await expect(page.getByRole("button", { name: /Входим/ })).toBeDisabled();
    await expect(password).toBeDisabled();
    release();
    await expect(page.getByRole("alert")).toContainText("Сервис входа временно недоступен");
    await expect(password).toBeEnabled();
    await expect(password).toHaveValue("test-only-password");
    await expect(password).not.toHaveAttribute("aria-invalid", "true");
  });
});
