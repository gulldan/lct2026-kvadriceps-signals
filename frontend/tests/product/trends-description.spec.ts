import { expect, test } from "./fixture";
import { analysisFixture } from "./trends-fixture";
import type { AnalysisJob } from "../../src/features/trend/trendApi";
import type { DescriptionJob } from "../../src/features/trend/descriptionApi";
import ru from "../../src/shared/i18n/locales/ru/trend.json" with { type: "json" };

function description(job: AnalysisJob, status: DescriptionJob["status"]): DescriptionJob {
  const candidate = job.result!.candidates[0]!;
  const fields = structuredClone({
    problem: candidate.brief.problem,
    advantage: candidate.brief.advantage,
    example: candidate.brief.example,
  });
  if (status === "validated")
    for (const value of Object.values(fields))
      if (value) value.text = `В источнике сказано: «${value.text}»`;
  return {
    job_id: job.id,
    candidate_id: candidate.id,
    version: "source-attribution-v1",
    bundle_sha256: "f".repeat(64),
    result_sha256: job.result_sha256,
    evidence_sha256: candidate.brief.evidence_sha256,
    status,
    attempt: 1,
    result:
      status === "prepared"
        ? null
        : {
            bundle_sha256: "f".repeat(64),
            status,
            fields,
            model: "UI-M model fixture",
            prompt_sha256: "b".repeat(64),
            latency_ms: 5,
            reason: status === "template" ? "model_unavailable" : "",
          },
  };
}

test("description prepares after facts and preserves source links on mobile", async ({ page }) => {
  const job = analysisFixture();
  const candidate = job.result!.candidates[0]!;
  await page.setViewportSize({ width: 390, height: 900 });
  await page.route("**/api/v1/trends/analyses/ui-run", (route) => route.fulfill({ json: job }));
  await page.route("**/api/v1/trends/analyses/ui-run/candidates/*/description", (route) =>
    route.fulfill({
      json: description(job, route.request().method() === "POST" ? "prepared" : "validated"),
    }),
  );
  await page.goto(`/trends/ui-run/topics/${candidate.id}`);
  await expect(page.getByText(ru.descriptionPreparing)).toBeVisible();
  await expect(page.getByRole("region", { name: ru.topic.motivation })).toContainText(
    candidate.brief.advantage!.text,
  );
  const evidence = page.locator(`#evidence-${candidate.id}-advantage-0`);
  const source = await evidence.getAttribute("href");
  await expect(page.getByText(ru.descriptionValidated)).toBeVisible();
  await expect(page.getByRole("region", { name: ru.topic.motivation })).toContainText(
    `В источнике сказано: «${candidate.brief.advantage!.text}»`,
  );
  await expect(evidence).toHaveAttribute("href", source!);
  await page.reload();
  await expect(page.getByRole("heading", { name: candidate.title })).toBeVisible();
});

test("invalid prose with a real citation leaves the original factual template", async ({
  page,
}) => {
  const job = analysisFixture();
  const candidate = job.result!.candidates[0]!;
  const invalid = description(job, "validated");
  invalid.result!.fields.advantage!.text = "Real citation, invented 10x deployment at Globex";
  await page.route("**/api/v1/trends/analyses/ui-run", (route) => route.fulfill({ json: job }));
  await page.route("**/api/v1/trends/analyses/ui-run/candidates/*/description", (route) =>
    route.fulfill({ json: invalid }),
  );
  await page.goto(`/trends/ui-run/topics/${candidate.id}`);
  await expect(page.getByText(ru.template)).toBeVisible();
  await expect(page.getByRole("region", { name: ru.topic.motivation })).toContainText(
    candidate.brief.advantage!.text,
  );
  await expect(page.getByText(/invented 10x deployment/)).toHaveCount(0);
  await expect(page.getByText(ru.descriptionValidated)).toHaveCount(0);
});

test("a late description for A cannot replace B or its fallback", async ({ page }) => {
  const a = analysisFixture(15, "run-a");
  const b = analysisFixture(15, "run-b");
  b.result!.candidates[0]!.brief.advantage!.text = "B preserves a different source claim.";
  let entered!: () => void;
  const started = new Promise<void>((resolve) => {
    entered = resolve;
  });
  let release!: () => void;
  const blocked = new Promise<void>((resolve) => {
    release = resolve;
  });
  await page.route("**/api/v1/trends/analyses/run-a", (route) => route.fulfill({ json: a }));
  await page.route("**/api/v1/trends/analyses/run-b", (route) => route.fulfill({ json: b }));
  await page.route("**/api/v1/trends/analyses/run-a/candidates/*/description", async (route) => {
    entered();
    await blocked;
    await route.fulfill({ json: description(a, "validated") }).catch(() => undefined);
  });
  await page.route("**/api/v1/trends/analyses/run-b/candidates/*/description", (route) =>
    route.fulfill({ json: description(b, "template") }),
  );
  await page.goto(`/trends/run-a/topics/${a.result!.candidates[0]!.id}`);
  await started;
  await page.goto(`/trends/run-b/topics/${b.result!.candidates[0]!.id}`);
  await expect(page.getByRole("region", { name: ru.topic.motivation })).toContainText(
    "B preserves a different source claim.",
  );
  release();
  await expect(page.getByText(ru.template)).toBeVisible();
  await expect(page.getByRole("region", { name: ru.topic.motivation })).not.toContainText(
    `В источнике сказано: «${a.result!.candidates[0]!.brief.advantage!.text}»`,
  );
  await expect(page.getByRole("region", { name: ru.topic.motivation })).toContainText(
    "B preserves a different source claim.",
  );
});
