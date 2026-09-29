import { expect, test } from "./fixture";
import { chooseRadio, inspectSurface } from "./checks";
import map from "../fixtures/trend-live-map.json" with { type: "json" };
import ru from "../../src/shared/i18n/locales/ru/trend.json" with { type: "json" };

for (const [width, height, theme] of [
  [1024, 768, "light"],
  [1280, 720, "dark"],
] as const) {
  test(`radar fits the laptop and keeps both modes aligned at ${width}px ${theme}`, async ({
    page,
  }, info) => {
    await page.setViewportSize({ width, height });
    await page.addInitScript((value) => localStorage.setItem("rag.theme", value), theme);
    let releaseCatalogue: (() => void) | undefined;
    const catalogueModule = new Promise<void>((resolve) => {
      releaseCatalogue = resolve;
    });
    await page.route("**/src/features/trend/pages/TechnologyCatalogue.tsx*", async (route) => {
      await catalogueModule;
      await route.continue();
    });
    const overview = structuredClone(map);
    // Все три цвета и обе области нужны, чтобы короткая фикстура не скрыла перенос легенды.
    for (const [index, topic] of overview.topics.entries()) {
      Object.assign(topic, {
        stage: "early_signal",
        branch: index % 2 ? "computing" : "earth",
        comparison: {
          prior_start: "2025-09-24",
          recent_start: "2026-03-27",
          end_exclusive: "2026-09-27",
          days: 184,
          prior: 100,
          current: [125, 175, 250][index % 3],
          fresh: index + 1,
        },
      });
    }
    await page.route("**/api/v1/trends/live", (route) => route.fulfill({ json: overview }));
    await page.goto("/trends");
    await expect(page.locator("[data-live-topic]")).toHaveCount(overview.topics.length);
    await inspectSurface(page, info, `radar-${width}-${theme}`);
    const dimensions = await page.evaluate(() => ({
      width: document.documentElement.scrollWidth,
      height: document.documentElement.scrollHeight,
    }));
    expect(dimensions).toEqual({ width, height });
    const heading = page.getByRole("heading", { level: 1 });
    const scope = page.getByRole("group", { name: ru.scope.entity, exact: true });
    const search = page.getByRole("textbox", { name: ru.study.live.searchLabel });
    const before = {
      heading: await heading.boundingBox(),
      scope: await scope.boundingBox(),
      search: await search.boundingBox(),
    };
    expect(before.scope!.height).toBeGreaterThanOrEqual(40);
    await expect(
      page.locator("summary").filter({ hasText: ru.study.live.collection.title }),
    ).toHaveCount(0);
    await expect(page.locator("summary").filter({ hasText: ru.study.live.how })).toHaveCount(0);
    await page.getByRole("button", { name: ru.study.live.quickQueries }).click();
    await expect(page.getByRole("menuitem")).toHaveCount(6);
    await page.keyboard.press("Escape");
    await chooseRadio(page, ru.scope.technologies);
    await expect(page.getByRole("status", { name: ru.study.loading, exact: true })).toBeVisible();
    expect(await heading.boundingBox()).toEqual(before.heading);
    expect(await scope.boundingBox()).toEqual(before.scope);
    await expect(
      page.getByRole("radio", { name: ru.scope.technologies, exact: true }),
    ).toBeFocused();
    releaseCatalogue?.();
    await expect(
      page.getByRole("link", { name: "Щелочной электролизёр", exact: true }),
    ).toBeVisible();
    await expect(heading).toHaveText(ru.study.title);
    const catalogueSearch = page.getByRole("textbox", { name: ru.catalogue.searchPlaceholder });
    expect(await heading.boundingBox()).toEqual(before.heading);
    expect(await scope.boundingBox()).toEqual(before.scope);
    expect(await catalogueSearch.boundingBox()).toEqual(before.search);
    await expect(page.getByText(ru.catalogue.intro, { exact: true })).toHaveCount(0);
    await inspectSurface(page, info, `catalogue-${width}-${theme}`);
    await chooseRadio(page, ru.scope.directions);
    await expect(search).toBeVisible();
    expect(await scope.boundingBox()).toEqual(before.scope);
    await page
      .locator("summary")
      .filter({ hasText: /Таблица направлений/ })
      .click();
    await expect(page.getByRole("table", { name: ru.study.live.table.aria })).toBeVisible();
    // Крупный текст должен продолжать страницу, а не обрезаться ради одного экрана.
    await page.evaluate(() => {
      document.documentElement.style.fontSize = "200%";
    });
    await inspectSurface(page, info, `radar-text200-${width}-${theme}`);
  });
}
