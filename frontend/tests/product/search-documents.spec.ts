import { expect, test } from "./fixture";
import { choose, chooseRadio, inspectSurface } from "./checks";

for (const width of [1280, 390]) {
  test(`search, follow-up, trace, clipboard and citations at ${width}px`, async ({
    page,
    api,
    context,
  }, info) => {
    api.state.messages[1]!.sources![0]!.source_index_snapshot = "snapshot-1";
    await context.grantPermissions(["clipboard-read", "clipboard-write"]);
    await page.setViewportSize({ width, height: 950 });
    await page.goto("/search");
    await page
      .getByRole("textbox", { name: "Поисковый запрос" })
      .fill("Какая температура сохраняет прочность?");
    await page.getByRole("textbox", { name: "Поисковый запрос" }).press("Enter");
    await expect(
      page.getByRole("heading", { name: "Какая температура сохраняет прочность?", exact: true }),
    ).toBeVisible();
    await expect(page.getByText("Термообработка", { exact: true })).toBeVisible();
    expect(
      api.calls.find((call) => call.path === "/chats" && call.method === "POST")?.body,
    ).toMatchObject({ title: "Какая температура сохраняет прочность?" });
    await page.getByRole("button", { name: "Как искали", exact: true }).click();
    await inspectSurface(page, info, "search-trace");
    await page.getByRole("button", { name: "Копировать", exact: true }).click();
    await expect
      .poll(() => page.evaluate(() => navigator.clipboard.readText()))
      .toContain("Термообработка");
    await page.getByRole("button", { name: "Открыть источник 1 в этом месте" }).click();
    await expect(page.getByRole("radio", { name: "Текст", exact: true })).toBeVisible();
    await expect(
      page.getByRole("link", { name: "Версия и происхождение источника" }),
    ).toBeVisible();
    for (const label of ["Документ", "Текст"]) {
      const lines = await page
        .getByRole("radio", { name: label, exact: true })
        .locator("..")
        .locator("span")
        .evaluate((element) => {
          const range = document.createRange();
          range.selectNodeContents(element);
          return (
            range.getBoundingClientRect().height /
            Number.parseFloat(getComputedStyle(element).lineHeight)
          );
        });
      expect(lines).toBeLessThanOrEqual(1.1);
    }
    await chooseRadio(page, "Текст");
    await expect(
      page.getByText("Сохранение прочности после термообработки составило 90 %.", { exact: true }),
    ).toBeVisible();
    await inspectSurface(page, info, "search-source");
    const preview =
      width >= 1024
        ? page.getByRole("complementary", { name: "Просмотр документа" })
        : page.getByRole("dialog");
    await preview
      .getByRole("button", { name: width >= 1024 ? "Закрыть просмотр" : "Закрыть", exact: true })
      .click();
    await page
      .getByRole("textbox", { name: "Уточняющий вопрос" })
      .fill("А если повысить температуру?");
    await page.getByRole("button", { name: "Спросить", exact: true }).click();
    await expect(
      page.getByRole("heading", { name: "А если повысить температуру?", exact: true }),
    ).toBeVisible();
    await expect(page.getByRole("button", { name: "Спросить", exact: true })).toBeDisabled();
    await page.reload();
    await expect(
      page.getByRole("heading", { name: "Какая температура сохраняет прочность?", exact: true }),
    ).toBeVisible();
    await expect(
      page.getByRole("heading", { name: "А если повысить температуру?", exact: true }),
    ).toBeVisible();
  });

  test(`history pagination, scope and local search at ${width}px`, async ({ page, api }) => {
    await page.setViewportSize({ width, height: 900 });
    await page.goto("/history");
    await page.getByRole("textbox").fill("Сравнение материалов 25");
    await expect(page.getByText("Ничего не нашлось", { exact: true })).toBeVisible();
    await page.getByRole("button", { name: "Следующая страница" }).click();
    await expect(page.getByRole("button", { name: /Сравнение материалов 25/ })).toBeVisible();
    await expect(page.getByRole("textbox")).toHaveValue("Сравнение материалов 25");
    await page.getByRole("button", { name: "Предыдущая страница" }).click();
    await page.getByRole("textbox").fill("");
    await chooseRadio(page, "Все пользователи");
    await expect(page.getByRole("radio", { name: "Все пользователи", exact: true })).toBeChecked();
    await chooseRadio(page, "Мои");
    await page.getByRole("textbox").fill("термостойкость");
    await expect(page.getByRole("button", { name: /Как повысить термостойкость/ })).toBeVisible();
    await expect(page.getByRole("button", { name: /Сравнение материалов 2 / })).toBeHidden();
    await page.getByRole("button", { name: /Как повысить термостойкость/ }).click();
    await expect(page).toHaveURL(/\/search\?chat=chat-1/);
    await expect
      .poll(() => api.calls.some((call) => call.path === "/chats/chat-1/messages"))
      .toBe(true);
  });

  test(`documents filter, paginate, refresh, preview, upload and open the board at ${width}px`, async ({
    page,
    api,
  }, info) => {
    await page.setViewportSize({ width, height: 900 });
    await page.goto("/operator/documents");
    await page.getByRole("button", { name: "Следующая страница" }).click();
    await expect(page.getByText("research-26.pdf", { exact: true })).toBeVisible();
    await page.getByRole("button", { name: "Предыдущая страница" }).click();
    await choose(page, "Фильтр по типу файла", "Текст");
    await expect(page.getByText("research-2.txt", { exact: true })).toBeVisible();
    await expect(page.getByText("research-1.pdf", { exact: true })).toBeHidden();
    await choose(page, "Фильтр по типу файла", "Все типы");
    await page.getByRole("textbox").fill("research-1.pdf");
    await expect(page.getByText("research-2.txt", { exact: true })).toBeHidden();
    await page.getByRole("button", { name: "Предпросмотр: research-1.pdf", exact: true }).click();
    const dialog = page.getByRole("dialog");
    await expect(dialog).toBeVisible();
    await chooseRadio(page, "Текст");
    await expect(
      dialog.getByText("Сохранение прочности после термообработки составило 90 %.", {
        exact: true,
      }),
    ).toBeVisible();
    await inspectSurface(page, info, "document-text");
    await chooseRadio(page, "Документ");
    await expect(dialog.locator("canvas").first()).toBeVisible();
    await dialog.getByRole("button", { name: "Закрыть", exact: true }).click();
    await page.getByRole("textbox").clear();
    const before = api.calls.filter((call) => call.path === "/admin/documents").length;
    await page.getByRole("button", { name: "Обновить", exact: true }).click();
    await expect
      .poll(() => api.calls.filter((call) => call.path === "/admin/documents").length)
      .toBeGreaterThan(before);
    const chooser = page.waitForEvent("filechooser");
    await page.getByRole("button", { name: "Загрузить", exact: true }).click();
    await (await chooser).setFiles(new URL("../fixtures/one-page.pdf", import.meta.url).pathname);
    await expect(page.getByText("one-page.pdf", { exact: true })).toBeVisible();
    await expect
      .poll(() => api.calls.some((call) => call.path === "/documents" && call.method === "POST"))
      .toBe(true);
    await page.getByRole("link", { name: /Сгенерировать/ }).click();
    await expect(page).toHaveURL(/\/hypotheses$/);
  });
}

