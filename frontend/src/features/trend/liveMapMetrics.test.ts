import { describe, expect, test } from "bun:test";
import fixture from "../../../tests/fixtures/trend-live-map.json";
import type { LiveTopic } from "./liveApi";
import {
  calendarRange,
  comparisonRanges,
  growthBand,
  publicationChange,
  symlogAxis,
} from "./liveMapMetrics";

describe("publication comparison", () => {
  test("dated comparison uses the current September window instead of completed June quarters", () => {
    const topic = {
      ...fixture.topics[0],
      comparison: {
        prior_start: "2025-09-08",
        recent_start: "2026-03-11",
        end_exclusive: "2026-09-11",
        days: 184,
        prior: 20,
        current: 35,
        fresh: 6,
      },
    } as LiveTopic;
    expect(publicationChange(topic, fixture.periods)).toEqual({
      prior: 20,
      current: 35,
      added: 15,
      fresh: 6,
      band: "strong",
    });
    expect(comparisonRanges(topic, fixture.periods, "ru").recent).toContain("сент");
    expect(comparisonRanges(topic, fixture.periods, "en").recent).toContain("Sep");
    expect(comparisonRanges(topic, fixture.periods, "ru").recent).not.toContain("июн");
  });
  test("calendar ranges show both years when needed and one shared year otherwise", () => {
    for (const locale of ["ru", "en", "zh"]) {
      expect(calendarRange(fixture.periods.slice(5, 7), locale).match(/2026/g)).toHaveLength(1);
      const crossing = calendarRange(fixture.periods.slice(0, 2), locale);
      expect(crossing).toContain("2024");
      expect(crossing).toContain("2025");
    }
  });
  test("uses the last four completed quarters and excludes the current partial quarter", () => {
    const topic = fixture.topics.find((row) => row.id === "mcp")! as LiveTopic;
    expect(publicationChange(topic, fixture.periods)).toEqual({
      prior: 109,
      current: 162,
      added: 53,
      fresh: 71,
      band: "moderate",
    });
    const reordered = { ...topic, series: topic.series.toReversed() };
    expect(publicationChange(reordered, fixture.periods)).toEqual(
      publicationChange(topic, fixture.periods),
    );
    const missing = { ...topic, series: topic.series.filter((row) => row.period !== "2026-Q2") };
    expect(publicationChange(missing, fixture.periods)).toBeNull();
    expect(publicationChange(topic, fixture.periods.slice(0, 3))).toBeNull();
  });

  test("zero baselines and decreases are never labelled as positive rates", () => {
    expect(growthBand(0, 78)).toBe("new");
    expect(growthBand(0, 0)).toBe("moderate");
    expect(growthBand(100, 90)).toBe("declining");
    for (const [current, expected] of [
      [150, "moderate"],
      [151, "strong"],
      [200, "strong"],
      [201, "rapid"],
      [400, "rapid"],
      [401, "surging"],
    ] as const)
      expect(growthBand(100, current)).toBe(expected);
  });

  test("symmetric log axes stay monotone, keep zero centred and label round values", () => {
    for (const values of [[0], [0, 1], [-20, 0, 250], [19, 254], [-2500, 8600], [-600, 400]]) {
      const axis = symlogAxis(values);
      expect(axis.at(Math.min(0, ...values))).toBeGreaterThanOrEqual(0);
      expect(axis.at(Math.max(0, ...values))).toBeLessThanOrEqual(1);
      for (const [low, high] of [
        [-1000, -300],
        [-1, 0],
        [0, 1],
        [7, 700],
      ] as const)
        expect(axis.at(low)).toBeLessThan(axis.at(high));
      for (const magnitude of [1, 42, 900])
        expect(axis.at(0)).toBeCloseTo((axis.at(-magnitude) + axis.at(magnitude)) / 2, 12);
      expect(axis.ticks).toContain(0);
      expect(axis.ticks.every((tick) => axis.at(tick) >= 0 && axis.at(tick) <= 1)).toBe(true);
    }
    // На линейной шкале разница между 0 и 5 была меньше пиксела.
    const crowded = symlogAxis([-600, -3, -1, 0, 1, 2, 5, 400]);
    expect(crowded.at(5) - crowded.at(0)).toBeGreaterThan(0.1);
    expect(crowded.ticks).toEqual([-300, -100, -30, -10, -3, -1, 0, 1, 3, 10, 30, 100, 300]);
  });
});
