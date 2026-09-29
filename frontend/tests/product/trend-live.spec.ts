import { readFile } from "node:fs/promises";
import { createRequire } from "node:module";
import { expect, test } from "./fixture";
import { choose, inspectSurface } from "./checks";
import live from "../fixtures/trend-live.json" with { type: "json" };
import mapFixture from "../fixtures/trend-live-map.json" with { type: "json" };
import detail from "../fixtures/trend-live-topic.json" with { type: "json" };
import search from "../fixtures/live-search-v1.json" with { type: "json" };
import ru from "../../src/shared/i18n/locales/ru/trend.json" with { type: "json" };

test("radar map has one tab stop and keyboard access to every direction after filtering", async ({
  page,
}) => {
  const overview = structuredClone(live);
  overview.topics = Array.from({ length: 164 }, (_, index) => {
    const topic = structuredClone(live.topics[0]!);
    Object.assign(topic, {
      id: `keyboard-${index}`,
      name: `Направление ${index}`,
      label: `Направление ${index}`,
      stage: index < 82 ? "emerging_signal" : "early_signal",
      comparison: {
        prior_start: "2025-09-08",
        recent_start: "2026-03-11",
        end_exclusive: "2026-09-11",
        days: 184,
        prior: 50,
        current: 100 + index * 10,
        fresh: index + 1,
      },
    });
    return topic;
  }).toReversed();
  await page.route("**/api/v1/trends/live", (route) => route.fulfill({ json: overview }));
  await page.route("**/api/v1/trends/live/topics/keyboard-163", (route) =>
    route.fulfill({ json: { ...detail, topic: overview.topics[0] } }),
  );
  await page.setViewportSize({ width: 1024, height: 768 });
  await page.goto("/trends");
  const map = page.getByRole("figure", { name: ru.study.live.map.aria, exact: true });
  const points = map.locator("[data-live-topic]");
  const entry = map.locator('[data-live-topic][tabindex="0"]');
  const first = map.locator('[data-live-topic="keyboard-0"]');
  const last = map.locator('[data-live-topic="keyboard-163"]');
  const preview = page.getByTestId("trend-preview").filter({ visible: true });
  await expect(points).toHaveCount(164);
  await expect(entry).toHaveCount(1);
  await expect(entry).toHaveAttribute("data-live-topic", "keyboard-0");
  await map.getByRole("button", { name: ru.study.live.map.help, exact: true }).focus();
  await page.keyboard.press("Tab");
  await expect(first).toBeFocused();
  await first.press("ArrowRight");
  await expect(map.locator('[data-live-topic="keyboard-1"]')).toBeFocused();
  await expect(preview).toContainText("Направление 1");
  await page.keyboard.press("End");
  await expect(last).toBeFocused();
  await last.press("ArrowRight");
  await expect(first).toBeFocused();
  await first.press("ArrowLeft");
  await expect(last).toBeFocused();
  await last.press("Home");
  await expect(first).toBeFocused();
  await first.press("Tab");
  await expect(map.getByRole("button", { name: ru.study.live.map.groupsHelp })).toBeFocused();
  await expect(preview).toHaveCount(0);
  await page.keyboard.press("Shift+Tab");
  await expect(first).toBeFocused();
  await first.press("End");
  const filter = page.getByRole("combobox", { name: ru.study.live.filterLabel });
  await filter.click();
  await page.getByRole("option", { name: /^зарождается/ }).click();
  await expect(points).toHaveCount(82);
  await expect(filter).toBeFocused();
  await expect(entry).toHaveCount(1);
  await expect(entry).toHaveAttribute("data-live-topic", "keyboard-0");
  await entry.focus();
  await entry.press("ArrowLeft");
  await expect(map.locator('[data-live-topic="keyboard-81"]')).toBeFocused();
  await choose(page, ru.study.live.filterLabel, `${ru.study.live.all} · 164`);
  await expect(points).toHaveCount(164);
  await expect(entry).toHaveAttribute("data-live-topic", "keyboard-81");
  await entry.focus();
  await entry.press("End");
  await last.press("Enter");
  await expect(page).toHaveURL(/topic=keyboard-163/);
  await expect(preview).toHaveCount(0);
  await expect(page.getByRole("button", { name: ru.study.live.back, exact: true })).toBeVisible();
});

