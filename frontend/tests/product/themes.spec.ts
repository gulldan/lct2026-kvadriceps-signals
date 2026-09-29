import { expect, test } from "./fixture";
import { inspectSurface } from "./checks";

test("a thousand map topics have one keyboard entry and a direct path to the list", async ({
  page,
  api,
}) => {
  const first = api.state.clusters[0]!;
  api.state.clusters = Array.from({ length: 1030 }, (_, index) => {
    const theme = structuredClone(first);
    theme.id = `topic-${index}`;
    theme.label = `Исследовательская тема ${index}`;
    theme.document_count = 1030 - index;
    theme.params!.itc!.axes = {
      ...theme.params!.itc!.axes!,
      momentum: (index % 103) / 102,
      novelty: Math.floor(index / 103) / 9,
    };
    return theme;
  });
  await page.setViewportSize({ width: 1024, height: 900 });
  await page.goto("/summary");
  const points = page.locator("[data-theme-point]");
  const firstPoint = page.locator('[data-theme-point="topic-0"]');
  const lastPoint = page.locator('[data-theme-point="topic-1029"]');
  const entry = page.locator('[data-theme-point][tabindex="0"]');
  const skip = page.getByRole("button", { name: "К списку тем", exact: true });
  await expect(points).toHaveCount(1030);
  await expect(entry).toHaveCount(1);
  await skip.focus();
  await page.keyboard.press("Tab");
  await expect(page.locator('[data-map-region="earlierDistinct"]')).toBeFocused();
  await page.keyboard.press("Tab");
  await page.keyboard.press("Tab");
  await expect(firstPoint).toBeFocused();
  await firstPoint.press("ArrowRight");
  await expect(page.locator('[data-theme-point="topic-1"]')).toBeFocused();
  await expect(entry).toHaveCount(1);
  await expect(page.getByRole("tooltip")).toContainText("Исследовательская тема 1");
  await page.keyboard.press("End");
  await expect(lastPoint).toBeFocused();
  await lastPoint.press("ArrowRight");
  await expect(firstPoint).toBeFocused();
  await firstPoint.press("ArrowLeft");
  await expect(lastPoint).toBeFocused();
  await lastPoint.press("Home");
  await expect(firstPoint).toBeFocused();
  await firstPoint.press("Tab");
  await expect(page.locator('[data-map-region="earlierSimilar"]')).toBeFocused();
  await page.keyboard.press("Shift+Tab");
  await expect(firstPoint).toBeFocused();
  await firstPoint.press("Enter");
  await expect(firstPoint).toHaveAttribute("aria-pressed", "true");
  await skip.focus();
  await skip.press("Enter");
  await expect(page.locator("#theme-list")).toBeFocused();
  await page.keyboard.press("Tab");
  await expect(
    page.locator('#theme-list details[name="theme-comparison"] > summary').first(),
  ).toBeFocused();
  const search = page.getByRole("textbox", { name: "Поиск по темам", exact: true });
  await search.fill("Исследовательская тема 1029");
  await expect(points).toHaveCount(1);
  await expect(search).toBeFocused();
  await expect(entry).toHaveAttribute("data-theme-point", "topic-1029");
  await lastPoint.focus();
  await lastPoint.press("ArrowRight");
  await expect(lastPoint).toBeFocused();
  await lastPoint.press("Enter");
  await page.getByRole("button", { name: "Вся карта", exact: true }).click();
  await expect(page.getByRole("group", { name: /^Карта тем:/ })).toBeFocused();
  await page.keyboard.press("Tab");
  await expect(lastPoint).toBeFocused();
  await search.fill("");
  api.state.clusters = [api.state.clusters[0]!];
  await page.getByRole("button", { name: "Обновить", exact: true }).click();
  await expect(points).toHaveCount(1);
  await expect(entry).toHaveAttribute("data-theme-point", "topic-0");
});

