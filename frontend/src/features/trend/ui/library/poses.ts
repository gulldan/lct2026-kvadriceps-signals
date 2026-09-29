import type { Vector3 } from "three";
import type { libraryArchivist, libraryDeskWork } from "./archivist";
import { DESK, SEAT } from "./layout";

export type LibraryPose =
  | "walk"
  | "climb"
  | "reach"
  | "browse"
  | "compare"
  | "descend"
  | "deliver"
  | "read"
  | "write"
  | "wait"
  | "prepare"
  | "breathe"
  | "look"
  | "adjust"
  | "inspect"
  | "pickup"
  | "wipe"
  | "chair"
  | "sit"
  | "open"
  | "settle";

export function poseArchivist(
  actor: ReturnType<typeof libraryArchivist>,
  work: ReturnType<typeof libraryDeskWork>,
  pose: LibraryPose,
  time: number,
  progress: number,
  carrying: boolean,
  destination: Vector3,
  still: boolean,
) {
  const stride = Math.sin(time * 11);
  const walking = pose === "walk";
  const climbing = pose === "climb" || pose === "descend";
  const reading = pose === "read" || pose === "write" || pose === "open" || pose === "settle";
  const sitting = pose === "sit" || pose === "open" || pose === "settle";
  const breath = pose === "breathe" ? Math.sin(progress * Math.PI) : 0;
  const inspection = pose === "inspect" ? Math.sin(progress * Math.PI) : 0;
  actor.body.position.y = walking
    ? Math.abs(stride) * 0.04
    : Math.sin(time * 2.3) * 0.009 + breath * 0.055;
  actor.body.scale.set(1 + breath * 0.035, 1 + breath * 0.06, 1 + breath * 0.06);
  actor.body.rotation.set(breath * -0.09, 0, walking ? stride * 0.045 : 0);
  actor.head.rotation.x = reading
    ? 0.2 + Math.sin(time * 1.4) * 0.045
    : pose === "reach"
      ? -0.23
      : breath * -0.2;
  actor.head.rotation.y =
    pose === "wait" || pose === "prepare"
      ? Math.sin(time * 0.7) * 0.22
      : walking
        ? Math.sin(time * 0.65) * 0.1
        : 0;
  actor.head.rotation.z = pose === "wipe" ? Math.sin(progress * Math.PI) * -0.13 : 0;
  if (pose === "look") {
    actor.head.rotation.y = Math.sin(progress * Math.PI * 2) * Math.sin(progress * Math.PI) * 0.52;
    actor.head.rotation.x = -Math.sin(progress * Math.PI) * 0.08;
  }
  if (pose === "inspect") actor.head.rotation.x = inspection * 0.32;
  if (pose === "browse" || pose === "compare") {
    actor.head.rotation.y = Math.sin(progress * Math.PI * 2) * 0.28;
    actor.head.rotation.x = pose === "browse" ? -0.12 : 0.27;
  }
  const blink = time % 5.7;
  const eyeOpening = still ? 1 : blink < 0.18 ? Math.max(0.04, Math.abs(blink - 0.09) / 0.09) : 1;
  actor.eyes.forEach((eye) => {
    eye.scale.y = pose === "breathe" ? Math.max(0.1, 1 - breath) : eyeOpening;
  });
  actor.feet.forEach((foot, index) => {
    foot.rotation.x = sitting
      ? -0.85
      : (walking ? 0.53 : climbing ? 0.68 : 0) * stride * (index ? 1 : -1);
    foot.position.z = sitting ? 0.12 : 0;
  });
  actor.wings.forEach((wing, index) => {
    wing.scale.y = 1;
    wing.position.y = pose === "write" && index === 1 ? 1.01 : 0.81;
    wing.position.z = pose === "write" && index === 1 ? 0.26 : 0;
    wing.rotation.x = carrying
      ? -0.92
      : climbing
        ? -1.5 + stride * 0.22 * (index ? 1 : -1)
        : walking
          ? -1 + stride * 0.035
          : 0;
    wing.rotation.y = 0;
    wing.rotation.z = pose === "reach" ? (index ? -2.8 : 0.4) : breath * (index ? -0.16 : 0.16);
  });
  if (pose === "write") actor.wings[1]!.rotation.x = -2.35 + Math.sin(time * 7) * 0.08;
  if (pose === "wipe") {
    const wipe = Math.sin(progress * Math.PI);
    actor.wings[1]!.rotation.set(-0.92 + 0.47 * wipe, 0, -2.65 * wipe);
  }
  if (pose === "adjust") {
    const touch = Math.sin(progress * Math.PI);
    actor.wings[1]!.rotation.set(-0.55 * touch, 0, -2.45 * touch);
    actor.head.rotation.z = -touch * 0.055;
  }
  if (pose === "browse") {
    actor.wings[1]!.rotation.set(-1.45, Math.sin(progress * Math.PI * 2) * 0.25, -0.32);
  }
  if (pose === "compare") {
    actor.wings.forEach((wing) => {
      wing.rotation.x = -1.1;
    });
  }
  if (pose === "inspect")
    actor.wings.forEach((wing) => {
      wing.rotation.x = -0.92 - inspection * 0.33;
    });
  if (pose === "deliver") {
    actor.wings.forEach((wing) => {
      wing.rotation.x = -1.1;
    });
  }
  if (pose === "open" || pose === "settle") {
    actor.wings.forEach((wing, index) => {
      wing.position.set(index ? 0.26 : -0.26, 0.91, 0.17);
      wing.scale.y = 1.45;
      wing.rotation.x = -Math.PI / 2;
      wing.rotation.y = pose === "open" && index === 1 ? (1 - progress) * 0.35 : 0;
    });
  }
  actor.handkerchief.visible = pose === "wipe" && progress > 0.16 && progress < 0.84;
  actor.handkerchief.position.set(0.07 + Math.sin(progress * Math.PI * 5) * 0.04, 1.23, 0.29);
  actor.handkerchief.rotation.z = Math.sin(progress * Math.PI * 5) * 0.12;
  actor.book.visible = carrying || pose === "compare";
  const handoff = pose === "deliver" ? Math.min(1, progress / 0.48) : 0;
  const retrieval = pose === "reach" ? Math.max(0, 1 - (progress - 0.65) / 0.35) : 0;
  actor.book.position.set(0, 0.62 + handoff * 0.63 + retrieval * 0.53, 0.31 + handoff * 0.47);
  actor.book.rotation.x = ((1 - handoff) * Math.PI) / 2;
  if (pose === "inspect") {
    actor.book.position.set(0, 0.62 + inspection * 0.26, 0.31 + inspection * 0.03);
    actor.book.rotation.x = Math.PI / 2 - inspection * 0.42;
  }
  if (pose === "pickup") {
    const bend = Math.sin(progress * Math.PI);
    const lowered = progress < 0.18 ? progress / 0.18 : progress > 0.6 ? (1 - progress) / 0.4 : 1;
    const angle = bend * 0.48;
    actor.body.rotation.x = angle;
    actor.body.position.y = -bend * 0.18;
    actor.head.rotation.x = bend * 0.28;
    actor.wings.forEach((wing) => {
      wing.rotation.x = -0.92 + 0.25 * bend;
    });
    const height = 0.62 - lowered * 0.53 - actor.body.position.y;
    const forward = 0.31 + lowered * 0.37;
    // Книга остаётся над полом, хотя корпус наклоняется вместе с руками.
    actor.book.position.set(
      0.08 * lowered,
      height * Math.cos(angle) + forward * Math.sin(angle),
      forward * Math.cos(angle) - height * Math.sin(angle),
    );
    actor.book.rotation.x = ((1 - lowered) * Math.PI) / 2 - angle;
  }
  if (pose === "deliver" && progress > 0.48) {
    const t = Math.min(1, (progress - 0.48) / 0.38);
    const eased = t * t * (3 - 2 * t);
    actor.book.position.x = (destination.x - DESK[0]) * eased;
    actor.book.position.y = 1.25 + (destination.y - 1.25) * eased + Math.sin(t * Math.PI) * 0.2;
    actor.book.position.z = 0.78 + (destination.z - DESK[1] - 0.78) * eased;
  }
  actor.pencil.visible = pose === "write";
  actor.pencil.rotation.x = Math.sin(time * 7) * 0.14;
  work.pageTurn.visible = pose !== "chair" && pose !== "sit" && pose !== "open";
  work.pageTurn.rotation.z =
    pose === "read" ? -Math.pow(Math.max(0, Math.sin(time * 0.85)), 5) * 2.8 : 0;
  work.opening.rotation.z =
    pose === "chair"
      ? -Math.PI * progress
      : pose === "sit"
        ? -Math.PI
        : pose === "open"
          ? -Math.PI * (1 - progress) ** 3
          : 0;
  if (pose === "chair") {
    const t = progress * progress * (3 - 2 * progress);
    work.chair.position.set(SEAT[0] + 1.2 * (1 - t), 0, SEAT[1] - 0.28 * t);
  } else if (sitting) {
    const t = pose === "sit" ? progress : 1;
    work.chair.position.set(SEAT[0], 0, SEAT[1] - 0.28 * (1 - t));
  } else {
    work.chair.position.set(SEAT[0] + 1.2, 0, SEAT[1]);
  }
}