test("current-month windows expose patents and web with dated evidence", async ({ page }, info) => {
  const overview = structuredClone(live) as typeof live & { topics: Record<string, unknown>[] };
  const periods = Array.from({ length: 14 }, (_, index) => {
    const start = new Date(Date.UTC(2025, 7 + index, 1));
    const end = index === 13 ? new Date("2026-09-11") : new Date(Date.UTC(2025, 8 + index, 1));
    return {
      period: start.toISOString().slice(0, 7),
      start: start.toISOString().slice(0, 10),
      end_exclusive: end.toISOString().slice(0, 10),
      complete: index < 13,
      population: 1000,
    };
  });
  overview.periods = periods;
  for (const [index, topic] of overview.topics.entries()) {
    topic.series = periods.map((p) => ({ ...topic.series[0]!, period: p.period, count: 10 }));
    Object.assign(topic, {
      comparison: {
        prior_start: "2025-09-08",
        recent_start: "2026-03-11",
        end_exclusive: "2026-09-11",
        days: 184,
        prior: 10,
        current: 20 + index * 20,
        fresh: index + 1,
      },
    });
  }
  const topic = overview.topics.find((row) => row.id === "mcp")!;
  topic.growth.delta_pp = -0.0004;
  Object.assign(topic, {
    semantic: {
      method: "title-centroid-cosine-v1",
      model: "local-model",
      revision: "revision-1",
      compared_topics: 34,
      sample_paper_ids: ["a", "b"],
      nearest_id: "solid-state-batteries",
      nearest_name: "Solid-state batteries",
      similarity: 0.741,
      distance: 0.259,
      query_similarity: null,
    },
  });
  for (const [key, total] of [
    ["patents", 3],
    ["web", 2],
  ] as const) {
    Object.assign(topic, {
      [key]: {
        source: key === "patents" ? "USPTO" : "MIT News",
        observed_at: "2026-09-10T12:00:00Z",
        counts: periods.map((_, index) => (index === 13 ? total : null)),
        total,
        partial: true,
        limits: [],
        records: [
          {
            id: key,
            title: key === "patents" ? "Dated patent evidence" : "Dated news evidence",
            date: "2026-09-08",
            url: "https://example.org/" + key,
            source: key === "patents" ? "USPTO" : "MIT News",
          },
        ],
      },
    });
  }
  await page.route("**/api/v1/trends/live", (route) => route.fulfill({ json: overview }));
  await page.route("**/api/v1/trends/live/topics/mcp", (route) =>
    route.fulfill({ json: { ...detail, periods, topic } }),
  );
  await page.setViewportSize({ width: 390, height: 1000 });
  await page.goto("/trends");
  await expect(page.getByTestId("trend-axis-x")).toContainText("сент");
  const point = page.getByRole("button", { name: "Model Context Protocol", exact: true });
  await point.scrollIntoViewIfNeeded();
  await point.focus();
  await page.keyboard.press("Tab");
  await page.keyboard.press("Shift+Tab");
  await expect(point).toBeFocused();
  // Кадр доставляет отложенную прокрутку: она не должна скрывать подсказку точки под фокусом.
  await page.evaluate(() => new Promise<void>((resolve) => requestAnimationFrame(() => resolve())));
  const preview = page.getByTestId("trend-preview").filter({ visible: true });
  await expect(preview).toContainText("Патенты");
  await expect(preview).toContainText("≥3");
  await expect(preview).toContainText("≥2");
  await expect(preview.getByTestId("trend-preview-change")).toContainText("сент");
  await point.press("Escape");
  await expect(preview).toHaveCount(0);
  await expect(point).toBeFocused();
  await point.press("Tab");
  await page.keyboard.press("Shift+Tab");
  await expect(point).toBeFocused();
  await expect(preview).toContainText("Патенты");
  await point.press("Enter");
  await expect(page.getByText("сентябрь 2026 г. · месяц ещё идёт", { exact: true })).toBeVisible();
  await expect(page.getByText("-0,0004 п.п.", { exact: true })).toBeVisible();
  await page.getByRole("tab", { name: new RegExp(ru.study.live.types.patents) }).click();
  await expect(page.getByRole("link", { name: "Dated patent evidence" })).toHaveAttribute(
    "href",
    "https://example.org/patents",
  );
  const chart = page.getByRole("figure", { name: ru.study.live.typesChart, exact: true });
  const latest = chart.getByRole("button", { name: "2026-09", exact: true });
  await latest.scrollIntoViewIfNeeded();
  await expect(latest).toBeInViewport();
  expect((await latest.boundingBox())!.height).toBeLessThanOrEqual(36);
  await expect(chart.locator("div[aria-hidden]").getByText("0", { exact: true })).toBeInViewport();
  await expect(chart.locator("svg > g > path").nth(3)).toHaveAttribute(
    "stroke",
    "var(--color-chart-4)",
  );
  await expect(chart.locator("svg > g > path").nth(4)).toHaveAttribute(
    "stroke",
    "var(--color-chart-5)",
  );
  await inspectSurface(page, info, "monthly-sources-mobile");
  const earlier = chart.getByRole("button", { name: "2025-08", exact: true });
  await earlier.scrollIntoViewIfNeeded();
  await earlier.click();
  await expect(earlier).toBeInViewport();
  await expect(earlier).toHaveAttribute("aria-pressed", "true");
});

