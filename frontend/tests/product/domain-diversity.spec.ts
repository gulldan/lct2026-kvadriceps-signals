import { expect, test } from "./fixture";
import { choose, inspectSurface } from "./checks";

for (const width of [1440, 390]) {
  test(`industry catalog offers all 37 areas without submitting a query at ${width}px`, async ({
    page,
    api,
  }, info) => {
    await page.setViewportSize({ width, height: 1000 });
    await page.goto("/trends?view=analysis");
    await page.getByRole("combobox", { name: "Выбрать отрасль для поиска" }).click();
    await expect(page.getByRole("option")).toHaveCount(37);
    await page.screenshot({
      path: info.outputPath("industry-options.png"),
      animations: "disabled",
    });
    await page.getByRole("option", { name: "Вода и водоочистка", exact: true }).click();
    await expect(page.locator("#trend-query")).toHaveValue("слабые сигналы: Вода и водоочистка");
    expect(api.calls.filter((call) => call.method === "POST")).toEqual([]);
    await inspectSurface(page, info, "industry-picker");
  });

  test(`corpus distribution shows real counts, unknown documents and small areas at ${width}px`, async ({
    page,
    api,
  }, info) => {
    await page.setViewportSize({ width, height: 1000 });
    await page.route("**/api/v1/admin/documents/stats", async (route) => {
      const response = api.dispatch("GET", route.request().url(), {});
      await route.fulfill({
        json: {
          ...(response.json as object),
          total: 100196,
          topic_distribution: {
            classified_documents: 100194,
            unclassified_documents: 2,
            method: "title_rules_v3",
            topics: [
              { id: "medicine", label: "Medicine", count: 50000 },
              { id: "energy", label: "Energy", count: 30000 },
              { id: "agriculture", label: "Agriculture", count: 15000 },
              { id: "environment", label: "Environment", count: 5000 },
              { id: "computing", label: "Computing", count: 107 },
              { id: "materials", label: "Materials", count: 1 },
              { id: "gaming", label: "Gaming", count: 14 },
              { id: "legal", label: "Legal", count: 44 },
              { id: "real_estate", label: "Real estate", count: 28 },
            ],
          },
        },
      });
    });
    await page.goto("/admin");
    const section = page.getByRole("region", { name: "Тематики базы знаний" });
    const figure = section.getByRole("figure");
    await expect(section.getByText(/для 100\s194 из 100\s196 документов/)).toBeVisible();
    await expect(
      section.getByText(/Предварительная оценка по терминам в заголовках/),
    ).toBeVisible();
    await expect(figure.getByRole("button")).toHaveCount(7);
    const unknown = figure.getByRole("button", { name: /^Тематика не определена: 2,/ });
    await expect(unknown).toHaveAttribute("aria-label", "Тематика не определена: 2, < 0,1 %");
    await expect(unknown).toContainText("< 0,1 %");
    await expect(figure.getByRole("button", { name: /^Другие тематики/ })).toContainText("< 0,1 %");
    await unknown.focus();
    await expect(unknown).toHaveAttribute("aria-pressed", "true");
    await unknown.press("Enter");
    await section.locator("summary").filter({ hasText: "Все отрасли и направления · 37" }).click();
    await expect(section.getByRole("cell", { name: /^Химия и материалы/ })).toBeVisible();
    await expect(section.getByRole("row")).toHaveCount(38);
    const emptyIndustry = section.getByRole("row", { name: /Знакомства и поиск партнёра/ });
    await expect(emptyIndustry.getByRole("cell").nth(1)).toHaveText("0");
    await expect(emptyIndustry.getByRole("cell").nth(2)).toHaveText("0 %");
    for (const name of [/Игровые технологии/, /Правовые технологии/, /Недвижимость/]) {
      const industry = section.getByRole("row", { name });
      await expect(industry.getByRole("cell").nth(2)).toHaveText("< 0,1 %");
    }
    await expect(
      section.getByRole("link", { name: "31 отрасль Dealroom и 6 исследовательских направлений" }),
    ).toHaveAttribute(
      "href",
      "https://knowledge.dealroom.co/knowledge/industries-and-sub-industries",
    );
    await section.getByText("От науки к рынку", { exact: true }).click();
    await expect(section.getByRole("link", { name: "AgFunder", exact: true })).toBeVisible();
    await inspectSurface(page, info, "corpus-distribution");
  });
}

