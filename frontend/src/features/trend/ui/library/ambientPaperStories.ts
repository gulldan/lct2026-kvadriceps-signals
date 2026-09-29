import { BufferGeometry, DoubleSide, Float32BufferAttribute, Group, Mesh, Vector3 } from "three";
import { batchArchitecture, type LibraryGeometry } from "./geometry";
import type { LibraryMaterials } from "./materials";
import type { AmbientScheduler } from "./ambientScheduler";
import { ambientStory, storyEnvelope, storyStep } from "./ambientStory";

const PAPER_STATIONS = [
  [-9.76, 1.265, 14.34],
  [7.96, 1.48, 15.37],
  [2.28, 1.27, 8.72],
] as const;

function travellingOrigami(
  g: LibraryGeometry,
  m: LibraryMaterials,
  root: Group,
  scheduler: AmbientScheduler,
) {
  const scene = new Group();
  scene.name = "curiosity-origami";
  root.add(scene);
  const crane = new Group();
  crane.name = "origami-crane";
  scene.add(crane);
  const paper = m.paper.clone();
  paper.side = DoubleSide;
  g.box(crane, paper, 0, 0.018, 0, 0.13, 0.036, 0.39);
  const wings: Group[] = [];
  for (const side of [-1, 1]) {
    const wing = new Group();
    wing.position.x = side * 0.052;
    crane.add(wing);
    const geometry = new BufferGeometry();
    geometry.setAttribute(
      "position",
      new Float32BufferAttribute(
        [
          0,
          0,
          -0.22,
          side * 0.48,
          0,
          -0.14,
          side * 0.05,
          0,
          0.24,
          0,
          0,
          -0.22,
          side * 0.24,
          0.03,
          0.045,
          side * 0.48,
          0,
          -0.14,
        ],
        3,
      ),
    );
    geometry.computeVertexNormals();
    const mesh = new Mesh(geometry, paper);
    mesh.castShadow = true;
    wing.add(mesh);
    g.rod(wing, m.oak, [0, 0.005, -0.22], [side * 0.24, 0.035, 0.045], 0.004);
    wings.push(wing);
  }
  const neck = new Group();
  neck.position.z = 0.17;
  crane.add(neck);
  g.box(neck, paper, 0, 0.16, 0.035, 0.07, 0.33, 0.07);
  const beak = g.box(neck, paper, 0, 0.291, 0.138, 0.055, 0.06, 0.22);
  beak.rotation.x = -0.25;
  const tail = new Group();
  tail.position.z = -0.18;
  crane.add(tail);
  const fold = g.box(tail, paper, 0, 0.055, -0.11, 0.08, 0.019, 0.28);
  fold.rotation.x = 0.42;
  let home = 0;
  let from = 0;
  let destination = 1;
  return ambientStory(
    scheduler,
    "origami",
    scene,
    (p, variant, quiet) => {
      const a = PAPER_STATIONS[from]!;
      const b = PAPER_STATIONS[destination]!;
      const travel = quiet ? 0 : storyStep(p, 0.28, 0.86);
      const dx = b[0] - a[0],
        dz = b[2] - a[2];
      const distance = Math.hypot(dx, dz);
      const arc = Math.sin(travel * Math.PI);
      const detour = arc * (variant % 2 ? -0.8 : 0.8);
      if (quiet) {
        const resting = PAPER_STATIONS[home]!;
        crane.position.set(resting[0], resting[1], resting[2]);
      } else
        crane.position.set(
          a[0] + dx * travel - (dz / distance) * detour,
          a[1] + (b[1] - a[1]) * travel + arc * (1.2 + variant * 0.12),
          a[2] + dz * travel + (dx / distance) * detour,
        );
      const opening = quiet ? 0 : storyStep(p, 0.02, 0.12) - storyStep(p, 0.14, 0.25);
      const flight = quiet ? 0 : storyStep(p, 0.26, 0.34) * (1 - storyStep(p, 0.79, 0.9));
      const wingbeat = Math.sin(travel * Math.PI * (variant % 2 ? 8 : 6)) * flight;
      wings[0]!.rotation.z = -(0.35 * (1 - opening) + wingbeat * 0.5);
      wings[1]!.rotation.z = 0.35 * (1 - opening) + wingbeat * 0.5;
      neck.rotation.x = (-opening * Math.PI) / 2;
      tail.rotation.x = opening * 0.5;
      crane.rotation.set(
        quiet ? 0 : (storyStep(p, 0.28, 0.47) - storyStep(p, 0.65, 0.86)) * -0.12,
        Math.atan2(dx, dz),
        quiet ? 0 : Math.sin(travel * Math.PI * 2) * flight * 0.16,
      );
      scene.userData.phase = p < 0.28 ? "fold" : p < 0.86 ? "fly" : "land";
      crane.userData.flying = !quiet && flight > 0.05;
    },
    (variant) => {
      from = home;
      destination = (home + 1 + (variant % 2)) % PAPER_STATIONS.length;
      scene.userData.origin = from;
      scene.userData.destination = destination;
    },
    () => {
      home = destination;
      scene.userData.home = home;
    },
  );
}

