import { expect, test } from "bun:test";
import { createData } from "../../../tests/product/data";
import {
  THEME_REGIONS,
  THEME_SORTS,
  nearbyThemes,
  themePoints,
  themeRegion,
  type ThemePoint,
} from "./themeMap";

test("map preserves measured axes, including ties, when the visible set changes", () => {
  const clusters = createData().clusters;
  const all = themePoints(clusters);
  const filtered = themePoints(clusters.slice(1));
  expect(all[0]).toMatchObject({ freshness: 0.6, novelty: 0.8 });
  expect(all[1]).toMatchObject({ freshness: 0.6, novelty: 0.8 });
  expect(filtered[0]).toEqual(all[1]);
});

test("unscored themes stay out of the map and below scored themes in score order", () => {
  const clusters = createData().clusters;
  clusters[0]!.params = {};
  clusters[0]!.document_count = 10000;
  expect(themePoints(clusters).map((point) => point.id)).toEqual(["cluster-2"]);
  expect(clusters.toSorted(THEME_SORTS.tech.cmp).map((cluster) => cluster.id)).toEqual([
    "cluster-2",
    "cluster-1",
  ]);
});

test("unknown freshness never creates a median coordinate; fresh sorting uses publications", () => {
  const clusters = createData().clusters;
  clusters[0]!.params!.itc = structuredClone(clusters[0]!.params!.itc!);
  clusters[0]!.params!.itc!.signals!.freshness_available = false;
  clusters[0]!.updated_at = "2030-01-01T00:00:00Z";
  expect(themePoints(clusters).map((point) => point.id)).toEqual(["cluster-2"]);
  expect(clusters.toSorted(THEME_SORTS.fresh.cmp)[0]!.id).toBe("cluster-2");
});

const pointAt = (id: string, freshness: number, novelty: number): ThemePoint => ({
  id,
  label: id,
  freshness,
  novelty,
  documents: 1,
});

test("overlapping circles keep every tied topic available without moving coordinates", () => {
  const selected = pointAt("selected", 0.6, 0.8);
  const points = [selected, pointAt("tie", 0.6, 0.8), pointAt("outside", 0.1, 0.1)];
  const original = structuredClone(points);
  expect(nearbyThemes(points, selected, 900, 384)).toEqual(points.slice(0, 2));
  expect(nearbyThemes(points.slice(1), selected, 900, 384)).toEqual([points[1]!]);
  expect(points).toEqual(original);
});

test("selection follows visual proximity on wide and narrow maps without taking distant topics", () => {
  const selected = pointAt("selected", 0.5, 0.5);
  const points = [
    selected,
    pointAt("horizontal", 0.56, 0.5),
    pointAt("vertical", 0.5, 0.56),
    pointAt("distant", 0.8, 0.8),
  ];
  expect(nearbyThemes(points, selected, 900, 384).map((point) => point.id)).toEqual([
    "selected",
    "vertical",
  ]);
  expect(nearbyThemes(points, selected, 280, 320).map((point) => point.id)).toEqual([
    "selected",
    "horizontal",
    "vertical",
  ]);
});

test("every measured topic belongs to one stable region, with midpoint ties on the upper side", () => {
  const points = [
    pointAt("upper-left", 0, 1),
    pointAt("upper-right", 1, 1),
    pointAt("lower-left", 0, 0),
    pointAt("lower-right", 1, 0),
    pointAt("midpoint", 0.5, 0.5),
    pointAt("below-midpoint", 0.499, 0.499),
  ];
  expect(points.map(themeRegion)).toEqual([
    "earlierDistinct",
    "recentDistinct",
    "earlierSimilar",
    "recentSimilar",
    "recentDistinct",
    "earlierSimilar",
  ]);
  const counts = THEME_REGIONS.map(
    (region) => points.filter((point) => themeRegion(point) === region).length,
  );
  expect(counts).toEqual([1, 2, 2, 1]);
  expect(counts.reduce((sum, count) => sum + count, 0)).toBe(points.length);
});
