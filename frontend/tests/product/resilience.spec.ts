import { expect, test } from "./fixture";
import { inspectSurface } from "./checks";

test("an older board response cannot overwrite the latest search", async ({ page }) => {
  await page.goto("/hypotheses");
  await page.getByRole("button", { name: /Все \d/ }).click();
  let release!: () => void;
  const pending = new Promise<void>((resolve) => {
    release = resolve;
  });
  let started = false;
  await page.route("**/api/v1/hypotheses/board?**", async (route) => {
    if (new URL(route.request().url()).searchParams.get("q") === "термо") {
      started = true;
      await pending;
    }
    await route.fallback();
  });
  const search = page.getByRole("textbox", { name: "Поиск по гипотезам", exact: true });
  await search.fill("термо");
  await expect.poll(() => started).toBe(true);
  await search.fill("не-существует-в-базе");
  await expect(
    page.getByText(
      "По текущим фильтрам ничего не найдено. Измените условия поиска или сбросьте фильтры.",
    ),
  ).toBeVisible();
  const completed = page.waitForResponse(
    (response) => new URL(response.url()).searchParams.get("q") === "термо",
  );
  release();
  await completed;
  await page.waitForLoadState("networkidle");
  await expect(search).toHaveValue("не-существует-в-базе");
  await expect(
    page.getByRole("heading", { name: "Термообработка повышает стойкость композита", exact: true }),
  ).toBeHidden();
});

const failures = [
  ["documents", "/operator/documents", "/admin/documents"],
  ["goals", "/kpi", "/kpis"],
  ["topics", "/summary", "/clusters"],
  ["topic", "/clusters/cluster-1", "/clusters/cluster-1"],
  ["history", "/history", "/chats"],
  ["saved search", "/search?chat=chat-1", "/chats/chat-1/messages"],
  ["board", "/hypotheses", "/hypotheses/board"],
  ["hypothesis", "/hypotheses/hypothesis-1", "/hypotheses/hypothesis-1"],
  ["direction", "/directions/direction-1", "/hypotheses/direction-1"],
  ["portfolio report", "/hypotheses/report", "/hypotheses/board"],
  ["hypothesis report", "/hypotheses/hypothesis-1/report", "/hypotheses/hypothesis-1"],
  ["overview", "/admin", "/admin/documents/stats"],
  ["accounts", "/admin/users", "/admin/users"],
  ["settings", "/admin/settings", "/admin/settings"],
  ["budget metrics", "/admin/metrics", "/admin/llm-usage"],
] as const;

for (const [name, path, endpoint] of failures) {
  test(`${name} explains a server failure and reloads without losing the route`, async ({
    page,
    api,
  }, info) => {
    await page.setViewportSize({ width: 390, height: 900 });
    api.failures.set(`GET ${endpoint}`, 500);
    await page.goto(path);
    const error = page
      .getByRole("alert")
      .filter({ has: page.getByRole("button", { name: "Повторить", exact: true }) });
    await expect(error).toBeVisible();
    await inspectSurface(page, info, "load-error");
    await error.getByRole("button", { name: "Повторить", exact: true }).click();
    await expect(error).toBeHidden();
    await expect
      .poll(
        () => api.calls.filter((call) => call.method === "GET" && call.path === endpoint).length,
      )
      .toBeGreaterThanOrEqual(2);
    await expect(page).toHaveURL(new RegExp(`${path.replace(/[?]/g, "\\?")}$`));
    await expect(page.locator("main")).toBeVisible();
  });
}

for (const [name, endpoint] of [
  ["Генерация", "/hypotheses/runtime-settings"],
  ["Оценка", "/hypotheses/scoring-weights"],
  ["Производительность", "/admin/perf"],
]) {
  test(`${name} does not replace a failed response with defaults or empty data`, async ({
    page,
    api,
  }, info) => {
    await page.setViewportSize({ width: 390, height: 900 });
    await page.goto(name === "Производительность" ? "/admin/metrics" : "/admin/settings");
    api.failures.set(`GET ${endpoint}`, 500);
    await page.getByRole("tab", { name, exact: true }).click();
    const retry = page.getByRole("button", { name: "Повторить", exact: true });
    await expect(retry).toBeVisible();
    await inspectSurface(page, info, "settings-load-error");
    await retry.click();
    await expect(retry).toBeHidden();
    await expect
      .poll(
        () => api.calls.filter((call) => call.method === "GET" && call.path === endpoint).length,
      )
      .toBe(2);
    if (name === "Генерация")
      await expect(page.locator("#runtime-generationTimeoutSec")).toHaveValue("300");
    if (name === "Оценка") await expect(page.getByRole("slider")).toHaveCount(6);
    if (name === "Производительность")
      await expect(
        page.getByText("Задачи по гипотезам — полное время", { exact: true }),
      ).toBeVisible();
  });
}