for (const width of [320, 390, 768, 1440]) {
  test(`live preview, keyboard and detail at ${width}px`, async ({ page }, info) => {
    await page.setViewportSize({ width, height: 1000 });
    await page.goto("/trends");
    await expect(page.getByTestId("trend-axis-x")).toContainText("Число статей");
    await expect(page.getByTestId("trend-axis-y")).toHaveText("Прирост статей за полгода");
    await expect(page.getByTestId("trend-axis-y")).toHaveCSS("font-weight", "700");
    const point = page.getByRole("button", { name: "Model Context Protocol", exact: true });
    await point.scrollIntoViewIfNeeded();
    await point.hover();
    const preview = page.getByTestId("trend-preview").filter({ visible: true });
    await expect(preview).toBeVisible();
    await expect(preview.getByRole("figure")).toHaveCount(3);
    await expect(preview).toContainText(ru.study.live.preview.papers);
    await expect(preview).toContainText(ru.study.live.preview.repos);
    await expect(preview).not.toContainText(/NaN|%|п\.п\.|полные кварталы|Доля статей|Q[1-4]/);
    const mcp = live.topics.find((topic) => topic.id === "mcp")!;
    for (const [label, value] of [
      [ru.study.live.preview.papers, mcp.papers_count],
      [ru.study.live.preview.repos, mcp.repos.reduce((a, b) => a + b, 0)],
      [ru.study.live.preview.hn, mcp.discussions.reduce((a, b) => a + b, 0)],
    ] as const) {
      await expect(
        preview.locator("dl > div").filter({ hasText: label }).locator("dd").first(),
      ).toHaveText(new Intl.NumberFormat("ru").format(value));
    }
    await expect(preview.locator("time").first()).toHaveAttribute(
      "datetime",
      live.periods[0]!.start,
    );
    await expect(preview.locator("time").last()).toHaveText(
      new Intl.DateTimeFormat("ru", { month: "short", year: "numeric", timeZone: "UTC" }).format(
        new Date(live.observed_at),
      ),
    );
    await expect(preview).toContainText("Сейчас:");
    await expect(preview).toContainText(ru.study.live.preview.newGroups);
    await expect(preview.getByTestId("trend-preview-change")).toContainText("→");
    for (const chart of await preview.getByRole("figure").all()) {
      const plot = chart.locator("svg");
      const size = await plot.boundingBox();
      expect(size!.width).toBeGreaterThan(100);
      expect(size!.height).toBeGreaterThanOrEqual(36);
      const heights = await plot
        .locator("rect")
        .evaluateAll((bars) => bars.map((bar) => Number(bar.getAttribute("height"))));
      expect(Math.max(...heights)).toBe(38);
    }
    const bounds = await preview.boundingBox();
    expect(bounds!.height).toBeLessThan(410);
    expect(bounds!.width).toBeLessThanOrEqual(320);
    expect(bounds!.x).toBeGreaterThanOrEqual(0);
    expect(bounds!.x + bounds!.width).toBeLessThanOrEqual(width);
    const pointBounds = await point.boundingBox();
    expect(
      bounds!.x < pointBounds!.x + pointBounds!.width &&
        bounds!.x + bounds!.width > pointBounds!.x &&
        bounds!.y < pointBounds!.y + pointBounds!.height &&
        bounds!.y + bounds!.height > pointBounds!.y,
    ).toBe(false);
    await inspectSurface(page, info, `live-preview-${width}`);
    await page.mouse.move(bounds!.x + bounds!.width / 2, bounds!.y + bounds!.height / 2);
    await expect(preview).toBeVisible();
    await page.keyboard.press("Escape");
    await expect(preview).toHaveCount(0);
    await page.mouse.move(0, 0);
    await point.focus();
    await expect(preview).toBeVisible();
    await point.press("Enter");
    await expect(page).toHaveURL(/topic=mcp/);
    await expect(preview).toHaveCount(0);
    const chart = page.getByRole("figure", { name: ru.study.live.typesChart, exact: true });
    await expect(chart).toBeVisible();
    await expect(chart.locator("svg > g > path")).toHaveCount(5);
    await expect(chart).not.toContainText("%");
    await expect(
      page.getByText(ru.study.live.partialQuarter.replace("{{period}}", "2026-Q3"), {
        exact: true,
      }),
    ).toBeVisible();
    await chart.getByRole("button", { name: "2025-Q3", exact: true }).click();
    await expect(
      chart.getByRole("listitem").filter({ hasText: ru.study.live.types.papers }),
    ).toHaveText(
      `${ru.study.live.types.papers} ${detail.topic.series
        .slice(0, 4)
        .reduce((sum, row) => sum + row.count, 0)}`,
    );
    await page.getByRole("tab", { name: new RegExp(ru.study.live.types.papers) }).click();
    await inspectSurface(page, info, `live-detail-${width}`);
    await page.getByRole("button", { name: ru.study.live.back, exact: true }).click();
    await expect(point).toBeVisible();
    await page
      .locator("summary")
      .filter({ hasText: /Таблица направлений/ })
      .click();
    await expect(page.getByRole("table", { name: ru.study.live.table.aria })).toBeVisible();
  });
}

