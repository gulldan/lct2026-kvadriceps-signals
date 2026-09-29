import { expect, test } from "bun:test";
import { Group, Mesh, MeshStandardMaterial, Points, Vector3 } from "three";
import { ambientScheduler, type AmbientKind } from "./ambientScheduler";
import { ambientVisitors } from "./ambientVisitors";
import { libraryEnvironment } from "./environment";
import { libraryGeometry } from "./geometry";
import type { LibraryMaterials } from "./materials";
import { libraryPathClear } from "./navigation";
import { libraryRandom } from "./random";
import { worldResidents } from "./worldResidents";
import { ambientCuriosities } from "./ambientCuriosities";
import { libraryExhibits } from "./exhibits";
import { libraryGallery } from "./gallery";

const curiosityKinds = [
  "domino",
  "owl",
  "bookmark",
  "planetarium",
  "cuckoo",
  "globe",
  "origami",
  "beetle",
  "rose",
  "penguin",
  "newton",
  "portrait",
] as const;

const kinds: AmbientKind[] = [
  "wind",
  "mouse",
  "moth",
  "drawer",
  "scroll",
  "feather",
  "dust",
  "tea",
  "sort",
  "letter",
  "cart",
  "lamp",
  "reader",
  "pneumatic",
  "cat",
  "chime",
  ...curiosityKinds,
];
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