test("overview links open the matching document status and search selection", async ({
  page,
  api,
}) => {
  api.state.documents[0]!.status = "parsing";
  await page.goto("/admin");
  await page.getByRole("link").filter({ hasText: "Ошибки" }).click();
  await expect(page).toHaveURL(/status=failed/);
  await expect(page.getByRole("combobox", { name: "Фильтр по состоянию" })).toContainText(
    "С ошибкой",
  );
  await expect(page.locator("tbody tr")).toHaveCount(1);
  await choose(page, "Фильтр по состоянию", "В обработке");
  await expect(page.locator("tbody tr")).toHaveCount(1);
  await expect(page.locator("tbody")).toContainText("Термостойкость полимерных композитов");
  await choose(page, "Фильтр по состоянию", "Все состояния");
  await expect(page.locator("tbody tr")).toHaveCount(20);
  await page.goto("/operator/documents?q=research-2.txt");
  await expect(page.getByRole("textbox", { name: "Поиск по документам" })).toHaveValue(
    "research-2.txt",
  );
  await expect(page.locator("tbody tr")).toHaveCount(1);
});

test("async document preview restores its opener or the search when the row disappears", async ({
  page,
}) => {
  let releaseResponse!: () => void;
  const holdResponse = () =>
    new Promise<void>((resolve) => {
      releaseResponse = resolve;
    });
  let pendingResponse = holdResponse();
  await page.route("**/api/v1/documents/doc-2/chunks", async (route) => {
    await pendingResponse;
    await route.fallback();
  });
  await page.goto("/operator/documents");
  const opener = page.getByRole("button", { name: "Предпросмотр: research-2.txt", exact: true });
  const search = page.getByRole("textbox", { name: "Поиск по документам", exact: true });
  const dialog = page.getByRole("dialog");

  try {
    await opener.focus();
    await page.keyboard.press("Enter");
    await expect(opener).toBeDisabled();
    releaseResponse();
    await expect(dialog).toBeVisible();
    await page.keyboard.press("Escape");
    await expect(opener).toBeFocused();

    pendingResponse = holdResponse();
    await opener.press("Enter");
    await expect(opener).toBeDisabled();
    await search.fill("research-1.pdf");
    await expect(opener).toHaveCount(0);
    releaseResponse();
    await expect(dialog).toBeVisible();
    await dialog.getByRole("button", { name: "Закрыть", exact: true }).click();
    await expect(search).toBeFocused();
    await expect(search).toHaveValue("research-1.pdf");
  } finally {
    releaseResponse();
  }
});

