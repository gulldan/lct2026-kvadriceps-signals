import type { Page } from "@playwright/test";
import { expect, test } from "./fixture";
import { inspectSurface } from "./checks";

async function searchGeometry(page: Page) {
  await page.evaluate(() => document.fonts.ready);
  const layout = (await page.locator('[data-slot="page-layout"]').boundingBox())!;
  const header = (await page.locator('[data-slot="page-header"]').boundingBox())!;
  const title = (await page.getByRole("heading", { level: 1 }).boundingBox())!;
  const bar = (await page
    .getByRole("textbox", { name: "Поисковый запрос", exact: true })
    .locator("xpath=ancestor::form")
    .boundingBox())!;
  return {
    layout: { x: layout.x, y: layout.y, width: layout.width },
    header,
    title: { x: title.x, y: title.y, height: title.height },
    bar,
  };
}

for (const [width, height] of [
  [1280, 720],
  [1024, 768],
] as const) {
  test(`search keeps its geometry and distinguishes cited documents from retrieval at ${width}px`, async ({
    page,
    api,
  }, info) => {
    await page.addInitScript(
      (theme) => localStorage.setItem("rag.theme", theme),
      width === 1024 ? "dark" : "light",
    );
    const snippets = [
      "Первый режим сохраняет прочность после длительного нагрева.",
      "Второй режим снижает деформацию после охлаждения.",
    ];
    const answer = api.state.messages[1]!;
    answer.content = `Первый режим проверен [S2]. Второй режим проверен [S4].`;
    answer.sources = [
      {
        document_id: "doc-1",
        filename: "research-1.pdf",
        chunk_id: "uncited-top",
        snippet: "Самый близкий результат поиска не процитирован в ответе.",
        score: 0.99,
      },
      {
        document_id: "doc-2",
        filename: "research-2.txt",
        chunk_id: "cited-first",
        snippet: snippets[0]!,
        score: 0.8,
      },
      {
        document_id: "doc-3",
        filename: "research-3.pdf",
        chunk_id: "uncited-other",
        snippet: "Дополнительные результаты испытаний.",
        score: 0.7,
      },
      {
        document_id: "doc-2",
        filename: "research-2.txt",
        chunk_id: "cited-second",
        snippet: snippets[1]!,
        score: 0.6,
      },
    ];
    answer.meta = undefined;
    await page.route("**/api/v1/documents/doc-2/chunks*", (route) =>
      route.fulfill({
        json: {
          document: api.state.documents[1],
          chunks: snippets.map((text, index) => ({
            id: index === 0 ? "cited-first" : "cited-second",
            index,
            text,
          })),
        },
      }),
    );
    let releaseAnswer!: () => void;
    const pendingAnswer = new Promise<void>((resolve) => {
      releaseAnswer = resolve;
    });
    await page.route("**/api/v1/chats/*/messages", async (route) => {
      if (route.request().method() === "POST") await pendingAnswer;
      await route.fallback();
    });
    await page.setViewportSize({ width, height });
    await page.goto("/history");
    const historyTitle = page.getByRole("heading", { level: 1 });
    await expect(historyTitle).toBeVisible();
    const titleY = (await historyTitle.boundingBox())!.y;
    await page.goto("/search");
    const query = page.getByRole("textbox", { name: "Поисковый запрос", exact: true });
    await expect(query).toBeVisible();
    const home = await searchGeometry(page);
    expect(home.title.y).toBe(titleY);

    await query.fill("Какие режимы обработки подтверждены источником?");
    await query.press("Enter");
    try {
      await expect(
        page.getByText("Ищем в документах и собираем ответ — обычно это занимает до минуты."),
      ).toBeVisible();
      expect(await searchGeometry(page)).toEqual(home);
    } finally {
      releaseAnswer();
    }

    const citedFooter = page.getByText("В ответе процитирован 1 источник", { exact: true });
    const citedHeading = page.getByText("Источники в ответе · 1", { exact: true });
    const chips = page.getByRole("button", {
      name: "Открыть источник 1 в этом месте",
      exact: true,
    });
    const retrieved = (count: number) =>
      page.locator("details").filter({
        has: page.locator("summary", { hasText: `Другие найденные документы · ${count}` }),
      });
    const assertCited = async () => {
      await expect(citedFooter).toBeVisible();
      await expect(citedHeading).toBeVisible();
      await expect(chips).toHaveCount(2);
      await expect(chips).toHaveText(["1", "1"]);
      await expect(retrieved(2)).toHaveCount(1);
      await expect(retrieved(2)).not.toHaveAttribute("open");
      await expect(page.getByRole("button").filter({ hasText: "research-1.pdf" })).toBeHidden();
      await expect(page.getByRole("button").filter({ hasText: "research-2.txt" })).toBeVisible();
    };
    await assertCited();
    expect(await searchGeometry(page)).toEqual(home);

    const preview = page.getByRole("complementary", { name: "Просмотр документа" });
    for (const [index, snippet] of snippets.entries()) {
      await chips.nth(index).click();
      await expect(preview).toBeVisible();
      await expect(preview.locator(".hl-frag")).toHaveText(snippet);
      await expect(
        preview.getByRole("button", { name: `Совпадение ${index + 1} из 2`, exact: true }),
      ).toBeVisible();
      expect((await page.locator('[data-slot="page-layout"]').boundingBox())!.width).toBeLessThan(
        home.layout.width,
      );
      await preview.getByRole("button", { name: "Закрыть просмотр", exact: true }).click();
      await expect(preview).toHaveCount(0);
      const restored = await searchGeometry(page);
      expect(restored.layout.width).toBe(home.layout.width);
      expect(restored.bar.width).toBe(home.bar.width);
    }
    await retrieved(2).locator("summary").click();
    for (const filename of ["research-1.pdf", "research-3.pdf"])
      await expect(retrieved(2).getByRole("button").filter({ hasText: filename })).toBeVisible();
    const sent = () =>
      api.calls.filter((call) => call.method === "POST" && call.path.endsWith("/messages")).length;
    const sentBeforeReload = sent();
    await page.reload();
    await assertCited();
    expect(sent()).toBe(sentBeforeReload);
    expect(await searchGeometry(page)).toEqual(home);

    answer.content = "Проверьте термостойкость на контрольных образцах.";
    await query.fill("Что проверить перед испытанием?");
    await query.press("Enter");
    const noCitations = page.getByText("В ответе нет ссылок на источники", { exact: true });
    for (const saved of [false, true]) {
      if (saved) await page.reload();
      await expect(noCitations).toBeVisible();
      await expect(citedHeading).toHaveCount(0);
      await expect(chips).toHaveCount(0);
      await expect(retrieved(3)).toHaveCount(1);
      await expect(retrieved(3)).not.toHaveAttribute("open");
      expect(await searchGeometry(page)).toEqual(home);
    }
    expect(sent()).toBe(sentBeforeReload + 1);
    await retrieved(3).locator("summary").click();
    await expect(retrieved(3)).toHaveAttribute("open");
    await page.evaluate(() => {
      document.documentElement.style.fontSize = "200%";
    });
    await inspectSurface(page, info, `search-text200-${width}`);
  });
}
