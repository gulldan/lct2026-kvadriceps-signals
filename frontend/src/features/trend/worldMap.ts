import atlas from "./worldMap.json";
import type { MoneySummary } from "./moneyApi";

/** Контур: world-atlas 110m в проекции Natural Earth, собран scripts/world-map/build.py. */
export type MapRegion = "americas" | "europe" | "latam" | "mena" | "africa" | "asia" | "cis";

export interface MapCountry {
  n: string;
  c: string | null;
  r: MapRegion;
  x: number;
  y: number;
  d: string;
}

export const WORLD = atlas as {
  width: number;
  height: number;
  labels: Record<MapRegion, [number, number]>;
  countries: MapCountry[];
};

export const VIEW = { x: 0, y: 50, width: 960, height: 385 } as const;
export const REGIONS: MapRegion[] = [
  "americas",
  "europe",
  "asia",
  "cis",
  "latam",
  "mena",
  "africa",
];
export const REGION_POINTS: Record<MapRegion, [number, number]> = {
  americas: [205, 165],
  latam: [331, 305],
  europe: [490, 125],
  cis: [700, 95],
  mena: [592, 202],
  africa: [520, 290],
  asia: [780, 225],
};

export type MapBounds = { x: number; y: number; width: number; height: number };
export type MapCamera = { x: number; y: number; zoom: number };
export const WORLD_CAMERA: MapCamera = { x: 480, y: 242.5, zoom: 1 };

// Заморские острова и разрыв антимеридиана не должны отдалять основной контур страны.
const COUNTRY_BOUNDS = new Map(
  WORLD.countries.map((country) => {
    const pieces = country.d
      .split("M")
      .filter(Boolean)
      .map((piece) => {
        const pairs = [...piece.matchAll(/(-?\d+(?:\.\d+)?),(-?\d+(?:\.\d+)?)/g)];
        const xs = pairs.map((pair) => Number(pair[1]));
        const ys = pairs.map((pair) => Number(pair[2]));
        return {
          x: Math.min(...xs),
          y: Math.min(...ys),
          width: Math.max(...xs) - Math.min(...xs),
          height: Math.max(...ys) - Math.min(...ys),
        };
      });
    return [country.n, pieces.toSorted((a, b) => b.width * b.height - a.width * a.height)[0]!];
  }),
);

export function mapBounds(countries: MapCountry[]): MapBounds {
  const bounds = countries.filter((item) => item.c).map((item) => COUNTRY_BOUNDS.get(item.n)!);
  if (!bounds.length) return VIEW;
  const x = Math.min(...bounds.map((item) => item.x));
  const y = Math.min(...bounds.map((item) => item.y));
  return {
    x,
    y,
    width: Math.max(...bounds.map((item) => item.x + item.width)) - x,
    height: Math.max(...bounds.map((item) => item.y + item.height)) - y,
  };
}

export function mapScale(width: number, height: number, zoom = 1) {
  return Math.min(width / VIEW.width, height / VIEW.height) * zoom;
}

export function fitMapCamera(bounds: MapBounds, width: number, height: number): MapCamera {
  const scale = Math.min(
    (width - 80) / Math.max(32, bounds.width),
    (height - 120) / Math.max(32, bounds.height),
  );
  return {
    x: bounds.x + bounds.width / 2,
    y: bounds.y + bounds.height / 2,
    zoom: Math.max(1, Math.min(8, scale / mapScale(width, height))),
  };
}

export function projectMapPoint(
  x: number,
  y: number,
  camera: MapCamera,
  width: number,
  height: number,
) {
  const scale = mapScale(width, height, camera.zoom);
  return { x: width / 2 + (x - camera.x) * scale, y: height / 2 + (y - camera.y) * scale };
}
const STAGES = ["emerging_signal", "early_signal", "established", "unassessed"] as const;

export const signalStageKey = (stage: string) =>
  (STAGES as readonly string[]).includes(stage) ? (stage as (typeof STAGES)[number]) : "unassessed";

/** Нулевая база не даёт сопоставимого темпа; крайние значения ограничены только в цвете. */
export function publicationGrowth(recent: number, prior: number): number | null {
  return prior > 0 ? ((recent - prior) / prior) * 100 : null;
}

export function growthColor(growth: number) {
  const strength = Math.min(100, Math.abs(growth) * (growth < 0 ? 2 : 1));
  const end = growth < 0 ? "decline" : "growth";
  return `color-mix(in oklab, var(--map-neutral), var(--map-${end}) ${strength}%)`;
}

/** Отсутствие источников не означает, что сделок в регионе нет. */
export function regionValue(board: MoneySummary | undefined, region: MapRegion): number | null {
  if (board?.status !== "ready") return null;
  const count = board.cells
    .filter((cell) => cell.region === region)
    .reduce((sum, cell) => sum + cell.count, 0);
  return count > 0 || board.sources.some((source) => source.region === region && !source.error)
    ? count
    : null;
}

export function publicationDelta(recent: number, prior: number) {
  const growth = publicationGrowth(recent, prior);
  if (growth === null) return "—";
  const value = Math.round(growth);
  return `${value > 0 ? "+" : ""}${value} %`;
}

/** Сначала подписываем крупнейшие значения, оставляя просвет между соседними названиями. */
export function mapLabels(
  points: { id: string; title: string; x: number; y: number; value: number | null }[],
  width: number,
  height = Infinity,
  obstacles: { left: number; right: number; top: number; bottom: number }[] = [],
) {
  const labels = new Map<string, { x: number; y: number }>();
  const bounds = [...obstacles];
  for (const point of points.toSorted((a, b) => (b.value ?? 0) - (a.value ?? 0))) {
    if (!point.value || point.x < 0 || point.x > width || point.y < 0 || point.y > height) continue;
    const half = Math.min(144, point.title.length * 8 + 8) / 2;
    for (const [dx, dy] of [
      [0, 0],
      [0, 44],
      [half + 36, 0],
      [-half - 36, 0],
      [0, -44],
      [half + 36, -44],
      [-half - 36, -44],
      [half + 36, 44],
      [-half - 36, 44],
    ]) {
      const x = point.x + dx!,
        y = point.y + dy!;
      const box = { left: x - half, right: x + half, top: y - 16, bottom: y + 16 };
      if (
        box.left < 0 ||
        box.right > width ||
        box.top < 0 ||
        box.bottom > height ||
        bounds.some(
          (other) =>
            box.left < other.right + 8 &&
            box.right > other.left - 8 &&
            box.top < other.bottom + 8 &&
            box.bottom > other.top - 8,
        )
      )
        continue;
      labels.set(point.id, { x, y });
      bounds.push(box);
      break;
    }
    if (labels.size === (width < 640 ? 2 : 6)) break;
  }
  return labels;
}
