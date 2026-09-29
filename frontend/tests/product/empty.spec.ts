import { expect, test } from "./fixture";
import { chooseRadio, inspectSurface } from "./checks";

for (const [path, key, message] of [
  ["/operator/documents", "documents", "Документов пока нет — загрузите первый"],
  ["/admin", "documents", "Документов пока нет"],
  ["/kpi", "kpis", "Создайте первый тезис"],
  ["/summary", "clusters", "Тем пока нет"],
  ["/history", "chats", "История пока пуста"],
  ["/hypotheses", "hypotheses", "Гипотез пока нет"],
] as const) {
  test(`${path} has an honest, accessible empty state`, async ({ page, api }, info) => {
    await page.setViewportSize({ width: 390, height: 900 });
    api.state[key] = [];
    if (path === "/history") api.state.searches = [];
    await page.goto(path);
    await expect(page.getByText(message, { exact: true })).toBeVisible();
    await expect(page.getByRole("button", { name: "Повторить", exact: true })).toBeHidden();
    await inspectSurface(page, info, "empty");
  });
}

test("a failed search can be retried in the same chat, and citations recover inside the same sheet", async ({
  page,
  api,
}, info) => {
  await page.setViewportSize({ width: 390, height: 900 });
  await page.goto("/search?chat=chat-1");
  api.failures.set("POST /chats/chat-1/messages", 500);
  await page
    .getByRole("textbox", { name: "Уточняющий вопрос", exact: true })
    .fill("Проверить эффект повторно");
  await page.getByRole("button", { name: "Спросить", exact: true }).click();
  const retry = page.getByRole("button", { name: "Спросить ещё раз", exact: true });
  await expect(retry).toBeVisible();
  await inspectSurface(page, info, "answer-error");
  await retry.click();
  await expect(retry).toBeHidden();
  await expect(
    page.getByRole("heading", { name: "Проверить эффект повторно", exact: true }),
  ).toBeVisible();
  expect(
    api.calls.filter((call) => call.method === "POST" && call.path === "/chats/chat-1/messages"),
  ).toHaveLength(2);
  api.failures.set("GET /documents/doc-1/chunks", 500);
  await page
    .getByRole("button", { name: "Открыть источник 1 в этом месте", exact: true })
    .first()
    .click();
  const dialog = page.getByRole("dialog");
  await chooseRadio(page, "Текст");
  await expect(dialog.getByRole("button", { name: "Повторить", exact: true })).toBeVisible();
  await dialog.getByRole("button", { name: "Повторить", exact: true }).click();
  await expect(dialog.getByRole("button", { name: "Повторить", exact: true })).toBeHidden();
  await dialog.getByRole("button", { name: "Закрыть", exact: true }).click();
  await expect(dialog).toBeHidden();
});

test("source-based generation and the detail export buttons use the selected documents and hypothesis", async ({
  page,
  api,
}, info) => {
  await page.goto("/search?chat=chat-1");
  await page
    .getByRole("button", { name: "Сгенерировать гипотезы из этих источников", exact: true })
    .click();
  await expect
    .poll(
      () =>
        api.calls.find((call) => call.method === "POST" && call.path === "/hypothesis-jobs")?.body,
    )
    .toMatchObject({ kind: "generate", document_ids: ["doc-1"], count: 3 });
  await page.goto("/hypotheses/hypothesis-1");
  const download = page.waitForEvent("download");
  await page.getByRole("button", { name: "Экспорт", exact: true }).click();
  await page.getByRole("menuitem", { name: "Документ Word", exact: true }).click();
  const file = await download;
  await file.saveAs(info.outputPath(file.suggestedFilename()));
  expect(file.suggestedFilename()).toMatch(/\.docx$/);
  await page.getByRole("button", { name: "Экспорт", exact: true }).click();
  await page.getByRole("menuitem", { name: "Печать и PDF", exact: true }).click();
  await expect(page).toHaveURL(/\/hypotheses\/hypothesis-1\/report$/);
});
