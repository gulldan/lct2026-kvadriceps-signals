import { describe, expect, test } from "bun:test";
import type { MoneySummary } from "./moneyApi";
import {
  growthColor,
  mapLabels,
  publicationDelta,
  publicationGrowth,
  regionValue,
  fitMapCamera,
  mapBounds,
  mapScale,
  projectMapPoint,
  WORLD,
} from "./worldMap";

describe("карта мира", () => {
  test("цвет ограничен шкалой, точный рост сохранён, нулевая база не становится ростом", () => {
    expect(publicationGrowth(150, 100)).toBe(50);
    expect(publicationGrowth(25, 100)).toBe(-75);
    expect(publicationGrowth(0, 100)).toBe(-100);
    expect(publicationGrowth(10, 0)).toBeNull();
    expect(publicationGrowth(0, 0)).toBeNull();
    expect(growthColor(150)).toBe(growthColor(100));
    expect(growthColor(-75)).toBe(growthColor(-50));
    expect(growthColor(0)).toContain("0%");
    expect(publicationDelta(10, 0)).toBe("—");
    expect(publicationDelta(250, 100)).toBe("+150 %");
  });

  test("отсутствие данных не подменяется нулём, уже собранные события сохраняются при ошибке источника", () => {
    const board: MoneySummary = {
      status: "ready",
      months: [],
      cells: [],
      science: [],
      latest: [],
      sources: [],
      total: 0,
      unsorted: 0,
    };
    expect(regionValue(undefined, "cis")).toBeNull();
    expect(regionValue(board, "cis")).toBeNull();
    board.sources.push({
      name: "registry",
      kind: "registry",
      region: "cis",
      license: "",
      url: "",
      records: 0,
      events: 0,
    });
    expect(regionValue(board, "cis")).toBe(0);
    board.sources[0]!.error = "unavailable";
    expect(regionValue(board, "cis")).toBeNull();
    board.cells.push({
      region: "cis",
      kind: "grant",
      stage: "grant",
      rubric: "energy",
      month: "2026-04",
      count: 4,
    });
    expect(regionValue(board, "cis")).toBe(4);
  });
});

test("крупнейшая страна подписана первой, близкие подписи не накладываются", () => {
  const points = [
    { id: "small", title: "Small", x: 310, y: 200, value: 2 },
    { id: "largest", title: "Largest country", x: 300, y: 200, value: 100 },
    { id: "distant", title: "Distant", x: 600, y: 300, value: 10 },
  ];
  expect([...mapLabels(points, 960).keys()]).toEqual(["largest", "distant", "small"]);
  expect(
    mapLabels(
      points.map((point) => ({
        id: point.id,
        title: point.title,
        value: point.value,
        x: point.x / 3,
        y: point.y / 3,
      })),
      350,
    ).has("largest"),
  ).toBe(true);
});

test("приближение центрирует основной контур и оставляет отступы на широком и узком экранах", () => {
  const russia = mapBounds(WORLD.countries.filter((item) => item.c === "RU"));
  expect(russia.width).toBeLessThan(500);
  const europe = mapBounds(WORLD.countries.filter((item) => item.r === "europe"));
  expect(europe.width).toBeLessThan(300);
  expect(mapBounds(WORLD.countries.filter((item) => item.r === "latam")).width).toBeLessThan(200);
  for (const [width, height] of [
    [960, 560],
    [342, 320],
  ]) {
    const camera = fitMapCamera(europe, width!, height!);
    expect(camera.zoom).toBeGreaterThan(1);
    expect(
      projectMapPoint(
        europe.x + europe.width / 2,
        europe.y + europe.height / 2,
        camera,
        width!,
        height!,
      ),
    ).toEqual({ x: width! / 2, y: height! / 2 });
    const scale = mapScale(width!, height!, camera.zoom);
    expect(europe.width * scale).toBeLessThanOrEqual(width! - 79);
    expect(europe.height * scale).toBeLessThanOrEqual(height! - 119);
  }
  expect(
    mapLabels([{ id: "covered", title: "Country", x: 100, y: 100, value: 1 }], 500, 300, [
      { left: 80, right: 120, top: 80, bottom: 120 },
    ]).get("covered"),
  ).not.toEqual({ x: 100, y: 100 });
});
