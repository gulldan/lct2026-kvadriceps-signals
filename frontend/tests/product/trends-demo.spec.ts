import { expect, test } from "./fixture";
import { inspectSurface } from "./checks";
import { analysisFixture, paperEvidence } from "./trends-fixture";
import ru from "../../src/shared/i18n/locales/ru/trend.json" with { type: "json" };
import doc from "../../src/shared/i18n/locales/ru/document.json" with { type: "json" };

for (const width of [390, 1440]) {
  test(`demo executes search, visualizes unranked matches and opens exact evidence at ${width}px`, async ({
    page,
  }, info) => {
    const job = structuredClone(analysisFixture(0));
    job.sources[0]!.source.publication = { value: "2024-03", precision: "month", basis: "UI-M" };
    for (const candidate of job.result!.candidates) {
      for (const period of candidate.measurements.series) {
        period.normalized_activity = null;
        period.comparable_exposure = null;
        period.comparable_scientific_works = null;
      }
    }
    const requests: Record<string, string>[] = [];
    await page.route("**/api/v1/trends/analyses", (route) => {
      const request = route.request().postDataJSON();
      requests.push(request);
      job.request = { ...request, cutoff: job.result!.cutoff };
      return route.fulfill({ status: 202, json: { ...job, status: "prepared", result: null } });
    });
    await page.route("**/api/v1/trends/analyses/ui-run", (route) => route.fulfill({ json: job }));
    await page.route("**/api/v1/trends/analyses/ui-run/candidates/*/description", (route) =>
      route.fulfill({ status: 503, json: {} }),
    );
    await page.route("**/api/v1/documents/ui-paper/source*", (route) =>
      route.fulfill({ json: paperEvidence }),
    );
    await page.setViewportSize({ width, height: 1000 });
    await page.goto("/trends?view=analysis");
    await page.getByRole("button", { name: ru.demo.run }).click();
    await expect(page).toHaveURL(/run=ui-run/);
    expect(requests).toHaveLength(1);
    expect(requests[0]!.query).toBe("RAG");
    expect(requests[0]!.request_id).toMatch(/^[\da-f-]{36}$/);
    await expect(page.getByRole("figure", { name: ru.map.network })).toBeVisible();
    await page.getByRole("tab", { name: ru.map.listTab }).click();
    const topics = page.getByRole("list", { name: ru.explore.topicList });
    await expect(topics.locator(":scope > li")).toHaveCount(6);
    await expect(page.locator("ol[aria-label] > li")).toHaveCount(0);
    await page.getByText(ru.map.about, { exact: true }).click();
    const chart = page.getByRole("figure", { name: ru.explore.publications });
    await expect(chart).toBeVisible();
    await chart.getByRole("button", { name: "2024: Научных статей: 1" }).focus();
    await expect(chart).toContainText("2024 · Научных статей: 1");
    await page.getByText(ru.explore.how, { exact: true }).click();
    await expect(
      page.getByText(
        ru.explore.stepSearch
          .replace("{{query}}", job.result!.search.applied_query)
          .replace("{{topics}}", "15"),
      ),
    ).toBeVisible();
    await inspectSurface(page, info, `demo-result-${width}`);
    const firstPageId = await topics.locator('a[id^="topic-"]').first().getAttribute("id");
    await page.getByRole("button", { name: "2", exact: true }).click();
    await expect(page).toHaveURL(/topicsPage=2/);
    const topic = topics.locator('a[id^="topic-"]').first();
    await expect(topic).not.toHaveAttribute("id", firstPageId!);
    const topicId = (await topic.getAttribute("id"))!.slice("topic-".length);
    await topic.click();
    await expect(page.getByRole("figure", { name: ru.explore.quarterPublications })).toBeVisible();
    await inspectSurface(page, info, `demo-topic-${width}`);
    const evidenceId = `evidence-${topicId}-advantage-0`;
    await page.locator(`#${evidenceId}`).click();
    await expect(page.locator("mark")).toHaveText("Our method is 10% faster in the benchmark.");
    await page.getByRole("link", { name: doc.source.backRadar }).click();
    await expect(page.locator(`#${evidenceId}`)).toBeFocused();
    await page.getByRole("link", { name: ru.backResults }).click();
    await expect(page).toHaveURL(/topicsPage=2/);
    await expect(page.locator(`#topic-${topicId}`)).toBeFocused();
    await page.reload();
    await expect(page.locator(`#topic-${topicId}`)).toBeFocused();
    expect(requests).toHaveLength(1);
  });
}

for (const [key, query] of [
  ["medicine", "cancer immunotherapy"],
  ["agriculture", "drought resistant crops"],
  ["energy", "sodium ion batteries"],
  ["materials", "biodegradable polymers"],
  ["robotics", "robotics"],
  ["aiSecurity", "AI security"],
] as const) {
  test(`${query} preset starts one normal analysis without a second submit`, async ({ page }) => {
    const requests: Record<string, string>[] = [];
    const job = analysisFixture(0, "preset-run", query);
    await page.route("**/api/v1/trends/analyses", (route) => {
      requests.push(route.request().postDataJSON());
      job.request = { ...job.request!, ...requests[0]! };
      return route.fulfill({ json: job });
    });
    await page.route("**/api/v1/trends/analyses/preset-run", (route) =>
      route.fulfill({ json: job }),
    );
    await page.goto("/trends?view=analysis");
    await page.getByRole("button", { name: ru.examples, exact: true }).click();
    await page.getByRole("menuitem", { name: ru.study.live.quickStarts[key], exact: true }).click();
    await expect(page).toHaveURL(/run=preset-run/);
    await expect(page.getByRole("figure", { name: ru.map.network })).toBeVisible();
    expect(requests).toHaveLength(1);
    expect(requests[0]!.query).toBe(query);
  });
}
