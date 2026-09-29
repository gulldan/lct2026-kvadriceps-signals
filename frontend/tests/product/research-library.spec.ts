import { expect, test } from "./fixture";
import { inspectSurface } from "./checks";
import type { Page } from "@playwright/test";
import type { LiveSearchState } from "../../src/features/trend/liveApi";
import result from "../fixtures/trend-live-topic.json" with { type: "json" };

// Программный рендерер делит ресурсы между страницами: сцены проверяем по очереди.
test.describe.configure({ mode: "default" });

async function navigateWithinDocument(page: Page, path: string) {
  await page.evaluate((nextPath) => {
    history.pushState(null, "", nextPath);
    dispatchEvent(new PopStateEvent("popstate"));
  }, path);
}

async function setDocumentHidden(page: Page, hidden: boolean) {
  await page.evaluate((value) => {
    Object.defineProperty(document, "hidden", { configurable: true, value });
    Object.defineProperty(document, "visibilityState", {
      configurable: true,
      value: value ? "hidden" : "visible",
    });
    document.dispatchEvent(new Event("visibilitychange"));
  }, hidden);
}

async function expectLibraryFitsViewport(page: Page) {
  await page.evaluate(() => document.fonts.ready);
  await expect
    .poll(() =>
      page.evaluate(
        () =>
          Math.max(document.documentElement.scrollHeight, document.body.scrollHeight) - innerHeight,
      ),
    )
    .toBeLessThanOrEqual(1);
  const progress = page.getByRole("region", { name: "Что делает поиск" });
  const scrollable = await progress.evaluate((root) =>
    [...root.querySelectorAll<HTMLElement>("*")]
      .filter(
        (element) =>
          element.scrollHeight > element.clientHeight + 1 &&
          /auto|scroll/.test(getComputedStyle(element).overflowY),
      )
      .map((element) => ({ tag: element.tagName, class: element.className })),
  );
  expect(scrollable, "The loader must not substitute an internal scrollbar").toEqual([]);
  await expect(progress).toBeInViewport({ ratio: 1 });
  const stages = page.getByRole("region", { name: "Этапы исследования" });
  await expect(stages).toBeInViewport({ ratio: 1 });
  await expect(stages.getByRole("listitem").last()).toBeInViewport({ ratio: 1 });
}

test("export the library poster", async ({ page }) => {
  test.skip(process.env.UPDATE_LIBRARY_POSTER !== "1", "Explicit artwork export only");
  await page.setViewportSize({ width: 1680, height: 900 });
  await page.emulateMedia({ reducedMotion: "reduce" });
  await page.goto("/tests/fixtures/research-library.html?debug=1");
  await expect(page.locator(".research-library")).toHaveAttribute("data-renderer", "ready");
  await page.getByRole("combobox", { name: "Этап поиска" }).click();
  await page.getByRole("option", { name: "Сравнение направлений", exact: true }).click();
  await expect(page.locator(".research-library")).toHaveAttribute("data-activity", "analyze");
  await page.addStyleTag({
    content:
      ":root, body { background: transparent !important; } .library-camera-tools { display: none !important; }",
  });
  await page.locator(".library-scene").screenshot({
    path: new URL("../../src/features/trend/ui/library/poster.png", import.meta.url).pathname,
    omitBackground: true,
  });
});

