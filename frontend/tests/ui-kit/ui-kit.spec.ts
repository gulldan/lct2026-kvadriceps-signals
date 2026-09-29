import { expect, test } from "@playwright/test";
import { AxeBuilder } from "@axe-core/playwright";

test.beforeEach(async ({ page }) => {
  await page.goto("/ui-kit.html");
  await expect(page.getByRole("heading", { level: 1 })).toHaveText("Общий язык интерфейса");
  await page.evaluate(() => document.fonts.ready);
});

for (const viewport of [
  { width: 1440, height: 1000 },
  { width: 1024, height: 1000 },
  { width: 768, height: 1024 },
  { width: 390, height: 844 },
]) {
  test(`catalog is accessible without overflow at ${viewport.width}px`, async ({ page }, info) => {
    await page.setViewportSize(viewport);
    await page.emulateMedia({ reducedMotion: "reduce" });
    const results = await new AxeBuilder({ page })
      .withTags(["wcag2a", "wcag2aa", "wcag21aa"])
      .analyze();
    expect(
      results.violations.map(({ id, nodes }) => ({
        id,
        targets: nodes.map((node) => node.target),
      })),
    ).toEqual([]);
    expect(
      await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth),
    ).toBe(true);
    await page.screenshot({
      path: info.outputPath(`catalog-${viewport.width}.png`),
      fullPage: true,
      animations: "disabled",
    });
  });
}

test("scroll areas support native keyboard scrolling with an accessible name", async ({ page }) => {
  const area = page.getByRole("region", { name: "Пример прокрутки", exact: true });
  await area.focus();
  expect(
    await area.evaluate((element) => element.scrollHeight - element.clientHeight),
  ).toBeGreaterThan(0);
  await area.press("End");
  // Следующая клавиша не должна обгонять нативную анимацию предыдущей прокрутки.
  await expect
    .poll(() =>
      area.evaluate((element) => element.scrollHeight - element.clientHeight - element.scrollTop),
    )
    .toBeLessThanOrEqual(1);
  await area.press("Home");
  await expect.poll(() => area.evaluate((element) => element.scrollTop)).toBe(0);
});

test("tabs and native segmented controls follow keyboard selection", async ({ page }) => {
  const first = page.getByRole("tab", { name: "Доказательства" });
  await first.focus();
  await page.keyboard.press("ArrowRight");
  await expect(page.getByRole("tab", { name: "План проверки" })).toBeFocused();
  await expect(page.getByRole("tabpanel", { name: "План проверки" })).toContainText(
    "Определите следующий эксперимент",
  );
  await page.keyboard.press("End");
  await expect(page.getByRole("tab", { name: "История" })).toHaveAttribute("aria-selected", "true");
  await page.keyboard.press("Home");
  await expect(first).toBeFocused();
  const group = page.getByRole("group", { name: "Представление образца", exact: true });
  await group.getByRole("radio", { name: "Список" }).focus();
  await page.keyboard.press("ArrowRight");
  await expect(group.getByRole("radio", { name: "Карточки" })).toBeChecked();
});

test("dialogs and sheets trap focus, close with Escape and restore focus", async ({ page }) => {
  const trigger = page.getByRole("button", { name: "Открыть диалог", exact: true });
  await trigger.click();
  const dialog = page.getByRole("dialog", { name: "Проверить гипотезу" });
  await expect(dialog).toBeVisible();
  const results = await new AxeBuilder({ page }).withTags(["wcag2a", "wcag2aa"]).analyze();
  expect(results.violations).toEqual([]);
  for (let i = 0; i < 6; i++) {
    await page.keyboard.press("Tab");
    expect(await dialog.evaluate((element) => element.contains(document.activeElement))).toBe(true);
  }
  await page.keyboard.press("Escape");
  await expect(dialog).toBeHidden();
  await expect(trigger).toBeFocused();
  for (const side of ["left", "right", "top", "bottom"]) {
    const sheetTrigger = page.getByRole("button", { name: `Sheet · ${side}`, exact: true });
    await sheetTrigger.click();
    await expect(page.getByRole("dialog", { name: "Источник гипотезы" })).toBeVisible();
    await page.keyboard.press("Escape");
    await expect(sheetTrigger).toBeFocused();
  }
});