test("missing channels stay unknown, retain colours and use the shared calendar", async ({
  page,
}) => {
  const missing = structuredClone(detail);
  missing.topic.repos = [];
  missing.topic.discussions = [0, 4];
  // Дыра в середине канала: накопление её пропускает, не обнуляясь, а дальше подписано «≥».
  Object.assign(missing.topic, {
    web: {
      source: "MIT News",
      observed_at: "2026-09-10T12:00:00Z",
      counts: [1, null, 2],
      total: 3,
      partial: false,
      limits: [],
      records: [],
    },
  });
  await page.route("**/api/v1/trends/live/topics/mcp", (route) => route.fulfill({ json: missing }));
  await page.goto("/trends?topic=mcp");
  const chart = page.getByRole("figure", { name: ru.study.live.typesChart, exact: true });
  await expect(chart).toBeVisible();
  await expect(chart).not.toContainText("NaN");
  const paths = chart.locator("svg > g > path");
  await expect(paths.nth(1)).toHaveAttribute("d", /^\s*$/);
  await expect(paths.nth(2)).toHaveAttribute("stroke", "var(--color-chart-3)");
  const legend = (name: string) => chart.getByRole("listitem").filter({ hasText: name });
  await chart.getByRole("button", { name: "2025-Q1", exact: true }).click();
  await expect(legend(ru.study.live.types.repos)).toHaveText(`${ru.study.live.types.repos} —`);
  await expect(legend(ru.study.live.types.hn)).toHaveText(`${ru.study.live.types.hn} 4`);
  await expect(legend(ru.study.live.types.web)).toHaveText(`${ru.study.live.types.web} —`);
  await chart.getByRole("button", { name: "2025-Q2", exact: true }).click();
  await expect(legend(ru.study.live.types.web)).toHaveText(`${ru.study.live.types.web} ≥3`);
  await chart.getByRole("button", { name: "2026-Q2", exact: true }).click();
  await expect(legend(ru.study.live.types.hn)).toHaveText(`${ru.study.live.types.hn} —`);
});

test("growth colours and sizes are independent of filters, axes fit the visible circles", async ({
  page,
}) => {
  const overview = structuredClone(live);
  for (const [index, current] of [125, 250, 500].entries()) {
    const topic = overview.topics[index]!;
    const counts = [50, 50, current / 2, current / 2];
    for (let period = 0; period < 4; period++) topic.series[period + 3]!.count = counts[period]!;
    Object.assign(topic, {
      stage: index < 2 ? "emerging_signal" : "early_signal",
      uniqueness: index / 2,
    });
  }
  await page.route("**/api/v1/trends/live", (route) => route.fulfill({ json: overview }));
  await page.goto("/trends");
  const first = page.locator('[data-live-topic="mcp"]');
  const second = page.locator('[data-live-topic="speculative-decoding"]');
  await expect(first).toBeVisible();
  const colors = await page
    .locator("[data-growth]")
    .evaluateAll((elements) =>
      elements.map((element) => getComputedStyle(element).backgroundColor),
    );
  expect(new Set(colors).size).toBe(3);
  const position = await first.getAttribute("style");
  const size = await first.locator("[data-growth]").getAttribute("style");
  const fill = await first
    .locator("[data-growth]")
    .evaluate((element) => getComputedStyle(element).backgroundColor);
  // Оси растягиваются под выбранную стадию, поэтому первый кружок уходит с прежнего места.
  await expect(page.locator("[data-live-topic]")).toHaveCount(3);
  const emerging = page.getByRole("combobox", { name: ru.study.live.filterLabel });
  await emerging.click();
  await page.getByRole("option", { name: /^зарождается/ }).click();
  await expect(emerging).toContainText("2");
  await expect(page.locator("[data-live-topic]")).toHaveCount(2);
  await expect(first).not.toHaveAttribute("style", position!);
  await expect(first.locator("[data-growth]")).toHaveAttribute("style", size!);
  await expect(first.locator("[data-growth]")).toHaveCSS("background-color", fill);
  await expect(second.locator("[data-growth]")).not.toHaveCSS("background-color", fill);
});