test("document status and preview fit a 1024px desktop and text originals remain readable in dark mode", async ({
  page,
  api,
}, info) => {
  api.state.documents[0]!.title =
    "Исследование термостойкости многослойных полимерных композитов для оборудования с переменными нагрузками";
  api.state.documents[0]!.status = "parsing";
  await page.addInitScript(() => localStorage.setItem("rag.theme", "dark"));
  await page.setViewportSize({ width: 1024, height: 900 });
  await page.goto("/operator/documents");
  await expect(page.getByRole("button", { name: "Свернуть меню", exact: true })).toBeVisible();
  await expect(page.getByRole("columnheader", { name: "Размер", exact: true })).toBeHidden();
  await expect(page.getByRole("columnheader", { name: "Обновлён", exact: true })).toBeHidden();
  const table = page.getByRole("table");
  const firstRows = table.locator("tbody tr");
  await expect(firstRows).toHaveCount(20);
  const geometry = await firstRows.evaluateAll((rows) =>
    rows.slice(0, 3).map((row) => {
      const frame = row.closest("section")!.getBoundingClientRect();
      const cells = row.querySelectorAll("td");
      return [cells[0]!, cells[4]!, cells[5]!].map((cell) => {
        const bounds = cell.getBoundingClientRect();
        return {
          visible: bounds.left >= frame.left && bounds.right <= frame.right + 1,
          width: bounds.width,
        };
      });
    }),
  );
  expect(
    geometry.flat().every((cell) => cell.visible && cell.width > 0),
    JSON.stringify(geometry),
  ).toBe(true);
  await expect(
    page.getByRole("button", { name: "Предпросмотр: research-2.txt", exact: true }),
  ).toBeInViewport();
  await inspectSurface(page, info, "documents-1024-dark");

  await page.setViewportSize({ width: 1280, height: 900 });
  await expect(page.getByRole("columnheader", { name: "Размер", exact: true })).toBeVisible();
  await page.route("**/api/v1/documents/doc-2/content", (route) =>
    route.fulfill({
      contentType: "text/plain; charset=utf-8",
      body:
        "Термообработка полимерных композитов\n\n" +
        "Сохранение прочности после термообработки составило 90 %. Измерения выполнены для образцов с разным составом и сопоставлены с исходными значениями. ".repeat(
          8,
        ),
    }),
  );
  await page.getByRole("button", { name: "Предпросмотр: research-2.txt", exact: true }).click();
  const dialog = page.getByRole("dialog");
  const reader = dialog.locator("pre");
  await expect(reader).toContainText("Термообработка полимерных композитов");
  await expect(reader).toHaveCSS("font-size", "16px");
  await expect(reader).toHaveCSS("line-height", "28px");
  await page.evaluate(() => document.fonts.ready);
  const measure = await reader.evaluate((element) => {
    const style = getComputedStyle(element);
    const context = document.createElement("canvas").getContext("2d")!;
    context.font = style.font;
    return element.getBoundingClientRect().width / context.measureText("0").width;
  });
  expect(measure).toBeGreaterThanOrEqual(64.5);
  expect(measure).toBeLessThanOrEqual(75);
  await inspectSurface(page, info, "document-original-dark");
});
