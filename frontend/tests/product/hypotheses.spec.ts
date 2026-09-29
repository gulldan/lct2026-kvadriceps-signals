import { readFile } from "node:fs/promises";
import { expect, test } from "./fixture";
import { choose, inspectSurface } from "./checks";
import { inspectControlAlignment } from "./design-contract";

test("shared board filters survive reload, chip removal and reset", async ({ page }) => {
  await page.goto("/hypotheses?trl=6&kpi=kpi-1&queue=all");
  const trl = page.getByRole("button", { name: "Убрать фильтр: Готовность 6/9", exact: true });
  const goal = page.getByRole("button", {
    name: "Убрать фильтр: Повысить термостойкость композита",
    exact: true,
  });
  await expect(trl).toBeVisible();
  await expect(goal).toBeVisible();
  const filters = page.getByRole("button", { name: /^Фильтры/ });
  await expect(filters).toContainText("2");
  await filters.click();
  await filters.click();
  await expect(trl).toBeVisible();
  await expect(goal).toBeVisible();
  await trl.click();
  await expect(page).toHaveURL(/\/hypotheses\?kpi=kpi-1&queue=all$/);
  await page.reload();
  await expect(trl).toBeHidden();
  await expect(goal).toBeVisible();
  await page.getByRole("button", { name: "Сброс", exact: true }).click();
  await expect(page).toHaveURL(/\/hypotheses\?queue=all$/);
  await expect(goal).toBeHidden();
});

