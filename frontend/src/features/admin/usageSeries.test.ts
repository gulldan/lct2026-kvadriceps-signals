import { expect, test } from "bun:test";
import { usageDays } from "./usageSeries";

const row = {
  day: "2026-09-08",
  requests: 12,
  prompt_tokens: 2000,
  completion_tokens: 1000,
  total_tokens: 3000,
  cost_usd: 0,
};

test("one occupied day stays one bucket in the seven-day window", () => {
  const window = { from: "2026-09-02", to: "2026-09-08", by_day: [row] };
  expect(usageDays(window, "requests").map((day) => day.value)).toEqual([0, 0, 0, 0, 0, 0, 12]);
  expect(usageDays(window, "tokens").map((day) => day.value)).toEqual([0, 0, 0, 0, 0, 0, 3000]);
});

test("calendar buckets handle month boundaries, timestamps and missing measurements", () => {
  const days = usageDays(
    { from: "2026-08-30T00:00:00Z", to: "2026-09-02T23:59:59Z", by_day: [] },
    "requests",
  );
  expect(days).toHaveLength(4);
  expect(days.every((day) => day.value === 0)).toBe(true);
  expect(new Set(days.map((day) => day.label)).size).toBe(4);
});
