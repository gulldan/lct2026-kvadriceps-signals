import { expect, test } from "./fixture";
import { chooseRadio, inspectSurface } from "./checks";
import search from "../fixtures/live-search-v1.json" with { type: "json" };
import ru from "../../src/shared/i18n/locales/ru/trend.json" with { type: "json" };
import type { LiveTopic } from "../../src/features/trend/liveApi";

const emptyPeriod = {
  period: "",
  count: 0,
  population: 0,
  share: null,
  groups: 0,
  new_groups: 0,
  retained_groups: 0,
};
// Снимок витрины урезан до карточек; API даже без истории возвращает структуру измерений с null.
const unmeasured = {
  growth: {
    prior: emptyPeriod,
    recent: emptyPeriod,
    delta_pp: null,
    share_ratio: null,
    count_ratio: null,
  },
  signals: {
    novelty: "unknown",
    momentum: "unknown",
    coherence: "query",
    spread: "unknown",
    application: "none_observed",
    concentration: "unknown",
  },
} satisfies Pick<LiveTopic, "growth" | "signals">;
const answer = {
  ...search,
  topic: { ...search.topic, ...unmeasured },
  candidates: search.candidates.map((candidate) => ({ ...candidate, ...unmeasured })),
};

for (const width of [390, 1440]) {
  test(`top-15 distinguishes model decisions from diagnostic cards and keeps empty results at ${width}px`, async ({
    page,
  }, info) => {
    let count = 15;
    let decisions = 15;
    await page.setViewportSize({ width, height: 1000 });
    await page.route("**/api/v1/trends/live/topics/mcp", (route) => {
      const candidates = answer.candidates.slice(0, count).map((candidate, index) => {
        const label =
          index < decisions
            ? "weak_signal"
            : decisions === 6 && index === 6
              ? "mature"
              : "insufficient_data";
        return Object.assign({}, candidate, {
          // Число независимых подтверждений здесь фиксировано: проверяем именно решения модели.
          independent_sources: 2,
          model_score: {
            value: 0.9,
            threshold: 0.75,
            model: "fixture",
            version: "1",
            basis: "Проверка выдачи",
            label,
            ...(label === "insufficient_data" ? { data_gap: "too_early" } : {}),
          },
        });
      });
      return route.fulfill({
        json: {
          ...answer,
          candidates,
          search_contract: "search-v12",
          counters: {
            ...answer.counters,
            selected: count,
            candidates: count + answer.rejected.length,
            rejected: answer.rejected.length,
            above_threshold: candidates.filter((row) => row.confidence?.above_threshold).length,
          },
        },
      });
    });

    for ([count, decisions] of [
      [15, 0],
      [15, 6],
      [15, 15],
      [14, 14],
      [0, 0],
    ] as const) {
      await page.goto("/trends?topic=mcp&list=cards");
      await expect(
        page.getByRole("tab", { name: `${ru.study.live.types.found} ${count}`, exact: true }),
      ).toHaveAttribute("aria-selected", "true");
      await expect(page.getByText(ru.tile.passed, { exact: true })).toHaveCount(decisions);
      const mature = decisions === 6 ? 1 : 0;
      await expect(page.getByText(ru.report.modelClass.mature, { exact: true })).toHaveCount(
        mature * 2,
      );
      await expect(
        page.getByText(ru.report.modelClass.insufficient_data, { exact: true }),
      ).toHaveCount(count - decisions - mature);
      const warning = page.getByText(
        ru.signalList.incompleteReason
          .replace("{{shown}}", String(count))
          .replace("{{count}}", String(decisions))
          .replace("{{limit}}", "15"),
        { exact: true },
      );
      if (count > 0 && count < 15) {
        await expect(
          page.getByText(ru.signalList.incompleteTitle.replace("{{limit}}", "15"), { exact: true }),
        ).toBeVisible();
        await expect(warning).toBeVisible();
        if (decisions === 6 || count === 14) {
          await warning.scrollIntoViewIfNeeded();
          await inspectSurface(page, info, `incomplete-top15-${width}-${decisions}`);
        }
        await chooseRadio(page, ru.signalList.viewTable);
        await expect(warning).toBeVisible();
      } else {
        await expect(warning).toHaveCount(0);
      }
      const toppedUp = page.getByText(
        ru.signalList.toppedUp.replace("{{count}}", String(decisions)).replace("{{limit}}", "15"),
        { exact: true },
      );
      await expect(toppedUp.filter({ visible: true })).toHaveCount(
        count === 15 && decisions < 15 ? 1 : 0,
      );
      await expect(page.getByText(ru.signalList.empty, { exact: true })).toHaveCount(
        count === 0 ? 1 : 0,
      );
    }
  });
}

