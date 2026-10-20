import { expect, test } from "./fixture";
import { inspectSurface } from "./checks";
import corpus from "../fixtures/signalgraph-corpus-v1.json" with { type: "json" };

const snapshot = "5fbebaf3-38d2-4679-bef5-ab0a3d1063a0";
const source = corpus.records.find((record) => record.id === "repo-original")!.source;
const evidence = {
  document_id: "source-proof",
  snapshot_id: snapshot,
  title: "Реализация метода поиска по научным источникам",
  filename: "README.txt",
  mime_type: "text/plain",
  source,
  text_hash: "a".repeat(64),
  parser_revision: "provider-text-v1",
  pipeline_revision: "source-index-v4",
  parser_metadata: {},
  chunks: Array.from({ length: 35 }, (_, index) => ({
    id: `fragment-${index + 1}`,
    index,
    text: `Контрольный текст ${index + 1}. <script>alert('source')</script> — сохраняется как текст.`,
    char_start: index * 100,
    char_end: index * 100 + 85,
    page_start: 0,
    page_end: 0,
    locator_kind: "text_span",
    section: "",
  })),
};

for (const width of [390, 768, 1440, 1920]) {
  test(`source revision, fragments, reload and access denial at ${width}px`, async ({
    page,
  }, info) => {
    let revoked = false;
    const requests: string[] = [];
    await page.route("**/api/v1/documents/source-proof/source*", (route) => {
      requests.push(route.request().url());
      const revision = new URL(route.request().url()).searchParams.get("snapshot");
      return route.fulfill(
        revoked || (revision && revision !== snapshot)
          ? { status: 404, json: { error: "Source unavailable" } }
          : { json: evidence },
      );
    });
    await page.setViewportSize({ width, height: 950 });
    await page.goto("/sources/source-proof");
    await expect(page.getByRole("heading", { name: evidence.title })).toBeVisible();
    await expect(page).toHaveURL(new RegExp(`snapshot=${snapshot}`));
    await expect(page.getByText("GitHub · реализация", { exact: true })).toBeVisible();
    await expect(page.getByRole("link", { name: "Открыть первоисточник" })).toHaveAttribute(
      "href",
      new RegExp(source.github!.commit_sha),
    );
    if (width < 1024) await page.getByText("Источник и даты", { exact: true }).click();
    await page
      .getByText("Проверить версию и происхождение", { exact: true })
      .filter({ visible: true })
      .click();
    await expect(page.getByText(snapshot, { exact: true }).filter({ visible: true })).toBeVisible();
    await inspectSurface(page, info, `source-version-${width}`);
    await page.getByRole("button", { name: "Далее", exact: true }).click();
    await page.getByRole("link", { name: "Фрагмент 32", exact: true }).click();
    await expect(page).toHaveURL(/fragment=fragment-32/);
    await page.reload();
    await expect(page.getByRole("link", { name: "Фрагмент 32", exact: true })).toBeVisible();
    await expect(page.getByText(evidence.chunks[31]!.text, { exact: true })).toBeVisible();
    expect(requests.slice(1).every((url) => url.includes(snapshot))).toBe(true);
    revoked = true;
    await page.reload();
    await expect(page.getByRole("alert")).toContainText("права на неё отозваны");
    await expect(page.getByRole("heading", { name: evidence.title })).toHaveCount(0);
    await expect(page.getByText(evidence.chunks[31]!.text, { exact: true })).toHaveCount(0);
    revoked = false;
    await page.getByRole("button", { name: "Повторить", exact: true }).click();
    await expect(page.getByRole("heading", { name: evidence.title })).toBeVisible();
  });
}

test("missing snapshot and fragment never silently switch to current text", async ({ page }) => {
  await page.route("**/api/v1/documents/source-proof/source*", (route) => {
    const wanted = new URL(route.request().url()).searchParams.get("snapshot");
    return route.fulfill(wanted === snapshot ? { json: evidence } : { status: 404, json: {} });
  });
  await page.goto(`/sources/source-proof?snapshot=${snapshot}&fragment=absent`);
  await expect(
    page.getByText("Указанный фрагмент отсутствует в этой версии.", { exact: false }),
  ).toBeVisible();
  await page.goto("/sources/source-proof?snapshot=unpublished");
  await expect(page.getByRole("alert")).toBeVisible();
  await expect(page).toHaveURL(/snapshot=unpublished/);
  await expect(page.getByRole("heading", { name: evidence.title })).toHaveCount(0);
});

test("investor context retains its genre and unknown publication date", async ({ page }) => {
  await page.route("**/api/v1/documents/source-proof/source*", (route) =>
    route.fulfill({
      json: {
        ...evidence,
        source: {
          ...source,
          source_kind: "analysis",
          provider: "web",
          genre: "investment_thesis",
          publication: null,
          observed_at: "2026-10-18T02:25:27Z",
          arxiv: null,
          github: null,
        },
      },
    }),
  );
  await page.setViewportSize({ width: 390, height: 950 });
  await page.goto("/sources/source-proof");
  await expect(page.getByText("Инвестиционный тезис", { exact: true })).toBeVisible();
  await page.getByText("Источник и даты", { exact: true }).click();
  const provenance = page.locator("dl").filter({ visible: true });
  await expect(
    provenance.locator("div").filter({ has: page.getByText("Публикация", { exact: true }) }),
  ).toContainText("Неизвестно");
  await expect(
    provenance.locator("div").filter({ has: page.getByText("Дата наблюдения", { exact: true }) }),
  ).toContainText("2026-10-18T02:25:27Z");
  await expect(
    page.getByText("Публичная позиция инвестора.", { exact: false }).filter({ visible: true }),
  ).toBeVisible();
});
