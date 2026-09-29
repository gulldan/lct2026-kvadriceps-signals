import { Group, Vector3 } from "three";
import type { LibraryGeometry } from "./geometry";
import type { LibraryMaterials } from "./materials";
import type { FloorPoint } from "./layout";
import { libraryPathClear, libraryRoute } from "./navigation";
import type { AmbientKind, AmbientRandom, AmbientScheduler } from "./ambientScheduler";
import { worldCart, worldCat, worldResident, type WorldResident } from "./worldModels";
import { worldProps } from "./worldProps";

type Action =
  | "walk"
  | "sort"
  | "pickup"
  | "deliver"
  | "load"
  | "unload"
  | "polish"
  | "feed"
  | "bind"
  | "nod";
type Step = { action: Action; point?: FloorPoint; duration?: number; heading?: number };
type Worker = {
  model: WorldResident;
  rank: number;
  kind?: AmbientKind;
  serial: number;
  steps: Step[];
  index: number;
  elapsed: number;
  route: FloorPoint[];
  waypoint: number;
  escaped: boolean;
  letter: boolean;
  cart: boolean;
  greeting: number;
  greetAfter: number;
  delivered: number;
  receiver: number;
};
const SORT: FloorPoint = [-9.8, 5.36];
const ARCHIVE: FloorPoint = [-11.4, 6.5];
const PRESS: FloorPoint = [-19.5, -12.7];
const BINDING: FloorPoint = [-18.1, 3.95];
const DESTINATIONS: readonly FloorPoint[] = [
  [3.3, 8.4],
  [-7.7, 14.9],
  [6.8, 14.1],
];
const LAMPS: readonly FloorPoint[] = [
  [-14.8, 8.95],
  [15.1, 7.85],
];
const LAMP_CLOTH = [
  [-14.8, 1.9, 10.2],
  [15.1, 1.9, 9],
] as const;
const walk = (point: FloorPoint): Step => ({ action: "walk", point });
const act = (action: Action, duration: number, heading = 0): Step => ({
  action,
  duration,
  heading,
});

const worker = (model: WorldResident, rank: number): Worker => ({
  model,
  rank,
  serial: 0,
  steps: [],
  index: 0,
  elapsed: 0,
  route: [],
  waypoint: 0,
  escaped: false,
  letter: false,
  cart: false,
  greeting: 0,
  greetAfter: 0,
  delivered: 0,
  receiver: 0,
});

function pose(person: Worker, action: Action | "idle" | "yield", time: number, progress = 0) {
  const model = person.model;
  const walking = action === "walk";
  const working = action === "sort" || action === "bind" || action === "feed";
  model.root.userData.pose = action;
  model.body.position.y = 0.7 + (walking ? Math.abs(Math.sin(time * 6.2)) * 0.029 : 0);
  model.body.rotation.x = action === "polish" ? -0.12 : working ? 0.12 : 0;
  model.head.rotation.x = working
    ? 0.2
    : action === "nod"
      ? Math.sin(progress * Math.PI) * 0.24
      : 0;
  model.head.rotation.y = 0;
  model.held.position.y = working ? 1.09 : 0.94;
  model.book.visible = action === "bind" || action === "unload";
  model.letter.visible =
    (person.letter && action !== "deliver") || (action === "feed" && progress < 0.3);
  model.brush.visible = false;
  for (let index = 0; index < 2; index++) {
    const side = index ? 1 : -1;
    model.feet[index]!.rotation.x = walking ? Math.sin(time * 6.2 + index * Math.PI) * 0.39 : 0;
    model.arms[index]!.rotation.x = working
      ? -1.05 + Math.sin(time * 4 + index * 1.3) * 0.17
      : action === "polish"
        ? -2.05 + Math.sin(time * 3.6) * 0.23
        : person.letter || person.cart
          ? -0.82
          : walking
            ? Math.sin(time * 6.2 + index * Math.PI) * 0.27
            : 0;
    model.arms[index]!.rotation.z = working ? side * 0.16 : 0;
  }
  if (person.greeting > 0) {
    model.head.rotation.x += Math.sin((1 - person.greeting / 1.35) * Math.PI) * 0.23;
    if (!working && !person.cart && !person.letter)
      model.arms[1]!.rotation.z = -Math.sin((1 - person.greeting / 1.35) * Math.PI) * 0.8;
  }
}

const CLERK_JOBS = ["sort", "letter", "pneumatic"] as const;
const BINDER_JOBS = ["cart", "lamp"] as const;