for (const width of [1440, 390]) {
  for (const colorScheme of ["light", "dark"] as const) {
    test(`theme index filter toggles in place at ${width}px in ${colorScheme}`, async ({
      page,
      api,
    }, info) => {
      const unscored = structuredClone(api.state.clusters[0]!);
      unscored.id = "cluster-unscored";
      unscored.label = "Полимерные композиты без индекса";
      delete unscored.params!.itc;
      api.state.clusters.push(unscored);
      await page.setViewportSize({ width, height: 950 });
      await page.emulateMedia({ colorScheme });
      await page.goto("/summary");
      await page.getByRole("tab", { name: /Сравнение тем/ }).click();
      const bar = page.locator('[data-slot="filter-bar"]');
      const toggle = bar.getByRole("button", { name: "Только с индексом", exact: true });
      const rows = page.locator('#theme-list details[name="theme-comparison"]');
      const geometry = () =>
        bar.locator("input, button").evaluateAll((controls) =>
          controls.map((control) => {
            const { x, y, width: controlWidth, height } = control.getBoundingClientRect();
            return { x, y, width: controlWidth, height };
          }),
        );
      await expect(rows).toHaveCount(3);
      const original = await geometry();
      const inactiveFill = await toggle.evaluate((el) => getComputedStyle(el).backgroundColor);
      await toggle.click();
      await expect(toggle).toHaveAttribute("aria-pressed", "true");
      await expect(toggle).not.toHaveCSS("background-color", inactiveFill);
      await expect(rows).toHaveCount(2);
      await expect(bar.getByRole("button", { name: "Сброс", exact: true })).toHaveCount(0);
      expect(await geometry()).toEqual(original);
      await inspectSurface(page, info, `index-filter-${colorScheme}`);
      await toggle.press("Space");
      await expect(toggle).toHaveAttribute("aria-pressed", "false");
      await expect(toggle).toHaveCSS("background-color", inactiveFill);
      await expect(rows).toHaveCount(3);
      await bar.getByRole("textbox").fill("Полимерные");
      await expect(rows).toHaveCount(2);
      await toggle.click();
      await expect(rows).toHaveCount(1);
      await toggle.click();
      await expect(rows).toHaveCount(2);
      await expect(bar.getByRole("textbox")).toHaveValue("Полимерные");
      await expect(bar.getByRole("button", { name: "Сброс", exact: true })).toHaveCount(0);
      expect(await geometry()).toEqual(original);
    });
  }

  test(`theme comparison explains one consistent index and loads sources on demand at ${width}px`, async ({
    page,
    api,
  }, info) => {
    await page.setViewportSize({ width, height: 950 });
    await page.goto("/summary");
    const mapTab = page.getByRole("tab", { name: "Карта тем", exact: true });
    await expect(page.getByRole("tab").first()).toHaveText("Карта тем");
    await expect(mapTab).toHaveAttribute("aria-selected", "true");
    await expect(page.getByRole("group", { name: /^Карта тем:/ })).toBeVisible();
    await page.getByRole("tab", { name: /Сравнение тем/ }).click();
    await page.reload();
    await expect(mapTab).toHaveAttribute("aria-selected", "true");
    await page.getByRole("tab", { name: /Сравнение тем/ }).click();
    await expect(page.getByRole("tab", { name: /Сравнение тем/ })).toHaveAttribute(
      "aria-selected",
      "true",
    );
    await expect(page.getByRole("group", { name: /^Карта тем:/ })).toHaveCount(0);
    expect(api.calls.some((call) => call.path === "/clusters/cluster-1")).toBe(false);
    const row = page
      .locator('details[name="theme-comparison"]')
      .filter({ hasText: "Полимерные композиты" });
    await row.locator("summary").first().click();
    await expect(
      row.getByRole("heading", { name: "Исследовательский профиль", exact: true }),
    ).toBeVisible();
    await expect(row).toContainText("Связей из гипотез на документы темы: 0.");
    await expect(row).toContainText("Даты известны для 6 из 8 документов");
    const formula = row.locator("details");
    await expect(formula).not.toHaveAttribute("open");
    await expect(formula.locator("summary > svg")).toHaveCSS("transform", "none");
    await formula.locator("summary").click();
    await expect(formula.getByRole("cell", { name: "40%", exact: true })).toBeVisible();
    await expect(formula.getByRole("cell", { name: "24", exact: true })).toBeVisible();
    await inspectSurface(page, info, "theme-calculation");
    await page.locator("summary").filter({ hasText: "Защитные покрытия" }).click();
    await expect(row).not.toHaveAttribute("open");
    await expect(page.locator('details[name="theme-comparison"][open]')).toHaveCount(1);
    await page.getByRole("link", { name: "Открыть тему и документы", exact: true }).click();
    await expect(page).toHaveURL(/\/clusters\/cluster-2$/);
    await expect(
      page.getByRole("heading", { name: "Исследовательский профиль темы", exact: true }),
    ).toBeVisible();
  });

  test(`missing theme measurements stay explicit and a failed detail can be retried at ${width}px`, async ({
    page,
    api,
  }, info) => {
    const first = api.state.clusters[0]!;
    first.params!.itc = structuredClone(first.params!.itc!);
    first.params!.itc!.signals!.freshness_available = false;
    first.params!.itc!.signals!.pub_count = 0;
    api.failures.set("GET /clusters/cluster-1", 503);
    await page.setViewportSize({ width, height: 950 });
    await page.goto("/summary");
    await page.getByRole("tab", { name: /Сравнение тем/ }).click();
    const row = page
      .locator('details[name="theme-comparison"]')
      .filter({ hasText: "Полимерные композиты" });
    await row.locator("summary").first().click();
    await expect(row).toContainText("Не удалось загрузить расчёт темы");
    await row.getByRole("button", { name: /Повторить/ }).click();
    await expect(row).toContainText("Нет данных");
    await expect(row).toContainText("Расчёт предварительный");
    await expect(row.getByRole("meter", { name: /^Свежесть источников:/ })).toHaveCount(0);
    await page.getByRole("tab", { name: "Карта тем", exact: true }).click();
    await expect(page.locator("[data-theme-point]")).toHaveCount(1);
    await page.locator("[data-theme-point]").click();
    await expect(page.locator("#theme-list")).toContainText("Защитные покрытия");
    await expect(page.locator("#theme-list")).not.toContainText("Полимерные композиты");
    await expect(page.getByText(/1 тема с неполными данными/)).toBeVisible();
    await inspectSurface(page, info, "theme-missing-data");
  });

  test(`circles preserve nearby topics through selection, sorting and filters at ${width}px`, async ({
    page,
    api,
  }, info) => {
    const [first, second] = api.state.clusters;
    second!.params = structuredClone(second!.params);
    second!.params!.itc!.axes = { ...second!.params!.itc!.axes!, momentum: 0.61, novelty: 0.84 };
    const outside = structuredClone(first!);
    outside.id = "cluster-outside";
    outside.label = "Отдельная тема";
    outside.params!.itc!.axes = { ...outside.params!.itc!.axes!, momentum: 0.1, novelty: 0.1 };
    api.state.clusters.push(outside);
    await page.setViewportSize({ width, height: 950 });
    await page.goto("/summary");
    const points = page.locator("[data-theme-point]");
    const rows = page.locator('#theme-list details[name="theme-comparison"]');
    const map = page.getByRole("group", { name: /^Карта тем:/ });
    await expect(points).toHaveCount(3);
    await expect(rows).toHaveCount(3);
    const origin = (await map.boundingBox())!.y;
    const point = page.locator('[data-theme-point="cluster-1"]');
    const originalStyle = await point.getAttribute("style");
    await point.focus();
    await point.press("Enter");
    await expect(point).toHaveAttribute("aria-pressed", "true");
    expect((await map.boundingBox())!.y).toBe(origin);
    await expect(rows).toHaveCount(2);
    await expect(page.locator("#theme-list")).not.toContainText("Отдельная тема");
    await page.getByRole("combobox", { name: "Сортировка тем" }).click();
    await page.getByRole("option", { name: "По числу документов", exact: true }).click();
    await expect(rows).toHaveCount(2);
    await expect(point).toHaveAttribute("aria-pressed", "true");
    await expect(point).toHaveAttribute("style", originalStyle!);
    await expect(page.locator("figcaption")).toContainText("Полимерные композиты");
    await inspectSurface(page, info, "theme-nearby-topics");
    await page.getByRole("button", { name: "Вся карта", exact: true }).click();
    await expect(map).toBeFocused();
    await expect(rows).toHaveCount(3);
    await expect(points).toHaveCount(3);
    await point.click();
    await page.getByRole("textbox", { name: "Поиск по темам", exact: true }).fill("Отдельная");
    await expect(points).toHaveCount(1);
    await expect(rows).toHaveCount(1);
    await expect(page.getByRole("button", { name: "Вся карта", exact: true })).toHaveCount(0);
    await expect(rows).toContainText("Отдельная тема");
    await points.first().click();
    await page
      .locator("figcaption")
      .getByRole("link", { name: "Отдельная тема", exact: true })
      .click();
    await expect(page).toHaveURL(/\/clusters\/cluster-outside$/);
  });

  test(`named map regions retain circles and reveal their exact topics at ${width}px`, async ({
    page,
    api,
  }, info) => {
    for (const [id, label, momentum, novelty] of [
      ["earlier-distinct", "Ранняя отличающаяся тема", 0.1, 0.9],
      ["earlier-similar", "Опорная тема", 0.1, 0.1],
      ["recent-similar", "Свежая близкая тема", 0.9, 0.1],
      ["midpoint", "Тема на границе", 0.5, 0.5],
    ] as const) {
      const theme = structuredClone(api.state.clusters[0]!);
      theme.id = id;
      theme.label = label;
      theme.params!.itc!.axes = { ...theme.params!.itc!.axes!, momentum, novelty };
      api.state.clusters.push(theme);
    }
    await page.setViewportSize({ width, height: 950 });
    await page.goto("/summary");
    const points = page.locator("[data-theme-point]");
    const regions = page.locator("[data-map-region]");
    const rows = page.locator('#theme-list details[name="theme-comparison"]');
    await expect(points).toHaveCount(6);
    await expect(regions).toHaveCount(4);
    const fills = await points.evaluateAll((elements) =>
      elements.map((element) => getComputedStyle(element.firstElementChild!).backgroundColor),
    );
    expect(fills).not.toContain("rgba(0, 0, 0, 0)");
    expect(new Set(fills).size).toBe(4);
    for (const [id, count] of [
      ["earlierDistinct", 1],
      ["recentDistinct", 3],
      ["earlierSimilar", 1],
      ["recentSimilar", 1],
    ] as const) {
      const region = page.locator(`[data-map-region="${id}"]`);
      await expect(region).toContainText(String(count));
      await region.focus();
      await region.press("Enter");
      await expect(region).toHaveAttribute("aria-pressed", "true");
      await expect(page.locator('[data-map-region][aria-pressed="true"]')).toHaveCount(1);
      await expect(rows).toHaveCount(count);
      await expect(points).toHaveCount(6);
      const names = await page
        .locator(`[data-theme-region="${id}"]`)
        .evaluateAll((elements) =>
          elements.map((element) =>
            element.getAttribute("aria-label")!.replace(/ — показать темы рядом$/, ""),
          ),
        );
      expect(
        await rows.locator("summary > div > div > span:first-child").allTextContents(),
      ).toEqual(expect.arrayContaining(names));
    }
    await inspectSurface(page, info, "theme-region-selection");
    const point = page.locator('[data-theme-point="cluster-1"]');
    await point.focus();
    await point.press("Enter");
    await expect(point).toHaveAttribute("aria-pressed", "true");
    await expect(page.locator('[data-map-region][aria-pressed="true"]')).toHaveCount(0);
    await expect(rows).toHaveCount(2);
    await page.getByRole("button", { name: "Вся карта", exact: true }).click();
    await expect(rows).toHaveCount(6);
    const stableStyle = await page
      .locator('[data-theme-point="earlier-similar"]')
      .getAttribute("style");
    await page.getByRole("textbox", { name: "Поиск по темам", exact: true }).fill("Опорная тема");
    await expect(points).toHaveCount(1);
    await expect(points).toHaveAttribute("style", stableStyle!);
    await expect(points).toHaveAttribute("data-theme-region", "earlierSimilar");
    await expect(page.locator('[data-map-region="recentDistinct"]')).toBeDisabled();
    const remaining = page.locator('[data-map-region="earlierSimilar"]');
    await expect(remaining).toBeEnabled();
    await remaining.click();
    await expect(rows).toHaveCount(1);
    await expect(rows).toContainText("Опорная тема");
  });

  test(`topic hints stay beside the pointer and inside the viewport at ${width}px`, async ({
    page,
    api,
  }, info) => {
    const first = api.state.clusters[0]!;
    const longName = "Исследование сверхдлинногоназваниякомпозиционногоматериала".repeat(3);
    api.state.clusters = [
      [0, 0],
      [0, 1],
      [1, 0],
      [1, 1],
    ].map(([momentum, novelty], index) => {
      const theme = structuredClone(first);
      theme.id = `corner-${index}`;
      theme.label = `${longName} ${index}`;
      theme.params!.itc!.axes = {
        ...theme.params!.itc!.axes!,
        momentum: momentum!,
        novelty: novelty!,
      };
      return theme;
    });
    await page.setViewportSize({ width, height: 950 });
    await page.goto("/summary");
    const tooltip = page.getByRole("tooltip");
    const caption = await page.locator("figcaption").textContent();
    for (const theme of api.state.clusters) {
      const point = page.locator(`[data-theme-point="${theme.id}"]`);
      await point.hover();
      await expect(tooltip).toContainText(theme.label);
      await expect(tooltip).toContainText(
        `8 док. · свежесть ${theme.params!.itc!.axes!.momentum * 100} · отличие ${theme.params!.itc!.axes!.novelty * 100}`,
      );
      await expect(point).toHaveAttribute("aria-describedby", (await tooltip.getAttribute("id"))!);
      await expect
        .poll(async () => {
          const bubble = (await tooltip.boundingBox())!;
          const circle = (await point.boundingBox())!;
          const x = circle.x + circle.width / 2;
          const y = circle.y + circle.height / 2;
          return (
            bubble.x >= 15 &&
            bubble.x + bubble.width <= width - 15 &&
            bubble.y >= 15 &&
            bubble.y + bubble.height <= 935 &&
            Math.hypot(
              Math.max(bubble.x - x, x - bubble.x - bubble.width, 0),
              Math.max(bubble.y - y, y - bubble.y - bubble.height, 0),
            ) < 40
          );
        })
        .toBe(true);
      await expect(page.locator("figcaption")).toHaveText(caption!);
      await tooltip.hover();
      await expect(tooltip).toBeVisible();
      await page.keyboard.press("Escape");
      await expect(tooltip).toHaveCount(0);
    }
    const point = page.locator('[data-theme-point="corner-0"]');
    await point.focus();
    await expect(tooltip).toContainText(`${longName} 0`);
    await inspectSurface(page, info, "theme-tooltip-keyboard");
    await point.press("ArrowRight");
    await expect(tooltip).toContainText(`${longName} 1`);
    await page.keyboard.press("Escape");
    await expect(tooltip).toHaveCount(0);
  });
}

test.describe("topic hints on touch screens", () => {
  test.use({ hasTouch: true });

  test("a tap shows the hint and an outside tap dismisses it without clearing selection", async ({
    page,
  }, info) => {
    await page.setViewportSize({ width: 390, height: 950 });
    await page.goto("/summary");
    const point = page.locator('[data-theme-point="cluster-2"]');
    await point.tap();
    const tooltip = page.getByRole("tooltip");
    await expect(tooltip).toContainText("Защитные покрытия");
    await expect(point).toHaveAttribute("aria-pressed", "true");
    await inspectSurface(page, info, "theme-tooltip-touch");
    await page.getByRole("heading", { name: "Темы", exact: true }).tap();
    await expect(tooltip).toHaveCount(0);
    await expect(point).toHaveAttribute("aria-pressed", "true");
    await point.tap();
    await expect(tooltip).toBeVisible();
    await page.getByRole("textbox", { name: "Поиск по темам", exact: true }).fill("Полимерные");
    await expect(tooltip).toHaveCount(0);
  });
});
