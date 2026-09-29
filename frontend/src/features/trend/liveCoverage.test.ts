import { describe, expect, test } from "bun:test";
import { liveExamined, liveFailureReason, liveRunOverdue } from "./liveCoverage";

const periods = Array.from({ length: 14 }, (_, index) => {
  const month = new Date(Date.UTC(2025, 7 + index, 1));
  return {
    period: month.toISOString().slice(0, 7),
    start: "",
    end_exclusive: index === 13 ? "2026-09-11" : "",
    complete: index < 13,
    population: 1000,
  };
});
const categories = "cat:cs.CL OR cat:cs.AI OR cat:q-bio.* OR cat:quant-ph";

describe("liveExamined", () => {
  test("counts complete months, the open month, the window population and the snapshot age", () => {
    const scope = liveExamined({
      periods,
      categories,
      collection: {
        phase: "idle",
        checked_at: "2026-09-13T10:00:00Z",
        next_run_at: "",
        last_success_at: "2026-09-12T20:30:00Z",
        last_source_at: "",
        error: "",
        channels: [],
      },
    });
    expect(scope.completeMonths).toBe(13);
    expect(scope.from).toBe("2025-08");
    expect(scope.to).toBe("2026-08");
    expect(scope.current).toEqual({ period: "2026-09", through: "2026-09-10" });
    expect(scope.works).toBe(14000);
    expect(scope.categories).toBe(4);
    expect(scope.staleHours).toBe(13.5);
  });

  test("an unknown collection state is null, not zero hours", () => {
    expect(liveExamined({ periods, categories, collection: null }).staleHours).toBeNull();
  });
});

describe("liveFailureReason", () => {
  test("known codes pass through, anything else is neither a source refusal nor a raw string", () => {
    expect(liveFailureReason("canceled")).toBe("canceled");
    expect(liveFailureReason("source")).toBe("source");
    expect(liveFailureReason("live: collection canceled: context canceled")).toBe("other");
    expect(liveFailureReason(undefined)).toBe("other");
  });
});

describe("liveRunOverdue", () => {
  test("a past appointment is overdue; a future or unknown one is not", () => {
    const now = Date.parse("2026-09-22T13:30:00Z");
    expect(liveRunOverdue("2026-09-22T13:26:00Z", now)).toBe(true);
    expect(liveRunOverdue("2026-09-22T19:00:00Z", now)).toBe(false);
    expect(liveRunOverdue("", now)).toBe(false);
  });
});