test("research remains usable when WebGL is unavailable", async ({ page }) => {
  await page.addInitScript(() => {
    const original = HTMLCanvasElement.prototype.getContext;
    Object.defineProperty(HTMLCanvasElement.prototype, "getContext", {
      value(this: HTMLCanvasElement, id: string, ...args: unknown[]) {
        return id === "webgl2" ? null : Reflect.apply(original, this, [id, ...args]);
      },
    });
  });
  await page.goto("/tests/fixtures/research-library.html");
  const library = page.locator(".research-library");
  await expect(library).toHaveAttribute("data-renderer", "fallback");
  await expect(page.getByRole("combobox", { name: "Этап поиска" })).toHaveCount(0);
  await expect(page.getByRole("button", { name: "Завершить поиск" })).toHaveCount(0);
  await expect(page.getByRole("switch", { name: "Анимация", exact: true })).toHaveCount(0);
  expect(
    await library.locator("img").evaluate((image: HTMLImageElement) => image.naturalWidth),
  ).toBeGreaterThan(500);
  await navigateWithinDocument(page, "/tests/fixtures/research-library.html?debug=1");
  await page.getByRole("combobox", { name: "Этап поиска" }).click();
  await page.getByRole("option", { name: "Написание описаний", exact: true }).click();
  await expect(library).toHaveAttribute("data-activity", "write");
  await expect(
    page.getByRole("region", { name: "Этапы исследования" }).locator('[aria-current="step"]'),
  ).toContainText("Написание описаний");
  await page.getByRole("button", { name: "Завершить поиск", exact: true }).click();
  await expect(library).toHaveCount(0);
  await expect(page.getByRole("tablist").first()).toBeVisible();
});

test("library details can be explored by mouse and keyboard without resuming reduced motion", async ({
  page,
}, info) => {
  await page.setViewportSize({ width: 1440, height: 1000 });
  await page.emulateMedia({ reducedMotion: "reduce" });
  await page.goto("/tests/fixtures/research-library.html");
  const library = page.locator(".research-library");
  const canvas = library.locator("canvas");
  const zoom = page.getByRole("button", { name: "Приблизить библиотеку", exact: true });
  await expect(library).toHaveAttribute("data-renderer", "ready");
  await expect(canvas).toHaveAttribute("data-animating", "false");
  // Игровой вид: камера сразу приближена и ведёт архивариуса.
  await expect(canvas).toHaveAttribute("data-zoom", "3.25");
  await expect(library).toHaveAttribute("data-exploring", "true");
  await inspectSurface(page, info, "library-world-desktop");
  await zoom.click();
  await expect(canvas).toHaveAttribute("data-zoom", "4");
  const box = (await canvas.boundingBox())!;
  const beforeDrag = await canvas.screenshot();
  await page.mouse.move(box.x + box.width * 0.5, box.y + box.height * 0.5);
  await page.mouse.down();
  await page.mouse.move(box.x + box.width * 0.6, box.y + box.height * 0.68, { steps: 8 });
  await page.mouse.up();
  expect((await canvas.screenshot()).equals(beforeDrag)).toBe(false);
  await expect(canvas).toHaveAttribute("data-animating", "false");
  // На максимальном масштабе кнопка приближения выключена, клавиши слушает кнопка отдаления.
  const zoomOut = page.getByRole("button", { name: "Отдалить библиотеку", exact: true });
  await zoomOut.focus();
  const beforeKey = await canvas.screenshot();
  // Вниз — обратно к архивариусу: вверх вид мог упереться в границу зала после перетаскивания.
  await zoomOut.press("ArrowDown");
  expect((await canvas.screenshot()).equals(beforeKey)).toBe(false);
  await inspectSurface(page, info, "library-world-details");
  await zoomOut.press("0");
  await expect(canvas).toHaveAttribute("data-zoom", "1");
  await expect(library).toHaveAttribute("data-exploring", "false");
  await page.setViewportSize({ width: 390, height: 844 });
  await expectLibraryFitsViewport(page);
  await inspectSurface(page, info, "library-world-mobile");
  await zoom.click();
  await expect(canvas).toHaveAttribute("data-zoom", "1.75");
  await expect(canvas).toHaveAttribute("data-animating", "false");
  await page.getByRole("button", { name: "Показать весь зал", exact: true }).click();
  await expect(canvas).toHaveAttribute("data-zoom", "1");
});

