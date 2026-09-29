import { Group, Vector3 } from "three";
import { batchArchitecture, type LibraryGeometry } from "./geometry";
import type { LibraryMaterials } from "./materials";
import type { FloorPoint } from "./layout";
import { libraryPathClear, libraryRoute } from "./navigation";
import { libraryFurniture } from "./furniture";
import type { AmbientRandom, AmbientScheduler } from "./ambientScheduler";

const HIDES: readonly FloorPoint[] = [
  [-14.9, -10.3],
  [-14.9, 7.5],
  [-3.7, 13.4],
  [14.9, 10.5],
  [14.9, -9.1],
  [1, -11.6],
];
const LAMPS: readonly [number, number, number][] = [
  [-14.58, 2.04, 10.22],
  [-1.48, 2.04, 9.02],
  [15.32, 2.04, 9.02],
];
const smooth = (value: number) => value * value * (3 - 2 * value);

export function ambientVisitors(
  g: LibraryGeometry,
  m: LibraryMaterials,
  root: Group,
  fixed: Group,
  scheduler: AmbientScheduler,
  random: AmbientRandom,
) {
  const furniture = libraryFurniture(g, m);
  const entrances: FloorPoint[] = [];
  HIDES.forEach(([x, z], index) => {
    const angle = index === 2 || index === 5 ? 0 : x < 0 ? Math.PI / 2 : -Math.PI / 2;
    const refuge = new Group();
    refuge.position.set(x, 0, z);
    refuge.rotation.y = angle;
    fixed.add(refuge);
    for (const side of [-1, 1]) g.box(refuge, m.wood, side * 0.41, 0.31, 0, 0.2, 0.62, 0.88);
    g.box(refuge, m.recess, 0, 0.3, -0.42, 0.67, 0.6, 0.05);
    for (let stack = 0; stack < 3; stack++)
      furniture.book(refuge, (stack % 2) * 0.07, 0.65 + stack * 0.12, 0, index + stack, 1.55);
    entrances.push([x + Math.sin(angle) * 0.85, z + Math.cos(angle) * 0.85]);
  });
  const mouse = new Group();
  mouse.name = "library-mouse";
  mouse.visible = false;
  mouse.scale.setScalar(1.12);
  root.add(mouse);
  const mouseBody = new Group();
  g.ball(mouseBody, m.oak, 0, 0.2, 0, 0.225, 0.18, 0.33);
  for (const side of [-1, 1]) {
    for (const z of [-0.19, 0.2])
      g.ball(mouseBody, m.claret, side * 0.15, 0.08, z, 0.071, 0.034, 0.085);
  }
  mouse.add(batchArchitecture(mouseBody));
  const head = new Group();
  head.position.set(0, 0.22, 0.25);
  mouse.add(head);
  const face = new Group();
  g.ball(face, m.oak, 0, 0, 0.14, 0.155, 0.13, 0.19);
  g.ball(face, m.claret, 0, -0.014, 0.315, 0.039, 0.025, 0.029);
  for (const side of [-1, 1]) {
    g.ball(face, m.oak, side * 0.12, 0.155, 0.12, 0.106, 0.13, 0.05);
    g.ball(face, m.claret, side * 0.12, 0.16, 0.153, 0.068, 0.084, 0.018);
    g.ball(face, m.ink, side * 0.105, 0.053, 0.235, 0.022);
    g.ball(face, m.light, side * 0.104, 0.061, 0.253, 0.007);
    g.rod(face, m.paper, [side * 0.075, -0.02, 0.255], [side * 0.235, 0.01, 0.29], 0.005);
  }
  head.add(batchArchitecture(face));
  const tail = new Group();
  tail.position.set(0, 0.15, -0.3);
  mouse.add(tail);
  const curve = new Group();
  for (let segment = 0; segment < 6; segment++) {
    const a = segment / 6;
    const b = (segment + 1) / 6;
    g.rod(
      curve,
      m.claret,
      [Math.sin(a * 4) * 0.095, -a * 0.065, -a * 0.55],
      [Math.sin(b * 4) * 0.095, -b * 0.065, -b * 0.55],
      0.018 - a * 0.011,
    );
  }
  tail.add(batchArchitecture(curve));

  const moth = new Group();
  moth.name = "reading-lamp-moth";
  moth.position.set(...LAMPS[0]!);
  moth.scale.setScalar(1.45);
  root.add(moth);
  g.ball(moth, m.brass, 0, 0, 0, 0.025, 0.03, 0.1);
  const wings: Group[] = [];
  for (const side of [-1, 1]) {
    const wing = new Group();
    wing.position.x = side * 0.017;
    moth.add(wing);
    const wingGeometry = new Group();
    g.ball(wingGeometry, m.paper, side * 0.093, 0, 0.015, 0.125, 0.014, 0.095);
    g.ball(wingGeometry, m.oak, side * 0.11, 0.012, 0.035, 0.025, 0.005, 0.028);
    wing.add(batchArchitecture(wingGeometry));
    wings.push(wing);
  }
  let mouseHome = Math.floor(random() * HIDES.length);
  let mouseDestination = mouseHome;
  let lastHome = -1;
  let mouseSerial = 0;
  let mouseRoute: FloorPoint[] = [];
  let waypoint = 0;
  let pause = 0;
  let pauseLength = 1;
  let rear = false;
  let mouseSpeed = 2.5;
  let escaped = false;
  const from: [number, number] = [0, 0];
  const candidate: [number, number] = [0, 0];
  let mothHome = 0;
  let mothDestination = 0;
  let mothSerial = 0;
  let flightTime = 0;
  let flightDuration = 12;
  let flightHover = 0;
  let flightHeight = 1;
  let flightArc = 1;
  const mothStart = new Vector3(...LAMPS[0]!);
  const mothEnd = new Vector3(...LAMPS[0]!);

  function startMouse(serial: number) {
    mouseSerial = serial;
    const destinations = HIDES.map((_, index) => index).filter(
      (index) => index !== mouseHome && index !== lastHome,
    );
    mouseDestination = destinations[Math.floor(random() * destinations.length)]!;
    const path = libraryRoute(entrances[mouseHome]!, entrances[mouseDestination]!, random);
    if (!path.length) {
      scheduler.finish("mouse");
      return;
    }
    mouseRoute = [HIDES[mouseHome]!, ...path, HIDES[mouseDestination]!];
    waypoint = 1;
    const home = HIDES[mouseHome]!;
    mouse.position.set(home[0], 0, home[1]);
    const entry = entrances[mouseHome]!;
    mouse.rotation.set(0, Math.atan2(entry[0] - home[0], entry[1] - home[1]), 0);
    pauseLength = pause = 0.7 + random() * 1.1;
    rear = false;
    escaped = false;
    mouseSpeed = 2.5 + random() * 1.3;
    mouse.userData.destination = mouseDestination;
    mouse.userData.origin = mouseHome;
  }
  function startMoth(serial: number, variant: number) {
    mothSerial = serial;
    mothDestination = (mothHome + 1 + Math.floor(random() * 2)) % LAMPS.length;
    mothStart.set(...LAMPS[mothHome]!);
    mothEnd.set(...LAMPS[mothDestination]!);
    flightTime = 0;
    flightDuration = mothStart.distanceTo(mothEnd) / (2.6 + random() * 1.2) + 4 + random() * 3;
    flightHover = variant % 2 ? 1.4 + random() * 1.8 : 0;
    flightHeight = 0.6 + random() * 1.3;
    flightArc = (random() > 0.5 ? 1 : -1) * (0.45 + random() * 1.3);
  }
  function evade(actor: Vector3, dt: number) {
    const dx = mouse.position.x - actor.x;
    const dz = mouse.position.z - actor.z;
    const distance = Math.hypot(dx, dz);
    if (distance > 1.35) return false;
    from[0] = mouse.position.x;
    from[1] = mouse.position.z;
    const angle = Math.atan2(dx, dz);
    for (let direction = 0; direction < 3; direction++) {
      const offset = direction === 0 ? 0 : direction === 1 ? Math.PI / 2 : -Math.PI / 2;
      candidate[0] = from[0] + Math.sin(angle + offset) * dt * 3.1;
      candidate[1] = from[1] + Math.cos(angle + offset) * dt * 3.1;
      if (!libraryPathClear(from, candidate, 0.45)) continue;
      mouse.position.set(candidate[0], 0, candidate[1]);
      mouse.rotation.y = angle + offset;
      mouse.userData.pose = "yield";
      escaped = true;
      return true;
    }
    mouse.userData.pose = "listen";
    return true;
  }
  function advanceMouse(dt: number, time: number, actor: Vector3) {
    const event = scheduler.event("mouse");
    if (!event) {
      mouse.visible = false;
      return;
    }
    if (event.serial !== mouseSerial) startMouse(event.serial);
    if (!scheduler.event("mouse")) return;
    mouse.visible = true;
    mouse.rotation.x = 0;
    if (evade(actor, dt)) return;
    if (escaped) {
      from[0] = mouse.position.x;
      from[1] = mouse.position.z;
      mouseRoute = [
        ...libraryRoute(from, entrances[mouseDestination]!, random),
        HIDES[mouseDestination]!,
      ];
      waypoint = 1;
      escaped = false;
      pause = 0;
    }
    if (pause > 0) {
      pause = Math.max(0, pause - dt);
      const gesture = Math.sin((1 - pause / pauseLength) * Math.PI);
      mouse.userData.pose = rear ? "rear" : "sniff";
      mouse.position.y = rear ? gesture * 0.17 : 0;
      mouse.rotation.x = rear ? -gesture * 0.53 : 0;
      head.rotation.y = Math.sin((pauseLength - pause) * 4.3) * 0.2 * gesture;
      tail.rotation.y = Math.sin((pauseLength - pause) * 2.7) * 0.09;
      return;
    }
    const target = mouseRoute[waypoint];
    if (!target) {
      mouse.visible = false;
      lastHome = mouseHome;
      mouseHome = mouseDestination;
      mouse.userData.home = mouseHome;
      scheduler.finish("mouse");
      return;
    }
    const dx = target[0] - mouse.position.x;
    const dz = target[1] - mouse.position.z;
    const distance = Math.hypot(dx, dz);
    const step = Math.min(distance, mouseSpeed * dt);
    if (distance > 0.001) {
      mouse.position.x += (dx / distance) * step;
      mouse.position.z += (dz / distance) * step;
      const heading = Math.atan2(dx, dz);
      const turn = Math.atan2(
        Math.sin(heading - mouse.rotation.y),
        Math.cos(heading - mouse.rotation.y),
      );
      mouse.rotation.y += turn * Math.min(1, dt * 13);
    }
    mouse.position.y = Math.sin(time * 24) * 0.018;
    head.rotation.y = 0;
    tail.rotation.y = Math.sin(time * 14) * 0.14;
    mouse.userData.pose = "walk";
    if (distance <= mouseSpeed * dt + 0.001) {
      waypoint++;
      if (waypoint < mouseRoute.length - 1 && random() < 0.32) {
        pauseLength = pause = 1.1 + random() * 2.1;
        rear = random() < 0.46;
      }
    }
  }
  function advanceMoth(dt: number, time: number) {
    const event = scheduler.event("moth");
    if (event && event.serial !== mothSerial) startMoth(event.serial, event.variant);
    if (!event) {
      moth.position.set(...LAMPS[mothHome]!);
      moth.rotation.set(0, 0.32, 0);
      for (let index = 0; index < wings.length; index++)
        wings[index]!.rotation.z = (index ? -1 : 1) * 0.35;
      return;
    }
    flightTime += dt;
    const half = flightDuration / 2;
    const movingTime =
      flightTime < half
        ? flightTime
        : flightTime < half + flightHover
          ? half
          : flightTime - flightHover;
    const progress = Math.min(1, movingTime / flightDuration);
    const blend = smooth(progress);
    const lift = Math.sin(Math.PI * blend);
    const dx = mothEnd.x - mothStart.x;
    const dz = mothEnd.z - mothStart.z;
    const span = Math.max(1, Math.hypot(dx, dz));
    moth.position.set(
      mothStart.x + dx * blend + (dz / span) * lift * flightArc,
      mothStart.y + (mothEnd.y - mothStart.y) * blend + lift * flightHeight,
      mothStart.z + dz * blend - (dx / span) * lift * flightArc,
    );
    moth.rotation.set(lift * 0.16, Math.atan2(dx, dz), Math.sin(time * 2.4) * lift * 0.16);
    for (let index = 0; index < wings.length; index++)
      wings[index]!.rotation.z = (index ? -1 : 1) * Math.sin(time * 19 + event.variant) * 0.82;
    if (progress >= 1) {
      mothHome = mothDestination;
      scheduler.finish("moth");
    }
  }
  function advance(dt: number, actor: Vector3, still: boolean) {
    if (still) {
      mouse.visible = false;
      moth.position.set(...LAMPS[mothHome]!);
      moth.rotation.set(0, 0.32, 0);
      for (let index = 0; index < wings.length; index++)
        wings[index]!.rotation.z = (index ? -1 : 1) * 0.35;
      return;
    }
    const time = scheduler.time();
    advanceMouse(dt, time, actor);
    advanceMoth(dt, time);
  }
  return { advance };
}
