import { expect, test } from "./fixture";
import { choose, chooseRadio, inspectSurface } from "./checks";

for (const width of [1440, 390]) {
  test(`account form validates, submits by Enter, filters and preserves the admin session at ${width}px`, async ({
    page,
    api,
  }, info) => {
    await page.setViewportSize({ width, height: 900 });
    await page.goto("/admin/users");
    await page.getByRole("button", { name: "Создать пользователя", exact: true }).click();
    const dialog = page.getByRole("dialog");
    await expect(dialog.getByRole("button", { name: "Создать", exact: true })).toBeDisabled();
    await dialog.getByLabel("Имя пользователя", { exact: true }).fill("new.researcher");
    await dialog.getByLabel("Пароль", { exact: true }).fill("123");
    await expect(dialog.getByLabel("Пароль", { exact: true })).toHaveAttribute(
      "aria-invalid",
      "true",
    );
    await expect(dialog.getByRole("button", { name: "Создать", exact: true })).toBeDisabled();
    await dialog.getByLabel("Пароль", { exact: true }).fill("review-password");
    await choose(page, "Роль", "Оператор");
    await inspectSurface(page, info, "create-user");
    await dialog.getByLabel("Имя пользователя", { exact: true }).press("Enter");
    await expect(dialog).toBeHidden();
    await expect(page.getByText("new.researcher", { exact: true })).toBeVisible();
    expect(api.calls.find((call) => call.path === "/auth/register")?.body).toMatchObject({
      username: "new.researcher",
      roles: ["operator"],
    });
    await expect(page.getByRole("button", { name: /review.admin/ })).toBeVisible();
    expect(await page.evaluate(() => localStorage.getItem("rag.auth"))).toBeNull();
    await page.getByRole("textbox").fill("anna");
    await expect(page.getByText("anna.petrova", { exact: true })).toBeVisible();
    await expect(page.getByText("new.researcher", { exact: true })).toBeHidden();
    await page.getByRole("textbox").fill("absent-user");
    await expect(page.getByText("Пользователи не найдены")).toBeVisible();
    await page.getByRole("button", { name: "Создать пользователя", exact: true }).click();
    await dialog.getByLabel("Имя пользователя", { exact: true }).fill("review.admin");
    await dialog.getByLabel("Пароль", { exact: true }).fill("review-password");
    await dialog.getByRole("button", { name: "Создать", exact: true }).click();
    await expect(page.getByText(/уже существует/)).toBeVisible();
    await dialog.getByRole("button", { name: "Отмена", exact: true }).click();
    await expect(dialog).toBeHidden();
  });

  test(`settings deployment status, draft cancellation and save recovery at ${width}px`, async ({
    page,
    api,
  }, info) => {
    await page.setViewportSize({ width, height: 900 });
    await page.goto("/admin/settings");
    await expect(page.getByLabel("Модель ИИ", { exact: true })).toHaveValue("test-model");
    await expect(page.getByRole("tab")).toHaveCount(4);
    await expect(page.getByLabel("Адрес сервера", { exact: true })).toBeHidden();
    await page.getByText("Подключение и доступ", { exact: true }).click();
    await expect(page.locator("#sys-url, #sys-apikey")).toHaveCount(0);
    await expect(page.getByText("Задан", { exact: true })).toBeVisible();
    await expect(page.getByText("http://test-model:8000/v1", { exact: true })).toBeHidden();
    await page.getByLabel("Модель ИИ", { exact: true }).fill("discarded-model");
    await page.getByRole("button", { name: "Отмена", exact: true }).click();
    await expect(page.getByLabel("Модель ИИ", { exact: true })).toHaveValue("test-model");
    await page.getByLabel("Модель ИИ", { exact: true }).fill("review-model");
    await page.getByText("Тарифы и валюта", { exact: true }).click();
    await page.getByLabel("Курс доллара", { exact: true }).fill("95");
    await page.getByLabel("Валюта тарифа", { exact: true }).fill("RUB");
    await page.getByLabel("Тариф за 1 млн токенов", { exact: true }).fill("review-model=2/4");
    api.failures.set("PUT /admin/settings", 500);
    await page.getByRole("button", { name: "Сохранить", exact: true }).click();
    await expect(page.getByText("Не удалось сохранить")).toBeVisible();
    await page.getByText("Тарифы и валюта", { exact: true }).click();
    await expect(page.getByLabel("Модель ИИ", { exact: true })).toHaveValue("review-model");
    await inspectSurface(page, info, "settings-draft");
    await page.getByRole("button", { name: "Сохранить", exact: true }).click();
    await expect(page.getByRole("button", { name: "Сохранить", exact: true })).toBeHidden();
    expect(
      api.calls.findLast((call) => call.method === "PUT" && call.path === "/admin/settings")?.body,
    ).toMatchObject({
      values: {
        VLLM_MODEL: "review-model",
        LLM_RUB_PER_USD: "95",
        LLM_COST_CURRENCY: "RUB",
        LLM_PRICES: "review-model=2/4",
      },
    });
    await page.reload();
    await expect(page.getByLabel("Модель ИИ", { exact: true })).toHaveValue("review-model");
    await page.getByRole("button", { name: "Сбросить модель" }).click();
    await page.getByRole("button", { name: "Сохранить", exact: true }).click();
    expect(
      api.calls.findLast((call) => call.method === "PUT" && call.path === "/admin/settings")?.body,
    ).toEqual({ values: { VLLM_MODEL: null }, revision: 1 });
  });

  test(`generation switches, runtime thresholds and all ranking sliders at ${width}px`, async ({
    page,
    api,
  }, info) => {
    await page.setViewportSize({ width, height: 1000 });
    await page.goto("/admin/settings");
    await page.getByRole("tab", { name: "Источники", exact: true }).click();
    for (const control of await page.getByRole("switch").all()) {
      const before = await control.getAttribute("aria-checked");
      await control.click();
      await expect(control).toHaveAttribute("aria-checked", before === "true" ? "false" : "true");
    }
    await page.getByText("Дополнительные параметры", { exact: true }).click();
    await expect(page.locator("#sys-mailto")).toHaveCount(0);
    await page.locator("#sys-quality").fill("0.65");
    await page.getByRole("button", { name: "Сохранить", exact: true }).click();
    expect(
      api.calls.findLast((call) => call.path === "/admin/settings" && call.method === "PUT")?.body,
    ).toMatchObject({
      values: {
        PUBSEARCH_ENABLED: "false",
        RAG_GEN_BREADTH: "true",
        KPI_SUGGEST: "false",
        RAG_GEN_QUALITY_FLOOR: "0.65",
      },
    });
    await page.getByRole("tab", { name: "Генерация", exact: true }).click();
    await expect(page.locator("#runtime-generationTimeoutSec")).toHaveValue("300");
    await page.getByRole("switch").click();
    await page.getByText("Лимиты и исключения", { exact: true }).click();
    await page.locator("#runtime-generationTimeoutSec").fill("420");
    await page.locator("#runtime-excluded").fill("Исключить пищевую промышленность");
    await page.getByRole("tab", { name: "Модель", exact: true }).click();
    await page.getByRole("tab", { name: "Генерация", exact: true }).click();
    await expect(page.locator("#runtime-generationTimeoutSec")).toHaveValue("420");
    await expect(page.locator("#runtime-excluded")).toHaveValue("Исключить пищевую промышленность");
    await inspectSurface(page, info, "generation");
    await page.getByRole("button", { name: "Сохранить", exact: true }).click();
    await expect.poll(() => api.state.runtime.generation_timeout_sec).toBe(420);
    expect(
      api.calls.findLast(
        (call) => call.path === "/hypotheses/runtime-settings" && call.method === "PUT",
      )?.body,
    ).toMatchObject({
      generation_timeout_sec: 420,
      deep_postprocess_enabled: true,
      excluded_directions: "Исключить пищевую промышленность",
    });
    await page.getByRole("button", { name: "По умолчанию", exact: true }).click();
    await expect(page.locator("#runtime-generationTimeoutSec")).toHaveValue("300");
    await page.getByRole("tab", { name: "Оценка", exact: true }).click();
    await expect(page.getByRole("slider")).toHaveCount(6);
    for (const slider of await page.getByRole("slider").all()) {
      const before = await slider.inputValue();
      await slider.focus();
      await slider.press("ArrowRight");
      await expect(slider).not.toHaveValue(before);
    }
    const draftWeight = await page.getByRole("slider").first().inputValue();
    await page.getByRole("tab", { name: "Генерация", exact: true }).click();
    await page.getByRole("tab", { name: "Оценка", exact: true }).click();
    await expect(page.getByRole("slider").first()).toHaveValue(draftWeight);
    await inspectSurface(page, info, "ranking");
    await page.getByRole("button", { name: "Сохранить", exact: true }).click();
    await expect
      .poll(
        () =>
          api.calls.filter(
            (call) => call.path === "/hypotheses/scoring-weights" && call.method === "PUT",
          ).length,
      )
      .toBe(1);
    await expect(page.getByRole("slider").first()).toBeEnabled();
    for (const slider of await page.getByRole("slider").all()) {
      await slider.focus();
      await slider.press("Home");
      await expect(slider).toHaveValue("0");
    }
    await page.getByRole("button", { name: "Сохранить", exact: true }).click();
    await expect(page.getByText(/хотя бы один/i)).toBeVisible();
    await page.getByRole("button", { name: "По умолчанию", exact: true }).click();
    await expect(page.getByRole("slider").first()).not.toHaveValue("0");
  });

  test(`metrics tabs, periods, chart units and refresh at ${width}px`, async ({
    page,
    api,
  }, info) => {
    await page.setViewportSize({ width, height: 900 });
    await page.goto("/admin/metrics");
    await chooseRadio(page, "30 дней");
    await expect(page.getByText(/за 30 дн/)).toBeVisible();
    await chooseRadio(page, "токены");
    await expect(page.getByRole("radio", { name: "токены", exact: true })).toBeChecked();
    await chooseRadio(page, "запросы");
    await chooseRadio(page, "7 дней");
    await page.getByRole("button", { name: "обновить", exact: true }).click();
    await page.getByRole("tab", { name: "Производительность", exact: true }).click();
    await expect.poll(() => api.calls.some((call) => call.path === "/admin/perf")).toBe(true);
    await inspectSurface(page, info, "performance");
    const before = api.calls.filter((call) => call.path === "/admin/perf").length;
    await page.getByRole("button", { name: "обновить", exact: true }).click();
    await expect
      .poll(() => api.calls.filter((call) => call.path === "/admin/perf").length)
      .toBeGreaterThan(before);
    await page.getByRole("tab", { name: "Бюджет и расход", exact: true }).click();
    await expect(page.getByRole("tab", { name: "Бюджет и расход", exact: true })).toHaveAttribute(
      "aria-selected",
      "true",
    );
  });
}
