import { Group, type Vector3 } from "three";
import { batchArchitecture, type LibraryGeometry } from "./geometry";
import type { LibraryMaterials } from "./materials";
import type { AmbientScheduler } from "./ambientScheduler";
import { ambientStory, storyEnvelope, storyStep } from "./ambientStory";

function pedestal(
  g: LibraryGeometry,
  m: LibraryMaterials,
  root: Group,
  x: number,
  z: number,
  width: number,
) {
  const group = new Group();
  group.position.set(x, 0, z);
  root.add(group);
  const fixed = new Group();
  g.box(fixed, m.wood, 0, 0.59, 0, width * 0.76, 1.12, width * 0.7);
  g.box(fixed, m.oak, 0, 1.17, 0, width, 0.13, width * 0.92);
  g.box(fixed, m.brass, 0, 1.24, 0, width * 0.94, 0.018, width * 0.85);
  for (const side of [-1, 1]) {
    g.box(fixed, m.brass, side * width * 0.3, 0.65, width * 0.36, 0.028, 0.85, 0.024);
    g.ball(fixed, m.brass, side * width * 0.3, 1.28, width * 0.37, 0.036);
  }
  group.add(batchArchitecture(fixed));
  return group;
}

function littleRose(
  g: LibraryGeometry,
  m: LibraryMaterials,
  root: Group,
  scheduler: AmbientScheduler,
) {
  const scene = pedestal(g, m, root, 21.5, 11.9, 1.48);
  scene.name = "curiosity-rose";
  const planet = new Group();
  planet.position.y = 1.54;
  scene.add(planet);
  const globe = new Group();
  g.ball(globe, m.floorAlt, 0, 0, 0, 0.43, 0.3, 0.39);
  for (let crater = 0; crater < 7; crater++) {
    const a = crater * 2.4;
    const ring = g.hoop(
      globe,
      m.oak,
      Math.cos(a) * 0.25,
      0.05 + (crater % 3) * 0.07,
      Math.sin(a) * 0.25,
      0.055,
    );
    ring.rotation.x = -Math.PI / 3;
  }
  g.rod(globe, m.sage, [0, 0.22, 0], [0.025, 0.75, 0], 0.025);
  for (const side of [-1, 1]) {
    const leaf = g.ball(globe, m.sage, side * 0.105, 0.46, 0, 0.15, 0.054, 0.046);
    leaf.rotation.z = side * 0.3;
  }
  planet.add(batchArchitecture(globe));
  const petals = Array.from({ length: 7 }, (_, index) => {
    const petal = new Group();
    petal.rotation.y = (index * Math.PI * 2) / 7;
    petal.position.y = 0.73;
    planet.add(petal);
    g.ball(petal, m.claret, 0, 0.055, 0.065, 0.083, 0.12, 0.06);
    return petal;
  });
  g.ball(planet, m.brass, 0, 0.76, 0, 0.043);
  const cover = new Group();
  cover.position.y = 1.31;
  scene.add(cover);
  const glass = m.paper.clone();
  glass.transparent = true;
  glass.opacity = 0.13;
  glass.depthWrite = false;
  const dome = g.ball(cover, glass, 0, 0.52, 0, 0.58, 0.71, 0.58);
  dome.castShadow = false;
  g.ball(cover, m.brass, 0, 1.24, 0, 0.065);
  const rim = g.hoop(cover, m.brass, 0, 0.05, 0, 0.51);
  rim.rotation.x = Math.PI / 2;
  let proximity = 0;
  const story = ambientStory(scheduler, "rose", scene, (p, _variant, quiet) => {
    const reveal = quiet ? 0 : storyEnvelope(p, 0.25, 0.72);
    const bloom = quiet ? 0 : Math.max(proximity * 0.6, storyEnvelope(p, 0.43, 0.68));
    cover.position.y = 1.31 + reveal * 0.62;
    petals.forEach((petal, index) => {
      petal.rotation.x = bloom * (0.65 + (index % 2) * 0.2);
    });
    planet.rotation.z = quiet ? 0 : Math.sin(scheduler.time() * 1.2) * bloom * 0.035;
    scene.userData.phase = p < 0.25 ? "uncover" : p < 0.72 ? "bloom" : "shelter";
  });
  return {
    advance(still: boolean, actor: Vector3) {
      proximity = Math.max(0, 1 - Math.hypot(actor.x - 21.5, actor.z - 11.9) / 4);
      story.advance(still);
    },
  };
}