test("preview distinguishes a missing source from a measured zero", async ({ page }) => {
  const overview = structuredClone(live);
  const mcp = overview.topics.find((topic) => topic.id === "mcp")!;
  Object.assign(mcp, { repos: [], discussions: mcp.series.map(() => 0) });
  await page.route("**/api/v1/trends/live", (route) => route.fulfill({ json: overview }));
  await page.goto("/trends");
  await page.getByRole("button", { name: "Model Context Protocol", exact: true }).hover();
  const preview = page.getByTestId("trend-preview").filter({ visible: true });
  for (const [label, total] of [
    [ru.study.live.preview.repos, "—"],
    [ru.study.live.preview.hn, "0"],
  ]) {
    await expect(
      preview.locator("dl > div").filter({ hasText: label }).locator("dd").first(),
    ).toHaveText(total!);
  }
  const missing = preview.getByRole("figure", { name: /^GitHub/ });
  await expect(missing.locator("svg text")).toHaveCount(overview.periods.length);
  await expect(missing).not.toContainText("0");
  const zero = preview.getByRole("figure", { name: /^Hacker News/ });
  await expect(zero.locator("svg text")).toHaveCount(0);
  await expect(zero).not.toContainText("—");
});

for (const language of ["en", "zh"]) {
  test(`preview fits at 390px in ${language}`, async ({ page }, info) => {
    await page.setViewportSize({ width: 390, height: 844 });
    await page.addInitScript((lang) => localStorage.setItem("rag.lang", lang), language);
    await page.goto("/trends");
    await page.locator('[data-live-topic="mcp"]').hover();
    const preview = page.getByTestId("trend-preview").filter({ visible: true });
    await expect(preview).toBeVisible();
    await expect(preview.getByRole("figure")).toHaveCount(3);
    await expect(preview).not.toContainText(/study\.|NaN|Q[1-4]|%/);
    const bounds = await preview.boundingBox();
    expect(bounds!.height).toBeLessThan(410);
    expect(bounds!.x).toBeGreaterThanOrEqual(0);
    expect(bounds!.x + bounds!.width).toBeLessThanOrEqual(390);
    await inspectSurface(page, info, `preview-${language}-390`);
  });
}

for (const width of [390, 1440]) {
  test(`full map explains measurements and uses distinct growth colours at ${width}px`, async ({
    page,
  }, info) => {
    await page.setViewportSize({ width, height: 1000 });
    await page.route("**/api/v1/trends/live", (route) => route.fulfill({ json: mapFixture }));
    await page.goto("/trends");
    await expect(page.locator("[data-live-topic]")).toHaveCount(22);
    await choose(page, ru.study.live.filterLabel, `${ru.study.live.all} · 27`);
    await expect(page.locator("[data-live-topic]")).toHaveCount(27);
    const colors = await page
      .locator("[data-growth]")
      .evaluateAll((elements) =>
        elements.map((element) => getComputedStyle(element).backgroundColor),
      );
    expect(new Set(colors).size).toBe(5);
    const map = page.getByRole("figure", { name: ru.study.live.map.aria, exact: true });
    await expect(map).toContainText("arXiv");
    await expect(map).not.toContainText(/п\.п\.|Объём × рост|Автопоиск/);
    await expect(page.getByTestId("trend-growth-legend")).toContainText(
      ru.study.live.map.bands.surging,
    );
    await inspectSurface(page, info, `full-map-${width}`);
    await page.getByRole("button", { name: "Model Context Protocol", exact: true }).hover();
    await expect(page.getByTestId("trend-preview").filter({ visible: true })).toContainText(
      "На 53 статьи больше",
    );
    await inspectSurface(page, info, `full-map-preview-${width}`);
  });
}