test("library follows server stages, source pauses and reconnection without inventing visits", async ({
  page,
}, info) => {
  await page.clock.install({ time: new Date("2026-09-27T00:00:00Z") });
  await page.setViewportSize({ width: 1440, height: 900 });
  await page.emulateMedia({ reducedMotion: "reduce" });
  let disconnected = false;
  let state: LiveSearchState = {
    query: "Новые технологии хранения энергии",
    status: "running",
    elapsed_seconds: 0,
  };
  await page.route("**/api/v1/trends/live/search?**", (route) =>
    route.fulfill({ status: disconnected ? 503 : 200, json: disconnected ? {} : state }),
  );
  await page.goto(`/trends?q=${encodeURIComponent(state.query)}`);
  const library = page.locator(".research-library");
  await expect(library).toHaveAttribute("data-activity", "prepare");
  await expect(library).toHaveAttribute("data-renderer", "ready");
  const canvas = library.locator("canvas");
  const animation = page.getByRole("switch", { name: "Анимация", exact: true });
  const queryField = page.getByRole("textbox", { name: "Поиск направления по запросу" });
  await expect(queryField).toHaveCount(0);
  await expect(animation).toHaveCount(0);
  await expect(library).toHaveAttribute("data-reduced-motion", "true");
  await expect(canvas).toHaveAttribute("data-animating", "false");
  state = { ...state, step: "github", elapsed_seconds: 30, visited_steps: ["arxiv", "github"] };
  await page.clock.runFor(2200);
  await expect(library).toHaveAttribute("data-activity", "collect");
  await expect(library).toHaveAttribute("data-shelf", "engineering");
  const stages = page.getByRole("region", { name: "Этапы исследования" });
  await expect(stages.locator('[aria-current="step"]')).toContainText("GitHub");
  await expect(stages.getByRole("listitem").filter({ hasText: "Этап пройден." })).toHaveCount(1);
  await expect(canvas).toHaveAttribute("data-consulted", "1");
  const pausedFrame = await canvas.getAttribute("data-frame");
  await page.clock.runFor(1000);
  await expect(canvas).toHaveAttribute("data-frame", pausedFrame!);
  await expectLibraryFitsViewport(page);
  const placement = await stages.evaluate((element) => ({
    stagesLeft: element.getBoundingClientRect().left,
    sceneRight: document.querySelector(".research-library")!.getBoundingClientRect().right,
  }));
  expect(placement.stagesLeft).toBeGreaterThanOrEqual(placement.sceneRight);
  await inspectSurface(page, info, "library-collect-desktop");
  await page.emulateMedia({ reducedMotion: "no-preference" });
  await expect(library).toHaveAttribute("data-reduced-motion", "false");
  await expect(library).toHaveAttribute("data-paused", "false");
  await expect(canvas).toHaveAttribute("data-animating", "true");
  await page.clock.runFor(200);
  expect(await canvas.getAttribute("data-frame")).not.toBe(pausedFrame);
  // Дальше проверяется протокол поиска; сотни синтетических кадров не нужны для опроса сервера.
  await page.emulateMedia({ reducedMotion: "reduce" });
  await expect(canvas).toHaveAttribute("data-animating", "false");

  state = { ...state, step: "arxiv", wait_seconds: 10, elapsed_seconds: 40 };
  await page.clock.runFor(2200);
  await expect(library).toHaveAttribute("data-activity", "wait");
  await expect(page.getByText(/arXiv: ожидаем ещё \d+ с\./)).toBeVisible();
  await expectLibraryFitsViewport(page);
  state = {
    ...state,
    wait_seconds: 0,
    step: "analysis",
    elapsed_seconds: 50,
    visited_steps: ["arxiv", "github", "analysis"],
    found: ["Твердотельные аккумуляторы"],
  };
  await page.clock.runFor(2200);
  await expect(library).toHaveAttribute("data-activity", "analyze");
  await expect(library).toHaveAttribute("data-shelf", "desk");
  await expect(canvas).toHaveAttribute("data-found", "1");
  disconnected = true;
  await page.clock.runFor(2200);
  await expect(library).toHaveAttribute("data-activity", "reconnect");
  await expect(library).toHaveAttribute("data-pose", "analyze");
  await expect(library).toHaveAttribute("data-paused", "true");
  await expect(page.getByText("Восстанавливаем соединение", { exact: true })).toBeVisible();
  await expectLibraryFitsViewport(page);
  disconnected = false;
  state = { ...state, step: "cards", visited_steps: [...state.visited_steps!, "cards"] };
  await page.clock.runFor(4500);
  await expect(library).toHaveAttribute("data-activity", "write");

  await page.setViewportSize({ width: 390, height: 844 });
  await page.emulateMedia({ reducedMotion: "reduce" });
  await expect(library).toHaveAttribute("data-reduced-motion", "true");
  await expect(canvas).toHaveAttribute("data-animating", "false");
  await expect(canvas).toHaveAttribute("data-motion", "write");
  await expectLibraryFitsViewport(page);
  await inspectSurface(page, info, "library-write-mobile");
  await page.setViewportSize({ width: 390, height: 720 });
  await expectLibraryFitsViewport(page);
  await setDocumentHidden(page, true);
  await page.emulateMedia({ reducedMotion: "no-preference" });
  await expect(library).toHaveAttribute("data-paused", "true");
  await expect(library).toHaveAttribute("data-reduced-motion", "false");
  await expect(canvas).toHaveAttribute("data-animating", "false");
  state = {
    ...state,
    status: "ready",
    detail: result as unknown as NonNullable<LiveSearchState["detail"]>,
  };
  await page.clock.runFor(2200);
  await expect(library).toHaveCount(0);
  await setDocumentHidden(page, false);
  await expect(page.getByRole("tablist").first()).toBeVisible();
  await expect(queryField).toBeVisible();
});

