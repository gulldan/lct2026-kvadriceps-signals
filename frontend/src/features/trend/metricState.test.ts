import { describe, expect, test } from "bun:test";
import { metricReasons, metricState } from "./metricState";

describe("metricState", () => {
  test("a measured zero is measured, not missing", () => {
    expect(metricState({ value: 0, status: "measured" })).toBe("measured");
  });
  test("null without observation is unknown and keeps its reasons", () => {
    const metric = { value: null, status: "unavailable", reason_codes: ["insufficient_quarters"] };
    expect(metricState(metric)).toBe("unknown");
    expect(metricReasons(metric)).toEqual(["unavailable", "insufficient_quarters"]);
  });
  test("not observed in the examined scope is its own state", () => {
    expect(metricState({ value: null, status: "not_observed" })).toBe("not_observed");
    expect(
      metricState({ value: null, status: "unavailable", reason_codes: ["not_observed_in_scope"] }),
    ).toBe("not_observed");
  });
  test("NaN never counts as a measurement", () => {
    expect(metricState({ value: Number.NaN, status: "measured" })).toBe("unknown");
  });
});
