import { expect, test } from "./fixture";
import { inspectSurface } from "./checks";
import ru from "../../src/shared/i18n/locales/ru/trend.json" with { type: "json" };
import common from "../../src/shared/i18n/locales/ru/common.json" with { type: "json" };
import doc from "../../src/shared/i18n/locales/ru/document.json" with { type: "json" };
import technology from "../fixtures/technology.json" with { type: "json" };
import { paperEvidence } from "./trends-fixture";

const mockSource = (page: import("@playwright/test").Page, id: string) =>
  page.route(`**/api/v1/documents/${id}/source*`, (route) =>
    route.fulfill({ json: { ...paperEvidence, document_id: id } }),
  );

for (const width of [390, 1440]) {
  test(`technology card shows names, sources, quotes and honest gaps at ${width}px`, async ({
    page,
  }, info) => {
    await page.setViewportSize({ width, height: 900 });
    await page.goto("/technologies/tech-1?return=%2Ftrends%3Fentity%3Dtechnologies");
    // Печатная версия повторяет тексты в скрытом блоке, поэтому ищем по видимой колонке.
    const card = page.locator('[data-slot="page-layout"]');
    await expect(page.getByRole("heading", { name: technology.canonical_name })).toBeVisible();
    await expect(page.getByText(ru.technology.status.proposed, { exact: true })).toBeVisible();
    const aliases = page.getByRole("region", { name: ru.technology.aliases.title });
    await expect(aliases.getByText("solid oxide electrolyser", { exact: true })).toBeVisible();
    await expect(aliases.getByText(ru.technology.aliases.basis.expert_review)).toBeVisible();
    await expect(card.getByText(ru.technology.restricted.replace("{{count}}", "1"))).toBeVisible();
    await expect(page.getByText(ru.technology.roles.primary, { exact: true })).toBeVisible();
    await expect(
      page.getByText(ru.technology.extraction.needs_review, { exact: true }),
    ).toBeVisible();
    await expect(page.getByText(ru.technology.sources.noQuotes)).toBeVisible();
    await expect(card.getByText(technology.mentions[0]!.evidence[0]!.quote)).toBeVisible();
    const quote = page.getByRole("link", { name: ru.technology.sources.quote }).first();
    expect(await quote.getAttribute("href")).toMatch(
      /\/sources\/doc-1\?start=12&end=96&textHash=hash-doc-1/,
    );
    await expect(card.getByText(ru.technology.statements.problem, { exact: true })).toHaveCount(1);
    await expect(page.getByRole("link", { name: ru.technology.back })).toHaveAttribute(
      "href",
      "/trends?entity=technologies",
    );
    await inspectSurface(page, info, `technology-card-${width}`);
  });
}

test("the technology card exports its own report to Word", async ({ page }, info) => {
  await page.goto("/technologies/tech-1");
  await expect(page.getByRole("heading", { name: technology.canonical_name })).toBeVisible();
  const download = page.waitForEvent("download");
  await page.getByRole("button", { name: common.actions.export, exact: true }).click();
  await page.getByRole("menuitem", { name: common.ui.export.word, exact: true }).click();
  const file = await download;
  await file.saveAs(info.outputPath(file.suggestedFilename()));
  expect(file.suggestedFilename()).toMatch(/\.docx$/);
});

test("expert correction needs a reason, is recorded with before/after and can be undone", async ({
  page,
}) => {
  await page.goto("/technologies/tech-1");
  await expect(page.getByText(ru.review.ops.describe, { exact: true })).toBeVisible();
  await page.getByRole("button", { name: ru.review.add }).click();
  const dialog = page.getByRole("dialog");
  await dialog.getByLabel(ru.review.fields.name).fill("Твердооксидный электролизёр пара");
  await dialog.getByRole("button", { name: ru.review.submit }).click();
  await expect(dialog.getByRole("alert")).toHaveText(ru.review.errors.reasonRequired);
  await dialog.getByLabel(ru.review.fields.reason).fill("Так называют в двух из трёх источников.");
  await dialog.getByRole("button", { name: ru.review.submit }).click();
  await expect(page.getByRole("status")).toHaveText(ru.review.saved);
  const entry = page.locator("li", { hasText: "Так называют в двух из трёх источников." });
  await expect(
    entry.getByText(ru.review.keys.canonical_name, { exact: false }).first(),
  ).toBeVisible();
  await expect(entry.getByText("Твердооксидный электролизёр пара")).toBeVisible();
  await entry.getByRole("button", { name: ru.review.history.revert }).click();
  await expect(entry.getByText(ru.review.history.reverted, { exact: true })).toBeVisible();
  await expect(page.getByText(ru.review.ops.revert, { exact: true }).first()).toBeVisible();
});

test.describe("plain accounts", () => {
  test.use({ roles: ["user"] });
  test("a user without expert rights does not see correction controls", async ({ page }) => {
    await page.goto("/technologies/tech-1");
    await expect(page.getByRole("heading", { name: technology.canonical_name })).toBeVisible();
    await expect(page.getByRole("button", { name: ru.review.add })).toHaveCount(0);
  });
});

test("an unavailable technology is refused in words, not shown as empty", async ({ page }) => {
  await page.goto("/technologies/tech-9");
  await expect(page.getByRole("alert")).toHaveText(ru.technology.notFound);
});

for (const [id, text] of [
  ["doc-2", doc.source.technologies.status.no_technology],
  ["doc-3", doc.source.technologies.status.failed.replace("{{reason}}", "model_timeout")],
  ["doc-4", doc.source.technologies.status.unsupported_language.replace("{{reason}}", "de")],
  ["doc-5", doc.source.technologies.status.not_extracted],
] as const) {
  test(`source ${id} explains its extraction status in words`, async ({ page }) => {
    await mockSource(page, id);
    await page.goto(`/sources/${id}`);
    await expect(page.getByText(text)).toBeVisible();
  });
}

test("a source lists its technologies with roles and links to the card", async ({ page }) => {
  await mockSource(page, "doc-1");
  await page.goto("/sources/doc-1");
  const block = page.getByRole("region", { name: doc.source.technologies.title });
  await expect(block.getByRole("link", { name: /Твердооксидный электролизёр/ })).toHaveAttribute(
    "href",
    /\/technologies\/tech-1\?return=/,
  );
  await expect(
    block.getByText(doc.source.technologies.roles.component, { exact: true }),
  ).toBeVisible();
  await expect(
    block.getByText(
      doc.source.technologies.status.complete
        .replace("{{processed}}", "6")
        .replace("{{total}}", "6"),
    ),
  ).toBeVisible();
});
