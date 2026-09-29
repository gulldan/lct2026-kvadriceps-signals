import { expect, test } from "./fixture";
import { inspectSurface } from "./checks";
import { analysisFixture, paperEvidence } from "./trends-fixture";
import ru from "../../src/shared/i18n/locales/ru/trend.json" with { type: "json" };
import doc from "../../src/shared/i18n/locales/ru/document.json" with { type: "json" };

test.use({ hasTouch: true });

test.beforeEach(async ({ page }) => {
  const job = structuredClone(analysisFixture(0));
  job.request!.start = "2023-01-01";
  job.sources[0]!.source.publication = { value: "2024-03", precision: "month", basis: "UI-M" };
  job.sources[1]!.source.publication = null;
  // Карта не должна ни ждать модель, ни собирать в консоли ошибки повторных запросов.
  await page.route("**/api/v1/trends/analyses/ui-run/candidates/*/description", (route) => {
    const id = route.request().url().split("/").at(-2)!;
    const candidate = job.result!.candidates.find((item) => item.id === id)!;
    return route.fulfill({
      json: {
        job_id: job.id,
        candidate_id: id,
        version: "source-attribution-v1",
        bundle_sha256: "f".repeat(64),
        result_sha256: job.result_sha256,
        evidence_sha256: candidate.brief.evidence_sha256,
        status: "template",
        attempt: 1,
        result: {
          bundle_sha256: "f".repeat(64),
          status: "template",
          fields: {
            problem: candidate.brief.problem,
            advantage: candidate.brief.advantage,
            example: candidate.brief.example,
          },
          model: "fixture",
          prompt_sha256: "b".repeat(64),
          latency_ms: 1,
          reason: "model_unavailable",
        },
      },
    });
  });
  await page.route("**/api/v1/trends/analyses/ui-run", (route) => route.fulfill({ json: job }));
  await page.route("**/api/v1/documents/ui-paper/source*", (route) =>
    route.fulfill({ json: paperEvidence }),
  );
});

for (const width of [1440, 390]) {
  test(`map leads, selection opens sources, and time views preserve context at ${width}px`, async ({
    page,
  }, info) => {
    await page.setViewportSize({ width, height: 1000 });
    const writes: string[] = [];
    const consoleErrors: string[] = [];
    page.on("console", (message) => {
      if (message.type() === "error") consoleErrors.push(message.text());
    });
    page.on("request", (request) => {
      if (request.method() === "POST" && request.url().includes("/trends/"))
        // Описание темы — единственная запись, которую карта вправе вызвать.
        if (!request.url().endsWith("/description")) writes.push(request.url());
    });
    await page.goto("/trends?run=ui-run");
    const graph = page.getByRole("figure", { name: ru.map.network });
    const svg = graph.locator(":scope > svg");
    await expect(graph).toBeVisible();
    await expect(graph.locator('[data-node-kind="topic"]')).toHaveCount(15);
    await expect(graph.locator('[data-node-kind="work"]')).toHaveCount(2);
    await expect(page.getByRole("list", { name: ru.explore.topicList })).toHaveCount(0);
    await expect(page.getByRole("heading", { name: ru.explore.title })).not.toBeVisible();
    await inspectSurface(page, info, `technology-map-${width}`);
    const before = await svg.getAttribute("viewBox");
    await page.getByRole("button", { name: ru.map.zoomIn, exact: true }).click();
    await expect(svg).not.toHaveAttribute("viewBox", before!);
    await page.getByRole("button", { name: ru.map.fit, exact: true }).click();
    await expect(svg).toHaveAttribute("viewBox", before!);
    await page.locator("#topic-ui-topic-1").press("Enter");
    await expect(page).toHaveURL(/mapTopic=ui-topic-1/);
    if (width === 390) {
      await expect(page.getByRole("dialog")).toBeVisible();
      await inspectSurface(page, info, "technology-selection-390");
      await page.getByRole("button", { name: "Закрыть", exact: true }).click();
      await expect(page.locator("#topic-ui-topic-1")).toBeFocused();
    }
    await graph
      .locator('[data-node-kind="work"]')
      .filter({ has: page.locator("title", { hasText: "Scientific source" }) })
      .press("Enter");
    const open = page.getByRole("link", { name: ru.map.openSource, exact: true });
    await expect(open).toHaveAttribute("href", /snapshot=ui-paper-v1/);
    await open.click();
    await expect(page.getByRole("heading", { name: paperEvidence.title })).toBeVisible();
    await page.getByRole("link", { name: doc.source.backRadar, exact: true }).click();
    await expect(graph).toBeVisible();
    await expect(page.locator('[data-node-kind="topic"]:focus')).toHaveCount(1);
    await page.getByRole("tab", { name: ru.map.timelineTab, exact: true }).click();
    const timeline = page.getByRole("figure", { name: ru.map.timeline });
    await expect(timeline).toBeVisible();
    const header = timeline.getByText("2024", { exact: true }).first();
    const rowCell = timeline.locator('svg[aria-label="2024"]').first();
    const axis = await header.boundingBox(),
      cell = await rowCell.boundingBox();
    expect(Math.abs(axis!.x + axis!.width / 2 - cell!.x - cell!.width / 2)).toBeLessThan(1);
    if (width === 390) {
      const topic = timeline.locator("button[id^='topic-']").first();
      const labelBefore = await topic.boundingBox();
      await timeline.getByRole("region").evaluate((element) => {
        element.scrollLeft = element.scrollWidth;
      });
      const labelAfter = await topic.boundingBox();
      expect(Math.abs(labelAfter!.x - labelBefore!.x)).toBeLessThan(13);
      await expect(page.getByText(ru.map.timelinePan, { exact: true })).toBeVisible();
      await timeline.getByRole("region").evaluate((element) => {
        element.scrollLeft = 0;
      });
    }
    await inspectSurface(page, info, `technology-time-${width}`);
    await page.reload();
    await expect(page.getByRole("tab", { name: ru.map.timelineTab })).toHaveAttribute(
      "aria-selected",
      "true",
    );
    expect(writes).toEqual([]);
    expect(consoleErrors).toEqual([]);
  });
}