export function worldResidents(
  g: LibraryGeometry,
  m: LibraryMaterials,
  root: Group,
  fixed: Group,
  scheduler: AmbientScheduler,
  random: AmbientRandom,
) {
  const clerkModel = worldResident(g, m, "clerk");
  const binderModel = worldResident(g, m, "binder");
  const reader = worldResident(g, m, "reader");
  root.add(clerkModel.root, binderModel.root, reader.root);
  clerkModel.root.position.set(SORT[0], 0, SORT[1]);
  binderModel.root.position.set(PRESS[0], 0, PRESS[1]);
  reader.root.position.set(-10, 0.05, 16.14);
  reader.root.rotation.y = Math.PI;
  reader.held.position.set(0, 1.1, 0.42);
  reader.book.visible = true;
  const page = new Group();
  page.position.set(0, 0.084, 0);
  reader.book.add(page);
  g.box(reader.book, m.paper, -0.11, 0.084, 0, 0.215, 0.008, 0.56);
  g.box(page, m.paper, 0.11, 0, 0, 0.215, 0.008, 0.56);
  const cart = worldCart(g, m);
  cart.root.position.set(PRESS[0], 0, PRESS[1] + 1.1);
  root.add(cart.root);
  const cat = worldCat(g, m);
  cat.root.position.set(-8.75, 0, 16.05);
  cat.root.rotation.y = -0.8;
  root.add(cat.root);
  const props = worldProps(g, m, root, fixed);

  const clerk = worker(clerkModel, 1);
  const binder = worker(binderModel, 2);
  const workers = [clerk, binder];
  const from: [number, number] = [0, 0];
  const target: [number, number] = [0, 0];
  const carriedFrom = new Vector3();
  let lastReceiver = -1;
  let lampIndex = 0;
  let lastLamp = -1;
  let cartAngle = 0;
  let cartRoute: FloorPoint[] = [];
  let cartWaypoint = 0;
  let cartRetryAt = 0;
  let readerWake = 0;
  let chimeSerial = 0;
  let cuckooSerial = 0;
  let readerSerial = 0;
  let greetingSerial = 0;
  const mouse = root.getObjectByName("library-mouse");
  const paperBird = root.getObjectByName("origami-crane");
  const bookmark = root.getObjectByName("living-bookmark");
  const bookmarkPosition = new Vector3();
  const catAttention = new Vector3();

  function beginStep(person: Worker) {
    person.elapsed = 0;
    person.escaped = false;
    const step = person.steps[person.index];
    if (!step) {
      if (person.kind) scheduler.finish(person.kind);
      person.model.root.userData.completed = Number(person.model.root.userData.completed ?? 0) + 1;
      person.kind = undefined;
      person.cart = false;
      person.model.brush.visible = false;
      return;
    }
    if (step.action === "walk" && step.point) {
      person.route = libraryRoute(
        [person.model.root.position.x, person.model.root.position.z],
        step.point,
        random,
      );
      person.waypoint = 1;
      if (!person.route.length) {
        person.index++;
        beginStep(person);
      }
    }
  }
  function start(person: Worker, kind: AmbientKind) {
    const event = scheduler.event(kind)!;
    person.kind = kind;
    person.serial = event.serial;
    person.index = 0;
    person.letter = false;
    person.delivered = 0;
    person.model.root.userData.episode = kind;
    person.model.root.userData.serial = event.serial;
    if (kind === "sort") person.steps = [walk(SORT), act("sort", 7 + event.strength * 3)];
    if (kind === "letter") {
      let receiver = Math.floor(random() * DESTINATIONS.length);
      if (receiver === lastReceiver) receiver = (receiver + 1) % DESTINATIONS.length;
      lastReceiver = receiver;
      person.receiver = receiver;
      const heading = receiver === 2 ? 0 : -Math.PI / 2;
      person.steps = [
        walk(SORT),
        act("pickup", 2),
        walk(DESTINATIONS[receiver]!),
        act("deliver", 2.5, heading),
        act("nod", 1.2, heading),
        walk(SORT),
      ];
    }
    if (kind === "pneumatic") person.steps = [walk(SORT), act("sort", 3.3), act("feed", 8.5)];
    if (kind === "cart") {
      person.steps = [
        walk(PRESS),
        act("load", 3, Math.PI),
        walk(ARCHIVE),
        act("unload", 7, Math.PI / 2),
        walk(BINDING),
        act("bind", 4, Math.PI),
        walk(PRESS),
      ];
    }
    if (kind === "lamp") {
      lampIndex = Math.floor(random() * LAMPS.length);
      if (lampIndex === lastLamp) lampIndex = 1 - lampIndex;
      lastLamp = lampIndex;
      person.steps = [
        walk(LAMPS[lampIndex]!),
        act("polish", 7),
        act("nod", 1),
        walk(BINDING),
        act("bind", 3, Math.PI),
        walk(PRESS),
      ];
    }
    beginStep(person);
  }
  function traffic(person: Worker, other: Worker, actor: Vector3, dt: number, time: number) {
    const position = person.model.root.position;
    const actorDistance = Math.hypot(position.x - actor.x, position.z - actor.z);
    if (actorDistance < 2.5 && time > person.greetAfter) {
      person.greeting = 1.35;
      person.greetAfter = time + 11 + random() * 5;
      greetingSerial++;
      person.model.root.userData.greeting = greetingSerial;
    }
    const otherPosition = other.model.root.position;
    const otherDistance = Math.hypot(position.x - otherPosition.x, position.z - otherPosition.z);
    const otherWalking = other.steps[other.index]?.action === "walk";
    const obstacle =
      actorDistance < 1.3
        ? actor
        : otherDistance < 1.2 && (!otherWalking || person.rank > other.rank)
          ? otherPosition
          : undefined;
    if (!obstacle) return false;
    const angle = Math.atan2(position.x - obstacle.x, position.z - obstacle.z);
    from[0] = position.x;
    from[1] = position.z;
    for (let option = 0; option < 3; option++) {
      const heading = angle + (option === 0 ? 0 : option === 1 ? Math.PI / 2 : -Math.PI / 2);
      target[0] = from[0] + Math.sin(heading) * dt * 2.15;
      target[1] = from[1] + Math.cos(heading) * dt * 2.15;
      if (!libraryPathClear(from, target, 0.45)) continue;
      position.set(target[0], 0, target[1]);
      person.escaped = true;
      pose(person, "yield", time);
      return true;
    }
    pose(person, "yield", time);
    return true;
  }
  function move(person: Worker, other: Worker, actor: Vector3, dt: number, time: number) {
    if (traffic(person, other, actor, dt, time)) return;
    if (person.escaped) {
      const goal = person.steps[person.index]?.point;
      if (goal) {
        person.route = libraryRoute(
          [person.model.root.position.x, person.model.root.position.z],
          goal,
          random,
        );
        person.waypoint = 1;
      }
      person.escaped = false;
    }
    const point = person.route[person.waypoint];
    if (!point) {
      person.index++;
      beginStep(person);
      return;
    }
    const position = person.model.root.position;
    const dx = point[0] - position.x,
      dz = point[1] - position.z;
    const distance = Math.hypot(dx, dz);
    const speed = person.cart ? 1.85 : person.rank === 1 ? 2.2 : 1.95;
    if (person.cart && cart.root.position.distanceTo(position) > 2.4) return;
    const step = Math.min(distance, speed * dt);
    if (distance > 0.001) {
      position.x += (dx / distance) * step;
      position.z += (dz / distance) * step;
      const heading = Math.atan2(dx, dz);
      position.y = 0;
      const turn = Math.atan2(
        Math.sin(heading - person.model.root.rotation.y),
        Math.cos(heading - person.model.root.rotation.y),
      );
      person.model.root.rotation.y += turn * Math.min(1, dt * 10);
    }
    pose(person, "walk", time);
    if (distance <= speed * dt + 0.001) person.waypoint++;
  }
  function work(person: Worker, dt: number, time: number) {
    const step = person.steps[person.index]!;
    person.elapsed += dt;
    const progress = Math.min(1, person.elapsed / (step.duration ?? 1));
    person.model.root.rotation.y = step.heading ?? 0;
    pose(person, step.action, time, progress);
    if (step.action === "sort") props.sort(progress, true);
    if (step.action === "pickup") person.letter = progress > 0.4;
    if (step.action === "deliver") {
      carriedFrom.set(
        person.model.root.position.x + Math.sin(person.model.root.rotation.y) * 0.43,
        1.17,
        person.model.root.position.z + Math.cos(person.model.root.rotation.y) * 0.43,
      );
      props.carryLetter(progress, carriedFrom, person.receiver);
    }
    if (step.action === "load") {
      for (let index = 0; index < cart.books.length; index++)
        cart.books[index]!.visible = index < Math.ceil(progress * 8);
    }
    if (step.action === "unload") {
      const count = Math.min(4, Math.floor(progress * 4));
      while (person.delivered < count) {
        props.depositBook();
        person.delivered++;
        cart.books[8 - person.delivered]!.visible = false;
      }
      carriedFrom.set(person.model.root.position.x + 0.37, 1.17, person.model.root.position.z);
      props.carryBook(progress >= 1 ? 1 : (progress * 4) % 1, carriedFrom);
    }
    if (step.action === "feed") props.pneumatic(progress, true);
    if (step.action === "polish") {
      const lamp = LAMP_CLOTH[lampIndex]!;
      props.lampWipe.visible = true;
      props.lampWipe.position.set(
        lamp[0] + Math.sin(progress * 11) * 0.14,
        lamp[1] + 0.12 + Math.cos(progress * 11) * 0.07,
        lamp[2] - 0.08,
      );
    }
    if (progress >= 1) {
      if (step.action === "deliver") {
        props.depositLetter(person.receiver);
        person.letter = false;
      }
      if (step.action === "load") person.cart = true;
      if (step.action === "polish") props.lampWipe.visible = false;
      if (step.action === "sort") props.sort(0, false);
      if (step.action === "feed") props.pneumatic(0, false);
      person.index++;
      beginStep(person);
    }
  }
  function followCart(dt: number, time: number) {
    if (!binder.cart) return;
    const destination = binder.model.root.position;
    const position = cart.root.position;
    let dx = destination.x - position.x,
      dz = destination.z - position.z;
    const distance = Math.hypot(dx, dz);
    if (distance < 0.94) return;
    from[0] = position.x;
    from[1] = position.z;
    const length = Math.min(distance - 0.91, dt * 2.85);
    target[0] = position.x + (dx / distance) * length;
    target[1] = position.z + (dz / distance) * length;
    if (!libraryPathClear(from, target, 0.45)) {
      if (time > cartRetryAt) {
        cartRoute = libraryRoute(from, [destination.x, destination.z]);
        cartWaypoint = 1;
        cartRetryAt = time + 0.7;
      }
      const via = cartRoute[cartWaypoint];
      if (!via) return;
      dx = via[0] - position.x;
      dz = via[1] - position.z;
      const span = Math.hypot(dx, dz);
      const amount = Math.min(span, dt * 2.85);
      if (span < 0.05) {
        cartWaypoint++;
        return;
      }
      target[0] = position.x + (dx / span) * amount;
      target[1] = position.z + (dz / span) * amount;
      if (!libraryPathClear(from, target, 0.45)) return;
    }
    cartAngle += Math.hypot(target[0] - position.x, target[1] - position.z) / 0.14;
    position.set(target[0], 0, target[1]);
    cart.root.rotation.y = Math.atan2(dx, dz);
    for (const wheel of cart.wheels) wheel.rotation.x = cartAngle;
  }
  function stories(dt: number, time: number, still: boolean) {
    const episode = still ? undefined : scheduler.event("reader");
    const chime = still ? undefined : scheduler.event("chime");
    const cuckoo = still ? undefined : scheduler.event("cuckoo");
    const bookmarkEvent = still ? undefined : scheduler.event("bookmark");
    if (episode && episode.serial !== readerSerial) {
      readerSerial = episode.serial;
      readerWake = 0;
    }
    if (chime && chime.serial !== chimeSerial) {
      chimeSerial = chime.serial;
      readerWake = 4;
    }
    if (
      cuckoo &&
      cuckoo.serial !== cuckooSerial &&
      (time - cuckoo.start) / cuckoo.duration > 0.32
    ) {
      cuckooSerial = cuckoo.serial;
      readerWake = 3.6;
    }
    readerWake = still ? 0 : Math.max(0, readerWake - dt);
    const progress = episode ? (time - episode.start) / episode.duration : 0;
    const bookmarkProgress = bookmarkEvent
      ? (time - bookmarkEvent.start) / bookmarkEvent.duration
      : 0;
    const curious = Boolean(bookmarkEvent && bookmarkProgress > 0.28 && bookmarkProgress < 0.76);
    if (curious && bookmark) bookmark.getWorldPosition(bookmarkPosition);
    const sleepy = episode && !curious && readerWake === 0 && progress > 0.22 && progress < 0.68;
    reader.head.rotation.x = sleepy
      ? 0.26 + Math.sin(progress * 12) * 0.06
      : readerWake > 0
        ? -0.12
        : 0.09;
    const readerLook = curious
      ? Math.atan2(
          bookmarkPosition.x - reader.root.position.x,
          bookmarkPosition.z - reader.root.position.z,
        ) - reader.root.rotation.y
      : 0;
    reader.head.rotation.y = Math.max(
      -0.65,
      Math.min(0.65, Math.atan2(Math.sin(readerLook), Math.cos(readerLook))),
    );
    reader.body.rotation.x = sleepy ? 0.11 : 0;
    reader.arms[0]!.rotation.x = -0.85;
    reader.arms[1]!.rotation.x =
      -0.85 + (episode && !sleepy ? Math.sin(progress * Math.PI * 5) * 0.16 : 0);
    page.rotation.z =
      episode && !sleepy ? Math.max(0, Math.sin(progress * Math.PI * 4)) ** 5 * 2.65 : 0;
    reader.root.userData.pose = sleepy
      ? "doze"
      : readerWake > 0
        ? "wake"
        : curious
          ? "wonder"
          : "read";
    const bellProgress = chime ? (time - chime.start) / chime.duration : 0;
    props.chime(bellProgress, Boolean(chime));
    const catEpisode = still ? undefined : scheduler.event("cat");
    const catProgress = catEpisode ? (time - catEpisode.start) / catEpisode.duration : 0;
    const stretch = catEpisode ? Math.sin(catProgress * Math.PI) ** 2 : 0;
    cat.body.scale.z = 1 + stretch * 0.25;
    cat.body.position.y = -stretch * 0.05;
    cat.head.rotation.x = -stretch * 0.12;
    cat.tail.rotation.y = catEpisode ? Math.sin(catProgress * Math.PI * 3) * 0.32 : 0;
    let attentionDistance = 9.5;
    let attention = "";
    if (!still && mouse?.visible) {
      const distance = mouse.position.distanceTo(cat.root.position);
      if (distance < attentionDistance) {
        attentionDistance = distance;
        catAttention.copy(mouse.position);
        attention = "mouse";
      }
    }
    if (!still && paperBird?.userData.flying) {
      const distance = paperBird.position.distanceTo(cat.root.position);
      if (distance < attentionDistance) {
        attentionDistance = distance;
        catAttention.copy(paperBird.position);
        attention = "origami";
      }
    }
    if (!still && bookmark?.visible && curious) {
      if (bookmarkPosition.distanceTo(cat.root.position) < attentionDistance) {
        catAttention.copy(bookmarkPosition);
        attention = "bookmark";
      }
    }
    const look = attention
      ? Math.atan2(catAttention.x - cat.root.position.x, catAttention.z - cat.root.position.z) -
        cat.root.rotation.y
      : 0;
    const angle = Math.max(-0.95, Math.min(0.95, Math.atan2(Math.sin(look), Math.cos(look))));
    cat.head.rotation.y += (angle - cat.head.rotation.y) * (still ? 1 : 1 - Math.exp(-dt * 2.6));
    if (attention === "origami" || attention === "bookmark") cat.head.rotation.x = -0.24;
    cat.root.userData.pose = attention ? "watch" : catEpisode ? "stretch" : "rest";
    cat.root.userData.attention = attention;
  }
  function advance(dt: number, actor: Vector3, still: boolean) {
    const time = scheduler.time();
    if (still) {
      for (const person of workers) {
        person.greeting = 0;
        pose(person, "idle", 0);
        person.model.letter.visible = person.letter;
      }
      props.quiet();
      stories(0, time, true);
      return;
    }
    for (const kind of CLERK_JOBS) {
      const event = scheduler.event(kind);
      if (event && event.serial !== clerk.serial) start(clerk, kind);
    }
    for (const kind of BINDER_JOBS) {
      const event = scheduler.event(kind);
      if (event && event.serial !== binder.serial) start(binder, kind);
    }
    for (let index = 0; index < workers.length; index++) {
      const person = workers[index]!;
      const other = workers[1 - index]!;
      person.greeting = Math.max(0, person.greeting - dt);
      if (!person.kind) {
        pose(person, "idle", time);
        continue;
      }
      const step = person.steps[person.index];
      if (!step) continue;
      if (step.action === "walk") move(person, other, actor, dt, time);
      else work(person, dt, time);
    }
    followCart(dt, time);
    stories(dt, time, false);
  }
  advance(0, new Vector3(), true);
  return { advance };
}
