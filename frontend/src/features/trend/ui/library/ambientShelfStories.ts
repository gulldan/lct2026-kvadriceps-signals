import { Group } from "three";
import { batchArchitecture, type LibraryGeometry } from "./geometry";
import type { LibraryMaterials } from "./materials";
import type { AmbientScheduler } from "./ambientScheduler";
import { ambientStory, storyEnvelope, storyStep } from "./ambientStory";

function shelfOwl(
  g: LibraryGeometry,
  m: LibraryMaterials,
  root: Group,
  scheduler: AmbientScheduler,
) {
  const scene = new Group();
  scene.name = "curiosity-owl";
  scene.position.set(-4.82, 1.81, -11);
  scene.rotation.y = Math.PI / 2;
  root.add(scene);
  const setting = new Group();
  g.box(setting, m.recess, 0, 0.58, -0.42, 1.38, 1.16, 0.04);
  g.box(setting, m.oak, 0, 0.018, -0.15, 1.41, 0.06, 0.55);
  for (const side of [-1, 1]) {
    for (let index = 0; index < 2; index++) {
      const x = side * (0.53 + index * 0.105);
      g.box(setting, index ? m.navy : m.claret, x, 0.45, -0.12, 0.095, 0.88, 0.4);
      for (const y of [0.15, 0.7]) g.box(setting, m.brass, x, y, 0.087, 0.077, 0.027, 0.014);
    }
  }
  scene.add(batchArchitecture(setting));
  const doors: Group[] = [];
  for (const side of [-1, 1]) {
    const door = new Group();
    door.position.set(side * 0.43, 0.02, 0.02);
    scene.add(door);
    const detail = new Group();
    g.box(detail, m.sage, -side * 0.207, 0.49, 0, 0.412, 0.97, 0.075);
    g.box(detail, m.brass, -side * 0.207, 0.49, 0.043, 0.32, 0.77, 0.012);
    g.box(detail, m.sage, -side * 0.207, 0.49, 0.055, 0.29, 0.73, 0.012);
    g.hoop(detail, m.brass, -side * 0.207, 0.54, 0.068, 0.105);
    door.add(batchArchitecture(detail));
    doors.push(door);
  }
  const owl = new Group();
  owl.name = "secret-shelf-owl";
  scene.add(owl);
  const feathers = new Group();
  g.ball(feathers, m.oak, 0, 0.39, 0, 0.25, 0.34, 0.22);
  g.ball(feathers, m.paper, 0, 0.37, 0.16, 0.17, 0.24, 0.08);
  for (const side of [-1, 1]) {
    g.ball(feathers, m.wood, side * 0.24, 0.4, 0, 0.09, 0.24, 0.16);
    g.rod(feathers, m.brass, [side * 0.11, 0.06, 0], [side * 0.11, 0.05, 0.19], 0.027);
  }
  owl.add(batchArchitecture(feathers));
  const head = new Group();
  head.position.y = 0.76;
  owl.add(head);
  const face = new Group();
  g.ball(face, m.oak, 0, 0, 0, 0.3, 0.27, 0.25);
  for (const side of [-1, 1]) {
    g.ball(face, m.paper, side * 0.135, 0.035, 0.206, 0.14, 0.15, 0.074);
    const ear = g.ball(face, m.wood, side * 0.22, 0.21, 0, 0.065, 0.17, 0.07);
    ear.rotation.z = -side * 0.3;
  }
  g.rod(face, m.brass, [0, -0.027, 0.21], [0, -0.14, 0.36], 0.065);
  head.add(batchArchitecture(face));
  const eyes = new Group();
  head.add(eyes);
  for (const side of [-1, 1]) {
    g.ball(eyes, m.ink, side * 0.135, 0.035, 0.277, 0.067, 0.083, 0.025);
    g.ball(eyes, m.light, side * 0.12, 0.07, 0.301, 0.022, 0.025, 0.012);
  }
  return ambientStory(scheduler, "owl", scene, (p, variant, quiet) => {
    const reveal = quiet ? 0 : storyEnvelope(p, 0.22, 0.75);
    doors[0]!.rotation.y = -reveal * 1.34;
    doors[1]!.rotation.y = reveal * 1.34;
    owl.visible = !quiet && p > 0.085 && p < 0.96;
    owl.position.z = -0.34 + storyStep(p, 0.15, 0.34) * (1 - storyStep(p, 0.68, 0.9)) * 0.57;
    const look = storyStep(p, 0.37, 0.44) - 2 * storyStep(p, 0.49, 0.58) + storyStep(p, 0.63, 0.68);
    head.rotation.y = quiet ? 0 : look * (variant % 2 ? -0.66 : 0.66);
    head.rotation.z = quiet ? 0 : (storyStep(p, 0.59, 0.63) - storyStep(p, 0.67, 0.71)) * 0.19;
    const blink = (storyStep(p, 0.45, 0.455) - storyStep(p, 0.465, 0.47)) * 0.92;
    eyes.scale.y = quiet ? 1 : 1 - blink;
    scene.userData.phase = p < 0.25 ? "open" : p < 0.69 ? "peek" : "hide";
  });
}

