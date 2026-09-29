import type { LibrarySection } from "../../liveSearchProgress";
import {
  DESK,
  LIBRARY_PARTITIONS,
  SHELF_ISLANDS,
  ROOM_DEPTH,
  ROOM_WIDTH,
  SHELF_TARGETS,
  type FloorPoint,
} from "./layout";

type Footprint = readonly [
  left: number,
  right: number,
  north: number,
  south: number,
  clearance?: number,
];
const CLEARANCE = 0.45;
const obstacles: Footprint[] = [];

function shelf(axis: "x" | "z", at: number, from: number, to: number) {
  obstacles.push(
    axis === "x"
      ? [at - 0.57, at + 0.57, from - 0.09, to + 0.09]
      : [from - 0.09, to + 0.09, at - 0.57, at + 0.57],
  );
}

for (const wall of LIBRARY_PARTITIONS) {
  let start: number = wall.from;
  for (const [from, to] of [...wall.gaps, [wall.to, wall.to] as const]) {
    if (from > start) shelf(wall.axis, wall.at, start, from);
    start = to;
  }
  for (const [from, to] of wall.gaps)
    for (const edge of [from - 0.22, to + 0.22])
      obstacles.push(
        wall.axis === "x"
          ? [wall.at - 0.324, wall.at + 0.324, edge - 0.324, edge + 0.324]
          : [edge - 0.324, edge + 0.324, wall.at - 0.324, wall.at + 0.324],
      );
}
for (const island of SHELF_ISLANDS) shelf(island.axis, island.at, island.from, island.to);

// Габариты станций оставляют боковые коридоры и подходы к аркам свободными.
for (const [x, z, width, depth, clearance] of [
  [-10, -7.1, 3.4, 2],
  [-11.9, -7.22, 1.2, 0.65],
  [-8.42, -7, 0.65, 0.75],
  [2.9, -7.4, 2.6, 1.6],
  [10, -7.2, 3.94, 1],
  [-9, 3.8, 4.1, 1.2],
  [10, 4.8, 3, 1.08],
  [11.65, 4.8, 0.75, 0.75],
  // К столу можно подойти вплотную: его край находится выше корпуса архивариуса.
  [0.25, 8.4, 4.53, 1.59, 0.32],
  [-10, 9.05, 2.65, 1.32],
  [10.3, 9.05, 2.65, 1.32],
  [-10.62, 10.12, 0.6, 0.57],
  [-9.35, 10.12, 0.6, 0.57],
  [9.68, 10.12, 0.6, 0.57],
  [10.95, 10.12, 0.6, 0.57],
  [-7.1, 5.4, 1.2, 1],
  [-15.1, 4.8, 1.3, 0.8],
  [-14.8, 10.2, 1.1, 1.1],
  [15.1, 9, 1.1, 1.1],
  [12, 7.1, 1, 0.8],
  [15.1, -7.3, 1.2, 0.85],
  [9.15, 7.15, 1.8, 0.9],
  [14.9, 3.8, 0.75, 0.75],
  [-7.05, -10.4, 1.08, 1.08],
  [14.55, 0.4, 0.9, 0.9],
  [-19.5, -14.5, 2.8, 2.2],
  [20.2, 2.5, 3.3, 3],
  [6.8, 15.5, 3.3, 1.4],
  [-10, 14.9, 3.2, 3.2],
  [-9.8, 6.5, 2.1, 1],
  [-18.1, 2.1, 1, 2.6],
  [-15.3, -17.3, 0.82, 1.63],
  [15.1, -14.8, 0.95, 0.95],
  [18.9, -15.8, 1.63, 0.82],
  [-15.1, 14, 0.9, 0.9],
  [15.55, 4.8, 0.82, 1.63],
  [-5.8, 15.9, 0.8, 0.8],
  [15.3, 11.6, 1, 1],
  [4.7, -11.8, 0.65, 0.65],
  [-21.4, 17, 1.1, 1.1],
  [-15.3, 17, 1.1, 1.1],
  [14.6, 15.8, 1.1, 1.1],
  [21.2, 15.8, 1.1, 1.1],
  [21.5, 11.9, 1.48, 1.37],
  [-21, 5, 1.6, 1.48],
  [2, 15, 1.4, 1.29],
] as const)
  obstacles.push([x - width / 2, x + width / 2, z - depth / 2, z + depth / 2, clearance]);

