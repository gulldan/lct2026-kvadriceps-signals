import { expect, test } from "./fixture";
import { inspectSurface } from "./checks";
import ru from "../../src/shared/i18n/locales/ru/trend.json" with { type: "json" };
import world from "../fixtures/trend-live-world.json" with { type: "json" };
import { MONEY_KINDS } from "../../src/features/trend/earlyMoney";

for (const [width, height, theme] of [
  [1600, 1000, "light"],
  [1280, 800, "dark"],
  [900, 900, "light"],
  [390, 844, "light"],
] as const) {
  test(`world map links geography, trends and accessible tooltips at ${width}px ${theme}`, async ({
    page,
  }, info) => {
    test.setTimeout(60_000);
    const errors: string[] = [];
    page.on("pageerror", (error) => errors.push(error.message));
    await page.setViewportSize({ width, height });
    await page.addInitScript((value) => localStorage.setItem("rag.theme", value), theme);
    await page.goto("/trends?entity=map");
    const top = page.getByRole("table", { name: ru.world.table.aria });
    const canvas = page.locator("[data-map-canvas]");
    await expect(top.locator("[data-map-row]")).toHaveCount(world.signals.length);
    await expect(page.locator("[data-map-marker]").first()).toBeVisible();
    const markerCount = await page.locator("[data-map-marker]").count();
    expect(markerCount).toBeGreaterThan(0);
    expect(markerCount).toBeLessThanOrEqual(6);
    await expect(page.getByRole("radio", { name: ru.world.scienceLayer, exact: true })).toHaveCount(
      0,
    );
    await expect(page.locator("[data-map-bubble]")).toHaveCount(0);
    await expect(page.locator("[data-map-region]")).toHaveCount(7);
    await expect(page.locator("[data-map-region-label]")).toHaveCount(7);
    // Сводка денег над картой не висит: она появляется только у выбранного региона.
    await expect(page.locator("[data-map-panel]")).toHaveCount(0);
    await expect(page.locator("[data-map-viewport]")).toHaveAttribute("data-map-zoom", "1");
    await expect(page.getByRole("button", { name: ru.world.about, exact: true })).toHaveCount(0);
    await expect(top.getByRole("columnheader", { name: /События/ })).toBeVisible();
    await inspectSurface(page, info, `world-${width}-${theme}`);
    if (width >= 1600) {
      const mapBox = await canvas.boundingBox();
      const tableBox = await top.boundingBox();
      expect(tableBox!.x).toBeGreaterThan(mapBox!.x);
      expect(mapBox!.width).toBeGreaterThan(tableBox!.width);
    }
    const performanceProbe = await page.evaluateHandle(() => {
      const durations: number[] = [];
      const observer = new PerformanceObserver((list) => {
        durations.push(...list.getEntries().map((entry) => entry.duration));
      });
      observer.observe({ type: "longtask" });
      return { durations, observer };
    });
    const first = world.signals[0]!;
    const firstRow = top.locator(`[data-map-row="${first.id}"]`);
    await firstRow.hover();
    await expect(page.locator(`[data-map-country="${first.lead[0]!.country}"]`)).toHaveAttribute(
      "data-emphasized",
      "true",
    );
    const listBefore = await top.boundingBox();
    await firstRow.getByRole("button").click();
    const selection = page.locator(`[data-map-selection="${first.id}"]`);
    await expect(selection).toBeVisible();
    expect(new URL(page.url()).searchParams.get("entity")).toBe("map");
    if (width >= 1600) expect((await top.boundingBox())!.x).toBe(listBefore!.x);
    await top.locator(`[data-map-row="${world.signals[1]!.id}"]`).hover();
    await expect(selection).toBeVisible();
    const longTasks = await performanceProbe.evaluate(({ durations, observer }) => {
      observer.disconnect();
      return {
        count: durations.length,
        maximumMs: Math.max(0, ...durations),
        totalMs: durations.reduce((sum, value) => sum + value, 0),
      };
    });
    await info.attach("map-interaction-long-tasks", {
      body: JSON.stringify(longTasks),
      contentType: "application/json",
    });
    await performanceProbe.dispose();
    await selection.getByRole("button", { name: ru.world.openDirection }).click();
    await expect.poll(() => new URL(page.url()).searchParams.get("topic")).toBe(first.id);
    expect(new URL(page.url()).searchParams.get("back")).toBe("map");
    await page.goBack();
    await expect(top).toBeVisible();

    if (width === 1600) {
      await page.getByRole("button", { name: ru.map.fullscreen, exact: true }).click();
      await expect.poll(() => page.evaluate(() => document.fullscreenElement !== null)).toBe(true);
      await expect
        .poll(async () => {
          const layout = await page.locator("[data-map-layout]").boundingBox();
          return Math.round(layout!.y + layout!.height);
        })
        .toBe(height - 24);
      await page.getByRole("combobox", { name: ru.world.countryFilter, exact: true }).click();
      await expect(page.getByRole("option", { name: "Китай", exact: true })).toBeVisible();
      expect(
        await page
          .getByRole("listbox")
          .evaluate((element) => document.fullscreenElement?.contains(element)),
      ).toBe(true);
      await page.keyboard.press("Escape");
    }
    if (width === 390) {
      await page.getByRole("combobox", { name: ru.world.countryFilter, exact: true }).click();
      await page.getByRole("option", { name: "Китай", exact: true }).click();
    }
    const marker = page.locator('[data-map-marker="CN"]');
    const tip = page.locator("[data-world-tooltip]");
    await marker.hover();
    await expect(tip).toBeVisible();
    expect(
      await page.locator("[data-map-viewport]").evaluate((element) => element.scrollLeft),
    ).toBe(0);
    await expect(tip).toContainText("Китай");
    await expect(tip).toContainText("События");
    if (width === 1600)
      expect(await tip.evaluate((element) => document.fullscreenElement?.contains(element))).toBe(
        true,
      );
    const anchor = await marker.boundingBox();
    const box = await tip.boundingBox();
    const x = anchor!.x + anchor!.width / 2;
    const y = anchor!.y + anchor!.height / 2;
    expect(x < box!.x || x > box!.x + box!.width || y < box!.y || y > box!.y + box!.height).toBe(
      true,
    );
    await page.mouse.move(box!.x + box!.width / 2, box!.y + box!.height / 2, { steps: 8 });
    await expect(tip).toBeVisible();
    await page.keyboard.press("Escape");
    await expect(tip).toHaveCount(0);
    if (width === 1600)
      await page.getByRole("button", { name: ru.map.fullscreen, exact: true }).click();
    if ((await marker.getAttribute("aria-pressed")) !== "true") await marker.click();
    await expect(marker).toHaveAttribute("aria-pressed", "true");
    await expect
      .poll(async () =>
        Number(await page.locator("[data-map-viewport]").getAttribute("data-map-zoom")),
      )
      .toBeGreaterThan(1);
    await expect(page.locator('[data-map-country="US"]')).toHaveAttribute("data-muted", "true");
    await expect(top.locator("[data-map-row]")).toHaveCount(
      world.signals.filter((signal) => signal.lead[0]?.country === "CN").length,
    );
    await expect
      .poll(() =>
        page
          .locator("[data-map-viewport]")
          .evaluate((element) =>
            element
              .getAnimations({ subtree: true })
              .some((animation) => animation.playState === "running"),
          ),
      )
      .toBe(false);
    await marker.scrollIntoViewIfNeeded();
    await page.mouse.move(0, 0);
    await marker.press("Tab");
    await page.keyboard.press("Shift+Tab");
    await expect(tip).toBeVisible();
    await page.keyboard.press("Escape");
    await expect(tip).toHaveCount(0);
    // Закрытие подсказки не сбрасывает выбранную страну.
    await expect(marker).toHaveAttribute("aria-pressed", "true");
    await inspectSurface(page, info, `world-country-${width}-${theme}`);
    await page.getByRole("button", { name: ru.world.reset, exact: true }).first().click();
    await expect(top.locator("[data-map-row]")).toHaveCount(world.signals.length);

    await expect.poll(async () => (await canvas.boundingBox())!.width).toBeLessThanOrEqual(width);
    const beforeZoom = await canvas.boundingBox();
    await page.getByRole("button", { name: ru.map.zoomIn, exact: true }).click();
    await expect
      .poll(async () => (await canvas.boundingBox())!.width)
      .toBeGreaterThan(beforeZoom!.width);
    await page.getByRole("button", { name: ru.map.fit, exact: true }).click();
    await expect
      .poll(() =>
        page
          .locator("[data-map-viewport]")
          .evaluate((element) =>
            element
              .getAnimations({ subtree: true })
              .some((animation) => animation.playState === "running"),
          ),
      )
      .toBe(false);
    const labels = await page
      .locator("[data-map-marker] > span:last-child, [data-map-region-label]")
      .evaluateAll((elements) =>
        elements.map((element) => {
          const rect = element.getBoundingClientRect();
          return {
            x: rect.x,
            y: rect.y,
            width: rect.width,
            height: rect.height,
            text: element.textContent,
          };
        }),
      );
    for (const [index, a] of labels.entries()) {
      for (const b of labels.slice(index + 1)) {
        expect(
          a.x + a.width <= b.x ||
            b.x + b.width <= a.x ||
            a.y + a.height <= b.y ||
            b.y + b.height <= a.y,
          `${a.text} overlaps ${b.text}`,
        ).toBe(true);
      }
    }
    const europe = page.locator('[data-map-region="europe"]');
    await europe.click();
    const panel = page.locator('[data-map-panel="europe"]');
    await expect(panel).toBeVisible();
    await expect(page.locator("[data-map-segment]")).toHaveCount(MONEY_KINDS.length);
    await expect
      .poll(async () =>
        Number(await page.locator("[data-map-viewport]").getAttribute("data-map-zoom")),
      )
      .toBeGreaterThan(1);
    await expect(page.locator('[data-map-country="DE"]')).toHaveAttribute(
      "data-emphasized",
      "true",
    );
    await expect(page.locator('[data-map-country="US"]')).toHaveAttribute("data-muted", "true");
    await inspectSurface(page, info, `world-region-${width}-${theme}`);
    await expect(panel.getByRole("heading")).toContainText(ru.money.regions.europe);
    // Отдельной вкладки инвесторов нет: фонды региона открываются под картой вместе с деньгами.
    await expect(page.locator('[data-map-investors="europe"]')).toContainText(
      "Rhine Seed Partners",
    );
    await expect(europe).toHaveAttribute("aria-pressed", "true");
    await page.getByRole("button", { name: ru.world.world, exact: true }).click();
    if (width === 1600) {
      const viewport = page.locator("[data-map-viewport]");
      await viewport.focus();
      await page.keyboard.press("ArrowRight");
      await page.keyboard.press("Home");
      await expect(viewport).toHaveAttribute("data-map-zoom", "1");
      const viewportBox = await viewport.boundingBox();
      await page.mouse.move(viewportBox!.x + 20, viewportBox!.y + viewportBox!.height / 2);
      await page.mouse.down();
      await page.mouse.move(viewportBox!.x + 90, viewportBox!.y + viewportBox!.height / 2 + 30, {
        steps: 8,
      });
      await page.mouse.up();
      await expect(
        page.getByRole("combobox", { name: ru.world.countryFilter, exact: true }),
      ).toHaveText(ru.world.world);
      await viewport.focus();
      await page.keyboard.press("Home");
    }
    await page.locator('[data-map-region="cis"]').click();
    await expect(page.getByText(ru.world.region.noSources, { exact: true })).toBeVisible();
    await expect(page.locator('[data-map-region="cis"]')).toHaveAccessibleName(/Нет данных/);
    await expect(top.locator("[data-map-row]")).toHaveCount(world.signals.length);
    await page.getByRole("combobox", { name: ru.world.countryFilter, exact: true }).click();
    await page.getByRole("option", { name: "Гренландия", exact: true }).click();
    await expect(
      page.getByRole("combobox", { name: ru.world.countryFilter, exact: true }),
    ).toHaveText("Гренландия");
    await expect(top.locator("[data-map-row]")).toHaveCount(0);
    expect(errors).toEqual([]);
  });
}

test("world map keeps publications available when investment data fails and can retry", async ({
  page,
  api,
}) => {
  api.failures.set("GET /trends/live/money", 503);
  await page.goto("/trends?entity=map");
  await expect(page.locator("[data-map-row]")).toHaveCount(world.signals.length);
  await expect(page.getByText(ru.world.moneyPending, { exact: false }).first()).toBeVisible();
  await page.getByRole("button", { name: ru.retry, exact: true }).click();
  await expect(page.getByText(ru.world.moneyPending.trim(), { exact: false })).toHaveCount(0);
  await expect(page.locator("[data-map-region]")).toHaveCount(7);
});