function simulate(seed: number, materials: LibraryMaterials) {
  const geometry = libraryGeometry();
  const root = new Group();
  const random = libraryRandom(seed);
  const scheduler = ambientScheduler(random);
  const fixed = new Group();
  const visitors = ambientVisitors(geometry, materials, root, fixed, scheduler, random);
  const curiosities = ambientCuriosities(geometry, materials, root, scheduler);
  const residents = worldResidents(geometry, materials, root, fixed, scheduler, random);
  const exhibits = libraryExhibits(geometry, materials, root, scheduler);
  const gallery = libraryGallery(geometry, materials, root, scheduler);
  const scenes = curiosityKinds.map((kind) => root.getObjectByName(`curiosity-${kind}`)!);
  const curiosityPhases = scenes.map(() => new Set<string>());
  const origami = root.getObjectByName("curiosity-origami")!;
  const cat = root.getObjectByName("reading-room-cat")!;
  const attention = new Set<string>();
  const paperJourneys: [number, number][] = [];
  let previousPaperJourneys = 0;
  const clerk = root.getObjectByName("library-clerk")!;
  const binder = root.getObjectByName("library-binder")!;
  const cart = root.getObjectByName("bookbinders-cart")!;
  const deliveries = root.getObjectByName("library-world-deliveries")!;
  const mouse = root.getObjectByName("library-mouse")!;
  const actor = new Vector3(0.25, 0, 7.22);
  const seen = new Set<number>();
  const trace: [AmbientKind, number, number][] = [];
  const finished = new Map<AmbientKind, number>();
  const active = new Set<AmbientKind>();
  const arrivals: [number, number, number][] = [];
  const poses = new Set<string>();
  const previous: [number, number] = [0, 0];
  const current: [number, number] = [0, 0];
  let departure = new Vector3();
  let visible = false;
  let safe = true;
  let rested = true;
  let interrupted = false;
  let actorLeavesAt = 0;
  let quietStarted = -1;
  let maxActive = 0;
  let residentInterrupted = false;
  const residentPoses = new Set<string>();
  const readerPoses = new Set<string>();
  const reader = root.getObjectByName("library-reader")!;
  const previousPeople = [clerk.position.clone(), binder.position.clone(), cart.position.clone()];
  let durableDeliveries = true;
  let previousLetters = 0;
  let previousBooks = 0;
  for (let frame = 0; frame < 43_200; frame++) {
    scheduler.advance(1 / 30);
    const time = scheduler.time();
    for (const kind of kinds) {
      const event = scheduler.event(kind);
      if (event && !seen.has(event.serial)) {
        seen.add(event.serial);
        trace.push([kind, event.variant, Math.round(event.start * 1000)]);
        const rest = finished.get(kind);
        if (rest !== undefined && event.start - rest < 14.9) rested = false;
      }
    }
    if (!interrupted && time > 50 && visible && mouse.userData.pose === "walk") {
      interrupted = true;
      actor.copy(mouse.position);
      actor.x += 0.7;
      actorLeavesAt = time + 1.2;
    }
    if (actorLeavesAt && time >= actorLeavesAt) {
      actor.set(0.25, 0, 7.22);
      actorLeavesAt = 0;
    }
    if (!residentInterrupted && !actorLeavesAt && time > 80 && clerk.userData.pose === "walk") {
      residentInterrupted = true;
      actor.copy(clerk.position);
      actor.x += 0.7;
      actorLeavesAt = time + 1.2;
    }
    visitors.advance(1 / 30, actor, false);
    curiosities.advance(false);
    residents.advance(1 / 30, actor, false);
    exhibits.advance(false, actor);
    gallery.advance(false, actor);
    for (let index = 0; index < scenes.length; index++)
      curiosityPhases[index]!.add(String(scenes[index]!.userData.phase));
    attention.add(String(cat.userData.attention));
    if (Number(origami.userData.completed) > previousPaperJourneys) {
      previousPaperJourneys = Number(origami.userData.completed);
      paperJourneys.push([Number(origami.userData.origin), Number(origami.userData.destination)]);
    }
    maxActive = Math.max(maxActive, kinds.filter((kind) => scheduler.event(kind)).length);
    for (const person of [clerk, binder]) residentPoses.add(String(person.userData.pose));
    readerPoses.add(String(reader.userData.pose));
    [clerk, binder, cart].forEach((person, index) => {
      const old = previousPeople[index]!;
      if (!libraryPathClear([old.x, old.z], [person.position.x, person.position.z], 0.3))
        safe = false;
      if (old.distanceTo(person.position) > 0.11) safe = false;
      old.copy(person.position);
    });
    const letterCount = Number(deliveries.userData.letters),
      bookCount = Number(deliveries.userData.books);
    if (letterCount < previousLetters || bookCount < previousBooks) durableDeliveries = false;
    previousLetters = letterCount;
    previousBooks = bookCount;
    if (mouse.visible && !visible) departure = mouse.position.clone();
    if (mouse.visible) {
      poses.add(String(mouse.userData.pose));
      current[0] = mouse.position.x;
      current[1] = mouse.position.z;
      if (visible && !libraryPathClear(previous, current, 0.24)) safe = false;
    }
    if (visible && !mouse.visible) {
      arrivals.push([
        Number(mouse.userData.origin),
        Number(mouse.userData.destination),
        departure.distanceTo(mouse.position),
      ]);
      quietStarted = time;
    }
    if (mouse.visible && !visible && quietStarted >= 0 && time - quietStarted < 27.9)
      rested = false;
    visible = mouse.visible;
    previous[0] = mouse.position.x;
    previous[1] = mouse.position.z;
    for (const kind of kinds) {
      if (scheduler.event(kind)) active.add(kind);
      else if (active.delete(kind)) finished.set(kind, time);
    }
  }
  visitors.advance(0, actor, true);
  curiosities.advance(true);
  residents.advance(0, actor, true);
  exhibits.advance(true, actor);
  gallery.advance(true, actor);
  const quiet = !mouse.visible;
  root.traverse((object) => {
    if (object instanceof Mesh) object.geometry.dispose();
  });
  geometry.dispose();
  return {
    trace,
    arrivals,
    poses: [...poses].toSorted(),
    residentPoses: [...residentPoses].toSorted(),
    readerPoses: [...readerPoses].toSorted(),
    safe,
    rested,
    quiet,
    maxActive,
    durableDeliveries,
    letters: previousLetters,
    books: previousBooks,
    clerkCompleted: Number(clerk.userData.completed ?? 0),
    binderCompleted: Number(binder.userData.completed ?? 0),
    curiosities: curiosityKinds.map((kind, index) => ({
      kind,
      phases: [...curiosityPhases[index]!].toSorted(),
      completed: Number(scenes[index]!.userData.completed),
    })),
    paperJourneys,
    attention: [...attention].toSorted(),
    repairs: Number(root.getObjectByName("curiosity-beetle")!.userData.repairs ?? 0),
  };
}

