import { Group } from "three";
import { batchArchitecture, type LibraryGeometry } from "./geometry";
import type { LibraryMaterials } from "./materials";
import type { AmbientScheduler } from "./ambientScheduler";
import { ambientStory, storyEnvelope, storyStep } from "./ambientStory";

function planetarium(
  g: LibraryGeometry,
  m: LibraryMaterials,
  root: Group,
  scheduler: AmbientScheduler,
) {
  const scene = new Group();
  scene.name = "curiosity-planetarium";
  scene.position.set(-8.42, 0.37, -7);
  root.add(scene);
  const stand = new Group();
  g.rod(stand, m.wood, [0, 0, 0], [0, 0.07, 0], 0.29);
  g.rod(stand, m.brass, [0, 0.08, 0], [0, 0.46, 0], 0.036);
  g.ball(stand, m.brass, 0, 0.46, 0, 0.094);
  for (let tick = 0; tick < 12; tick++) {
    const a = (tick * Math.PI) / 6;
    g.ball(stand, m.brass, Math.cos(a) * 0.25, 0.087, Math.sin(a) * 0.25, 0.017);
  }
  scene.add(batchArchitecture(stand));
  const crank = new Group();
  crank.position.set(0.27, 0.12, 0);
  crank.rotation.y = Math.PI / 2;
  scene.add(crank);
  g.hoop(crank, m.brass, 0, 0, 0, 0.11);
  g.rod(crank, m.brass, [0, 0, 0], [0.09, 0, 0], 0.022);
  g.ball(crank, m.oak, 0.09, 0, 0.05, 0.035);
  const rings = new Group();
  rings.position.y = 0.48;
  scene.add(rings);
  const latitude = g.hoop(rings, m.brass, 0, 0, 0, 0.39);
  latitude.rotation.x = Math.PI / 2;
  const meridian = g.hoop(rings, m.brass, 0, 0, 0, 0.39);
  meridian.rotation.y = Math.PI / 2;
  const orbit = new Group();
  rings.add(orbit);
  g.rod(orbit, m.oak, [-0.34, 0, 0], [0.34, 0, 0], 0.018);
  g.ball(orbit, m.sage, 0.34, 0, 0, 0.083);
  g.ball(orbit, m.paper, -0.34, 0, 0, 0.054);
  const pointer = new Group();
  pointer.position.y = 0.13;
  scene.add(pointer);
  g.rod(pointer, m.brass, [0, 0, 0], [0, 0, 0.22], 0.017);
  g.ball(pointer, m.claret, 0, 0, 0.23, 0.041);
  return ambientStory(scheduler, "planetarium", scene, (p, variant, quiet) => {
    const wind = storyStep(p, 0.05, 0.22);
    const revolution = storyStep(p, 0.23, 0.73);
    const align = storyStep(p, 0.75, 0.87) - storyStep(p, 0.91, 1);
    crank.rotation.z = quiet ? 0 : -wind * Math.PI * 6;
    rings.rotation.set(quiet ? 0.28 : 0.28 + align * 0.78, 0, quiet ? -0.15 : -0.15 + align * 0.15);
    orbit.rotation.y = quiet ? 0 : revolution * Math.PI * 2 * (2 + variant);
    pointer.rotation.y = quiet ? 0 : revolution * Math.PI * 2;
    scene.userData.phase = p < 0.23 ? "wind" : p < 0.75 ? "orbit" : "align";
  });
}