test("forms, menus, feedback and pagination operate without an API", async ({ page }) => {
  await expect(page.getByRole("article", { name: "Источники гипотезы" })).toHaveClass(/bg-card/);
  const calls: string[] = [];
  const errors: string[] = [];
  page.on("request", (request) => {
    if (new URL(request.url()).pathname.startsWith("/api/")) calls.push(request.url());
  });
  page.on("pageerror", (error) => errors.push(error.message));
  await page.getByLabel("Название", { exact: true }).fill("Полимер");
  await page.getByRole("combobox", { name: "Формат источника" }).click();
  await page.getByRole("option", { name: "Word", exact: true }).click();
  await expect(page.getByRole("combobox", { name: "Формат источника" })).toHaveText("Word");
  await page.getByRole("switch", { name: "Уведомления" }).click();
  await expect(page.getByRole("switch", { name: "Уведомления" })).not.toBeChecked();
  await page.getByRole("button", { name: "Действия", exact: true }).click();
  await page.getByRole("menuitemcheckbox", { name: "Показать архив" }).click();
  await page.getByRole("button", { name: "Действия", exact: true }).click();
  await expect(page.getByRole("menuitemcheckbox", { name: "Показать архив" })).toHaveAttribute(
    "aria-checked",
    "true",
  );
  await page.keyboard.press("Escape");
  await page.getByRole("button", { name: "2", exact: true }).click();
  await expect(page.getByRole("cell", { name: "Исследование 4", exact: true })).toBeVisible();
  await page
    .locator("#feedback")
    .getByRole("button", { name: /повторить/i })
    .click();
  await expect(page.getByText("Повторная загрузка выполнена — пример.")).toBeVisible();
  await page.getByRole("button", { name: "Успех", exact: true }).click();
  await expect(page.getByText("Изменения сохранены", { exact: true })).toBeVisible();
  await expect(page.locator("[data-sonner-toaster]")).toHaveCSS("z-index", "60");
  expect(calls).toEqual([]);
  expect(errors).toEqual([]);
});

test("tokens reach controls and reduced motion keeps loading feedback without movement", async ({
  page,
}) => {
  const input = page.getByLabel("Название", { exact: true });
  const controls = [
    input,
    page.getByRole("combobox", { name: "Формат источника" }),
    page
      .getByRole("group", { name: "Стандартный · default", exact: true })
      .getByRole("button")
      .first(),
    page.getByRole("group", { name: "Крупное представление образца", exact: true }),
  ];
  await page.evaluate(() => document.documentElement.style.setProperty("--control-md", "48px"));
  for (const control of controls) await expect(control).toHaveCSS("height", "48px");
  await page.evaluate(() => document.documentElement.style.removeProperty("--control-md"));
  const controlHeight = await page.evaluate(() => {
    const probe = document.createElement("div");
    probe.style.height = "var(--control-md)";
    document.body.append(probe);
    const height = getComputedStyle(probe).height;
    probe.remove();
    return height;
  });
  for (const control of controls) await expect(control).toHaveCSS("height", controlHeight);
  for (const [size, height, token] of [
    ["default", controlHeight, "--control-md"],
    ["sm", "32px", "--control-sm"],
  ] as const) {
    const bar = page.getByRole("group", { name: `Панель фильтров · ${size}`, exact: true });
    const rowControls = bar.locator("input:not([type=radio]), button, fieldset");
    await expect(rowControls).toHaveCount(5);
    for (const control of await rowControls.all())
      await expect(control).toHaveCSS("height", height);
    await page.evaluate((name) => document.documentElement.style.setProperty(name, "48px"), token);
    for (const control of await rowControls.all())
      await expect(control).toHaveCSS("height", "48px");
    await page.evaluate((name) => document.documentElement.style.removeProperty(name), token);
  }
  await page.emulateMedia({ reducedMotion: "reduce" });
  const spinner = page.locator(".animate-spin").first();
  await expect(spinner).toHaveCSS("animation-iteration-count", "infinite");
  const loadingFeedback = await spinner.evaluate((element) => {
    const animation = element.getAnimations()[0]!;
    const duration = Number(animation.effect!.getTiming().duration);
    animation.pause();
    animation.currentTime = 0;
    const start = getComputedStyle(element).opacity;
    animation.currentTime = duration / 2;
    return {
      duration,
      start: Number(start),
      middle: Number(getComputedStyle(element).opacity),
      transform: getComputedStyle(element).transform,
    };
  });
  expect(loadingFeedback.duration).toBeGreaterThanOrEqual(1000);
  expect(loadingFeedback.middle).toBeLessThan(loadingFeedback.start);
  expect(loadingFeedback.middle).toBeGreaterThanOrEqual(0.5);
  expect(loadingFeedback.transform).toBe("none");
  await expect(input).toHaveCSS("transition-property", /background-color/);
  await expect(input).not.toHaveCSS("transition-duration", "0s");
  await expect(page.getByRole("textbox", { name: "Название с ошибкой" })).toHaveAttribute(
    "aria-describedby",
    "kit-error-message",
  );
});