test("an older taxonomy does not claim zero counts for industries it never classified", async ({
  page,
  api,
}) => {
  await page.route("**/api/v1/admin/documents/stats", async (route) => {
    const response = api.dispatch("GET", route.request().url(), {});
    await route.fulfill({
      json: {
        ...(response.json as object),
        topic_distribution: {
          classified_documents: 24,
          unclassified_documents: 2,
          method: "title_rules_v1",
          topics: [{ id: "medicine", label: "Medicine", count: 24 }],
        },
      },
    });
  });
  await page.goto("/admin");
  const section = page.getByRole("region", { name: "Тематики базы знаний" });
  await section.locator("summary").filter({ hasText: "Все отрасли и направления · 1" }).click();
  await expect(section.getByRole("row")).toHaveCount(2);
  await expect(section.getByRole("link", { name: /31 отрасль Dealroom/ })).toHaveCount(0);
});

test("different-area order preserves filters and explicit ranking, and non-IT prompts are usable", async ({
  page,
  api,
}) => {
  const base = api.state.hypotheses[0]!;
  const areas = [
    "Генеративное ранжирование",
    "Анализ адаптеров языковых моделей",
    "Алгоритмы поиска",
    "Медицина",
    "Энергетика",
  ];
  api.state.hypotheses = areas.map((area, index) => ({
    ...structuredClone(base),
    id: `diverse-${index}`,
    kpi_id: undefined,
    title: `${area} — подход ${index + 1}`,
    statement: "Применение подхода для выбранной задачи",
    function_area: area,
    status: "approved",
    assessment: {
      ...base.assessment,
      ranking: { ...base.assessment!.ranking!, score: 1 - index / 10 },
    },
  }));
  const ordering: string[] = [];
  page.on("request", (request) => {
    const url = new URL(request.url());
    if (url.pathname === "/api/v1/hypotheses/board")
      ordering.push(url.searchParams.get("order_by") ?? "");
  });
  await page.goto("/hypotheses");
  const titles = page.getByRole("heading", { level: 3 });
  await expect(titles).toHaveText([
    "Генеративное ранжирование — подход 1",
    "Медицина — подход 4",
    "Энергетика — подход 5",
    "Анализ адаптеров языковых моделей — подход 2",
    "Алгоритмы поиска — подход 3",
  ]);
  expect(ordering.at(-1)).toBe("diversity");
  await choose(page, "Сортировка гипотез", "Лучшие для проверки");
  await expect(titles).toHaveText(areas.map((area, index) => `${area} — подход ${index + 1}`));
  expect(ordering.at(-1)).toBe("");
  await choose(page, "Сортировка гипотез", "Разные области");
  await page.getByRole("textbox", { name: "Поиск по гипотезам" }).fill("Медицина");
  await expect(titles).toHaveText(["Медицина — подход 4"]);
  await page.getByRole("button", { name: "Сгенерировать гипотезы", exact: true }).click();
  const dialog = page.getByRole("dialog");
  await dialog.getByRole("button", { name: "Медицина", exact: true }).click();
  await expect(dialog.locator("textarea")).toHaveValue(/ранней диагностике заболеваний/);
  expect(api.calls.filter((call) => call.method === "POST")).toEqual([]);
  await page.keyboard.press("Escape");
  const thesis = api.state.kpis[0]!;
  api.state.kpis = areas.map((area, index) => ({
    ...structuredClone(thesis),
    id: `domain-thesis-${index}`,
    title: `${area} — подход ${index + 1}`,
    description: "Применение подхода для выбранной задачи",
    function_area: area,
    status: "active",
    created_at: new Date(Date.UTC(2026, 9, 17 - index)).toISOString(),
  }));
  await page.goto("/kpi");
  await expect(titles).toHaveText([
    "Генеративное ранжирование — подход 1",
    "Медицина — подход 4",
    "Энергетика — подход 5",
    "Анализ адаптеров языковых моделей — подход 2",
    "Алгоритмы поиска — подход 3",
  ]);
  await choose(page, "Порядок тезисов", "Сначала новые");
  await expect(titles).toHaveText(areas.map((area, index) => `${area} — подход ${index + 1}`));

  const cluster = api.state.clusters[0]!;
  api.state.clusters = areas.map((area, index) => ({
    ...structuredClone(cluster),
    id: `domain-topic-${index}`,
    label: area,
    keywords: [],
    document_count: 30 - index,
    params: {
      ...cluster.params,
      itc: { ...cluster.params!.itc!, techscore: 0.9 - index / 10 },
    },
  }));
  await page.goto("/summary");
  const themes = page.locator('#theme-list details[name="theme-comparison"] > summary');
  await expect(themes).toContainText([areas[0]!, areas[3]!, areas[4]!, areas[1]!, areas[2]!]);
  await page.getByRole("tab", { name: /Сравнение тем/ }).click();
  await expect(themes).toContainText([areas[0]!, areas[3]!, areas[4]!, areas[1]!, areas[2]!]);
  await choose(page, "Сортировка тем", "По индексу темы");
  await expect(themes).toContainText(areas);
});
