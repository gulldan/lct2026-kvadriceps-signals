import { expect, test } from "./fixture";
import { inspectSurface } from "./checks";
import { analysisFixture } from "./trends-fixture";
import ru from "../../src/shared/i18n/locales/ru/trend.json" with { type: "json" };
import en from "../../src/shared/i18n/locales/en/trend.json" with { type: "json" };
import zh from "../../src/shared/i18n/locales/zh/trend.json" with { type: "json" };

for (const [width, language, copy] of [
  [390, "ru", ru],
  [768, "en", en],
  [1024, "zh", zh],
  [1440, "ru", ru],
] as const) {
  test(`worked example and readable saved method at ${width}px ${language}`, async ({
    page,
  }, info) => {
    test.setTimeout(90_000);
    const trendRequests: string[] = [];
    // Колокольчик спрашивает свои сводки на любой странице, к анализу они отношения не имеют.
    page.on("request", (request) => {
      if (request.url().includes("/api/v1/trends/analyses")) trendRequests.push(request.url());
    });
    await page.addInitScript((lang) => localStorage.setItem("rag.lang", lang), language);
    await page.emulateMedia({ reducedMotion: "reduce" });
    await page.setViewportSize({ width, height: 950 });
    await page.goto("/trends?view=analysis");
    const exampleLink = page.getByRole("link", { name: copy.demo.open });
    await expect(exampleLink).toBeInViewport();
    await expect(page.getByRole("button", { name: copy.demo.run })).toBeVisible();
    await inspectSurface(page, info, `onboarding-${width}-${language}`);
    await exampleLink.click();
    await expect(page.getByRole("heading", { name: copy.demo.title })).toBeVisible();
    for (const [name, id] of [
      ["HyDE", "2212.10496v1"],
      ["Query2doc", "2303.07678v1"],
    ]) {
      await expect(
        page.getByRole("link", { name: copy.demo.paperLink.replace("{{name}}", name!) }),
      ).toHaveAttribute("href", `https://arxiv.org/abs/${id}`);
    }
    await inspectSurface(page, info, `example-${width}-${language}`);
    await page.getByRole("link", { name: copy.methodTitle }).click();
    await expect(page.getByText(copy.methodGuide.definition)).toBeVisible();
    await expect(page.getByText(copy.methodSaved)).toHaveCount(0);
    await page.getByRole("link", { name: copy.demo.back }).click();
    await expect(page.locator("#method-link")).toBeFocused();
    expect(trendRequests, "Reading the example does not run analysis").toEqual([]);
    const exampleJob = analysisFixture(0, "example-run", "query expansion");
    await page.route("**/api/v1/trends/analyses", async (route) => {
      exampleJob.request = route.request().postDataJSON();
      await route.fulfill({ json: exampleJob });
    });
    await page.route("**/api/v1/trends/analyses/example-run", (route) =>
      route.fulfill({ json: exampleJob }),
    );
    await page.getByRole("button", { name: copy.demo.useQuery }).click();
    await expect(page).toHaveURL(/run=example-run/);
    await expect(page.getByRole("figure", { name: copy.map.network })).toBeVisible();
    await expect(page.locator("#trend-query")).toHaveValue("query expansion");
    await page.reload();
    await expect(page.locator("#trend-query")).toHaveValue("query expansion");
    expect(trendRequests.filter((url) => url.endsWith("/analyses"))).toHaveLength(1);

    const job = analysisFixture();
    job.method.explanation = {
      ...job.method.explanation,
      definition: ru.methodGuide.definition,
      counts: ru.methodGuide.counts,
      history: ru.methodGuide.history,
      earliest: ru.methodGuide.earliest,
      brief: ru.methodGuide.brief,
      order: ru.methodGuide.order,
      formula:
        "rate = (papers + smoothing) / (exposure + 2 × smoothing); change = ln(recent_rate / prior_rate). " +
        "saved-formula-".repeat(60),
      limitations: [ru.methodGuide.limitations],
    };
    job.method.version = "saved-version-".repeat(50);
    job.method.parameters.source = "archive-".repeat(90);
    job.method.parameters.emergence.recent_quarters = 6;
    await page.route("**/api/v1/trends/analyses/ui-run", (route) => route.fulfill({ json: job }));
    await page.goto("/trends/ui-run/method");
    await expect(page.getByText(job.method.explanation.definition, { exact: true })).toBeVisible();
    const textStyle = await page
      .locator("article p")
      .first()
      .evaluate((element) => ({
        size: parseFloat(getComputedStyle(element).fontSize),
        line: parseFloat(getComputedStyle(element).lineHeight),
        width: element.getBoundingClientRect().width,
      }));
    expect(textStyle.size).toBeGreaterThanOrEqual(16);
    expect(textStyle.line).toBeGreaterThanOrEqual(26);
    if (width === 1440) expect(textStyle.width).toBeGreaterThanOrEqual(800);
    await expect(page.getByText(job.method.explanation.formula, { exact: true })).toBeHidden();
    await inspectSurface(page, info, `method-reading-${width}-${language}`);
    await page.getByText(copy.methodCalculation, { exact: true }).click();
    await expect(page.locator("dl > div").filter({ hasText: copy.recent_quarters })).toHaveText(
      `${copy.recent_quarters}6`,
    );
    await page.getByText(copy.technical, { exact: true }).click();
    await expect(page.getByText(job.method.version, { exact: true })).toBeVisible();
    if (width === 390)
      await page.evaluate(() => (document.documentElement.style.fontSize = "200%"));
    await inspectSurface(page, info, `method-expanded-${width}-${language}`);
  });
}
