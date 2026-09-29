import { expect, test } from "./fixture";
import { inspectSurface } from "./checks";
import { analysisFixture, paperEvidence, repoEvidence } from "./trends-fixture";
import ru from "../../src/shared/i18n/locales/ru/trend.json" with { type: "json" };
import en from "../../src/shared/i18n/locales/en/trend.json" with { type: "json" };
import zh from "../../src/shared/i18n/locales/zh/trend.json" with { type: "json" };
import ruDoc from "../../src/shared/i18n/locales/ru/document.json" with { type: "json" };
import enDoc from "../../src/shared/i18n/locales/en/document.json" with { type: "json" };
import zhDoc from "../../src/shared/i18n/locales/zh/document.json" with { type: "json" };

test.beforeEach(async ({ page }) => {
  await page.route("**/api/v1/trends/analyses/*/candidates/*/description", (route) =>
    route.fulfill({ status: 503, json: { error: "model unavailable in this UI-M scenario" } }),
  );
});

for (const status of [429, 507]) {
  test(`analysis admission ${status} preserves the request and previous result`, async ({
    page,
  }) => {
    const requests: unknown[] = [];
    await page.setViewportSize({ width: 390, height: 950 });
    await page.route("**/api/v1/trends/analyses", async (route) => {
      requests.push(route.request().postDataJSON());
      await route.fulfill({ status, json: { error: "bounded pilot admission" } });
    });
    await page.goto(
      "/trends?request=predeclared-request&q=RAG&start=2023-01-01&cutoff=2025-01-01T00%3A00%3A00Z&previous=ui-run",
    );
    await expect(page.getByRole("alert")).toContainText(
      status === 429 ? ru.queueFull : ru.historyFull,
    );
    await expect(page.getByRole("link", { name: ru.previous })).toHaveAttribute(
      "href",
      "/trends?run=ui-run",
    );
    if (status === 429) {
      await page.getByRole("button", { name: ru.retry }).click();
      await expect.poll(() => requests.length).toBe(2);
      expect(requests[1]).toEqual(requests[0]);
    } else {
      await expect(page.getByRole("button", { name: ru.retry })).toHaveCount(0);
      expect(requests).toHaveLength(1);
    }
    await expect(page).toHaveURL(/request=predeclared-request/);
  });
}

for (const [width, language, copy, doc] of [
  [390, "ru", ru, ruDoc],
  [768, "en", en, enDoc],
  [1024, "zh", zh, zhDoc],
  [1440, "ru", ru, ruDoc],
] as const) {
  test(`radar 15 topics, exact source, method and back at ${width}px ${language}`, async ({
    page,
  }, info) => {
    test.setTimeout(90_000);
    const job = analysisFixture();
    await page.addInitScript((lang) => localStorage.setItem("rag.lang", lang), language);
    await page.emulateMedia({ reducedMotion: "reduce" });
    await page.route("**/api/v1/trends/analyses/ui-run", (route) => route.fulfill({ json: job }));
    await page.route("**/api/v1/documents/ui-paper/source*", (route) =>
      route.fulfill({ json: paperEvidence }),
    );
    await page.route("**/api/v1/documents/ui-repo/source*", (route) =>
      route.fulfill({ json: repoEvidence }),
    );
    await page.setViewportSize({ width, height: 950 });
    await page.goto(
      "/trends?run=ui-run&q=RAG&start=2024-01-01&cutoff=2024-08-10&mode=reconstruction&view=list",
    );
    await expect(page.getByRole("heading", { name: copy.complete })).toBeVisible();
    await expect(page.locator("ol[aria-label] > li")).toHaveCount(15);
    await inspectSurface(page, info, `radar-15-${width}-${language}`);
    await page.locator("#topic-ui-topic-0").click();
    await expect(
      page.getByRole("heading", { level: 1, name: job.result!.candidates[0]!.title }),
    ).toBeVisible();
    await expect(page.getByText("2024-03", { exact: true })).toBeVisible();
    await expect(page.getByText(copy.precision_month, { exact: true }).first()).toBeVisible();
    await inspectSurface(page, info, `topic-${width}-${language}`);
    await page.getByText(copy.table, { exact: true }).click();
    const table = page.getByRole("table", { name: copy.table });
    await expect(table.getByRole("row").filter({ hasText: "2024-Q1" })).toContainText("0");
    await expect(table.getByRole("row").filter({ hasText: "2024-Q2" })).toContainText(copy.unknown);
    await expect(table.getByRole("row").filter({ hasText: "2024-Q3" })).toContainText(
      copy.partialPeriod,
    );
    const providers = page.getByRole("group", { name: copy.providerFilter });
    await providers.getByRole("button", { name: /^GitHub/ }).click();
    await expect(page.locator("#source-ui-paper")).toHaveCount(0);
    await expect(page.locator("#source-ui-repo")).toBeVisible();
    await page.locator("#evidence-ui-topic-0-advantage-0").click();
    await expect(page).toHaveURL(/snapshot=ui-paper-v1.*start=32/);
    await expect(page.getByRole("heading", { name: copy.selectedQuote })).toBeVisible();
    await expect(page.locator("mark")).toHaveText("Our method is 10% faster in the benchmark.");
    await inspectSurface(page, info, `quote-${width}-${language}`);
    await page.getByRole("link", { name: doc.source.backRadar }).click();
    await expect(page.locator("#evidence-ui-topic-0-advantage-0")).toBeFocused();
    await expect(providers.getByRole("button", { name: /^GitHub/ })).toHaveAttribute(
      "aria-pressed",
      "true",
    );
    await page.getByRole("link", { name: copy.methodLink }).click();
    await expect(page.getByRole("heading", { name: copy.methodTitle })).toBeVisible();
    await inspectSurface(page, info, `method-${width}-${language}`);
    await page.getByRole("link", { name: copy.backTopic }).click();
    await expect(page.locator("#method-link")).toBeFocused();
    await page.getByRole("link", { name: copy.backResults }).click();
    await expect(page.locator("#topic-ui-topic-0")).toBeFocused();
    await expect(page).toHaveURL(/start=2024-01-01&cutoff=2024-08-10&mode=reconstruction/);
    await page.reload();
    await expect(page.locator("#topic-ui-topic-0")).toBeFocused();
  });
}

