import { expect, test } from "bun:test";
import { MeshStandardMaterial, Vector3 } from "three";
import { libraryArchivist, libraryDeskWork, libraryLadder } from "./archivist";
import { libraryGeometry } from "./geometry";
import { libraryMotion, type LibrarySceneState } from "./motion";
import type { LibraryMaterials } from "./materials";
import {
  DESK,
  SEAT,
  LIBRARY_PARTITIONS,
  SHELF_ISLANDS,
  SHELF_TARGETS,
  type FloorPoint,
} from "./layout";
import { libraryPathClear, libraryRoute, libraryTrip } from "./navigation";
import { libraryRandom } from "./random";
import { LIBRARY_SECTIONS } from "../../liveSearchProgress";
import { libraryPatrol } from "./patrol";

const materialNames = [
  "wood",
  "oak",
  "paper",
  "brass",
  "navy",
  "sage",
  "claret",
  "ink",
  "plaster",
  "floor",
  "floorAlt",
  "recess",
  "light",
  "active",
];

function crossesSolidShelf(from: FloorPoint, to: FloorPoint) {
  return [...LIBRARY_PARTITIONS, ...SHELF_ISLANDS].some((wall) => {
    const axis = wall.axis === "x" ? 0 : 1;
    const across = axis === 0 ? 1 : 0;
    const delta = to[axis] - from[axis];
    if (Math.abs(delta) < 0.0001) return false;
    const fraction = (wall.at - from[axis]) / delta;
    if (fraction <= 0 || fraction >= 1) return false;
    const crossing = from[across] + (to[across] - from[across]) * fraction;
    return (
      crossing > wall.from &&
      crossing < wall.to &&
      !(
        "gaps" in wall &&
        wall.gaps.some(([start, end]) => crossing > start + 0.56 && crossing < end - 0.56)
      )
    );
  });
}

function safeRoute(route: readonly FloorPoint[]) {
  return (
    route.length > 0 &&
    route
      .slice(1)
      .every(
        (point, index) =>
          libraryPathClear(route[index]!, point) && !crossesSolidShelf(route[index]!, point),
      )
  );
}

test("seeded journeys explore the wings while shelves, shelters and work stations stay connected through archways", () => {
  const random = libraryRandom(1729);
  const visited: FloorPoint[] = [];
  for (const section of LIBRARY_SECTIONS) {
    let previous: FloorPoint | undefined;
    const journeys = Array.from({ length: 40 }, () => {
      const journey = libraryTrip(section, random, previous);
      expect(journey.target).not.toBe(previous);
      previous = journey.target;
      return journey;
    });
    visited.push(...journeys.flatMap(({ outbound, homeward }) => [...outbound, ...homeward]));
    expect(new Set(journeys.map(({ outbound }) => JSON.stringify(outbound))).size).toBeGreaterThan(
      2,
    );
    expect(
      journeys.every(
        ({ outbound, homeward, target }) =>
          outbound[0] === DESK &&
          outbound.at(-1) === target &&
          homeward[0] === target &&
          homeward.at(-1) === DESK &&
          safeRoute(outbound) &&
          safeRoute(homeward),
      ),
    ).toBe(true);
    expect(new Set(journeys.map(({ target }) => target)).size).toBe(SHELF_TARGETS[section].length);
    expect(
      journeys.every(({ target, heading }) =>
        SHELF_TARGETS[section].some((shelf) => shelf.point === target && shelf.heading === heading),
      ),
    ).toBe(true);
    // У внутренних полок направление должно вести к книгам, оставляя место для подъёма.
    expect(
      SHELF_TARGETS[section].slice(2).every(({ point, heading }) => {
        const toward = (distance: number): FloorPoint => [
          point[0] + Math.sin(heading) * distance,
          point[1] + Math.cos(heading) * distance,
        ];
        return (
          libraryPathClear(point, toward(0.42), 0.3) && !libraryPathClear(point, toward(1.3), 0)
        );
      }),
    ).toBe(true);
  }
  expect(libraryTrip("science", libraryRandom(27))).toEqual(
    libraryTrip("science", libraryRandom(27)),
  );
  expect(visited.some(([x]) => x < -19)).toBe(true);
  expect(visited.some(([x]) => x > 17)).toBe(true);
  expect(visited.some(([, z]) => z > 13)).toBe(true);
  expect(visited.some(([, z]) => z < -13)).toBe(true);
  const destinations: FloorPoint[] = [
    DESK,
    ...Object.values(SHELF_TARGETS)
      .flat()
      .map(({ point }) => point),
    [-14.9, -10.3],
    [-14.9, 7.5],
    [-3.7, 13.4],
    [-3.7, 14.25],
    [14.9, 10.5],
    [14.9, -9.1],
    [1, -11.6],
    [-9.8, 5.36],
    [3.3, 8.4],
    [-8, 9],
    [12.3, 9],
    [-19.5, -12.7],
    [6.8, 14.1],
    [19.2, 5],
    [-7.7, 14.9],
    [-18.1, 3.95],
    [-14.8, 8.95],
    [15.1, 7.85],
  ];
  expect(
    destinations.every((from) => destinations.every((to) => safeRoute(libraryRoute(from, to)))),
  ).toBe(true);
  expect(libraryPathClear([-7, -10], [-4, -10])).toBe(false);
  expect(libraryPathClear([-7, -6.9], [-4, -6.9])).toBe(true);
  expect(libraryPathClear([8, -7.2], [12, -7.2])).toBe(false);
  expect(libraryPathClear([-18, -6], [-15, -6])).toBe(false);
  expect(libraryPathClear([-18, -10.8], [-15, -10.8])).toBe(true);
  expect(libraryPathClear([18, 2.5], [22.4, 2.5])).toBe(false);
});