test("the palette keeps readable contrast and loads both Cyrillic font families", async ({
  page,
}) => {
  await expect(page.locator("[data-palette-anchor]")).toHaveCount(5);
  await expect(
    page.getByRole("heading", { level: 1, name: "Общий язык интерфейса", exact: true }),
  ).toHaveCSS("font-family", /Onest/);
  await expect(page.getByLabel("Название", { exact: true })).toHaveCSS("font-family", /Onest/);
  const audit = await page.evaluate(async () => {
    await document.fonts.load('600 30px "Onest Variable"', "Научная гипотеза");
    await document.fonts.load('400 16px "Onest Variable"', "Проверить источник");
    await document.fonts.load('400 13px "JetBrains Mono Variable"', "0123456789");
    const styles = getComputedStyle(document.documentElement);
    const ctx = document.createElement("canvas").getContext("2d")!;
    const luminance = (token: string) => {
      ctx.fillStyle = styles.getPropertyValue(`--${token}`).trim();
      ctx.fillRect(0, 0, 1, 1);
      const rgb = [...ctx.getImageData(0, 0, 1, 1).data].slice(0, 3).map((channel) => {
        const value = channel / 255;
        return value <= 0.04045 ? value / 12.92 : ((value + 0.055) / 1.055) ** 2.4;
      });
      return rgb[0]! * 0.2126 + rgb[1]! * 0.7152 + rgb[2]! * 0.0722;
    };
    const pairs: [string, string, number][] = [
      ["foreground", "background", 7],
      ["muted-foreground", "secondary", 4.5],
      ["primary-foreground", "primary", 4.5],
      ["brand-foreground", "brand", 4.5],
      ["brand", "secondary", 4.5],
      ["sidebar-primary-foreground", "sidebar-primary", 4.5],
      ["accent-foreground", "accent", 4.5],
      ["ok", "ok-wash", 4.5],
      ["warn", "warn-wash", 4.5],
      ["risk", "risk-wash", 4.5],
      ["complement", "complement-wash", 4.5],
      ["complement-foreground", "complement", 4.5],
      ["background", "ok", 4.5],
      ["background", "warn", 4.5],
      ["background", "muted-foreground", 4.5],
      ["media-ink-foreground", "media-ink", 4.5],
      ...[1, 2, 3, 4, 5].map((index): [string, string, number] => [
        `chart-${index}`,
        "background",
        4.5,
      ]),
      ["input", "card", 3],
      ["ring", "background", 3],
    ];
    return {
      fonts: ["Onest Variable", "JetBrains Mono Variable"].map((family) =>
        [...document.fonts].some(
          (font) => font.family.includes(family) && font.status === "loaded",
        ),
      ),
      contrast: pairs.map(([foreground, background, minimum]) => {
        const a = luminance(foreground);
        const b = luminance(background);
        return {
          foreground,
          background,
          minimum,
          ratio: (Math.max(a, b) + 0.05) / (Math.min(a, b) + 0.05),
        };
      }),
    };
  });
  expect(audit.fonts).toEqual([true, true]);
  for (const { foreground, background, minimum, ratio } of audit.contrast) {
    expect(ratio, `${foreground} on ${background}`).toBeGreaterThanOrEqual(minimum);
  }
});