test("ready research keeps its canvas and makes results available before the finale ends", async ({
  page,
}) => {
  await page.clock.install({ time: new Date("2026-09-27T00:00:00Z") });
  await page.emulateMedia({ reducedMotion: "reduce" });
  await page.setViewportSize({ width: 1440, height: 900 });
  let state: LiveSearchState = {
    query: "Новые технологии хранения энергии",
    status: "running",
    step: "cards",
    elapsed_seconds: 150,
    visited_steps: ["arxiv", "github", "analysis", "cards"],
    found: ["Твердотельные аккумуляторы"],
  };
  await page.route("**/api/v1/trends/live/search?**", (route) => route.fulfill({ json: state }));
  await page.goto(`/trends?q=${encodeURIComponent(state.query)}`);
  const library = page.locator(".research-library");
  await expect(library).toHaveAttribute("data-renderer", "ready");
  // Сначала завершаем загрузку сцены: остановленные часы не должны задерживать её инициализацию.
  await page.clock.pauseAt(await page.evaluate(() => Date.now() + 50));
  await page.emulateMedia({ reducedMotion: "no-preference" });
  await expect(library).toHaveAttribute("data-reduced-motion", "false");
  const canvas = await library.locator("canvas").elementHandle();
  state = {
    ...state,
    status: "ready",
    elapsed_seconds: 0,
    detail: result as unknown as NonNullable<LiveSearchState["detail"]>,
  };
  // Пропуск времени проверяет опрос сервера и оставляет финалу один кадр.
  await page.clock.fastForward(2200);
  await expect(library).toHaveAttribute("data-activity", "finish");
  expect(await canvas!.evaluate((node) => node.isConnected)).toBe(true);
  await expect(page.getByRole("switch", { name: "Анимация", exact: true })).toHaveCount(0);
  await expect(page.getByRole("textbox", { name: "Поиск направления по запросу" })).toHaveCount(0);
  const showResults = page.getByRole("button", { name: "Показать результаты", exact: true });
  await expect(showResults).toBeEnabled();
  await expectLibraryFitsViewport(page);
  await expect(page.getByRole("tablist")).toHaveCount(0);
  const timer = page.locator("time[datetime^='PT']");
  await expect(timer).toHaveText("2:30");
  await page.clock.fastForward(1000);
  await expect(timer).toHaveText("2:30");
  await showResults.focus();
  await showResults.press("Enter");
  await expect(library).toHaveCount(0);
  await expect(page.getByRole("tablist").first()).toBeVisible();
});

