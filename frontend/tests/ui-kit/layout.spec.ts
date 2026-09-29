import { expect, test } from "@playwright/test";

for (const width of [390, 768, 1024, 1440]) {
  test(`specimens keep their alignment with uneven content at ${width}px`, async ({ page }) => {
    await page.setViewportSize({ width, height: 1000 });
    await page.goto("/ui-kit.html");
    await page.evaluate(() => document.fonts.ready);

    // Бейдж, перенесённая подсказка или ошибка не должны сдвигать соседнее поле вниз.
    await page.locator("#kit-title-hint").evaluate((element) => {
      element.textContent =
        "Название должно помогать отличать эту гипотезу от других исследований в базе знаний. ".repeat(
          3,
        );
    });
    const fields = await page
      .locator("#kit-title, #kit-limit, #kit-error, #kit-disabled, #kit-format, #kit-range")
      .evaluateAll((elements) =>
        elements.map((element) => {
          const { x, y, height } = element.getBoundingClientRect();
          return { id: element.id, x, y, height };
        }),
      );
    const controlHeight = await page.evaluate(() => {
      const probe = document.createElement("div");
      probe.style.height = "var(--control-md)";
      document.body.append(probe);
      const height = getComputedStyle(probe).height;
      probe.remove();
      return height;
    });
    // Высота берётся из токена: числом в тесте она отстаёт от дизайн-системы.
    for (const field of fields) expect(`${field.height}px`, field.id).toBe(controlHeight);
    for (let index = 0; index < fields.length; index += 2) {
      const first = fields[index]!;
      const second = fields[index + 1]!;
      expect(Math.abs(width >= 640 ? first.y - second.y : first.x - second.x)).toBeLessThan(1);
    }

    const palette = await page.locator("[data-palette-anchor]").evaluateAll((elements) =>
      elements.map((element) =>
        [element.querySelector("span")!, ...element.querySelectorAll("p")].map((part) => {
          const { x, y } = part.getBoundingClientRect();
          return { x, y };
        }),
      ),
    );
    for (let row = 0; row < 3; row++) {
      const positions = palette.map((parts) => (width >= 640 ? parts[row]!.y : parts[row]!.x));
      expect(Math.max(...positions) - Math.min(...positions)).toBeLessThan(1);
    }

    const compact = await page
      .getByRole("group", { name: "Представление образца", exact: true })
      .boundingBox();
    const standard = await page
      .getByRole("group", { name: "Крупное представление образца", exact: true })
      .boundingBox();
    expect(compact).not.toBeNull();
    expect(standard).not.toBeNull();
    expect(
      compact!.x + compact!.width + 16 <= standard!.x ||
        compact!.y + compact!.height + 16 <= standard!.y,
    ).toBe(true);

    const cards = [
      page.getByRole("article", { name: "Источники гипотезы" }),
      page.getByText("Компактная карточка", { exact: true }).locator("../.."),
    ];
    const metrics = ["2 438", "38"].map((value) =>
      page.getByText(value, { exact: true }).locator(".."),
    );
    for (let column = 0; column < 2; column++) {
      const card = await cards[column]!.boundingBox();
      const metric = await metrics[column]!.boundingBox();
      expect(card).not.toBeNull();
      expect(metric).not.toBeNull();
      expect(Math.abs(card!.x - metric!.x)).toBeLessThan(1);
      expect(Math.abs(card!.width - metric!.width)).toBeLessThan(1);
    }

    await page.getByText("Семантические токены и печатная палитра", { exact: true }).click();
    expect(
      await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth),
    ).toBe(true);
  });
}