test("seven, zero, unknown periods and incomplete candidate are honest UI states", async ({
  page,
}, info) => {
  let job = analysisFixture(7);
  await page.route("**/api/v1/trends/analyses/ui-run", (route) => route.fulfill({ json: job }));
  await page.goto("/trends?run=ui-run&view=list");
  await expect(page.locator("ol[aria-label] > li")).toHaveCount(7);
  await expect(
    page.getByRole("heading", { name: ru.partial.replace("{{count}}", "7") }),
  ).toBeVisible();
  await inspectSurface(page, info, "radar-seven");
  job = analysisFixture(0);
  job.result!.search.mode = "lexical_fallback";
  job.result!.candidates[0]!.brief.example = null;
  for (const period of job.result!.candidates[0]!.measurements.series) {
    period.normalized_activity = null;
    period.comparable_exposure = null;
  }
  await page.reload();
  await expect(
    page.getByRole("heading", { name: ru.explore.foundTopics.replace("{{count}}", "15") }),
  ).toBeVisible();
  await expect(page.locator("ol[aria-label] > li")).toHaveCount(0);
  await page.getByText(ru.map.about, { exact: true }).click();
  await expect(page.getByText(ru.fallback)).toBeVisible();
  await inspectSurface(page, info, "radar-zero");
  // Неполная справка идёт после четырнадцати полных с той же относимостью.
  await page.getByRole("button", { name: "3", exact: true }).click();
  await page.locator("#topic-ui-topic-0").click();
  await expect(page.getByText(ru.candidateOnly)).toBeVisible();
  await expect(page.getByText(ru.missingClaim)).toBeVisible();
  await expect(page.getByText(ru.noChart)).toBeVisible();
  await expect(page.getByRole("figure", { name: ru.explore.quarterPublications })).toBeVisible();
  await page.evaluate(() => (document.documentElement.style.fontSize = "200%"));
  await page.setViewportSize({ width: 390, height: 950 });
  await inspectSurface(page, info, "topic-unknown-zoom200");
});

test("submission retry preserves its identity, input and URL across reload", async ({ page }) => {
  await page.addInitScript(() => Object.defineProperty(crypto, "randomUUID", { value: undefined }));
  const requests: Record<string, string>[] = [];
  let fail = true;
  await page.route("**/api/v1/trends/analyses", (route) => {
    requests.push(route.request().postDataJSON());
    return route.fulfill(
      fail
        ? { status: 503, json: {} }
        : {
            status: 202,
            json: {
              ...analysisFixture(),
              status: "prepared",
              result: null,
            },
          },
    );
  });
  await page.route("**/api/v1/trends/analyses/ui-run", (route) =>
    route.fulfill({ json: analysisFixture(7) }),
  );
  await page.goto("/trends?view=analysis");
  await page.getByLabel(ru.query, { exact: true }).fill("GraphRAG");
  await page.getByLabel(ru.query, { exact: true }).press("Enter");
  await expect(page.getByRole("alert")).toContainText(ru.submitError);
  await expect(page).toHaveURL(/request=/);
  await page.reload();
  await expect(page.getByRole("alert")).toContainText(ru.submitError);
  await expect(page.getByLabel(ru.query, { exact: true })).toHaveValue("GraphRAG");
  fail = false;
  await page.getByRole("button", { name: ru.retry, exact: true }).click();
  await expect(page).toHaveURL(/run=ui-run/);
  expect(requests).toHaveLength(3);
  expect(requests[0]!.request_id).toMatch(
    /^[a-f0-9]{8}-[a-f0-9]{4}-4[a-f0-9]{3}-[89ab][a-f0-9]{3}-[a-f0-9]{12}$/,
  );
  expect(requests.every((request) => /^\d{4}-\d{2}-\d{2}$/.test(request.start!))).toBe(true);
  expect(new Set(requests.map((value) => value.request_id)).size).toBe(1);
});