test("concept comparison shows its scope, nearest topic and missing model state", async ({
  page,
}) => {
  const measured = {
    ...detail,
    topic: {
      ...detail.topic,
      limits: [
        {
          text: "semantic discovery: deterministic population sample; counts are sample observations",
        },
        {
          code: "scholar_capped",
          values: { taken: 1200, found: 33046 },
          text: "scholar: 1200 of 33046 works taken",
        },
      ],
      semantic: {
        method: "title-centroid-cosine-v1",
        model: "local-model",
        revision: "revision-1",
        compared_topics: 2,
        sample_paper_ids: ["a", "b"],
        nearest_id: "agent-memory",
        nearest_name: "Agent memory",
        similarity: 0.72,
        distance: 0.28,
        query_similarity: 0.63,
      },
    },
  };
  await page.route("**/api/v1/trends/live/topics/mcp", (route) =>
    route.fulfill({ json: measured }),
  );
  await page.goto("/trends?topic=mcp");
  await expect(page.getByText("28 из 100", { exact: true })).toBeVisible();
  await page
    .getByText(ru.study.live.coverage.summary.replace("{{total}}", "2"), { exact: true })
    .filter({ visible: true })
    .click();
  await expect(
    page.getByText(ru.study.live.coverage.partial, { exact: true }).filter({ visible: true }),
  ).toBeVisible();
  await expect(page.getByText(ru.study.live.coverage.semantic_unavailable)).toHaveCount(0);
  await expect(
    page
      .getByText("В анализ взято 1 200 научных работ из 33 046 найденных.")
      .filter({ visible: true }),
  ).toBeVisible();
  const profile = page.getByRole("region", { name: ru.study.profile.measures, exact: true });
  await expect(profile).toContainText("Репозиториев: 1");
  await expect(profile).toContainText("Кандидаты");
  await expect(profile).not.toContainText("Есть реализации");
  await page.getByRole("tab", { name: new RegExp(ru.study.live.types.repos) }).click();
  await expect(page.getByText("репозиторий", { exact: true })).toBeVisible();
  await expect(page.getByText("Ближе всего: Память агентов", { exact: true })).toBeVisible();
  await page.getByRole("button", { name: "Отличие идеи", exact: true }).hover();
  await expect(page.getByRole("tooltip")).toContainText("2 направлениями");
  await expect(page.getByRole("tooltip")).toContainText("не доказательство мировой новизны");
  await expect(page.getByRole("tooltip")).toContainText("revision-1");
  await page.keyboard.press("Escape");
  await page.unroute("**/api/v1/trends/live/topics/mcp");
  await page.goto("/trends?topic=mcp");
  await expect(
    page.getByText("Не измерено", { exact: true }).filter({ visible: true }).first(),
  ).toBeVisible();
  await expect(page.getByText("28 из 100", { exact: true })).toHaveCount(0);
});

