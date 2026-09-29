import { expect, test } from "./fixture";
import { choose, inspectSurface } from "./checks";

for (const width of [1440, 390]) {
  test(`every navigation destination, language, profile mode, logout and home links at ${width}px`, async ({
    page,
  }, info) => {
    await page.setViewportSize({ width, height: 900 });
    await page.goto("/");
    const nav = page
      .getByRole("navigation", { name: "Радар", exact: true })
      .filter({ visible: true });
    for (const [label, path] of [
      ["Документы", "/operator/documents"],
      ["Тезисы", "/kpi"],
      ["Прогнозы и гипотезы", "/hypotheses"],
      ["Темы документов", "/summary"],
      ["История", "/history"],
      ["Обзор", "/admin"],
      ["Метрики", "/admin/metrics"],
      ["Пользователи", "/admin/users"],
      ["Настройки", "/admin/settings"],
      ["Поиск по документам", "/search"],
    ]) {
      if (width < 768)
        await page.getByRole("button", { name: "Развернуть меню", exact: true }).click();
      await nav.getByRole("link", { name: label!, exact: true }).click();
      await expect(page).toHaveURL(new RegExp(`${path === "/" ? "/$" : path + "$"}`));
      await expect(page.locator("main")).toBeVisible();
      if (width < 768)
        await expect(page.getByRole("dialog", { name: "Главное меню" })).toBeHidden();
    }
    for (const [language, code] of [
      ["English", "en"],
      ["中文", "zh"],
      ["Русский", "ru"],
    ]) {
      await page.getByRole("button", { name: /^(Русский|English|中文)$/ }).click();
      await page.getByRole("menuitem", { name: language!, exact: true }).click();
      await expect(page.locator("html")).toHaveAttribute("lang", code!);
    }
    await page.getByRole("button", { name: /review.admin/ }).click();
    await page.getByRole("menuitem", { name: "Оператор", exact: true }).click();
    await expect(page.getByRole("button", { name: /review.admin, Оператор/ })).toBeVisible();
    await expect(nav.getByRole("link", { name: "Настройки", exact: true })).toBeHidden();
    await page.getByRole("button", { name: /review.admin/ }).click();
    await page.getByRole("menuitem", { name: "Пользователь", exact: true }).click();
    await page.getByRole("button", { name: /review.admin/ }).click();
    await page.getByRole("menuitem", { name: "Администратор", exact: true }).click();
    await page.getByRole("button", { name: "Уведомления", exact: true }).click();
    const notificationBox = await page
      .getByRole("dialog", { name: "Уведомления", exact: true })
      .boundingBox();
    expect(notificationBox).not.toBeNull();
    expect(notificationBox!.x).toBeGreaterThanOrEqual(0);
    expect(notificationBox!.x + notificationBox!.width).toBeLessThanOrEqual(width);
    await inspectSurface(page, info, "notifications");
    await page.keyboard.press("Escape");
    await page.goto("/missing-page");
    await page.getByRole("link", { name: "На главную", exact: true }).click();
    // Корень сразу уводит на радар: «/» ловился только в миг до перенаправления.
    await expect(page).toHaveURL(/\/trends$/);
    await page.getByRole("button", { name: /review.admin/ }).click();
    await page.getByRole("menuitem", { name: "Выйти", exact: true }).click();
    await expect(page).toHaveURL(/\/login$/);
    await expect(page.getByLabel("Имя пользователя", { exact: true })).toBeFocused();
    expect(await page.evaluate(() => localStorage.getItem("rag.auth"))).toBeNull();
    await inspectSurface(page, info, "login");
  });

  test(`topic filtering, map keyboard entry, source preview and generation at ${width}px`, async ({
    page,
    api,
  }, info) => {
    await page.setViewportSize({ width, height: 950 });
    await page.goto("/summary");
    await page
      .getByRole("textbox", { name: "Поиск по темам", exact: true })
      .fill("несуществующая тема");
    await expect(page.getByText("Ничего не нашлось", { exact: true })).toBeVisible();
    await page.getByRole("button", { name: "Сбросить фильтры", exact: true }).click();
    await page.getByRole("combobox", { name: "Сортировка тем" }).click();
    const sorts = await page.getByRole("option").allTextContents();
    await page.keyboard.press("Escape");
    for (const sort of sorts) await choose(page, "Сортировка тем", sort.trim());
    await page.getByRole("button", { name: "Только с индексом", exact: true }).click();
    await expect(
      page.getByRole("button", { name: "Только с индексом", exact: true }),
    ).toHaveAttribute("aria-pressed", "true");
    await page.getByRole("button", { name: "Только с индексом", exact: true }).click();
    await expect(
      page.getByRole("button", { name: "Только с индексом", exact: true }),
    ).toHaveAttribute("aria-pressed", "false");
    await page.getByRole("tab", { name: "Карта тем", exact: true }).click();
    await expect(
      page.getByRole("group", {
        name: "Карта тем: свежесть источников и отличие от базы",
        exact: true,
      }),
    ).toBeVisible();
    await inspectSurface(page, info, "topic-map");
    const point = page.locator('[data-theme-point="cluster-1"]');
    await point.focus();
    await point.press("Enter");
    await expect(point).toHaveAttribute("aria-pressed", "true");
    await page.locator("summary").filter({ hasText: "Полимерные композиты" }).press("Enter");
    await page.getByRole("link", { name: "Открыть тему и документы", exact: true }).click();
    await expect(page).toHaveURL(/\/clusters\/cluster-1$/);
    await page.getByRole("button", { name: /research-1.pdf/ }).click();
    await expect(page.getByRole("dialog")).toBeVisible();
    await inspectSurface(page, info, "cluster-source");
    await page.keyboard.press("Escape");
    await page.getByRole("button", { name: "Сгенерировать гипотезы по теме", exact: true }).click();
    await expect
      .poll(() =>
        api.calls.some(
          (call) =>
            call.method === "POST" &&
            call.path === "/hypothesis-jobs" &&
            call.body.kpi_title === "Полимерные композиты",
        ),
      )
      .toBe(true);
    await page.goto("/directions/direction-1");
    await page.getByRole("button", { name: "Сгенерировать гипотезы", exact: true }).click();
    await expect.poll(() => api.state.jobs.length).toBe(2);
  });
}

test.describe("deep link authentication", () => {
  test.use({ roles: null });
  test("login preserves saved-chat query parameters", async ({ page }) => {
    await page.goto("/search?chat=chat-1");
    await expect(page).toHaveURL(/\/login$/);
    await page.getByLabel("Имя пользователя", { exact: true }).fill("review.admin");
    await page.getByLabel("Пароль", { exact: true }).fill("review-password");
    await page.getByLabel("Пароль", { exact: true }).press("Enter");
    await expect(page).toHaveURL(/\/search\?chat=chat-1$/);
    await expect(
      page.getByRole("heading", { name: "Как повысить термостойкость композита?", exact: true }),
    ).toBeVisible();
  });
});
