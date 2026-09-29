import { Vector3 } from "three";
import type { LibraryActivity, LibrarySection } from "../../liveSearchProgress";
import type { libraryArchivist, libraryLadder, libraryDeskWork } from "./archivist";
import { DESK, SEAT, LADDER_PARK, type FloorPoint } from "./layout";
import { libraryPatrol } from "./patrol";
import { libraryRandom } from "./random";
import { poseArchivist, type LibraryPose } from "./poses";

export interface LibrarySceneState {
  activity: LibraryActivity;
  shelf?: LibrarySection;
  found: number;
  consulted: number;
}
interface Segment {
  pose: LibraryPose;
  from: Vector3;
  to: Vector3;
  duration: number;
  home: readonly FloorPoint[];
  carrying: boolean;
  heading?: number;
}
const ground = ([x, z]: FloorPoint) => new Vector3(x, 0, z);
const desk = ground(DESK);
const seated = new Vector3(SEAT[0], 0.27, SEAT[1]);
const mode = (activity: LibraryActivity) => (activity === "wait" ? "collect" : activity);
const step = (
  pose: LibraryPose,
  from: Vector3,
  to: Vector3,
  duration: number,
  path: readonly FloorPoint[] = [DESK],
  withBook = false,
): Segment => ({
  pose,
  from: from.clone(),
  to: to.clone(),
  duration,
  home: path,
  carrying: withBook,
});
export function libraryMotion(
  actor: ReturnType<typeof libraryArchivist>,
  ladder: ReturnType<typeof libraryLadder>,
  work: ReturnType<typeof libraryDeskWork>,
  initial: LibrarySceneState,
  random: () => number = libraryRandom(),
) {
  let state = initial;
  let heading = Math.PI;
  const patrol = libraryPatrol(random);
  let previousPause: LibraryPose | undefined;
  let walkSpeed = 1.9;
  let elapsed = 0;
  let time = random() * 20;
  let returning = false;
  let changed = false;
  let carrying = false;
  let deposited = false;
  let finishing = false;
  let finished = false;
  let queue: Segment[] = [];
  let current: Segment | undefined;
  let home: readonly FloorPoint[] = [DESK];
  let shelfFloor = desk.clone();
  let destination = new Vector3();
  actor.root.position.copy(desk);
  ladder.position.set(LADDER_PARK[0], 0, LADDER_PARK[1]);
  ladder.rotation.y = Math.PI;

  const walk = (from: Vector3, to: Vector3, path: readonly FloorPoint[], withBook: boolean) =>
    step("walk", from, to, Math.max(0.12, from.distanceTo(to) / walkSpeed), path, withBook);

  function pause(point: Vector3, path: readonly FloorPoint[], withBook: boolean) {
    const choices: LibraryPose[] = withBook
      ? ["inspect", "wipe", "look"]
      : ["breathe", "look", "adjust"];
    const candidates = choices.filter((pose) => pose !== previousPause);
    const pose =
      withBook && random() < 0.13
        ? "pickup"
        : candidates[Math.floor(random() * candidates.length)]!;
    previousPause = pose;
    return step(
      pose,
      point,
      point,
      (pose === "pickup" ? 2.4 : 1.5) + random() * 1.1,
      path,
      withBook,
    );
  }

  function startNext() {
    current = queue.shift();
    elapsed = 0;
    deposited = false;
    if (current) {
      home = current.home;
      if (current.heading !== undefined) heading = current.heading;
      if (current.pose === "climb") shelfFloor = current.from.clone();
    }
    if (current?.pose === "deliver")
      destination = work.nextMaterialPosition().add(work.root.position);
    if (finishing && !current) finished = true;
  }

  // Путь домой хранится у каждого отрезка: смена источника не срезает углы сквозь стеллажи.
  function returnToDesk(finale = false) {
    returning = true;
    changed = false;
    queue = [];
    let position = actor.root.position.clone();
    if (position.y > 0.01) {
      queue.push(
        step("descend", position, shelfFloor, Math.max(0.3, position.y / 0.9), home, carrying),
      );
      position = shelfFloor.clone();
    } else if (current?.pose === "walk") {
      queue.push(walk(position, current.to, home, carrying));
      position = current.to.clone();
    }
    for (let i = 0; i < home.length; i++) {
      const next = ground(home[i]!);
      if (position.distanceTo(next) > 0.01)
        queue.push(walk(position, next, home.slice(i), carrying));
      position = next;
    }
    if (position.distanceTo(desk) > 0.01) queue.push(walk(position, desk, [DESK], carrying));
    if (finale) {
      const travel = queue.reduce((duration, item) => duration + item.duration, 0);
      for (const item of queue) item.duration *= Math.min(1, 3.6 / Math.max(0.1, travel));
    }
    if (carrying) queue.push(step("deliver", desk, desk, 1.15, [DESK], true));
    if (finale) {
      queue.push(step("chair", desk, desk, 0.65));
      queue.push(step("sit", desk, seated, 0.65));
      queue.push(step("open", seated, seated, 1.15));
      queue.push(step("settle", seated, seated, 0.9));
    }
    startNext();
  }

  function collect() {
    const journey = patrol(state.shelf ?? "science");
    walkSpeed = 2.05 + random() * 0.35;
    const route = journey.inspection?.onward ?? journey.outbound;
    const back = journey.homeward;
    shelfFloor = ground(route.at(-1)!);
    const top = shelfFloor
      .clone()
      .add(
        new Vector3(
          Math.sin(journey.heading) * 0.42,
          1.3 + random() * 0.58,
          Math.cos(journey.heading) * 0.42,
        ),
      );
    returning = false;
    changed = false;
    queue = [];
    if (journey.inspection) {
      const { approach, home: inspectionHome, heading: inspectionHeading } = journey.inspection;
      for (let i = 1; i < approach.length; i++) {
        const path = approach.slice(0, i + 1).toReversed();
        queue.push(walk(ground(approach[i - 1]!), ground(approach[i]!), path, false));
      }
      const point = ground(journey.inspection.point);
      queue.push({
        ...step("browse", point, point, 3.2 + random() * 1.5, inspectionHome),
        heading: inspectionHeading,
      });
      queue.push({
        ...step("compare", point, point, 1.6 + random() * 0.8, inspectionHome),
        heading: inspectionHeading,
      });
    }
    const restAt = random() < 0.72 ? 1 + Math.floor(random() * (route.length - 2)) : -1;
    for (let i = 1; i < route.length; i++) {
      const path = journey.inspection
        ? [...route.slice(0, i + 1).toReversed(), ...journey.inspection.home.slice(1)]
        : route.slice(0, i + 1).toReversed();
      const point = ground(route[i]!);
      queue.push(walk(ground(route[i - 1]!), point, path, false));
      if (i === restAt) queue.push(pause(point, path, false));
    }
    queue.push({
      ...step("climb", shelfFloor, top, 2 + random() * 0.65, back),
      heading: journey.heading,
    });
    queue.push(step("reach", top, top, 1.35 + random() * 0.75, back));
    queue.push(step("descend", top, shelfFloor, 1.8 + random() * 0.6, back, true));
    const homeRestAt = journey.stop
      ? back.indexOf(journey.stop)
      : random() < 0.65
        ? Math.floor(random() * (back.length - 1))
        : -1;
    if (homeRestAt === 0) queue.push(pause(shelfFloor, back, true));
    for (let i = 1; i < back.length; i++) {
      const point = ground(back[i]!);
      queue.push(walk(ground(back[i - 1]!), point, back.slice(i), true));
      if (i === homeRestAt) queue.push(pause(point, back.slice(i), true));
    }
    queue.push(step("deliver", desk, desk, 1.4, [DESK], true));
    queue.push(step("read", desk, desk, 2 + random() * 2));
    startNext();
  }

  function update(next: LibrarySceneState) {
    if (next.activity === "reconnect") return;
    if (next.activity === "finish" && !finishing) {
      finishing = true;
      state = next;
      returnToDesk(true);
    } else if (!finishing) {
      if (next.shelf !== state.shelf || mode(next.activity) !== mode(state.activity))
        changed = true;
      state = next;
    }
    work.folder.visible = next.found > 0;
  }

  function advance(dt: number, still = false): LibraryPose {
    if (still) {
      current = undefined;
      queue = [];
      home = [DESK];
      changed = returning = carrying = false;
      actor.root.position.copy(finishing ? seated : desk);
      ladder.position.set(LADDER_PARK[0], 0, LADDER_PARK[1]);
      ladder.rotation.y = Math.PI;
      if (finishing) finished = true;
    }
    time += dt;
    if (!current && state.activity === "collect" && !still) collect();
    const waiting = state.activity === "wait";
    let remaining = waiting || still ? 0 : dt;
    // Не теряем остаток кадра на коротких отрезках: финал укладывается в срок при любом числе арок.
    while (current && remaining > 0) {
      const consumed = Math.min(remaining, Math.max(0, current.duration - elapsed));
      elapsed += consumed;
      remaining -= consumed;
      const t = Math.min(1, elapsed / current.duration);
      const eased = current.pose === "walk" ? t : t * t * (3 - 2 * t);
      actor.root.position.lerpVectors(current.from, current.to, eased);
      carrying = current.carrying || (current.pose === "reach" && t > 0.65);
      if (current.pose === "deliver" && t >= 0.86) {
        if (!deposited) work.deposit();
        deposited = true;
        carrying = false;
      }
      if (elapsed >= current.duration - 0.000001) {
        if (changed && !returning && !finishing) returnToDesk();
        else startNext();
      } else break;
    }
    const idle: LibraryPose = finishing
      ? "settle"
      : state.activity === "write"
        ? "write"
        : state.activity === "prepare"
          ? "prepare"
          : "read";
    const pose: LibraryPose = waiting ? "wait" : (current?.pose ?? idle);
    const progress = current ? Math.min(1, elapsed / current.duration) : 1;
    let facing = 0;
    if (pose === "walk" && current)
      facing = Math.atan2(current.to.x - current.from.x, current.to.z - current.from.z);
    else if (["climb", "reach", "descend", "browse", "compare"].includes(pose)) facing = heading;
    else if (["wait", "breathe", "look", "adjust", "inspect", "pickup", "wipe"].includes(pose))
      facing = actor.root.rotation.y;
    const turn = Math.atan2(
      Math.sin(facing - actor.root.rotation.y),
      Math.cos(facing - actor.root.rotation.y),
    );
    actor.root.rotation.y += turn * (still ? 1 : Math.min(1, dt * 10));
    poseArchivist(actor, work, pose, time, progress, carrying, destination, still);

    // Лестница катится рядом; при подъёме её колёса остаются на полу.
    if (pose === "walk") {
      const offset = carrying || returning ? -0.52 : 0;
      const blend = Math.min(1, dt * 8);
      ladder.position.x +=
        (actor.root.position.x + Math.sin(facing) * offset - ladder.position.x) * blend;
      ladder.position.z +=
        (actor.root.position.z + Math.cos(facing) * offset - ladder.position.z) * blend;
      const angle = Math.atan2(
        Math.sin(facing - ladder.rotation.y),
        Math.cos(facing - ladder.rotation.y),
      );
      ladder.rotation.y += angle * blend;
    } else if (["climb", "reach", "descend"].includes(pose)) {
      ladder.position.lerp(shelfFloor, Math.min(1, dt * 8));
      ladder.rotation.y = heading;
    } else if (finishing && ["chair", "sit", "open", "settle"].includes(pose)) {
      ladder.position.x += (LADDER_PARK[0] - ladder.position.x) * Math.min(1, dt * 4);
      ladder.position.z += (LADDER_PARK[1] - ladder.position.z) * Math.min(1, dt * 4);
    }
    return pose;
  }

  update(initial);
  return {
    update,
    advance,
    get finished() {
      return finished;
    },
    get deliveries() {
      return work.pile.length;
    },
  };
}