test("a saved query exports all 15 complete cards to Word and opens with its technologies first", async ({
  page,
}, info) => {
  const candidates = search.candidates.map((candidate, index) => ({
    ...candidate,
    label: `Технология ${index + 1}: ${candidate.label}`,
    model_score: {
      value: (100 - index) / 100,
      model: "word-export-fixture",
      version: "1",
      basis: "Порядок тестовой выдачи",
      label: "weak_signal",
      threshold: 0.75,
    },
  }));
  expect(candidates).toHaveLength(15);
  expect(
    new Set(candidates.flatMap(({ card }) => [card.summary, card.advantage, card.example])).size,
  ).toBe(45);
  const answer = {
    ...detail,
    candidates,
    rejected: search.rejected,
    counters: {
      ...search.counters,
      model_scored: candidates.length,
      model_above_threshold: candidates.length,
    },
    search_contract: "search-v11",
  };
  await page.route("**/api/v1/trends/live/search*", (route) =>
    route.fulfill({ json: { query: "цифровой рубль", status: "ready", detail: answer } }),
  );
  await page.goto(
    "/trends?q=%D1%86%D0%B8%D1%84%D1%80%D0%BE%D0%B2%D0%BE%D0%B9+%D1%80%D1%83%D0%B1%D0%BB%D1%8C",
  );
  const download = page.waitForEvent("download");
  await page.getByRole("button", { name: "Экспорт", exact: true }).click();
  await page.getByRole("menuitem", { name: "Документ Word", exact: true }).click();
  const file = await download;
  expect(file.suggestedFilename()).toMatch(/\.docx$/);
  const path = info.outputPath(file.suggestedFilename());
  await file.saveAs(path);
  // Архив читает зависимость самого docx, независимо от её размещения в node_modules.
  const zip = createRequire(import.meta.resolve("docx"))("jszip") as {
    loadAsync(data: Uint8Array): Promise<{
      file(path: string): { async(type: "string"): Promise<string> } | null;
    }>;
  };
  const document = (await zip.loadAsync(await readFile(path))).file("word/document.xml");
  expect(document).not.toBeNull();
  const word = await page.evaluate(
    (xml) => {
      const doc = new DOMParser().parseFromString(xml, "application/xml");
      const ns = "http://schemas.openxmlformats.org/wordprocessingml/2006/main";
      const paragraphs = [...doc.getElementsByTagNameNS(ns, "p")].map((paragraph) => ({
        text: [...paragraph.getElementsByTagNameNS(ns, "t")]
          .map((run) => run.textContent ?? "")
          .join(""),
        style: paragraph.getElementsByTagNameNS(ns, "pStyle")[0]?.getAttributeNS(ns, "val"),
      }));
      return {
        errors: doc.getElementsByTagName("parsererror").length,
        paragraphs,
      };
    },
    await document!.async("string"),
  );
  expect(word.errors).toBe(0);
  const labels = candidates.map((candidate) => candidate.label);
  const headings = word.paragraphs.flatMap(({ text, style }, index) =>
    style === "Heading2" && labels.includes(text) ? [{ text, index }] : [],
  );
  expect(headings.map(({ text }) => text)).toEqual(labels);
  for (const [index, candidate] of candidates.entries()) {
    const section = word.paragraphs
      .slice(headings[index]!.index + 1, headings[index + 1]?.index)
      .map(({ text }) => text);
    for (const field of ["summary", "advantage", "example"] as const) {
      const content = candidate.card[field];
      expect(content).not.toBe("");
      expect(section, `${candidate.label}: ${field}`).toContain(content);
    }
  }
  let asked: string | null = null;
  await page.route("**/api/v1/trends/live/executive*", (route) => {
    asked = new URL(route.request().url()).searchParams.get("q");
    return route.fulfill({
      json: {
        run_id: "run",
        query: "цифровой рубль",
        observed_at: detail.observed_at,
        sources_processed: 120,
        candidates: 40,
        selected: search.candidates.length,
        items: [
          { id: "x", name: "Токенизация", horizon: "1-3", step: "pilot", reason: "Есть кейс" },
        ],
        model: "test-model",
        written_at: detail.observed_at,
      },
    });
  });
  const brief = page.waitForEvent("download");
  await page.getByRole("button", { name: "Экспорт", exact: true }).click();
  await page.getByRole("menuitem", { name: ru.executive.menuWord, exact: true }).click();
  expect((await brief).suggestedFilename()).toMatch(/^Справка.*\.docx$/);
  expect(asked).toBe("цифровой рубль");
  await expect(page.getByRole("figure", { name: ru.study.live.typesChart })).toBeVisible();
  const found = page.getByRole("tab", { name: new RegExp(ru.study.live.types.found) });
  await expect(found).toHaveAttribute("aria-selected", "true");
  await expect(
    page.getByRole("button", { name: /Искусственный интеллект в промышленности/ }).first(),
  ).toBeVisible();
  await page.getByRole("tab", { name: new RegExp(ru.study.live.types.papers) }).click();
  await expect(found).toHaveAttribute("aria-selected", "false");
});

test("radar directions filter by field of science and the detail names the field", async ({
  page,
}) => {
  await page.goto("/trends");
  await choose(page, ru.study.live.filterLabel, `${ru.study.live.all} · 3`);
  await page.getByText("Таблица направлений").click();
  const rows = page
    .getByRole("table", { name: ru.study.live.table.aria })
    .locator("tbody tr")
    .filter({ has: page.getByRole("button") });
  await expect(rows).toHaveCount(3);
  const branch = page.getByRole("combobox", { name: ru.study.live.branchLabel, exact: true });
  await branch.click();
  await page.getByRole("option", { name: new RegExp(ru.study.live.branches.materials) }).click();
  await expect(rows).toHaveCount(1);
  await expect(rows.first()).toContainText(ru.study.live.branches.materials);
  await branch.click();
  await page.getByRole("option", { name: new RegExp(`^${ru.study.live.all}`) }).click();
  await expect(rows).toHaveCount(3);
  await expect(
    page.getByRole("columnheader", { name: ru.study.live.table.citations }),
  ).toBeVisible();
  await expect(rows.first()).toContainText("1,8×");
  await page
    .getByRole("button", { name: /Model Context Protocol/ })
    .first()
    .click();
  // Подпись строки повторяется у луча круговой сводки, поэтому ищем в самом списке профиля.
  const profileRows = page
    .getByRole("region", { name: ru.study.profile.measures, exact: true })
    .getByRole("list");
  await expect(
    profileRows.getByText(ru.study.live.profile.impact.label, { exact: true }),
  ).toBeVisible();
  await expect(
    profileRows.getByText(ru.study.live.profile.impact.high, { exact: true }),
  ).toBeVisible();
  await page.getByRole("button", { name: ru.study.profile.short.spread, exact: true }).click();
  await expect(
    profileRows
      .locator("li")
      .filter({ has: page.getByText(ru.study.profile.spread.label, { exact: true }) }),
  ).toBeFocused();
});