test("late A never replaces B and changed method stays tied to its run", async ({ page }) => {
  let finishA: (() => void) | undefined;
  await page.route("**/api/v1/trends/analyses", async (route) => {
    const query = route.request().postDataJSON().query as string;
    if (query === "AI")
      await new Promise<void>((resolve) => {
        finishA = resolve;
      });
    await route
      .fulfill({ json: analysisFixture(7, query === "AI" ? "run-a" : "run-b", query) })
      .catch(() => {});
  });
  await page.route("**/api/v1/trends/analyses/run-b", (route) => {
    const job = analysisFixture(7, "run-b", "RAG");
    job.method.parameters.emergence.recent_quarters = 6;
    return route.fulfill({ json: job });
  });
  await page.goto("/trends?view=analysis");
  await page.getByLabel(ru.query, { exact: true }).fill("AI");
  await page.getByRole("button", { name: ru.analyze }).click();
  await expect.poll(() => !!finishA).toBe(true);
  await page.getByLabel(ru.query, { exact: true }).fill("RAG");
  await page.getByRole("button", { name: ru.analyze }).click();
  await expect(page).toHaveURL(/run=run-b/);
  finishA!();
  await expect(page.getByRole("figure", { name: ru.map.network })).toBeVisible();
  await expect(page.getByLabel(ru.query, { exact: true })).toHaveValue("RAG");
  await page.getByRole("link", { name: ru.methodLink }).click();
  await expect(page.locator("dl > div").filter({ hasText: ru.recent_quarters })).toHaveText(
    `${ru.recent_quarters}6`,
  );
});

test("bad ranked projection and denied saved source cannot appear as complete", async ({
  page,
}) => {
  const job = analysisFixture();
  job.result!.ranking.ordered_ids[1] = job.result!.ranking.ordered_ids[0]!;
  await page.route("**/api/v1/trends/analyses/ui-run", (route) => route.fulfill({ json: job }));
  await page.goto("/trends?run=ui-run&q=RAG");
  await expect(page.getByRole("alert")).toContainText(ru.loadError);
  await expect(page.locator("ol[aria-label] > li")).toHaveCount(0);
  await page.route("**/api/v1/documents/ui-paper/source*", (route) =>
    route.fulfill({ status: 404, json: {} }),
  );
  await page.goto("/sources/ui-paper?snapshot=ui-paper-v1&analysis=ui-run");
  await expect(page.getByRole("alert")).toBeVisible();
  await expect(page.getByText(paperEvidence.chunks[0]!.text)).toHaveCount(0);
});

test("saved PDF uses the snapshot endpoint; missing quote never highlights another range", async ({
  page,
}) => {
  const originalRequests: string[] = [];
  await page.route("**/api/v1/documents/ui-paper/source?*", (route) =>
    route.fulfill({
      json: {
        ...paperEvidence,
        filename: "one-page.pdf",
        mime_type: "application/pdf",
      },
    }),
  );
  await page.route("**/api/v1/documents/ui-paper/source/content?*", (route) => {
    originalRequests.push(route.request().url());
    return route.fulfill({
      path: new URL("../fixtures/one-page.pdf", import.meta.url).pathname,
      contentType: "application/pdf",
    });
  });
  await page.goto("/sources/ui-paper?snapshot=ui-paper-v1&start=32&end=74&textHash=wrong");
  await expect(page.getByText(ru.quoteUnavailable)).toBeVisible();
  await expect(page.locator("mark")).toHaveCount(0);
  await page.getByRole("radio", { name: "Документ", exact: true }).focus();
  await page.getByRole("radio", { name: "Документ", exact: true }).press("Space");
  await expect(page.locator("canvas").first()).toBeVisible();
  expect(originalRequests).toHaveLength(1);
  expect(originalRequests[0]).toContain("/source/content?snapshot=ui-paper-v1");
});