function quietComposition(root: Group) {
  const values: number[] = [];
  root.traverse((object) => {
    values.push(
      object.visible ? 1 : 0,
      object.position.x,
      object.position.y,
      object.position.z,
      object.rotation.x,
      object.rotation.y,
      object.rotation.z,
    );
  });
  return values;
}

test("ambient episodes vary by session, respect rests and complete safe journeys between different refuges", () => {
  const material = new MeshStandardMaterial();
  const materials = Object.fromEntries(
    materialNames.map((name) => [name, material]),
  ) as LibraryMaterials;
  const first = simulate(1729, materials);
  expect(simulate(1729, materials)).toEqual(first);
  expect(simulate(2718, materials).trace).not.toEqual(first.trace);
  expect(new Set(first.trace.map(([kind]) => kind)).size).toBe(kinds.length);
  expect(
    first.trace.every(([kind], index) => index === 0 || kind !== first.trace[index - 1]![0]),
  ).toBe(true);
  expect(first.arrivals.length).toBeGreaterThanOrEqual(4);
  expect(first.arrivals.every(([from, to, distance]) => from !== to && distance > 5)).toBe(true);
  expect(new Set(first.arrivals.map(([from, to]) => `${from}:${to}`)).size).toBeGreaterThanOrEqual(
    4,
  );
  expect(first.poses).toEqual(expect.arrayContaining(["walk", "sniff", "rear", "yield"]));
  expect(first.safe && first.rested && first.quiet).toBe(true);
  expect(first.maxActive).toBeLessThanOrEqual(3);
  expect(first.durableDeliveries).toBe(true);
  expect(first.letters).toBeGreaterThanOrEqual(2);
  expect(first.books).toBeGreaterThanOrEqual(4);
  expect(first.readerPoses).toEqual(expect.arrayContaining(["read", "doze", "wake"]));
  expect(first.clerkCompleted).toBeGreaterThanOrEqual(4);
  expect(first.binderCompleted).toBeGreaterThanOrEqual(2);
  expect(first.residentPoses).toEqual(
    expect.arrayContaining(["sort", "feed", "deliver", "unload", "polish", "bind", "yield"]),
  );
  expect(first.curiosities.every((story) => story.completed >= 1 && story.phases.length >= 4)).toBe(
    true,
  );
  expect(first.paperJourneys.length).toBeGreaterThanOrEqual(2);
  expect(first.paperJourneys.every(([from, to]) => from !== to)).toBe(true);
  expect(new Set(first.paperJourneys.map(([, to]) => to)).size).toBeGreaterThanOrEqual(2);
  expect(first.attention).toEqual(expect.arrayContaining(["mouse", "bookmark", "origami"]));
  expect(first.readerPoses).toContain("wonder");
  expect(first.repairs).toBeGreaterThanOrEqual(1);

  const geometry = libraryGeometry();
  const environment = libraryEnvironment(geometry, materials, 1729);
  const candle = environment.root.getObjectByName("candle-flame")!;
  const actor = new Vector3(-3.7, 0, -11);
  environment.advance(0, actor);
  for (let frame = 0; frame < 30; frame++) {
    actor.z += 0.06;
    environment.advance(0.05, actor);
  }
  expect(Math.abs(candle.rotation.x)).toBeGreaterThan(0.05);
  for (let frame = 0; frame < 1200; frame++) environment.advance(0.05, actor);
  environment.advance(0, actor, true);
  const still = quietComposition(environment.root);
  for (let frame = 0; frame < 30; frame++) environment.advance(0.1, actor, true);
  expect(quietComposition(environment.root)).toEqual(still);
  expect(candle.rotation.x).toBe(0);
  expect(candle.rotation.z).toBe(0);
  let finite = true;
  environment.root.traverse((object) => {
    if (object instanceof Mesh || object instanceof Points) {
      const values = object.geometry.attributes.position!.array;
      for (const value of values) if (!Number.isFinite(value)) finite = false;
    }
  });
  expect(finite).toBe(true);
  environment.dispose();
  environment.root.traverse((object) => {
    if (object instanceof Mesh) object.geometry.dispose();
  });
  geometry.dispose();
  material.dispose();
});