for (const width of [1440, 390]) {
  test(`board queues, every facet, sort and view at ${width}px`, async ({ page }, info) => {
    await page.setViewportSize({ width, height: 950 });
    await page.goto("/hypotheses");
    const help = page.getByRole("button", { name: "О разделе", exact: true });
    await expect(page.locator("#hypotheses-help")).toBeHidden();
    await help.click();
    await expect(page.locator("#hypotheses-help")).toContainText("Гипотеза — проверяемое");
    await help.press("Enter");
    await expect(page.locator("#hypotheses-help")).toBeHidden();
    await expect(help).toBeFocused();
    const filters = page.getByRole("button", { name: "Фильтры", exact: true });
    await expect(filters).toHaveAttribute("aria-expanded", "false");
    await expect(page.getByRole("button", { name: "Область применения", exact: true })).toHaveCount(
      0,
    );
    for (const name of [
      /Противоречия \d/,
      /На проверке \d/,
      /Недостаточно данных \d/,
      /Все \d/,
      /Есть подтверждения \d/,
    ]) {
      const queue = page.getByRole("button", { name });
      await queue.click();
      await expect(queue).toHaveAttribute("aria-pressed", "true");
    }
    await page.getByRole("button", { name: /Все \d/ }).click();
    await page.getByRole("combobox", { name: "Сортировка гипотез" }).click();
    const sorts = await page.getByRole("option").allTextContents();
    await page.keyboard.press("Escape");
    for (const sort of sorts) await choose(page, "Сортировка гипотез", sort.trim());
    await filters.click();
    await expect(filters).toHaveAttribute("aria-expanded", "true");
    await inspectSurface(page, info, "filters-open");
    for (const facet of [
      "Область применения",
      "Функциональная область",
      "Тип источника",
      "Готовность (УГТ)",
      "Тезис",
    ]) {
      await page.getByRole("button", { name: facet, exact: true }).click();
      const option = page.getByRole("menuitemcheckbox").first();
      await option.click();
      await expect(option).toBeChecked();
      await inspectControlAlignment(page);
      await option.click();
      await expect(option).not.toBeChecked();
      await page.keyboard.press("Escape");
    }
    await filters.click();
    await expect(filters).toHaveAttribute("aria-expanded", "false");
    await page.getByRole("textbox", { name: "Поиск по гипотезам" }).fill("нет-такой-технологии");
    await expect(
      page.getByRole("heading", { name: "Термообработка повышает стойкость композита" }),
    ).toBeHidden();
    await inspectSurface(page, info, "filters-active");
    await page.getByRole("textbox", { name: "Поиск по гипотезам" }).clear();
    await page.getByRole("tab", { name: /Черновые темы/ }).click();
    await expect(
      page.getByRole("heading", {
        name: "Термостойкие композиты для защитных деталей",
        exact: true,
      }),
    ).toBeVisible();
    await page.getByRole("tab", { name: "Черновики", exact: true }).click();
    await inspectSurface(page, info, "drafts");
    await page.getByRole("tab", { name: "Методология", exact: true }).click();
    await expect(page.getByRole("button", { name: "Экспорт", exact: true })).toBeDisabled();
    await inspectSurface(page, info, "methodology");
    await page.getByRole("tab", { name: /Гипотезы \d/ }).click();
    await page.getByRole("button", { name: /Термообработка повышает стойкость композита/ }).click();
    await expect(page).toHaveURL(/\/hypotheses\/hypothesis-1$/);
    await page.getByRole("button", { name: "Назад", exact: true }).click();
    await expect(page).toHaveURL(/\/hypotheses\??/);
  });

  test(`graph filtering, zoom and opening a hypothesis at ${width}px`, async ({ page }, info) => {
    await page.setViewportSize({ width, height: 950 });
    await page.goto("/hypotheses");
    await page.getByRole("tab", { name: "Связи с источниками", exact: true }).click();
    await choose(page, "Фильтр по тезису", "Повысить термостойкость композита");
    for (const stance of ["Подтверждает", "Противоречит", "Контекст", "Все связи"])
      await choose(page, "Тип связи с источником", stance);
    for (const action of ["Приблизить", "Отдалить", "Вписать в экран"])
      await page.getByRole("button", { name: action, exact: true }).click();
    await inspectSurface(page, info, "graph");
    await page.getByRole("textbox", { name: /Поиск.*граф/ }).fill("Термообработка");
    await page.getByRole("button", { name: "Открыть гипотезу", exact: true }).first().click();
    await expect(page).toHaveURL(/\/hypotheses\/hypothesis-1$/);
  });

  test(`all score explanations and refinement at ${width}px`, async ({ page, api }, info) => {
    await page.setViewportSize({ width, height: 950 });
    await page.goto("/hypotheses/hypothesis-1");
    for (const label of [
      "Приоритет проверки",
      "Проверка по базе",
      "Новизна",
      "Готовность (УГТ)",
      "Индекс темы",
    ]) {
      await page
        .getByRole("button", { name: new RegExp(`^${label.replace(/[()]/g, "\\$&")}`) })
        .first()
        .click();
      await expect(page.getByRole("dialog")).toBeVisible();
      await expect(
        page.getByRole("dialog").getByRole("button", { name: "Закрыть", exact: true }),
      ).toBeInViewport();
      await inspectSurface(page, info, `score-${label}`);
      if (label === "Новизна") {
        await page
          .getByRole("dialog")
          .getByRole("button", { name: "Улучшить (ИИ-агенты)", exact: true })
          .click();
        await expect.poll(() => api.state.jobs.some((job) => job.kind === "refine")).toBe(true);
      }
      await page
        .getByRole("dialog")
        .getByRole("button", { name: "Закрыть", exact: true })
        .click({ timeout: 2000 });
    }
  });

  test(`expert decisions and comment history at ${width}px`, async ({ page, api }) => {
    await page.setViewportSize({ width, height: 950 });
    await page.goto("/hypotheses/hypothesis-1");
    for (const [label, status] of [
      ["Наблюдать", "under_review"],
      ["Отказ", "rejected"],
      ["Пилот", "approved"],
    ]) {
      await page.getByRole("button", { name: label!, exact: true }).click();
      await expect.poll(() => api.state.hypotheses[0]?.status).toBe(status);
    }
    await page
      .getByRole("textbox", { name: "Основание решения или комментарий…", exact: true })
      .fill("Нужна контрольная серия образцов");
    await page.getByRole("button", { name: "Добавить комментарий", exact: true }).click();
    await expect(
      page.getByRole("textbox", { name: "Основание решения или комментарий…", exact: true }),
    ).toHaveValue("");
    await expect
      .poll(() => api.state.revisions.at(-1)?.summary)
      .toBe("Нужна контрольная серия образцов");
    await page
      .locator("#history")
      .getByRole("button", { name: /История/ })
      .first()
      .click();
    await expect(
      page.locator("#history").getByText(/Нужна контрольная серия образцов/),
    ).toBeVisible();
  });

  test(`hypothesis text, roadmap and detail actions at ${width}px`, async ({ page, api }, info) => {
    await page.setViewportSize({ width, height: 950 });
    await page.goto("/hypotheses/hypothesis-1");
    await page.getByRole("button", { name: "Изменить", exact: true }).first().click();
    const dialog = page.getByRole("dialog");
    await dialog.getByLabel("Утверждение", { exact: true }).clear();
    await expect(dialog.getByRole("button", { name: "Сохранить", exact: true })).toBeDisabled();
    await dialog
      .getByLabel("Утверждение", { exact: true })
      .fill("Проверить сохранение прочности на контрольной серии.");
    await dialog.getByLabel("Заголовок", { exact: true }).fill("Уточнённая гипотеза");
    await dialog
      .getByLabel("Обоснование", { exact: true })
      .fill("Сопоставить результаты с исходным материалом.");
    await inspectSurface(page, info, "hypothesis-edit");
    await dialog.getByRole("button", { name: "Сохранить", exact: true }).click();
    await expect(
      page.getByRole("heading", { name: "Уточнённая гипотеза", exact: true }),
    ).toBeVisible();
    await page.getByRole("button", { name: "Изменить", exact: true }).nth(1).click();
    await dialog.getByRole("button", { name: "Добавить этап", exact: true }).click();
    await dialog.getByLabel("Этап 1", { exact: true }).fill("Подготовить образцы");
    await dialog.getByLabel("Цель этапа", { exact: true }).fill("Получить контрольную серию");
    await dialog.getByRole("button", { name: "Добавить этап", exact: true }).click();
    await dialog.getByRole("button", { name: "Удалить этап", exact: true }).last().click();
    await dialog.getByLabel("Стоимость", { exact: true }).fill("60 000 ₽");
    await dialog.getByLabel("Срок", { exact: true }).fill("3 недели");
    await inspectSurface(page, info, "roadmap-edit");
    await dialog.getByRole("button", { name: "Сохранить", exact: true }).click();
    await expect
      .poll(() => api.state.hypotheses[0]?.detail.experiment_plan?.estimated_time)
      .toBe("3 недели");
    await page
      .locator("#details")
      .getByRole("button", { name: "Подробности", exact: true })
      .click();
    await expect(
      page.locator("#details").getByRole("button", { name: "Подробности", exact: true }),
    ).toHaveAttribute("aria-expanded", "true");
    for (const label of ["Конкуренты", "Научные специальности"]) {
      const toggle = page
        .getByRole("button", { name: label, exact: true })
        .and(page.locator("[aria-expanded]"));
      await toggle.click();
      await expect(toggle).toHaveAttribute("aria-expanded", "false");
      await toggle.click();
    }
    await page.getByRole("button", { name: "Обновить план", exact: true }).click();
    await expect
      .poll(() =>
        api.calls.some((call) => call.method === "POST" && call.path.endsWith("/experiment")),
      )
      .toBe(true);
    for (const button of await page.getByRole("button", { name: "Обновить", exact: true }).all())
      await button.click();
    await expect
      .poll(() => api.state.jobs.map((job) => job.kind).toSorted())
      .toEqual(["competitors", "tag"]);
  });
}