test("ready search cards survive background poll errors without leaking into another query", async ({
  page,
}, info) => {
  await page.clock.install({ time: new Date("2026-09-27T00:00:00Z") });
  await page.clock.pauseAt(new Date("2026-09-27T00:00:01Z"));
  await page.emulateMedia({ reducedMotion: "reduce" });
  let failure = 0;
  let state = "ready";
  let pending: "digests_pending" | "rescoring" | undefined = "digests_pending";
  let requests = 0;
  const refreshed: boolean[] = [];
  await page.route("**/api/v1/trends/live/search?**", (route) => {
    requests += 1;
    const params = new URL(route.request().url()).searchParams;
    refreshed.push(params.has("refresh"));
    return route.fulfill({
      status: failure || 200,
      json: failure
        ? { error: "Temporary poll failure" }
        : {
            query: params.get("q"),
            status: state,
            elapsed_seconds: state === "ready" ? 720 : 0,
            step: "arxiv",
            ...(state === "ready"
              ? { detail: { ...answer, ...(pending ? { [pending]: true } : {}) } }
              : {}),
          },
    });
  });
  await page.goto("/trends?q=проверка");
  const found = page.getByRole("tab", {
    name: `${ru.study.live.types.found} 15`,
    exact: true,
  });
  const warning = page.getByText(ru.study.live.failedTitle, { exact: true });
  await expect(found).toHaveAttribute("aria-selected", "true");

  for (failure of [503, 502, 429]) {
    const failed = page.waitForResponse(
      (response) =>
        response.url().includes("/trends/live/search?") && response.status() === failure,
    );
    await page.clock.runFor(5100);
    await failed;
    await expect(found).toBeVisible();
    await expect(warning).toBeVisible();
    await expect(page.getByRole("heading", { name: "Радар на паузе" })).toHaveCount(0);
    if (failure === 503) {
      // Часы опроса остановлены, поэтому проверка не запускает зависящий от таймеров аудит.
      for (const width of [1440, 390]) {
        await page.setViewportSize({ width, height: 1000 });
        await warning.scrollIntoViewIfNeeded();
        await page.screenshot({
          path: info.outputPath(`ready-poll-error-${width}.png`),
          fullPage: width === 1440,
          animations: "disabled",
        });
        expect(await page.evaluate(() => document.documentElement.scrollWidth)).toBeLessThanOrEqual(
          width,
        );
      }
    }
    failure = 0;
    pending = "rescoring";
    await page.clock.runFor(10_100);
    await expect(warning).toHaveCount(0);
    await expect(found).toBeVisible();
  }

  state = "running";
  await page.clock.runFor(5100);
  await expect(page.getByRole("region", { name: ru.study.live.progress.plan })).toBeVisible();
  await expect(found).toHaveCount(0);
  state = "ready";
  pending = undefined;
  await page.clock.runFor(2200);
  await expect(found).toBeVisible();
  const settledRequests = requests;
  await page.clock.runFor(20_000);
  expect(requests).toBe(settledRequests);

  failure = 503;
  await page.getByRole("textbox", { name: ru.study.live.searchLabel }).fill("другой запрос");
  await page.getByRole("button", { name: ru.study.live.searchButton, exact: true }).click();
  await expect(page.getByRole("heading", { name: "Радар на паузе" })).toBeVisible();
  await expect(found).toHaveCount(0);
  await expect(warning).toHaveCount(0);
  failure = 0;
  state = "failed";
  await page.getByRole("textbox", { name: ru.study.live.searchLabel }).fill("неуспешный запрос");
  await page.getByRole("button", { name: ru.study.live.searchButton, exact: true }).click();
  await expect(page.getByRole("alert")).toContainText(ru.study.live.searchFailed);
  await expect(found).toHaveCount(0);
  expect(refreshed).not.toContain(true);
});

test("technology measurements keep source evidence separate from the direction selection", async ({
  page,
}) => {
  await page.route("**/api/v1/trends/live/topics/mcp", (route) => route.fulfill({ json: answer }));
  await page.goto(
    `/trends?topic=mcp&signal=${encodeURIComponent(search.candidates[0]!.id)}&view=measurements`,
  );

  const papers = page.getByRole("tab", { name: new RegExp(ru.study.live.types.papers) });
  await expect(papers).toHaveAttribute("aria-selected", "true");
  await expect(page.getByRole("tab", { name: new RegExp(ru.study.live.types.found) })).toHaveCount(
    0,
  );
  await expect(page.getByText(ru.signalList.countMismatch)).toHaveCount(0);
  await expect(page.locator("#profile-row-momentum")).toContainText(
    ru.study.profile.momentum.unknown,
  );
  await expect(page.locator("#profile-row-momentum")).not.toContainText("NaN");
  await expect(page.getByRole("tabpanel")).toContainText(search.papers[0]!.title);

  await page.getByRole("button", { name: ru.report.backToTechnology, exact: true }).click();
  await page.getByRole("button", { name: ru.report.backToList, exact: true }).click();
  const found = page.getByRole("tab", { name: new RegExp(ru.study.live.types.found) });
  await expect(found).toHaveAttribute("aria-selected", "true");
  await expect(found).toContainText(String(search.candidates.length));
});

test("an executive brief can recover from a failed request without closing the dialog", async ({
  page,
}) => {
  await page.route("**/api/v1/trends/live/topics/mcp", (route) => route.fulfill({ json: answer }));
  let attempts = 0;
  await page.route("**/api/v1/trends/live/executive?*", (route) => {
    attempts += 1;
    return route.fulfill(
      attempts === 1
        ? { status: 502, json: { error: "gateway unavailable" } }
        : {
            json: {
              run_id: "recovered",
              query: search.topic.name,
              observed_at: search.observed_at,
              written_at: search.observed_at,
              sources_processed: search.counters.sources_processed,
              candidates: search.counters.candidates,
              selected: search.candidates.length,
              items: [
                {
                  id: search.candidates[0]!.id,
                  name: "Технология",
                  step: "watch",
                  horizon: "1-3",
                  benefit: "Проверка качества производственных данных",
                  reason: "Требуется проверка аналитиком",
                },
              ],
            },
          },
    );
  });
  await page.goto("/trends?topic=mcp");
  await page.getByRole("button", { name: ru.executive.menuScreen, exact: true }).click();
  const dialog = page.getByRole("dialog");
  await expect(dialog.getByRole("alert")).toContainText(ru.executive.failed);
  await dialog.getByRole("button", { name: ru.executive.retry, exact: true }).click();
  await expect(dialog).toContainText("Проверка качества производственных данных");
  await expect(dialog.getByRole("alert")).toHaveCount(0);
  expect(attempts).toBe(2);
});