test("one source still surveys every shelf before reshuffling, with safe routes home from inspection stops", () => {
  const patrol = libraryPatrol(libraryRandom(2718));
  const visited = new Set<FloorPoint>();
  const tours = Array.from({ length: 24 }, () => patrol("science"));
  for (const tour of tours) {
    visited.add(tour.target);
    if (!tour.inspection) continue;
    const { point, approach, onward, home } = tour.inspection;
    visited.add(point);
    expect([approach, onward, home].every(safeRoute)).toBe(true);
    expect(approach[0]).toBe(DESK);
    expect(approach.at(-1)).toBe(point);
    expect(onward[0]).toBe(point);
    expect(onward.at(-1)).toBe(tour.target);
    expect(home[0]).toBe(point);
    expect(home.at(-1)).toBe(DESK);
  }
  expect(visited.size).toBe(Object.values(SHELF_TARGETS).flat().length);
  expect(tours.filter(({ inspection }) => inspection).length).toBeGreaterThan(20);
});

test("the archivist conserves deliveries, waits safely and finishes a remote return within 8.2 seconds", () => {
  const material = new MeshStandardMaterial();
  const materials = Object.fromEntries(
    materialNames.map((name) => [name, material]),
  ) as LibraryMaterials;
  const geometry = libraryGeometry();
  const actor = libraryArchivist(geometry, materials);
  const ladder = libraryLadder(geometry, materials);
  const work = libraryDeskWork(geometry, materials);
  const state: LibrarySceneState = {
    activity: "collect",
    shelf: "science",
    found: 0,
    consulted: 1,
  };
  const motion = libraryMotion(actor, ladder, work, state, libraryRandom(1729));
  let pose = motion.advance(0);
  for (let i = 0; i < 1200 && pose !== "climb"; i++) pose = motion.advance(0.05);
  for (let i = 0; i < 15; i++) motion.advance(0.05);
  expect(actor.root.position.y).toBeGreaterThan(0.1);
  motion.update({ ...state, activity: "wait" });
  const waitingAt = actor.root.position.clone();
  for (let i = 0; i < 100; i++) expect(motion.advance(0.05)).toBe("wait");
  expect(actor.root.position.equals(waitingAt)).toBe(true);

  motion.update({ ...state, shelf: "world", consulted: 2 });
  const poses = new Set<string>();
  const reached = new Set<number>();
  let highestDelivery = 0;
  let previousCount = 0;
  let maximumStep = 0;
  let leftAisle = false;
  for (let i = 0; i < 7200; i++) {
    const previous = actor.root.position.clone();
    pose = motion.advance(0.05);
    poses.add(pose);
    maximumStep = Math.max(maximumStep, actor.root.position.distanceTo(previous));
    const { x, z } = actor.root.position;
    leftAisle ||=
      !libraryPathClear([previous.x, previous.z], [x, z], 0.3) ||
      crossesSolidShelf([previous.x, previous.z], [x, z]);
    if (pose === "reach") reached.add(Math.round(x));
    if (pose === "deliver") highestDelivery = Math.max(highestDelivery, actor.book.position.y);
    expect(motion.deliveries).toBeGreaterThanOrEqual(previousCount);
    previousCount = motion.deliveries;
  }
  expect(maximumStep).toBeLessThan(0.2);
  expect(leftAisle).toBe(false);
  expect([...poses]).toEqual(
    expect.arrayContaining(["browse", "compare", "reach", "deliver", "descend"]),
  );
  expect(
    ["look", "adjust", "breathe", "inspect", "wipe", "pickup"].filter((name) => poses.has(name))
      .length,
  ).toBeGreaterThanOrEqual(3);
  expect(reached.size).toBeGreaterThanOrEqual(3);
  expect(highestDelivery).toBeGreaterThan(1.15);
  expect(work.pile.length).toBeGreaterThanOrEqual(3);
  const delivered = [...work.pile];
  motion.update({ activity: "analyze", found: 1, consulted: 0 });
  for (let i = 0; i < 1600; i++) motion.advance(0.05);
  expect(actor.root.position.x).toBeCloseTo(DESK[0]);
  expect(actor.root.position.z).toBeCloseTo(DESK[1]);
  expect(motion.advance(0.05)).toBe("read");
  expect(work.folder.visible).toBe(true);
  expect(delivered.every((book) => book.visible && book.parent === work.root)).toBe(true);
  motion.update({ activity: "write", found: 1, consulted: 3 });
  expect(motion.advance(0.05)).toBe("write");
  expect(actor.pencil.visible).toBe(true);

  motion.update(state);
  for (let i = 0; i < 1400; i++) if (motion.advance(0.05) === "descend") break;
  expect(actor.book.visible).toBe(true);
  expect(actor.root.position.distanceTo(new Vector3(DESK[0], 0, DESK[1]))).toBeGreaterThan(12);
  const beforeFinish = motion.deliveries;
  motion.update({ ...state, activity: "finish" });
  const finale = new Set<string>();
  for (let i = 0; i < 82 && !motion.finished; i++) {
    const previous = actor.root.position.clone();
    finale.add(motion.advance(0.1));
    const { x, z } = actor.root.position;
    leftAisle ||=
      !libraryPathClear([previous.x, previous.z], [x, z], 0.3) ||
      crossesSolidShelf([previous.x, previous.z], [x, z]);
  }
  expect(leftAisle).toBe(false);
  expect(motion.finished).toBe(true);
  expect([...finale]).toEqual(expect.arrayContaining(["chair", "sit", "open", "settle"]));
  expect(actor.root.position.x).toBeCloseTo(SEAT[0]);
  expect(actor.root.position.z).toBeCloseTo(SEAT[1]);
  expect(work.chair.position.z).toBeCloseTo(SEAT[1]);
  expect(work.opening.rotation.z).toBeCloseTo(0);
  expect(motion.deliveries).toBe(beforeFinish + 1);
  motion.update({ ...state, activity: "finish" });
  motion.advance(1);
  expect(motion.deliveries).toBe(beforeFinish + 1);
  expect(delivered.every((book) => book.visible && book.parent === work.root)).toBe(true);
  geometry.dispose();
  material.dispose();
});