test("prepared and validated states remain incomplete until a published result arrives", async ({
  page,
}, info) => {
  let state = "prepared";
  await page.route("**/api/v1/trends/analyses/ui-run", (route) =>
    route.fulfill({
      json: {
        ...analysisFixture(7),
        status: state,
        result: state === "published" ? analysisFixture(7).result : null,
      },
    }),
  );
  await page.goto("/trends?run=ui-run&previous=ui-old&q=RAG");
  await expect(page.getByRole("status")).toContainText(ru.refreshing);
  await expect(page.locator("ol[aria-label] > li")).toHaveCount(0);
  await inspectSurface(page, info, "radar-refreshing");
  state = "validated";
  await expect(page.getByRole("status")).toContainText(ru.validating);
  state = "published";
  await expect(page.getByRole("figure", { name: ru.map.network })).toBeVisible();
  await page.getByRole("tab", { name: ru.map.listTab }).click();
  await expect(page.locator("ol[aria-label] > li")).toHaveCount(7);
  await expect(page.getByRole("link", { name: ru.previous })).toHaveAttribute(
    "href",
    "/trends?run=ui-old",
  );
});

test("chart labels remain readable at phone width and 200 percent text size", async ({
  page,
}, info) => {
  await page.route("**/api/v1/trends/analyses/ui-run", (route) =>
    route.fulfill({ json: analysisFixture() }),
  );
  await page.setViewportSize({ width: 390, height: 950 });
  await page.goto("/trends/ui-run/topics/ui-topic-0");
  const figure = page.locator("figure");
  await figure.scrollIntoViewIfNeeded();
  const label = figure.getByText("2024-Q1", { exact: true });
  expect(
    await label.evaluate((el) => Number.parseFloat(getComputedStyle(el).fontSize)),
  ).toBeGreaterThanOrEqual(12);
  await inspectSurface(page, info, "chart-390");
  await page.evaluate(() => (document.documentElement.style.fontSize = "200%"));
  await figure.scrollIntoViewIfNeeded();
  expect(
    await label.evaluate((el) => Number.parseFloat(getComputedStyle(el).fontSize)),
  ).toBeGreaterThanOrEqual(24);
  await inspectSurface(page, info, "chart-390-zoom200");
});

test("late saved request metadata does not erase text being edited", async ({ page }) => {
  let release: (() => void) | undefined;
  await page.route("**/api/v1/trends/analyses/ui-run", async (route) => {
    await new Promise<void>((resolve) => {
      release = resolve;
    });
    await route.fulfill({ json: analysisFixture() });
  });
  await page.goto("/trends?run=ui-run");
  await page.getByLabel(ru.query, { exact: true }).fill("GraphRAG");
  release!();
  await expect(page.getByRole("figure", { name: ru.map.network })).toBeVisible();
  await expect(page.getByLabel(ru.query, { exact: true })).toHaveValue("GraphRAG");
  await expect(page.getByLabel(ru.query, { exact: true })).toBeFocused();
});

test("provisional ranking, two of three fields and topic labels from descriptions", async ({
  page,
}) => {
  const job = analysisFixture(7);
  const firstId = job.result!.ranking.ordered_ids[0]!;
  const first = job.result!.candidates.find((item) => item.id === firstId)!;
  first.brief.advantage = null;
  for (const id of job.result!.ranking.ordered_ids)
    job.result!.candidates.find((item) => item.id === id)!.assessment.basis = "observed_counts";
  const label = "Извлечение фактов из документов";
  const summary = "Тема о том, как находить проверяемые факты в больших наборах документов.";
  await page.route("**/api/v1/trends/analyses/ui-run", (route) => route.fulfill({ json: job }));
  await page.route(`**/api/v1/trends/analyses/ui-run/candidates/${firstId}/description`, (route) =>
    route.fulfill({
      json: {
        job_id: "ui-run",
        candidate_id: firstId,
        version: "source-attribution-v1",
        bundle_sha256: "f".repeat(64),
        result_sha256: job.result_sha256,
        evidence_sha256: first.brief.evidence_sha256,
        status: "validated",
        attempt: 1,
        result: {
          bundle_sha256: "f".repeat(64),
          status: "validated",
          fields: { problem: first.brief.problem, advantage: null, example: first.brief.example },
          label,
          summary,
          model: "fake",
          prompt_sha256: "a".repeat(64),
          latency_ms: 10,
          reason: "",
        },
      },
    }),
  );
  await page.goto("/trends?run=ui-run&view=list");
  await expect(page.locator("ol[aria-label] > li")).toHaveCount(7);
  await expect(page.getByText(ru.provisionalRanking)).toBeVisible();
  await expect(page.locator(`#topic-${firstId}`)).toHaveText(label);
  await expect(page.getByText(ru.sourceTitle.replace("{{title}}", first.title))).toBeVisible();
  await expect(page.getByText(summary)).toBeVisible();
  await expect(page.getByText(ru.missingClaim).first()).toBeVisible();
  await page.locator(`#topic-${firstId}`).click();
  await expect(page.getByRole("heading", { level: 1, name: label })).toBeVisible();
  await expect(page.getByText(summary)).toBeVisible();
  await expect(page.getByText(ru.growthCounts)).toBeVisible();
});