function bookDomino(
  g: LibraryGeometry,
  m: LibraryMaterials,
  root: Group,
  scheduler: AmbientScheduler,
) {
  const scene = new Group();
  scene.name = "curiosity-domino";
  scene.position.set(-4.82, 1.81, 3.3);
  scene.rotation.y = Math.PI / 2;
  root.add(scene);
  const detail = new Group();
  g.box(detail, m.oak, 0, 0.03, -0.08, 1.4, 0.06, 0.48);
  for (const x of [-0.67, 0.67]) {
    g.box(detail, m.brass, x, 0.14, -0.05, 0.065, 0.22, 0.44);
    g.ball(detail, m.brass, x, 0.27, -0.05, 0.069);
  }
  scene.add(batchArchitecture(detail));
  const books: Group[] = [];
  const colors = [m.claret, m.navy, m.sage, m.oak, m.claret];
  for (let index = 0; index < 5; index++) {
    const book = new Group();
    book.position.set(-0.52 + index * 0.22, 0.063, -0.08);
    scene.add(book);
    const binding = new Group();
    const height = 0.66 - (index % 2) * 0.065;
    g.box(binding, colors[index]!, 0, height / 2, 0, 0.145, height, 0.35);
    g.box(binding, m.paper, 0, height / 2, -0.031, 0.108, height - 0.035, 0.296);
    for (const y of [0.12, height - 0.11])
      g.box(binding, m.brass, 0, y, 0.178, 0.139, 0.025, 0.015);
    book.add(batchArchitecture(binding));
    books.push(book);
  }
  const reset = new Group();
  reset.position.set(-0.63, 0.07, -0.3);
  scene.add(reset);
  g.rod(reset, m.brass, [0, 0, 0], [0, 0.41, 0], 0.027);
  g.rod(reset, m.brass, [0, 0.41, 0], [0, 0.41, 0.4], 0.024);
  const crank = new Group();
  crank.position.set(-0.62, 0.17, 0.22);
  scene.add(crank);
  g.hoop(crank, m.brass, 0, 0, 0, 0.12);
  g.rod(crank, m.oak, [0, 0, 0], [0, 0.105, 0], 0.017);
  g.ball(crank, m.wood, 0, 0.105, 0.045, 0.03);
  return ambientStory(scheduler, "domino", scene, (p, variant, quiet) => {
    for (let index = 0; index < books.length; index++) {
      const fall = storyStep(p, 0.13 + index * 0.045, 0.22 + index * 0.045);
      const restore = storyStep(p, 0.58 + (4 - index) * 0.06, 0.68 + (4 - index) * 0.06);
      books[index]!.rotation.z = quiet ? 0 : -fall * (1 - restore) * (0.43 + variant * 0.012);
    }
    const winding = storyStep(p, 0.49, 0.59);
    const sweep = storyStep(p, 0.58, 0.91);
    reset.position.x = -0.63 + (quiet ? 0 : (1 - sweep) * winding * 1.13);
    reset.rotation.x = quiet ? 0 : (winding - storyStep(p, 0.91, 1)) * -0.12;
    crank.rotation.z = quiet ? 0 : (winding + sweep * 2) * Math.PI * 2;
    scene.userData.phase = p < 0.4 ? "topple" : p < 0.58 ? "wait" : "restore";
  });
}

function livingBookmark(
  g: LibraryGeometry,
  m: LibraryMaterials,
  root: Group,
  scheduler: AmbientScheduler,
) {
  const scene = new Group();
  scene.name = "curiosity-bookmark";
  scene.position.set(-10.45, 1.33, 15.07);
  root.add(scene);
  const ghost = new Group();
  ghost.name = "living-bookmark";
  scene.add(ghost);
  const paper = new Group();
  g.box(paper, m.paper, 0, 0.24, 0, 0.31, 0.68, 0.026);
  for (let tab = 0; tab < 3; tab++) {
    const fold = g.box(
      paper,
      m.paper,
      -0.103 + tab * 0.103,
      -0.13 - (tab % 2) * 0.04,
      0.026,
      0.103,
      0.2,
      0.021,
    );
    fold.rotation.x = (tab - 1) * 0.36;
  }
  g.box(paper, m.claret, 0, 0.555, 0.018, 0.32, 0.052, 0.012);
  for (const side of [-1, 1]) {
    g.ball(paper, m.ink, side * 0.068, 0.34, 0.022, 0.026, 0.043, 0.012);
    g.hoop(paper, m.brass, side * 0.069, 0.34, 0.035, 0.055);
  }
  ghost.add(batchArchitecture(paper));
  return ambientStory(scheduler, "bookmark", scene, (p, variant, quiet) => {
    ghost.visible = !quiet && p > 0.045 && p < 0.98;
    const lift = storyEnvelope(p, 0.2, 0.75);
    const excursion = storyStep(p, 0.27, 0.55) * (1 - storyStep(p, 0.61, 0.86));
    ghost.position.set(
      excursion * (variant % 2 ? -0.74 : 0.74),
      -0.37 + lift * 1.21 + Math.sin(p * Math.PI * 4) * excursion * 0.1,
      excursion * 0.59,
    );
    ghost.rotation.set(
      excursion * 0.16,
      Math.sin(p * Math.PI * 2) * excursion * (variant % 2 ? -0.66 : 0.66),
      Math.sin(p * Math.PI * 3) * excursion * 0.14,
    );
    scene.userData.phase = p < 0.25 ? "emerge" : p < 0.74 ? "inspect" : "return";
  });
}

export function ambientShelfStories(
  g: LibraryGeometry,
  m: LibraryMaterials,
  root: Group,
  scheduler: AmbientScheduler,
) {
  const stories = [
    shelfOwl(g, m, root, scheduler),
    bookDomino(g, m, root, scheduler),
    livingBookmark(g, m, root, scheduler),
  ];
  return {
    advance: (still: boolean) => {
      for (const story of stories) story.advance(still);
    },
  };
}