test("query changes cancel the old finale while reduced motion and saved searches release results directly", async ({
  page,
}) => {
  await page.clock.install({ time: new Date("2026-09-27T00:00:00Z") });
  await page.emulateMedia({ reducedMotion: "reduce" });
  const originalQuery = "Новые технологии хранения энергии";
  const nextQuery = "Повторное использование аккумуляторов";
  const savedQuery = "Водородные технологии";
  const searches: Record<string, LiveSearchState> = {
    [originalQuery]: {
      query: originalQuery,
      status: "running",
      step: "cards",
      elapsed_seconds: 150,
    },
    [nextQuery]: {
      query: nextQuery,
      status: "running",
      step: "web",
      elapsed_seconds: 15,
    },
    [savedQuery]: {
      query: savedQuery,
      status: "running",
      reopening: true,
      step: "rescore",
      elapsed_seconds: 2,
    },
  };
  await page.route("**/api/v1/trends/live/search?**", (route) => {
    const query = new URL(route.request().url()).searchParams.get("q")!;
    return route.fulfill({ json: searches[query] });
  });
  await page.goto(`/trends?q=${encodeURIComponent(originalQuery)}`);
  const library = page.locator(".research-library");
  await expect(library).toHaveAttribute("data-renderer", "ready");
  await page.clock.pauseAt(await page.evaluate(() => Date.now() + 50));
  await page.emulateMedia({ reducedMotion: "no-preference" });
  await expect(library).toHaveAttribute("data-reduced-motion", "false");
  searches[originalQuery] = {
    ...searches[originalQuery]!,
    status: "ready",
    detail: result as unknown as NonNullable<LiveSearchState["detail"]>,
  };
  await page.clock.fastForward(2200);
  await expect(library).toHaveAttribute("data-activity", "finish");

  const queryField = page.getByRole("textbox", { name: "Поиск направления по запросу" });
  await expect(queryField).toHaveCount(0);
  await navigateWithinDocument(page, `/trends?q=${encodeURIComponent(nextQuery)}`);
  await expect(library).toHaveAttribute("data-activity", "collect");
  await expect(library).toHaveAttribute("data-renderer", "ready");
  await page.emulateMedia({ reducedMotion: "reduce" });
  await expect(library).toHaveAttribute("data-reduced-motion", "true");
  await page.clock.fastForward(10_000);
  await expect(library).toHaveAttribute("data-activity", "collect");
  await expect(page.getByRole("button", { name: "Показать результаты" })).toHaveCount(0);
  await expect(page.getByRole("tablist")).toHaveCount(0);
  await expect(page.getByRole("heading", { name: `Ищем «${nextQuery}»…` })).toBeVisible();
  await page.emulateMedia({ reducedMotion: "no-preference" });

  searches[nextQuery] = {
    ...searches[nextQuery]!,
    status: "ready",
    detail: result as unknown as NonNullable<LiveSearchState["detail"]>,
  };
  await page.clock.fastForward(2200);
  await expect(library).toHaveAttribute("data-activity", "finish");
  await page.emulateMedia({ reducedMotion: "reduce" });
  await expect(library).toHaveCount(0);
  await expect(page.getByRole("tablist").first()).toBeVisible();
  await expect(queryField).toBeVisible();

  await page.emulateMedia({ reducedMotion: "no-preference" });
  await queryField.fill(savedQuery);
  await queryField.press("Enter");
  await expect(page.getByRole("heading", { name: /Открываем сохранённый ответ/ })).toBeVisible();
  await expect(library).toHaveCount(0);
  searches[savedQuery] = {
    ...searches[savedQuery]!,
    status: "ready",
    reopening: false,
    detail: result as unknown as NonNullable<LiveSearchState["detail"]>,
  };
  await page.clock.fastForward(1200);
  await expect(library).toHaveCount(0);
  await expect(page.getByRole("button", { name: "Показать результаты" })).toHaveCount(0);
  await expect(page.getByRole("tablist").first()).toBeVisible();
});