test("publication filters, empty years and topic filters recover without another analysis", async ({
  page,
}) => {
  await page.goto("/trends?run=ui-run");
  const range = page.getByRole("slider", { name: ru.map.publicationFilter });
  await expect(range).toBeVisible();
  await range.press("Home");
  await expect(page).toHaveURL(/mapYear=2023/);
  await expect(page.getByText(ru.map.noYearWorks, { exact: true })).toBeVisible();
  await expect(range).toBeVisible();
  await range.press("ArrowRight");
  await expect(page.locator('[data-node-kind="work"]')).toHaveCount(1);
  await page.getByRole("button", { name: ru.map.allYears, exact: true }).click();
  await expect(page.locator('[data-node-kind="work"]')).toHaveCount(2);
  const filter = page.getByRole("textbox", { name: ru.map.filter });
  await filter.fill("does not exist in this corpus");
  await expect(page.getByText(ru.map.noFilterMatches)).toBeVisible();
  await filter.fill("");
  await expect(page.locator('[data-node-kind="topic"]')).toHaveCount(15);
  await page.getByRole("button", { name: ru.map.play, exact: true }).click();
  await expect(page).toHaveURL(/mapYear=2023/);
  await expect(page.getByRole("button", { name: ru.map.play, exact: true })).toBeVisible({
    timeout: 6000,
  });
  await expect(page).not.toHaveURL(/mapYear=/);
});

test("graph arrow navigation, pan and fullscreen remain usable", async ({ page }) => {
  await page.goto("/trends?run=ui-run");
  const graph = page.getByRole("figure", { name: ru.map.network });
  const svg = graph.locator(":scope > svg");
  await expect(graph).toBeVisible();
  await graph.locator('[data-node-id][tabindex="0"]').focus();
  const focused = await page.locator("[data-node-id]:focus").getAttribute("data-node-id");
  await page.locator("[data-node-id]:focus").press("ArrowRight");
  await expect(page.locator("[data-node-id]:focus")).not.toHaveAttribute("data-node-id", focused!);
  const box = await svg.boundingBox();
  const before = await svg.getAttribute("viewBox");
  await page.mouse.move(box!.x + 5, box!.y + 5);
  await page.mouse.down();
  await page.mouse.move(box!.x + 55, box!.y + 45);
  await page.mouse.up();
  await expect(svg).not.toHaveAttribute("viewBox", before!);
  for (const control of [ru.map.zoomIn, ru.map.focus]) {
    await page.getByRole("button", { name: control, exact: true }).click();
    const zoomed = (await svg.getAttribute("viewBox"))!.split(" ").slice(2);
    await graph.locator('[data-node-id][tabindex="0"]').focus();
    for (let step = 0; step < 10; step++) {
      await page.locator("[data-node-id]:focus").press("ArrowRight");
      await expect
        .poll(() =>
          svg.evaluate((element) => {
            const circle = element.querySelector("[data-node-id]:focus > circle")!;
            const x = Number(circle.getAttribute("cx")),
              y = Number(circle.getAttribute("cy"));
            const viewport = (element as SVGSVGElement).viewBox.baseVal;
            return (
              x > viewport.x &&
              x < viewport.x + viewport.width &&
              y > viewport.y &&
              y < viewport.y + viewport.height
            );
          }),
        )
        .toBe(true);
      expect((await svg.getAttribute("viewBox"))!.split(" ").slice(2)).toEqual(zoomed);
    }
  }
  await page.getByRole("button", { name: ru.map.fullscreen, exact: true }).click();
  await expect.poll(() => page.evaluate(() => !!document.fullscreenElement)).toBe(true);
  const full = await svg.boundingBox();
  expect(full!.height).toBeGreaterThan(box!.height);
  await page.getByRole("button", { name: ru.map.fullscreen, exact: true }).click();
  await expect.poll(() => page.evaluate(() => !!document.fullscreenElement)).toBe(false);
});

test("pointer selection opens the work at its visible center regardless of overlapping targets", async ({
  page,
}) => {
  await page.setViewportSize({ width: 390, height: 1000 });
  await page.goto("/trends?run=ui-run");
  const graph = page.getByRole("figure", { name: ru.map.network });
  const svg = graph.locator(":scope > svg");
  await expect(graph.locator('[data-node-kind="work"]')).toHaveCount(2);
  const ids = await graph
    .locator('[data-node-kind="work"]')
    .evaluateAll((nodes) => nodes.map((node) => node.getAttribute("data-node-id")!));
  for (const id of ids) {
    await svg.scrollIntoViewIfNeeded();
    const center = await graph.locator(`[data-node-id="${id}"]`).evaluate((node) => {
      const circle = node.querySelector("circle")!;
      const point = new DOMPoint(
        Number(circle.getAttribute("cx")),
        Number(circle.getAttribute("cy")),
      ).matrixTransform(circle.getScreenCTM()!);
      return { x: point.x, y: point.y };
    });
    await page.touchscreen.tap(center.x, center.y);
    await expect.poll(() => new URL(page.url()).searchParams.get("mapWork")).toBe(id.slice(5));
    await expect(page.getByRole("dialog")).toBeVisible();
    await expect(graph.locator(`[data-node-id="${id}"]`)).toHaveAttribute("aria-pressed", "true");
    await page.getByRole("button", { name: "Закрыть", exact: true }).click();
  }
});