test("a direction from the map shows its technologies exactly like a query answer", async ({
  page,
}) => {
  await page.goto("/trends?topic=mcp");
  await expect(page.getByRole("tab")).toHaveCount(8);
  await page.getByRole("button", { name: ru.sources, exact: true }).hover();
  await expect(page.getByRole("tooltip")).toContainText(ru.study.live.tabsLegend);
  const found = page.getByRole("tab", { name: new RegExp(ru.study.live.types.found) });
  await expect(found).toHaveAttribute("aria-selected", "true");
  await expect(page.getByText(ru.study.live.technologies.pending)).toBeVisible();
  await page.route("**/api/v1/trends/live/topics/mcp", (route) =>
    route.fulfill({
      json: {
        ...detail,
        candidates: search.candidates,
        rejected: search.rejected,
        counters: search.counters,
        search_contract: "search-v11",
      },
    }),
  );
  await page.reload();
  await expect(found).toContainText(String(search.candidates.length));
  await page
    .getByRole("button", { name: /Искусственный интеллект в промышленности/ })
    .first()
    .click();
  await expect(page).toHaveURL(/topic=mcp&signal=/);
  await expect(
    page.getByText(/^Технология \d+ из 15 · направление: Model Context Protocol$/),
  ).toBeVisible();
  await expect(
    page.getByRole("heading", { name: /Искусственный интеллект в промышленности/ }).first(),
  ).toBeVisible();
});

test("a watched direction brings what appeared into the bell and opening it reads the digest", async ({
  page,
  api,
}) => {
  await page.goto("/trends?topic=mcp");
  await page.getByRole("button", { name: "Следить", exact: true }).click();
  await expect(page.getByRole("button", { name: "Вы следите", exact: true })).toBeVisible();
  expect(api.state.watches).toMatchObject([{ kind: "topic", target: "mcp" }]);
  Object.assign(api.state.watches[0]!, {
    digest: { technologies: ["Реестр инструментов"], sources: 3 },
    digest_at: "2026-09-09T03:00:00Z",
  });
  await page.goto("/trends");
  await page.getByRole("button", { name: /Новое по подпискам: 1/ }).click();
  const digest = page.getByRole("link", { name: /Новая технология: Реестр инструментов/ });
  await expect(digest).toContainText("+3 источника");
  await digest.click();
  await expect(page).toHaveURL(/topic=mcp/);
  await expect
    .poll(() => api.calls.some((call) => call.path === "/trends/live/watches/ack"))
    .toBe(true);
  expect(api.state.watches[0]!.digest).toBeUndefined();
});

test("the model check opens from the radar with its metrics and the reference set", async ({
  page,
}, info) => {
  await page.goto("/trends");
  await page.getByRole("link", { name: ru.modelCheck.link }).click();
  await expect(page).toHaveURL(/\/trends\/model$/);
  await expect(page.getByRole("heading", { level: 1 })).toHaveText(ru.modelCheck.title);
  await expect(page.getByText("F1 модели", { exact: true }).locator("..")).toContainText(
    "81,4 ± 1,2",
  );
  await expect(
    page.getByText(/Выше коридора: точность 92,4.*Ниже коридора: полнота 72,7/),
  ).toBeVisible();
  await expect(page.getByText(/Ошибочно названы сигналом: Фотоплетизмография/)).toBeVisible();
  await expect(
    page
      .getByRole("table", { name: ru.modelCheck.sourceOutTitle })
      .getByRole("row", { name: /Нынешний хайп/ }),
  ).toContainText("57");
  await expect(
    page.getByText(/Из 3 сигналов эталона модель признаёт слабыми сигналами 2/),
  ).toBeVisible();
  const items = page.getByRole("table", { name: ru.modelCheck.itemsTitle });
  await expect(items.getByRole("row")).toHaveCount(4);
  await expect(items.getByRole("row", { name: /теплового управления/ })).toContainText(
    "Не признан",
  );
  await expect(items).not.toContainText("Фотоплетизмография");
  for (const width of [390, 1440]) {
    await page.setViewportSize({ width, height: 900 });
    await inspectSurface(page, info, `model-check-${width}`);
  }
  await page.getByRole("link", { name: "К радару технологий" }).click();
  await expect(page).toHaveURL(/\/trends$/);
});