test("supported hypotheses keep incomplete claims readable on laptops", async ({
  page,
  api,
}, info) => {
  const hypothesis = api.state.hypotheses[0]!;
  const title =
    "Совместная проверка диктора и ключевого слова для безопасного голосового доступа к банковским сервисам";
  const statement =
    "Если объединить распознавание ключевого слова с проверкой диктора, то доля активаций чужим голосом снизится относительно распознавания только ключевого слова. Проверить на одинаковом наборе записей; устойчивость к синтетической речи оценить отдельно, сохранив сопоставимые условия измерения.";
  hypothesis.title = title;
  hypothesis.statement = statement;
  hypothesis.assessment.schema = {
    complete: false,
    score: 0.5,
    missing: ["target", "validation_plan"],
  };
  api.state.hypotheses = [
    hypothesis,
    ...[2, 3].map((index) =>
      Object.assign(structuredClone(hypothesis), {
        id: `hypothesis-${index}`,
        title: `${title} — вариант ${index}`,
      }),
    ),
  ];

  for (const width of [1024, 1280]) {
    await page.setViewportSize({ width, height: 900 });
    await page.goto("/hypotheses");
    await expect(page.getByRole("button", { name: "Свернуть меню", exact: true })).toBeVisible();
    await expect(
      page.getByRole("button", { name: "Есть подтверждения 3", exact: true }),
    ).toHaveAttribute("aria-pressed", "true");
    const cards = page.getByRole("button").filter({ has: page.getByRole("heading", { level: 3 }) });
    await expect(cards).toHaveCount(3);
    const first = cards.filter({ has: page.getByRole("heading", { name: title, exact: true }) });
    await expect(first.getByText("Гипотеза неполная", { exact: false })).toBeVisible();
    await expect(
      first.getByText("не хватает: целевой показатель, план проверки", { exact: true }),
    ).toBeVisible();
    await expect(first.getByText("Приоритет проверки", { exact: true })).toBeVisible();
    for (const text of [
      first.getByRole("heading", { name: title, exact: true }),
      first.getByText(statement, { exact: true }),
    ]) {
      expect(
        await text.evaluate((element) => element.scrollHeight <= element.clientHeight + 1),
      ).toBe(true);
    }
    const bounds = await cards.evaluateAll((elements) =>
      elements.map((element) => {
        const rect = element.getBoundingClientRect();
        return { top: rect.top, width: rect.width };
      }),
    );
    expect(bounds.every((rect) => rect.width >= 336)).toBe(true);
    expect(Math.abs(bounds[0]!.top - bounds[1]!.top)).toBeLessThan(1);
    expect(bounds[2]!.top).toBeGreaterThan(bounds[0]!.top);
    await inspectSurface(page, info, `supported-hypotheses-${width}`);
  }

  await page.goto("/hypotheses/report");
  await expect(
    page.getByRole("heading", { name: "1. Есть подтверждения", exact: true }),
  ).toBeVisible();
  await expect(page.getByText(/Перед экспериментом проверьте полноту гипотезы/)).toBeVisible();
  await expect(page.getByText("Готовы к эксперименту", { exact: true })).toHaveCount(0);
});

