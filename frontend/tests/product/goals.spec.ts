import { expect, test } from "./fixture";
import { choose, chooseRadio, inspectSurface } from "./checks";

test("restoring the last archived goal keeps the active view reachable", async ({ page, api }) => {
  api.state.kpis = api.state.kpis.slice(0, 1);
  api.state.kpis[0]!.status = "archived";
  await page.goto("/kpi");
  await chooseRadio(page, "Архив (1)");
  await page.getByRole("button", { name: "Вернуть из архива", exact: true }).click();
  await chooseRadio(page, "Активные (1)");
  await expect(
    page.getByRole("heading", { name: api.state.kpis[0]!.title, exact: true }),
  ).toBeVisible();
  await expect(page.getByRole("radio", { name: "Архив (0)", exact: true })).toBeVisible();
});

test("a thesis opens radar results, remembers the search and is one click away", async ({
  page,
  api,
}) => {
  await page.goto("/kpi");
  await page.getByRole("link", { name: "Найти сигналы под тезис", exact: true }).click();
  await expect(page).toHaveURL(/\/trends\?q=.+&thesis=kpi-1/);
  await expect(page.getByText(`Открыто из тезиса «${api.state.kpis[0]!.title}»`)).toBeVisible();
  await expect
    .poll(() => api.calls.some((call) => call.method === "PUT" && call.path === "/kpis/kpi-1"))
    .toBe(true);
  await page.getByRole("link", { name: "Вернуться к тезису", exact: true }).click();
  await expect(page).toHaveURL(/\/kpi\/kpi-1$/);
  await expect(page.getByRole("heading", { name: "Сигналы по тезису", exact: true })).toBeVisible();
  await expect(page.getByText("Стадия по шкале ТЗ: пилот", { exact: true })).toBeVisible();
  await page.getByRole("link", { name: "Смысловая память агентов", exact: true }).click();
  await expect(page).toHaveURL(/thesis=kpi-1&signal=candidate-memory/);
});

test("a thesis exposes generated drafts and keeps its filter when opening them", async ({
  page,
  api,
}) => {
  const draft = api.state.hypotheses[0]!;
  draft.kpi_id = "kpi-1";
  draft.generation = { kind: "fallback_evidence" };
  await page.goto("/kpi/kpi-1");
  await expect(page.getByRole("link", { name: "Гипотезы по тезису", exact: true })).toHaveAttribute(
    "href",
    "/hypotheses?kpi=kpi-1&kind=hypotheses&queue=all",
  );
  await expect(
    page.getByRole("link", { name: "Направления по тезису", exact: true }),
  ).toHaveAttribute("href", "/hypotheses?kpi=kpi-1&kind=directions&queue=all");
  await page.getByRole("link", { name: "Черновики по тезису", exact: true }).click();
  await expect(page).toHaveURL(/\/hypotheses\?kpi=kpi-1&kind=drafts&queue=all$/);
  await expect(page.getByText(draft.title, { exact: true })).toBeVisible();
});