function wanderingPenguin(
  g: LibraryGeometry,
  m: LibraryMaterials,
  root: Group,
  scheduler: AmbientScheduler,
) {
  const scene = pedestal(g, m, root, -21, 5, 1.6);
  scene.name = "curiosity-penguin";
  const fixed = new Group();
  g.box(fixed, m.paper, 0, 1.27, 0, 1.43, 0.04, 1.23);
  for (let peak = 0; peak < 3; peak++) {
    const mountain = g.box(
      fixed,
      peak % 2 ? m.paper : m.floorAlt,
      -0.36 + peak * 0.31,
      1.56 + (peak % 2) * 0.16,
      -0.35,
      0.42,
      0.42,
      0.28,
    );
    mountain.rotation.z = Math.PI / 4;
  }
  scene.add(batchArchitecture(fixed));
  const bird = new Group();
  bird.position.set(-0.46, 1.32, 0.42);
  scene.add(bird);
  const body = new Group();
  g.ball(body, m.ink, 0, 0.22, 0, 0.105, 0.18, 0.1);
  g.ball(body, m.paper, 0, 0.205, 0.066, 0.075, 0.133, 0.047);
  g.ball(body, m.ink, 0, 0.39, 0.009, 0.087);
  g.ball(body, m.brass, 0, 0.37, 0.103, 0.03, 0.021, 0.063);
  for (const side of [-1, 1]) {
    g.ball(body, m.paper, side * 0.046, 0.401, 0.077, 0.024);
    g.ball(body, m.ink, side * 0.046, 0.402, 0.096, 0.012);
    const flipper = g.ball(body, m.ink, side * 0.118, 0.245, -0.002, 0.035, 0.11, 0.045);
    flipper.rotation.z = side * 0.32;
  }
  bird.add(batchArchitecture(body));
  const feet = [-1, 1].map((side) =>
    g.ball(bird, m.brass, side * 0.052, 0.047, 0.032, 0.04, 0.024, 0.085),
  );
  const tracks = Array.from({ length: 10 }, (_, index) => {
    const track = g.ball(
      scene,
      m.floorAlt,
      -0.45 + index * 0.062 + (index % 2 ? 0.045 : -0.045),
      1.298,
      0.39 - index * 0.058,
      0.02,
      0.003,
      0.036,
    );
    return track;
  });
  return ambientStory(scheduler, "penguin", scene, (p, variant, quiet) => {
    const journey = storyStep(p, 0.1, 0.72);
    const retreat = storyStep(p, 0.86, 1);
    const travel = quiet ? 0 : journey * (1 - retreat);
    bird.position.set(-0.46 + travel * 0.68, 1.32, 0.42 - travel * 0.62);
    bird.rotation.y = p < 0.77 ? 2.3 : 2.3 + storyStep(p, 0.78, 0.85) * Math.PI;
    const walk = !quiet && ((p > 0.1 && p < 0.72) || p > 0.86);
    bird.rotation.z = walk ? Math.sin(p * Math.PI * 44) * 0.13 : 0;
    feet.forEach((foot, index) => {
      foot.position.y =
        0.047 + (walk ? Math.max(0, Math.sin(p * Math.PI * 44 + index * Math.PI)) * 0.045 : 0);
    });
    tracks.forEach((track, index) => {
      track.visible = !quiet && index < journey * 10 && p < 0.94;
    });
    body.rotation.y =
      !quiet && p > 0.73 && p < 0.82 ? Math.sin(p * 40) * 0.2 * (variant % 2 ? 1 : -1) : 0;
    scene.userData.phase = p < 0.72 ? "wander" : p < 0.86 ? "contemplate" : "return";
  });
}

function newtonCradle(
  g: LibraryGeometry,
  m: LibraryMaterials,
  root: Group,
  scheduler: AmbientScheduler,
) {
  const scene = pedestal(g, m, root, 2, 15, 1.4);
  scene.name = "curiosity-newton";
  const fixed = new Group();
  for (const z of [-0.28, 0.28]) {
    for (const x of [-0.53, 0.53]) g.rod(fixed, m.brass, [x, 1.27, z], [x, 2.17, z], 0.031);
    g.rod(fixed, m.brass, [-0.53, 2.17, z], [0.53, 2.17, z], 0.031);
  }
  scene.add(batchArchitecture(fixed));
  const pendulums = Array.from({ length: 5 }, (_, index) => {
    const pendulum = new Group();
    pendulum.position.set((index - 2) * 0.19, 2.16, 0);
    scene.add(pendulum);
    for (const z of [-0.28, 0.28]) g.rod(pendulum, m.ink, [0, 0, z], [0, -0.51, 0], 0.008);
    g.ball(pendulum, m.brass, 0, -0.57, 0, 0.095);
    return pendulum;
  });
  return ambientStory(scheduler, "newton", scene, (p, variant, quiet) => {
    const energy = quiet ? 0 : storyEnvelope(p, 0.12, 0.48) * Math.exp(-Math.max(0, p - 0.25) * 2);
    const swing = Math.sin(p * Math.PI * (16 + variant * 2));
    pendulums.forEach((pendulum, index) => {
      pendulum.rotation.z =
        index === 0
          ? Math.max(0, swing) * energy * 0.86
          : index === 4
            ? Math.min(0, swing) * energy * 0.86
            : 0;
    });
    scene.userData.phase = p < 0.12 ? "release" : p < 0.8 ? "transfer" : "settle";
  });
}

export function libraryExhibits(
  g: LibraryGeometry,
  m: LibraryMaterials,
  root: Group,
  scheduler: AmbientScheduler,
) {
  const rose = littleRose(g, m, root, scheduler);
  const penguin = wanderingPenguin(g, m, root, scheduler);
  const newton = newtonCradle(g, m, root, scheduler);
  return {
    advance(still: boolean, actor: Vector3) {
      rose.advance(still, actor);
      penguin.advance(still);
      newton.advance(still);
    },
  };
}