test("debug playback controls pause only the scene and the finale releases the actual result", async ({
  page,
}, info) => {
  await page.clock.install();
  await page.emulateMedia({ reducedMotion: "reduce" });
  await page.goto("/tests/fixtures/research-library.html?debug=1");
  await page.getByRole("combobox", { name: "Этап поиска" }).click();
  await page.getByRole("option", { name: "Написание описаний", exact: true }).click();
  const library = page.locator(".research-library");
  const canvas = library.locator("canvas");
  await expect(library).toHaveAttribute("data-renderer", "ready");
  await expect(canvas).toHaveAttribute("data-motion", "write");
  await page.clock.pauseAt(await page.evaluate(() => Date.now() + 50));
  await page.emulateMedia({ reducedMotion: "no-preference" });
  await expect(library).toHaveAttribute("data-reduced-motion", "false");
  const animation = page.getByRole("switch", { name: "Анимация", exact: true });
  await expect(animation).toBeChecked();
  await animation.focus();
  await animation.press("Space");
  await expect(animation).not.toBeChecked();
  await expect(library).toHaveAttribute("data-paused", "true");
  await expect(canvas).toHaveAttribute("data-animating", "false");
  const pausedFrame = await canvas.getAttribute("data-frame");
  await page.clock.fastForward(1000);
  await expect(canvas).toHaveAttribute("data-frame", pausedFrame!);
  // Скрытые отладочные настройки не управляют обычным предпросмотром.
  await navigateWithinDocument(page, "/tests/fixtures/research-library.html");
  await expect(animation).toHaveCount(0);
  await expect(library).toHaveAttribute("data-paused", "false");
  await expect(canvas).toHaveAttribute("data-animating", "true");
  await navigateWithinDocument(page, "/tests/fixtures/research-library.html?debug=1");
  await expect(animation).not.toBeChecked();
  await expect(canvas).toHaveAttribute("data-animating", "false");
  const resumedFrame = await canvas.getAttribute("data-frame");
  await animation.press("Space");
  await expect(animation).toBeChecked();
  await expect(library).toHaveAttribute("data-paused", "false");
  await expect(canvas).toHaveAttribute("data-animating", "true");
  await page.clock.fastForward(100);
  expect(await canvas.getAttribute("data-frame")).not.toBe(resumedFrame);
  await page.getByRole("button", { name: "Завершить поиск", exact: true }).click();
  // Одинаковое время кадров при параллельном запуске: скорость программного рендера не меняет сюжет.
  for (let frame = 0; frame < 18; frame++) await page.clock.fastForward(100);
  await expect(canvas).toHaveAttribute("data-motion", "open");
  await canvas.screenshot({ path: info.outputPath("library-finale.png") });
  for (let frame = 0; frame < 20; frame++) await page.clock.fastForward(100);
  await expect(library).toHaveCount(0);
  await expect(page.getByRole("tablist").first()).toBeVisible();
  await page.getByRole("button", { name: "Начать заново", exact: true }).click();
  await expect(animation).toBeChecked();
  await expect(library).toHaveAttribute("data-renderer", "ready");
  await animation.press("Space");
  await expect(library).toHaveAttribute("data-paused", "true");
  await page.getByRole("button", { name: "Завершить поиск", exact: true }).click();
  await expect(library).toHaveCount(0);
  await expect(page.getByRole("tablist").first()).toBeVisible();
});
