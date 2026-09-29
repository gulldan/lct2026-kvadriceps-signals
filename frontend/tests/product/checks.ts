import { AxeBuilder } from "@axe-core/playwright";
import type { Page, TestInfo } from "@playwright/test";
import { writeFile } from "node:fs/promises";
import { expect } from "./fixture";
import { inspectControlAlignment } from "./design-contract";

export async function inspectSurface(page: Page, info: TestInfo, name: string) {
  await page.evaluate(() => document.fonts.ready);
  await inspectControlAlignment(page);
  await page.screenshot({
    path: info.outputPath(`${name}.png`),
    fullPage: true,
    animations: "disabled",
  });
  await page.screenshot({ path: info.outputPath(`${name}-viewport.png`), animations: "disabled" });
  await writeFile(info.outputPath(`${name}.yml`), await page.locator("body").ariaSnapshot());
  const overflow = await page.evaluate(() => ({
    viewport: innerWidth,
    content: document.documentElement.scrollWidth,
  }));
  expect.soft(overflow.content, JSON.stringify(overflow)).toBeLessThanOrEqual(overflow.viewport);
  const audit = await new AxeBuilder({ page })
    .withTags(["wcag2a", "wcag2aa", "wcag21aa"])
    .analyze();
  expect
    .soft(
      audit.violations.map(({ id, nodes }) => ({
        id,
        targets: nodes.map(({ target, failureSummary }) => ({ target, failureSummary })),
      })),
    )
    .toEqual([]);
}

export async function choose(page: Page, name: string, option: string) {
  await page.getByRole("combobox", { name, exact: true }).click();
  await page.getByRole("option", { name: option, exact: true }).click();
}

/** Нажимается видимая подпись: сам input скрыт. */
export async function chooseRadio(page: Page, name: string) {
  const radio = page.getByRole("radio", { name, exact: true });
  await radio.locator("..").click();
  await expect(radio).toBeChecked();
}