test("all board export formats contain data and both report print buttons work", async ({
  page,
  context,
  api,
}, info) => {
  await context.grantPermissions(["clipboard-read", "clipboard-write"]);
  await page.addInitScript(() => {
    window.print = () => document.documentElement.setAttribute("data-printed", "true");
  });
  await page.goto("/hypotheses");
  await page.getByRole("button", { name: /Все \d/ }).click();
  const labels = [
    "CSV для Excel",
    "JSON для интеграций",
    "CSV для Jira",
    "JSON задач (трекеры)",
    "Документ Word",
  ];
  for (const label of labels) {
    await page.getByRole("button", { name: "Экспорт", exact: true }).click();
    const download = page.waitForEvent("download");
    await page.getByRole("menuitem", { name: label, exact: true }).click();
    const file = await download;
    const path = info.outputPath(file.suggestedFilename());
    await file.saveAs(path);
    const bytes = await readFile(path);
    expect(bytes.byteLength).toBeGreaterThan(50);
    if (!file.suggestedFilename().endsWith(".docx"))
      expect(bytes.toString("utf8")).toContain("Термообработка");
  }
  await page.getByRole("button", { name: "Экспорт", exact: true }).click();
  await page.getByRole("menuitem", { name: "Краткий отчёт", exact: true }).click();
  await inspectSurface(page, info, "digest");
  await page.getByRole("dialog").getByRole("button", { name: "Копировать", exact: true }).click();
  await expect
    .poll(() => page.evaluate(() => navigator.clipboard.readText()))
    .toContain("Термообработка");
  const markdown = page.waitForEvent("download");
  await page.getByRole("dialog").getByRole("button", { name: "Скачать .md", exact: true }).click();
  expect((await markdown).suggestedFilename()).toMatch(/\.md$/);
  await page.keyboard.press("Escape");
  await page.getByRole("button", { name: "Экспорт", exact: true }).click();
  await page.getByRole("menuitem", { name: "Печать и PDF", exact: true }).click();
  await expect(page).toHaveURL(/\/hypotheses\/report$/);
  for (const path of ["/hypotheses/report", "/hypotheses/hypothesis-1/report"]) {
    await page.goto(path);
    const download = page.waitForEvent("download");
    await page.getByRole("button", { name: "Экспорт", exact: true }).click();
    await page.getByRole("menuitem", { name: "Документ Word", exact: true }).click();
    expect((await download).suggestedFilename()).toMatch(/\.docx$/);
    await page.getByRole("button", { name: "Экспорт", exact: true }).click();
    await page.getByRole("menuitem", { name: "Печать и PDF", exact: true }).click();
    await expect(page.locator("html")).toHaveAttribute("data-printed", "true");
    await page.emulateMedia({ media: "print" });
    await expect(page.locator("aside").first()).toBeHidden();
    await page.emulateMedia({ media: "screen" });
  }
  expect(api.unhandled).toEqual([]);
});