test("settings validate rate, quality and runtime bounds before sending any update", async ({
  page,
  api,
}) => {
  await page.goto("/admin/settings");
  const saves = () => api.calls.filter((call) => call.method === "PUT");
  const save = page.getByRole("button", { name: "Сохранить", exact: true });
  await page.getByText("Тарифы и валюта", { exact: true }).click();
  await page.locator("#sys-rub-rate").fill("-1");
  await page.getByText("Тарифы и валюта", { exact: true }).click();
  await save.click();
  expect(saves()).toHaveLength(0);
  await expect(page.locator("#sys-rub-rate")).toBeFocused();
  const modelTab = page.getByRole("tab", { name: "Модель", exact: true });
  await modelTab.press("ArrowRight");
  await expect(modelTab).toHaveAttribute("aria-selected", "true");
  await expect(page.locator("#sys-rub-rate")).toBeFocused();
  await page.locator("#sys-rub-rate").fill("95");
  await page.locator("#sys-rub-rate").press("Enter");
  await expect(save).toBeHidden();
  expect(saves()).toHaveLength(1);
  await page.getByRole("tab", { name: "Источники", exact: true }).click();
  await page.getByText("Дополнительные параметры", { exact: true }).click();
  await page.locator("#sys-quality").fill("2");
  await save.last().click();
  expect(saves()).toHaveLength(1);
  await expect(page.locator("#sys-quality")).toBeFocused();
  await page.getByRole("tab", { name: "Оценка", exact: true }).click();
  await expect(page.getByRole("tab", { name: "Источники", exact: true })).toHaveAttribute(
    "aria-selected",
    "true",
  );
  await page.locator("#sys-quality").fill("0.65");
  await save.last().click();
  await expect.poll(() => saves().length).toBe(2);
  await page.getByRole("tab", { name: "Генерация", exact: true }).click();
  await page.getByText("Лимиты и исключения", { exact: true }).click();
  const timeout = page.locator("#runtime-generationTimeoutSec");
  for (const invalid of ["", "29", "601", "40.5"]) {
    await timeout.fill(invalid);
    await save.click();
    expect(saves()).toHaveLength(2);
    await expect(timeout).toBeFocused();
  }
  await timeout.fill("480");
  await timeout.press("Enter");
  await expect.poll(() => api.state.runtime.generation_timeout_sec).toBe(480);
});

test("settings show loading and lock the submitted draft until the response arrives", async ({
  page,
  api,
}) => {
  let releaseLoad!: () => void;
  const pendingLoad = new Promise<void>((resolve) => {
    releaseLoad = resolve;
  });
  await page.route("**/api/v1/admin/settings", async (route) => {
    await pendingLoad;
    await route.fallback();
  });
  await page.goto("/admin/settings");
  await expect(page.getByRole("status", { name: "Загрузка…", exact: true })).toBeVisible();
  await expect(page.locator("#sys-model")).toBeHidden();
  releaseLoad();
  await page.locator("#sys-model").fill("saved-once");
  await page.getByText("Подключение и доступ", { exact: true }).click();
  let releaseSave!: () => void;
  const pendingSave = new Promise<void>((resolve) => {
    releaseSave = resolve;
  });
  await page.route("**/api/v1/admin/settings", async (route) => {
    if (route.request().method() === "PUT") await pendingSave;
    await route.fallback();
  });
  await page.getByRole("button", { name: "Сохранить", exact: true }).click();
  await expect(page.getByRole("button", { name: "Сохранение…", exact: true })).toBeDisabled();
  await expect(page.locator("#sys-model")).toBeDisabled();
  await expect(page.getByRole("button", { name: "Сбросить модель" })).toBeDisabled();
  releaseSave();
  await expect(page.locator("#sys-model")).toBeEnabled();
  expect(
    api.calls.filter((call) => call.method === "PUT" && call.path === "/admin/settings"),
  ).toHaveLength(1);
  await page.reload();
  await expect(page.locator("#sys-model")).toHaveValue("saved-once");
});