function crosses(from: FloorPoint, to: FloorPoint, rect: Footprint, clearance: number) {
  clearance = Math.min(clearance, rect[4] ?? clearance);
  let near = 0;
  let far = 1;
  for (const axis of [0, 1] as const) {
    const lower = rect[axis * 2]! - clearance;
    const upper = rect[axis * 2 + 1]! + clearance;
    const delta = to[axis] - from[axis];
    if (Math.abs(delta) < 0.00001) {
      if (from[axis] < lower || from[axis] > upper) return false;
    } else {
      const first = (lower - from[axis]) / delta;
      const last = (upper - from[axis]) / delta;
      near = Math.max(near, Math.min(first, last));
      far = Math.min(far, Math.max(first, last));
      if (near > far) return false;
    }
  }
  return true;
}

export function libraryPathClear(from: FloorPoint, to: FloorPoint, clearance = CLEARANCE) {
  return (
    [from, to].every(
      ([x, z]) =>
        Math.abs(x) <= ROOM_WIDTH / 2 - clearance && Math.abs(z) <= ROOM_DEPTH / 2 - clearance,
    ) && obstacles.every((rect) => !crosses(from, to, rect, clearance))
  );
}

const aisles = {
  desk: DESK,
  south: [0.25, 5.5],
  midEast: [0.25, 2.1],
  midWest: [0.25, 0.8],
  north: [0.25, -9.8],
  worldIslandNorth: [0.25, -12.7],
  worldIslandWest: [-4, -12.7],
  worldIslandSouth: [0.25, -6.5],
  rear: [0.25, -15.1],
  westRear: [-11.2, -15.1],
  westRearDoorNorth: [-11.2, -13],
  westRearDoorSouth: [-11.2, -10.9],
  westInside: [-4.1, -9.8],
  westDoorIn: [-4.1, -6.9],
  westDoorOut: [-6.8, -6.9],
  westInner: [-6.8, -8.6],
  westNorth: [-13.3, -9.8],
  westSplit: [-13.3, -4.7],
  westCrossNorth: [-10.9, -4.7],
  westCrossSouth: [-10.9, -2.6],
  westCross: [-10.9, 0.8],
  westMid: [-13, 0.8],
  westArchive: [-13, 2.3],
  westSorterAisle: [-13, 5.36],
  westSorter: [-9.8, 5.36],
  westArchiveSouth: [-13, 8.2],
  westSouth: [-13, 10.9],
  deskWest: [-3.4, 5.5],
  southWest: [-3.4, 10.9],
  archiveIslandWestNorth: [-7.2, 10.9],
  archiveIslandWestSouth: [-7.2, 13.7],
  archiveIslandEastNorth: [-1.6, 10.9],
  archiveIslandEastSouth: [-1.6, 13.7],
  westPortalNorthIn: [-15.1, -10.8],
  westPortalNorthOut: [-18, -10.8],
  westWingNorth: [-19.2, -10.8],
  westPress: [-19.5, -12.7],
  westPortalMidIn: [-15.1, -2.3],
  westPortalMidOut: [-18, -2.3],
  westWingMid: [-19.2, -2.3],
  westPortalSouthIn: [-15.1, 8.6],
  westPortalSouthOut: [-18, 8.6],
  westWingSouth: [-19.2, 8.6],
  westWingFront: [-19.2, 14.7],
  westGallery: [-13.5, 14.7],
  westFront: [-13.5, 17.3],
  eastInside: [4.8, -9.8],
  eastDoorIn: [4.8, -6.3],
  eastDoorOut: [7.4, -6.3],
  eastInner: [7.4, -9.1],
  eastNorth: [13.5, -9.1],
  eastRear: [11.6, -15.1],
  eastRearWest: [7.5, -15.1],
  eastRearWestSouth: [7.5, -11.7],
  eastRearEast: [13.4, -15.1],
  eastRearEastSouth: [13.4, -11.7],
  eastRearDoorNorth: [11.6, -11.6],
  eastRearDoorSouth: [11.6, -9.3],
  eastSplit: [13.5, -3.1],
  eastCrossNorth: [11.6, -3.1],
  eastCrossSouth: [11.6, -0.7],
  eastCross: [11.6, 2.1],
  eastMid: [13.5, 2.1],
  eastAtlas: [13.5, 7.1],
  eastSouth: [13.5, 10.9],
  eastLanding: [13.5, 10.3],
  deskEast: [3.7, 5.5],
  southEast: [3.7, 10.9],
  eastPortalNorthIn: [15.1, -11.6],
  eastPortalNorthOut: [18, -11.6],
  eastWingNorth: [19.2, -11.6],
  eastIslandWestNorth: [18.4, -12.2],
  eastIslandWestSouth: [18.4, -5.9],
  eastIslandEastNorth: [21.8, -12.2],
  eastIslandEastSouth: [21.8, -5.9],
  eastPortalMidIn: [15.1, -3.1],
  eastPortalMidOut: [18, -3.1],
  eastWingMid: [19.2, -3.1],
  eastGardenNorth: [17.7, -3.1],
  eastGardenSouth: [17.7, 5.2],
  eastGarden: [19.2, 5],
  eastPortalSouthIn: [15.1, 7.82],
  eastPortalSouthOut: [18, 7.82],
  eastWingSouth: [19.2, 8.6],
  eastWingFront: [19.2, 14.7],
  eastGallery: [13.2, 14.7],
  eastFront: [13.2, 17.3],
  foyerNorth: [3.7, 13.7],
  foyer: [6.8, 14.1],
  front: [0.25, 17.3],
} satisfies Record<string, FloorPoint>;
type Aisle = keyof typeof aisles;
const chains: readonly (readonly Aisle[])[] = [
  ["desk", "south", "midEast", "midWest", "worldIslandSouth", "north"],
  [
    "north",
    "worldIslandNorth",
    "worldIslandWest",
    "westInside",
    "westDoorIn",
    "westDoorOut",
    "westInner",
    "westNorth",
  ],
  ["worldIslandSouth", "westDoorIn"],
  ["westNorth", "westSplit", "westCrossNorth", "westCrossSouth", "westCross", "westMid"],
  ["midWest", "westCross"],
  ["midWest", "westDoorIn"],
  [
    "westMid",
    "westArchive",
    "westSorterAisle",
    "westArchiveSouth",
    "westSouth",
    "archiveIslandWestNorth",
    "southWest",
    "deskWest",
    "south",
  ],
  ["westSorterAisle", "westSorter"],
  ["north", "eastInside", "eastDoorIn", "eastDoorOut", "eastInner", "eastNorth"],
  ["eastNorth", "eastSplit", "eastCrossNorth", "eastCrossSouth", "eastCross", "eastMid"],
  ["midEast", "eastCross"],
  ["midEast", "eastDoorIn"],
  ["eastMid", "eastAtlas", "eastSouth", "southEast", "deskEast", "south"],
  ["eastAtlas", "eastLanding", "eastSouth"],
  ["southWest", "southEast"],
  ["north", "rear", "westRear", "westRearDoorNorth", "westRearDoorSouth", "westNorth"],
  [
    "rear",
    "eastRearWest",
    "eastRear",
    "eastRearEast",
    "eastRearEastSouth",
    "eastRearDoorNorth",
    "eastRearDoorSouth",
    "eastNorth",
  ],
  ["eastRearWest", "eastRearWestSouth", "eastRearDoorNorth"],
  ["eastRearDoorSouth", "eastInner"],
  ["westNorth", "westPortalNorthIn", "westPortalNorthOut", "westWingNorth", "westPress"],
  ["westWingNorth", "westWingMid", "westWingSouth", "westWingFront"],
  ["westMid", "westPortalMidIn", "westPortalMidOut", "westWingMid"],
  ["westArchiveSouth", "westPortalSouthIn", "westPortalSouthOut", "westWingSouth"],
  ["westWingFront", "westGallery", "westFront", "front"],
  ["westSouth", "westGallery"],
  ["eastRearDoorNorth", "eastPortalNorthIn", "eastPortalNorthOut", "eastWingNorth"],
  [
    "eastWingNorth",
    "eastIslandWestNorth",
    "eastIslandWestSouth",
    "eastWingMid",
    "eastGardenNorth",
    "eastGardenSouth",
    "eastGarden",
  ],
  ["eastWingNorth", "eastIslandEastNorth", "eastIslandEastSouth", "eastWingMid"],
  ["eastSplit", "eastPortalMidIn", "eastPortalMidOut", "eastWingMid"],
  ["eastAtlas", "eastPortalSouthIn", "eastPortalSouthOut", "eastWingSouth"],
  ["eastGardenSouth", "eastPortalSouthOut"],
  ["eastGarden", "eastWingSouth", "eastWingFront", "eastGallery", "eastFront", "front"],
  ["eastSouth", "eastGallery"],
  ["archiveIslandWestNorth", "archiveIslandWestSouth", "front", "foyerNorth", "southEast"],
  ["southWest", "archiveIslandEastNorth", "archiveIslandEastSouth", "front"],
  ["foyerNorth", "foyer"],
];
const names = Object.keys(aisles) as Aisle[];
const points: FloorPoint[] = names.map((name) => aisles[name]);
const edges = chains
  .flatMap((chain) =>
    chain
      .slice(1)
      .map((name, index) => [names.indexOf(chain[index]!), names.indexOf(name)] as const),
  )
  .filter(([from, to]) => libraryPathClear(points[from]!, points[to]!));