function bindingBeetle(
  g: LibraryGeometry,
  m: LibraryMaterials,
  root: Group,
  scheduler: AmbientScheduler,
) {
  const scene = new Group();
  scene.name = "curiosity-beetle";
  scene.position.set(-18.1, 1.19, 2.82);
  scene.rotation.y = -Math.PI / 2;
  root.add(scene);
  const book = new Group();
  g.box(book, m.claret, 0, 0.017, 0, 0.82, 0.038, 0.52);
  g.box(book, m.paper, 0.023, 0.073, 0, 0.745, 0.076, 0.475);
  g.box(book, m.claret, -0.39, 0.075, 0, 0.04, 0.13, 0.52);
  scene.add(batchArchitecture(book));
  const cover = new Group();
  cover.position.set(-0.39, 0.116, 0);
  scene.add(cover);
  const ornament = new Group();
  g.box(ornament, m.claret, 0.395, 0, 0, 0.82, 0.031, 0.52);
  g.box(ornament, m.brass, 0.395, 0.018, 0, 0.65, 0.007, 0.39);
  g.box(ornament, m.claret, 0.395, 0.023, 0, 0.61, 0.007, 0.35);
  const crest = g.hoop(ornament, m.brass, 0.4, 0.03, 0, 0.094);
  crest.rotation.x = -Math.PI / 2;
  cover.add(batchArchitecture(ornament));
  const stitches: Group[] = [];
  for (let index = 0; index < 4; index++) {
    const stitch = new Group();
    stitch.position.set(-0.392, 0.08, -0.185 + index * 0.12);
    stitch.visible = false;
    scene.add(stitch);
    g.rod(stitch, m.paper, [-0.008, -0.039, -0.033], [-0.032, 0.035, 0], 0.009);
    g.rod(stitch, m.paper, [-0.032, 0.035, 0], [0.067, 0.05, 0.026], 0.009);
    stitches.push(stitch);
  }
  const beetle = new Group();
  beetle.name = "binding-scarab";
  scene.add(beetle);
  const shell = new Group();
  g.ball(shell, m.brass, 0, 0.091, 0, 0.127, 0.085, 0.19);
  g.box(shell, m.navy, 0, 0.174, -0.016, 0.017, 0.009, 0.29);
  g.ball(shell, m.wood, 0, 0.083, 0.19, 0.081, 0.063, 0.075);
  for (const side of [-1, 1]) {
    g.rod(shell, m.brass, [side * 0.04, 0.11, 0.21], [side * 0.13, 0.18, 0.3], 0.011);
    g.ball(shell, m.ink, side * 0.045, 0.117, 0.241, 0.02);
  }
  beetle.add(batchArchitecture(shell));
  const legs: Group[] = [];
  for (let index = 0; index < 6; index++) {
    const side = index % 2 ? 1 : -1;
    const leg = new Group();
    leg.position.set(side * 0.09, 0.07, -0.12 + Math.floor(index / 2) * 0.11);
    beetle.add(leg);
    g.rod(leg, m.wood, [0, 0, 0], [side * 0.095, 0.025, -0.027], 0.017);
    g.rod(leg, m.wood, [side * 0.095, 0.025, -0.027], [side * 0.12, -0.07, 0.009], 0.014);
    legs.push(leg);
  }
  const needle = new Group();
  needle.position.set(0.09, 0.12, 0.19);
  beetle.add(needle);
  g.rod(needle, m.brass, [0, 0, 0], [0, -0.12, 0.17], 0.009);
  g.hoop(needle, m.brass, 0, 0.021, -0.008, 0.024);
  let repaired = false;
  let repairs = 0;
  const approach = new Vector3();
  return ambientStory(
    scheduler,
    "beetle",
    scene,
    (p, variant, quiet) => {
      const opening = quiet ? 0 : storyEnvelope(p, 0.16, 0.8);
      cover.rotation.z = opening * (0.17 + variant * 0.025);
      const stitching = storyStep(p, 0.36, 0.73);
      for (let index = 0; index < stitches.length; index++)
        stitches[index]!.visible = repaired || (!quiet && stitching > (index + 1) / 4);
      const arrival = storyStep(p, 0.17, 0.34);
      const retreat = storyStep(p, 0.79, 0.97);
      approach.set(
        -0.02 - arrival * 0.41 + retreat * 0.41,
        -0.11 + arrival * 0.235 - retreat * 0.235,
        -0.16 + stitching * 0.34 - retreat * 0.34,
      );
      beetle.position.copy(approach);
      beetle.visible = !quiet && p > 0.17 && p < 0.97;
      beetle.rotation.y = (storyStep(p, 0.27, 0.36) * Math.PI) / 2 - retreat * Math.PI;
      const working = !quiet && p > 0.35 && p < 0.76;
      needle.rotation.x = working ? Math.sin(stitching * Math.PI * 8) * 0.64 : 0;
      const walking = !quiet && ((p > 0.17 && p < 0.34) || (p > 0.79 && p < 0.97));
      for (let index = 0; index < legs.length; index++)
        legs[index]!.rotation.y = walking ? Math.sin(p * 100 + index * Math.PI) * 0.35 : 0;
      scene.userData.phase = p < 0.35 ? "inspect" : p < 0.77 ? "stitch" : "close";
    },
    undefined,
    () => {
      repaired = true;
      scene.userData.repairs = ++repairs;
      for (const stitch of stitches) stitch.visible = true;
    },
  );
}

export function ambientPaperStories(
  g: LibraryGeometry,
  m: LibraryMaterials,
  root: Group,
  scheduler: AmbientScheduler,
) {
  const origami = travellingOrigami(g, m, root, scheduler);
  const beetle = bindingBeetle(g, m, root, scheduler);
  return {
    advance: (still: boolean) => {
      origami.advance(still);
      beetle.advance(still);
    },
  };
}
