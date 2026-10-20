import { expect, test } from "./fixture";
import ru from "../../src/shared/i18n/locales/ru/trend.json" with { type: "json" };

for (const [reason, message] of [
  ["query_required", ru.catalogue.queryRequired],
  ["catalogue_empty", ru.catalogue.emptyTitle],
  ["below_relevance_floor", ru.catalogue.belowFloor],
]) {
  test(`catalogue explains ${reason} and preserves its channel`, async ({ page }) => {
    await page.route("**/api/v1/technologies?**", (route) =>
      route.fulfill({
        json: { technologies: [], reason, catalogue_version: "audit", schema_version: "1" },
      }),
    );
    await page.goto("/trends?entity=technologies&channel=patents");
    await expect(page.getByText(message!, { exact: true })).toBeVisible();
    await expect(page).toHaveURL(/entity=technologies&channel=patents/);
  });
}

test("catalogue clears old results while the next query is loading", async ({ page }) => {
  let release: (() => void) | undefined;
  const pending = new Promise<void>((resolve) => {
    release = resolve;
  });
  await page.route("**/api/v1/technologies?**", async (route) => {
    if (new URL(route.request().url()).searchParams.get("q") === "second") await pending;
    await route.fulfill({
      json: {
        technologies: [
          {
            technology_id: "one",
            canonical_name: "Old result",
            description: "",
            match_reasons: ["name"],
          },
        ],
        catalogue_version: "audit",
        schema_version: "1",
      },
    });
  });
  await page.goto("/trends?entity=technologies&channel=patents&q=first");
  await expect(page.getByText("Old result", { exact: true })).toBeVisible();
  const search = page.getByRole("textbox", { name: ru.catalogue.searchPlaceholder });
  await search.fill("second");
  await search.press("Enter");
  await expect(page.getByText("Old result", { exact: true })).toHaveCount(0);
  expect(new URL(page.url()).searchParams.get("channel")).toBe("patents");
  release?.();
  await expect(page.getByText("Old result", { exact: true })).toBeVisible();
});

test("radar detail and back preserve the channel", async ({ page }) => {
  await page.goto("/trends?channel=patents");
  await page.getByRole("button", { name: "Model Context Protocol", exact: true }).click();
  await expect(page).toHaveURL(/topic=mcp/);
  expect(new URL(page.url()).searchParams.get("channel")).toBe("patents");
  await page.getByRole("button", { name: ru.study.live.back, exact: true }).click();
  await expect(page).not.toHaveURL(/topic=/);
  expect(new URL(page.url()).searchParams.get("channel")).toBe("patents");
});

test("language and role filters travel to the API and explain an empty result", async ({
  page,
}) => {
  const requests: string[] = [];
  await page.route("**/api/v1/technologies?**", (route) => {
    requests.push(route.request().url());
    return route.fulfill({
      json: {
        technologies: [],
        reason: "below_relevance_floor",
        catalogue_version: "audit",
        schema_version: "1",
      },
    });
  });
  await page.goto("/trends?entity=technologies&q=electrolysis&language=en&role=primary");
  await expect(page.getByText(ru.catalogue.filters.emptyWithFilters)).toBeVisible();
  const last = new URL(requests.at(-1)!);
  expect(last.searchParams.get("language")).toBe("en");
  expect(last.searchParams.get("role")).toBe("primary");
  await page.getByRole("button", { name: ru.catalogue.filters.reset }).first().click();
  await expect(page).not.toHaveURL(/language=/);
  await expect(page.getByText(ru.catalogue.belowFloor)).toBeVisible();
});

test("an empty catalogue is not offered as an entity on the radar", async ({ page }) => {
  await page.route("**/api/v1/technologies?**", (route) =>
    route.fulfill({
      json: {
        technologies: [],
        reason: "catalogue_empty",
        catalogue_version: "audit",
        schema_version: "1",
      },
    }),
  );
  await page.goto("/trends");
  await expect(page.getByRole("heading", { level: 1 })).toBeVisible();
  await expect(page.getByRole("radio", { name: ru.scope.technologies, exact: true })).toHaveCount(
    0,
  );
  await expect(page.getByRole("radio", { name: ru.scope.map })).toHaveCount(1);
});