const distance = (from: FloorPoint, to: FloorPoint) => Math.hypot(to[0] - from[0], to[1] - from[1]);

/** Случайная стоимость выбирает иной проход, не меняя проверенную геометрию рёбер. */
export function libraryRoute(
  from: FloorPoint,
  to: FloorPoint,
  random?: () => number,
): FloorPoint[] {
  const vertices = [...points];
  const links = edges.map(([a, b]) => ({
    a,
    b,
    weight: distance(vertices[a]!, vertices[b]!) * (1 + (random?.() ?? 0) * 2.6),
  }));
  const connect = (point: FloorPoint) => {
    const existing = vertices.findIndex((vertex) => distance(vertex, point) < 0.001);
    if (existing >= 0) return existing;
    const index = vertices.push(point) - 1;
    vertices
      .slice(0, index)
      .map((vertex, a) => ({ a, distance: distance(vertex, point) }))
      .filter(({ a }) => libraryPathClear(vertices[a]!, point))
      .toSorted((a, b) => a.distance - b.distance)
      .slice(0, 3)
      .forEach(({ a, distance: weight }) => links.push({ a, b: index, weight }));
    return index;
  };
  const start = connect(from);
  const end = connect(to);
  const costs = Array<number>(vertices.length).fill(Infinity);
  const previous = Array<number>(vertices.length).fill(-1);
  const visited = new Set<number>();
  costs[start] = 0;
  while (!visited.has(end)) {
    let current = -1;
    for (let index = 0; index < vertices.length; index++)
      if (!visited.has(index) && (current < 0 || costs[index]! < costs[current]!)) current = index;
    if (current < 0 || !Number.isFinite(costs[current])) return [];
    visited.add(current);
    for (const link of links) {
      const next = link.a === current ? link.b : link.b === current ? link.a : -1;
      if (next < 0 || visited.has(next)) continue;
      const cost = costs[current]! + link.weight;
      if (cost < costs[next]!) {
        costs[next] = cost;
        previous[next] = current;
      }
    }
  }
  const route: FloorPoint[] = [];
  for (let index = end; index >= 0; index = previous[index]!) route.unshift(vertices[index]!);
  return route;
}

export function libraryTrip(section: LibrarySection, random: () => number, previous?: FloorPoint) {
  const targets = SHELF_TARGETS[section].filter(({ point }) => point !== previous);
  const { point: target, heading } = targets[Math.floor(random() * targets.length)]!;
  const stops = {
    science: [aisles.westPress, aisles.westGallery],
    engineering: [aisles.eastGarden, aisles.foyer],
    world: [aisles.eastGarden, aisles.westGallery],
    corpus: [aisles.westPress, aisles.westGallery],
  }[section];
  const stop = random() < 0.28 ? stops[Math.floor(random() * stops.length)] : undefined;
  const outbound = libraryRoute(DESK, target, random);
  const homeward = stop
    ? [...libraryRoute(target, stop, random).slice(0, -1), ...libraryRoute(stop, DESK, random)]
    : libraryRoute(target, DESK, random);
  return {
    outbound,
    homeward,
    heading,
    target,
    stop,
  };
}