function clockCuckoo(
  g: LibraryGeometry,
  m: LibraryMaterials,
  root: Group,
  scheduler: AmbientScheduler,
) {
  const scene = new Group();
  scene.name = "curiosity-cuckoo";
  scene.position.set(15.1, 2.54, -6.85);
  root.add(scene);
  const casework = new Group();
  g.box(casework, m.wood, 0, 0.25, -0.14, 0.66, 0.51, 0.28);
  g.box(casework, m.recess, 0, 0.27, 0.011, 0.43, 0.35, 0.03);
  for (const side of [-1, 1]) {
    const roof = g.box(casework, m.oak, side * 0.18, 0.55, -0.04, 0.4, 0.065, 0.41);
    roof.rotation.z = -side * 0.35;
  }
  g.ball(casework, m.brass, 0, 0.66, -0.03, 0.055);
  scene.add(batchArchitecture(casework));
  const door = new Group();
  door.position.set(0, 0.094, 0.045);
  scene.add(door);
  g.box(door, m.oak, 0, 0.171, 0, 0.46, 0.345, 0.036);
  g.hoop(door, m.brass, 0, 0.195, 0.022, 0.09);
  const bird = new Group();
  bird.position.y = 0.17;
  scene.add(bird);
  const body = new Group();
  g.ball(body, m.claret, 0, 0.11, 0, 0.105, 0.14, 0.17);
  g.ball(body, m.paper, 0, 0.24, 0.09, 0.106, 0.1, 0.1);
  g.rod(body, m.brass, [0, 0.215, 0.14], [0, 0.205, 0.265], 0.035);
  for (const side of [-1, 1]) g.ball(body, m.ink, side * 0.055, 0.256, 0.164, 0.021);
  bird.add(batchArchitecture(body));
  const wings: Group[] = [];
  for (const side of [-1, 1]) {
    const wing = new Group();
    wing.position.set(side * 0.075, 0.16, 0);
    bird.add(wing);
    g.ball(wing, m.brass, side * 0.09, 0, -0.04, 0.13, 0.027, 0.17);
    wings.push(wing);
  }
  return ambientStory(scheduler, "cuckoo", scene, (p, variant, quiet) => {
    const opening = quiet ? 0 : storyEnvelope(p, 0.2, 0.8);
    door.rotation.x = (opening * Math.PI) / 2;
    const reveal = quiet ? 0 : storyStep(p, 0.18, 0.29) * (1 - storyStep(p, 0.75, 0.9));
    bird.visible = !quiet && reveal > 0;
    bird.position.z = -0.18 + reveal * 0.58;
    const call =
      p > 0.31 && p < 0.72
        ? Math.max(0, Math.sin(((p - 0.31) / 0.41) * Math.PI * (variant % 2 ? 6 : 4)))
        : 0;
    bird.rotation.x = -call * 0.23;
    wings[0]!.rotation.z = -call * 0.91;
    wings[1]!.rotation.z = call * 0.91;
    scene.userData.phase = p < 0.3 ? "door" : p < 0.74 ? "call" : "retreat";
  });
}

function searchingGlobe(
  g: LibraryGeometry,
  m: LibraryMaterials,
  root: Group,
  scheduler: AmbientScheduler,
) {
  const scene = new Group();
  scene.name = "curiosity-globe";
  scene.position.set(12, 1.09, 7.1);
  root.add(scene);
  const mount = new Group();
  g.rod(mount, m.brass, [-0.42, 0, 0.16], [-0.42, 0.2, 0.16], 0.041);
  g.ball(mount, m.wood, -0.42, 0.02, 0.16, 0.095, 0.032, 0.095);
  scene.add(batchArchitecture(mount));
  const arm = new Group();
  arm.position.set(-0.42, 0.2, 0.16);
  scene.add(arm);
  g.rod(arm, m.brass, [0, 0, 0], [0.25, 0.46, 0.35], 0.029);
  const lens = new Group();
  lens.position.set(0.25, 0.64, 0.35);
  arm.add(lens);
  g.hoop(lens, m.brass, 0, 0, 0, 0.22);
  g.hoop(lens, m.oak, 0, 0, 0.007, 0.205);
  const glass = m.paper.clone();
  glass.transparent = true;
  glass.opacity = 0.17;
  glass.depthWrite = false;
  g.ball(lens, glass, 0, 0, 0, 0.192, 0.192, 0.017);
  const crank = new Group();
  crank.position.set(0.36, 0.1, 0.1);
  crank.rotation.y = Math.PI / 2;
  scene.add(crank);
  g.rod(crank, m.brass, [0, 0, 0], [0, 0.14, 0], 0.022);
  g.ball(crank, m.oak, 0, 0.14, 0.06, 0.047);
  let turn = 0;
  let from = 0;
  let to = 0;
  const story = ambientStory(
    scheduler,
    "globe",
    scene,
    (p, _variant, quiet) => {
      const deploy = quiet ? 0 : storyEnvelope(p, 0.24, 0.76);
      arm.rotation.z = (1 - deploy) * 1.36;
      arm.rotation.y = -0.32 + deploy * 0.32;
      const search = storyStep(p, 0.25, 0.65);
      if (!quiet) turn = from + (to - from) * search;
      crank.rotation.z = quiet ? 0 : -search * Math.PI * 4;
      lens.rotation.y = deploy * (storyStep(p, 0.66, 0.69) - storyStep(p, 0.73, 0.76)) * 0.28;
      scene.userData.phase = p < 0.25 ? "deploy" : p < 0.65 ? "search" : "examine";
      scene.userData.longitude = turn;
    },
    (variant) => {
      from = turn;
      to = from + Math.PI * 2 + ((variant + 1) * Math.PI) / 2;
    },
  );
  return { advance: story.advance, turn: () => turn };
}

export function ambientInstrumentStories(
  g: LibraryGeometry,
  m: LibraryMaterials,
  root: Group,
  scheduler: AmbientScheduler,
) {
  const stars = planetarium(g, m, root, scheduler);
  const cuckoo = clockCuckoo(g, m, root, scheduler);
  const globe = searchingGlobe(g, m, root, scheduler);
  return {
    advance: (still: boolean) => {
      stars.advance(still);
      cuckoo.advance(still);
      globe.advance(still);
    },
    globeTurn: globe.turn,
  };
}