for (const width of [1440, 390]) {
  test(`goal creation, edit, attachments, archive and confirmed deletion at ${width}px`, async ({
    page,
    api,
  }, info) => {
    await page.setViewportSize({ width, height: 950 });
    await page.goto("/kpi");
    await page.getByRole("button", { name: "Создать тезис", exact: true }).click();
    const dialog = page.getByRole("dialog");
    await expect(dialog.getByRole("button", { name: "Создать тезис", exact: true })).toBeDisabled();
    await dialog
      .getByLabel("Что ищем и зачем", { exact: true })
      .fill("Увеличить прочность на 20 %");
    await dialog.getByLabel("Гипотез", { exact: true }).fill("4");
    const chooser = page.waitForEvent("filechooser");
    await dialog.getByRole("button", { name: "Добавить файлы или перетащите их сюда" }).click();
    await (await chooser).setFiles(new URL("../fixtures/one-page.pdf", import.meta.url).pathname);
    await expect(dialog.getByText("one-page.pdf", { exact: true })).toBeVisible();
    await expect(dialog.getByRole("button", { name: "Создать тезис", exact: true })).toBeEnabled();
    await inspectSurface(page, info, "goal-create");
    await dialog.getByRole("button", { name: "Создать тезис", exact: true }).click();
    await expect(dialog).toBeHidden();
    await expect(
      page.getByRole("heading", { name: "Увеличить прочность на 20 %", exact: true }),
    ).toBeVisible();
    expect(
      api.calls.find((call) => call.path === "/kpis/kpi-3/documents" && call.method === "POST")
        ?.body,
    ).toMatchObject({ document_ids: ["doc-27"] });
    const card = page.locator('[data-slot="card-content"]').filter({
      has: page.getByRole("heading", { name: "Увеличить прочность на 20 %", exact: true }),
    });
    await card.getByRole("button", { name: "Изменить", exact: true }).click();
    await expect(dialog.getByLabel("Что ищем и зачем", { exact: true })).toHaveValue(
      "Увеличить прочность на 20 %",
    );
    await dialog.getByRole("button", { name: "Убрать файл", exact: true }).click();
    await expect
      .poll(() =>
        api.calls.some(
          (call) => call.method === "DELETE" && call.path === "/kpis/kpi-3/documents/doc-27",
        ),
      )
      .toBe(true);
    await dialog
      .getByLabel("Что ищем и зачем", { exact: true })
      .fill("Увеличить прочность на 25 %");
    await dialog.getByRole("button", { name: "Сохранить", exact: true }).click();
    await expect(dialog).toBeHidden();
    const updated = page.locator('[data-slot="card-content"]').filter({
      has: page.getByRole("heading", { name: "Увеличить прочность на 25 %", exact: true }),
    });
    await updated.getByRole("button", { name: "В архив", exact: true }).click();
    await chooseRadio(page, "Архив (2)");
    await updated.getByRole("button", { name: "Вернуть из архива", exact: true }).click();
    await chooseRadio(page, "Активные (2)");
    await updated.getByRole("button", { name: "Удалить", exact: true }).click();
    await inspectSurface(page, info, "goal-delete");
    await dialog.getByRole("button", { name: "Отмена", exact: true }).click();
    expect(api.state.kpis.some((kpi) => kpi.id === "kpi-3")).toBe(true);
    await updated.getByRole("button", { name: "Удалить", exact: true }).click();
    api.failures.set("DELETE /kpis/kpi-3", 500);
    await dialog.getByRole("button", { name: "Удалить", exact: true }).click();
    await expect(dialog).toBeVisible();
    await expect(page.getByText(/Тестовая ошибка/)).toBeVisible();
    await dialog.getByRole("button", { name: "Удалить", exact: true }).click();
    await expect(dialog).toBeHidden();
    await expect.poll(() => api.state.kpis.some((kpi) => kpi.id === "kpi-3")).toBe(false);
  });

  test(`goal suggestions, generation and graph directions at ${width}px`, async ({
    page,
    api,
  }, info) => {
    await page.setViewportSize({ width, height: 950 });
    await page.goto("/kpi");
    await page.getByRole("button", { name: "Предложить тезисы из базы", exact: true }).click();
    await expect(
      page.getByRole("heading", { name: "Повысить устойчивость покрытия", exact: true }),
    ).toBeVisible();
    await page
      .locator('[data-slot="card-content"]')
      .filter({
        has: page.getByRole("heading", { name: "Повысить устойчивость покрытия", exact: true }),
      })
      .getByRole("button", { name: "Создать тезис", exact: true })
      .click();
    const dialog = page.getByRole("dialog");
    await expect(dialog.getByLabel("Что ищем и зачем", { exact: true })).toHaveValue(
      /Повысить устойчивость/,
    );
    await dialog.getByRole("button", { name: "Закрыть", exact: true }).click();
    await page.getByRole("button", { name: "Сгенерировать гипотезы", exact: true }).click();
    await expect(dialog.getByRole("combobox", { name: "Тезис", exact: true })).toContainText(
      "Повысить термостойкость композита",
    );
    await choose(page, "Тезис", "Новый тезис — опишу текстом");
    await expect(dialog.getByLabel("Что ищем и зачем", { exact: true })).toBeVisible();
    await choose(page, "Тезис", "Повысить термостойкость композита");
    await dialog
      .getByLabel("Уточнения и ограничения", { exact: true })
      .fill("Срок проверки — две недели");
    await dialog.getByRole("button", { name: "Сгенерировать гипотезы", exact: true }).click();
    await expect(dialog).toBeHidden();
    await expect.poll(() => api.state.jobs.length).toBeGreaterThan(0);
    await expect
      .poll(() =>
        api.calls.some((call) => call.method === "POST" && call.path === "/hypothesis-jobs"),
      )
      .toBe(true);
    await page.getByRole("button", { name: "Черновые темы из документов", exact: true }).click();
    await expect(dialog.getByText("Новый режим обработки", { exact: true })).toBeVisible();
    await inspectSurface(page, info, "directions");
    await dialog.getByRole("button", { name: "Сохранить как гипотезу", exact: true }).click();
    await expect(
      dialog.getByRole("button", { name: "Сохранено в черновики", exact: true }),
    ).toBeDisabled();
    expect(
      api.state.hypotheses.some((hypothesis) => hypothesis.title === "Новый режим обработки"),
    ).toBe(true);
    await dialog.getByRole("button", { name: "research-1.pdf", exact: true }).click();
    await expect(
      page.getByRole("dialog").last().getByRole("radio", { name: "Текст", exact: true }),
    ).toBeVisible();
    await page.keyboard.press("Escape");
    await expect(page.getByRole("dialog", { name: "Черновые темы из графа знаний" })).toBeVisible();
    await page.keyboard.press("Escape");
    await page.getByRole("button", { name: /Уведомления/ }).click();
    await page.getByRole("button", { name: /Очистить/ }).click();
    await expect(page.getByRole("button", { name: /Очистить/ })).toBeHidden();
  });
}
