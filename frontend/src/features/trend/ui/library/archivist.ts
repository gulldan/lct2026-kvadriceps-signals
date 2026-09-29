import { Group, Vector3 } from "three";
import type { LibraryGeometry } from "./geometry";
import type { LibraryMaterials } from "./materials";
import { libraryFurniture } from "./furniture";
import { DESK_BOOK, SEAT } from "./layout";

export function libraryArchivist(g: LibraryGeometry, m: LibraryMaterials) {
  // Оперение не должно получать древесные волокна общих материалов зала.
  const plumage = m.oak.clone();
  plumage.userData = {};
  plumage.roughness = 0.92;
  const crest = m.wood.clone();
  crest.userData = {};
  crest.color.lerp(m.oak.color, 0.35);
  const down = m.paper.clone();
  down.color.lerp(m.oak.color, 0.18);
  down.roughness = 0.95;
  const iris = m.brass.clone();
  iris.metalness = 0;
  iris.roughness = 0.35;
  iris.emissive.copy(m.brass.color);
  iris.emissiveIntensity = 0.35;
  const root = new Group();
  const body = new Group();
  root.add(body);
  g.ball(body, m.navy, 0, 0.59, 0, 0.3, 0.37, 0.25);
  g.ball(body, down, 0, 0.64, 0.18, 0.18, 0.26, 0.11);
  // Поперечные шевроны на светлой груди — рисунок оперения настоящей совы.
  for (let row = 0; row < 4; row++) {
    for (const column of row % 2 ? [-0.045, 0.045] : [-0.085, 0, 0.085]) {
      const y = 0.8 - row * 0.075;
      const lift = 1 - ((y - 0.64) / 0.26) ** 2 - (column / 0.18) ** 2;
      const z = 0.186 + 0.11 * Math.sqrt(Math.max(0, lift));
      for (const side of [-1, 1]) {
        const stroke = g.box(body, crest, column + side * 0.012, y, z, 0.026, 0.008, 0.008);
        stroke.rotation.z = side * -0.55;
      }
    }
  }
  for (const y of [0.52, 0.64, 0.76]) g.ball(body, m.brass, 0.1, y, 0.255, 0.022);
  for (const side of [-1, 1]) {
    const lapel = g.box(body, m.ink, side * 0.13, 0.74, 0.225, 0.05, 0.2, 0.02);
    lapel.rotation.set(-0.25, side * 0.45, side * 0.35);
  }
  g.rod(body, m.brass, [0.1, 0.64, 0.255], [0.16, 0.57, 0.245], 0.007);
  g.rod(body, m.brass, [0.16, 0.57, 0.245], [0.2, 0.6, 0.22], 0.007);
  g.box(body, m.ink, 0.2, 0.57, 0.215, 0.1, 0.012, 0.03);
  for (let feather = -1; feather <= 1; feather++) {
    const plume = g.ball(body, crest, feather * 0.06, 0.26, -0.22, 0.045, 0.13, 0.03);
    plume.rotation.set(0.6, 0, feather * 0.3);
  }
  // Шарф из тёплой шерсти с полосой и двумя кистями.
  const scarf = g.hoop(body, m.claret, 0, 0.87, 0.01, 0.21);
  scarf.rotation.x = Math.PI / 2;
  scarf.scale.z = 0.24;
  g.hoop(body, m.claret, 0, 0.86, 0.01, 0.19).rotation.x = Math.PI / 2;
  for (const [x, lean] of [
    [0.1, 0.12],
    [0.17, 0.28],
  ] as const) {
    const end = g.box(body, m.claret, x, 0.72, 0.21, 0.07, 0.2, 0.025);
    end.rotation.z = lean;
    g.box(end, m.paper, 0, -0.2, 0, 1.02, 0.12, 1.1);
    for (const fringe of [-0.3, 0, 0.3]) g.box(end, m.claret, fringe, -0.58, 0, 0.14, 0.18, 0.9);
  }
  const head = new Group();
  head.position.set(0, 1.03, 0);
  body.add(head);
  const eyes: Group[] = [];
  g.ball(head, plumage, 0, 0, 0, 0.37, 0.31, 0.29);
  for (const side of [-1, 1]) {
    const ear = g.ball(head, crest, side * 0.27, 0.23, -0.035, 0.11, 0.21, 0.09);
    ear.rotation.z = -side * 0.38;
    const tip = g.ball(head, plumage, side * 0.32, 0.36, -0.04, 0.045, 0.1, 0.04);
    tip.rotation.z = -side * 0.55;
    g.ball(head, down, side * 0.153, -0.014, 0.216, 0.182, 0.208, 0.11);
    const rim = g.hoop(head, crest, side * 0.153, -0.02, 0.232, 0.19);
    rim.scale.y *= 1.12;
    const brow = g.ball(head, crest, side * 0.14, 0.13, 0.3, 0.12, 0.035, 0.035);
    brow.rotation.z = side * 0.32;
    const eye = new Group();
    eye.position.set(side * 0.15, 0.015, 0.318);
    head.add(eye);
    g.ball(eye, iris, 0, 0, -0.004, 0.068, 0.078, 0.026);
    g.ball(eye, m.ink, 0, 0, 0.012, 0.042, 0.05, 0.02);
    g.ball(eye, m.light, -0.018, 0.024, 0.026, 0.016);
    eyes.push(eye);
    g.hoop(head, m.brass, side * 0.162, 0.01, 0.343, 0.15);
    g.rod(head, m.brass, [side * 0.3, 0.02, 0.32], [side * 0.34, 0.045, 0.05], 0.012);
    for (let i = 0; i < 3; i++) {
      const feather = g.ball(
        head,
        down,
        side * (0.12 + i * 0.07),
        -0.19,
        0.17,
        0.035,
        0.085,
        0.035,
      );
      feather.rotation.z = side * 0.4;
    }
  }
  g.rod(head, m.brass, [-0.025, 0.026, 0.344], [0.025, 0.026, 0.344], 0.014);
  g.ball(head, crest, 0, -0.08, 0.34, 0.055, 0.07, 0.07);
  const hook = g.ball(head, m.ink, 0, -0.13, 0.36, 0.03, 0.045, 0.035);
  hook.rotation.x = 0.4;
  const wings = [-1, 1].map((side) => {
    const wing = new Group();
    wing.position.set(side * 0.26, 0.81, 0);
    body.add(wing);
    const feathers = g.ball(wing, m.navy, side * 0.04, -0.17, 0, 0.11, 0.28, 0.12);
    feathers.rotation.z = side * 0.14;
    // Маховые перья выглядывают из рукава сюртука.
    for (let quill = 0; quill < 3; quill++) {
      const primary = g.ball(
        wing,
        quill === 1 ? plumage : crest,
        side * (0.03 + quill * 0.022),
        -0.4 - quill * 0.012,
        -0.03 + quill * 0.035,
        0.035,
        0.1,
        0.035,
      );
      primary.rotation.z = side * (0.1 + quill * 0.12);
    }
    g.ball(wing, m.oak, side * 0.045, -0.39, 0.015, 0.065, 0.09, 0.07);
    g.hoop(wing, m.brass, side * 0.045, -0.33, 0.01, 0.07).rotation.x = Math.PI / 2;
    return wing;
  });
  const feet = [-1, 1].map((side) => {
    const foot = new Group();
    foot.position.set(side * 0.14, 0.21, 0);
    root.add(foot);
    g.rod(foot, crest, [0, 0, 0], [0, -0.12, 0], 0.055);
    g.ball(foot, m.wood, 0, -0.14, 0.06, 0.11, 0.065, 0.18);
    for (const toe of [-1, 0, 1])
      g.ball(foot, m.ink, toe * 0.045, -0.155, 0.22, 0.018, 0.018, 0.03);
    return foot;
  });
  // Поза покоя нужна, чтобы привязать скелет готовой модели к этим же группам.
  for (const part of [body, head, ...wings, ...feet]) part.userData.rest = part.position.clone();
  root.name = "library-archivist";
  const book = libraryFurniture(g, m).book(body, 0, 0.62, 0.31, 1, 0.85);
  book.rotation.x = Math.PI / 2;
  book.visible = false;
  const pencil = new Group();
  pencil.position.set(0.31, 1.266, 0.58);
  body.add(pencil);
  g.rod(pencil, m.brass, [0, 0, 0], [0.1, 0.34, 0], 0.018);
  pencil.visible = false;
  const handkerchief = new Group();
  body.add(handkerchief);
  g.box(handkerchief, m.paper, 0, 0, 0, 0.17, 0.035, 0.15);
  g.box(handkerchief, m.sage, 0, 0.02, 0, 0.13, 0.005, 0.016);
  handkerchief.visible = false;
  root.userData.rig = { body, head, limbs: wings, feet, keep: [book, pencil, handkerchief] };
  return { root, body, head, wings, feet, eyes, book, pencil, handkerchief };
}

export function libraryLadder(g: LibraryGeometry, m: LibraryMaterials) {
  const root = new Group();
  root.userData.slot = "ladder";
  for (const side of [-1, 1]) {
    g.rod(root, m.oak, [side * 0.3, 0.12, 0.3], [side * 0.3, 3.02, 0.87], 0.048);
    const wheel = g.hoop(root, m.ink, side * 0.34, 0.11, 0.3, 0.11);
    wheel.rotation.y = Math.PI / 2;
    g.ball(root, m.brass, side * 0.35, 0.11, 0.3, 0.05);
    g.ball(root, m.brass, side * 0.3, 3.02, 0.87, 0.065);
  }
  for (let i = 0; i < 10; i++) {
    g.box(root, m.oak, 0, 0.26 + i * 0.277, 0.328 + i * 0.055, 0.63, 0.045, 0.16);
  }
  return root;
}

export function libraryDeskWork(g: LibraryGeometry, m: LibraryMaterials) {
  const root = new Group();
  root.position.set(DESK_BOOK[0], 1.2, DESK_BOOK[1]);
  const f = libraryFurniture(g, m);
  const opening = new Group();
  opening.position.y = 0.03;
  root.add(opening);
  for (const side of [-1, 1]) {
    const parent = side === 1 ? opening : root;
    g.box(parent, m.wood, side * 0.23, 0, 0, 0.46, 0.035, 0.63);
    const page = g.box(parent, m.paper, side * 0.225, 0.035, 0, 0.44, 0.035, 0.6);
    page.rotation.z = side * -0.06;
    for (let i = 0; i < 7; i++)
      g.box(parent, m.oak, side * 0.23, 0.06, -0.22 + i * 0.068, 0.31, 0.002, 0.008);
  }
  const pageTurn = new Group();
  pageTurn.position.y = 0.068;
  root.add(pageTurn);
  g.box(pageTurn, m.paper, 0.22, 0, 0, 0.44, 0.008, 0.59);
  const pile: Group[] = [];
  function nextMaterialPosition() {
    const index = pile.length;
    return new Vector3(
      index % 2 ? 1.35 : -1.35,
      0.055 + Math.floor(index / 4) * 0.09,
      index % 4 < 2 ? 0 : 0.62,
    );
  }
  function deposit() {
    const position = nextMaterialPosition();
    const book = f.book(root, position.x, position.y, position.z, pile.length, 0.85);
    pile.push(book);
  }
  const chair = new Group();
  chair.position.set(SEAT[0] + 1.2, 0, SEAT[1]);
  f.chair(chair, 0, 0, Math.PI);
  const folder = new Group();
  folder.position.set(0, 0, 0.66);
  root.add(folder);
  g.box(folder, m.claret, 0, 0, 0, 0.6, 0.065, 0.77);
  g.box(folder, m.paper, 0.025, 0.045, -0.025, 0.52, 0.016, 0.68);
  g.box(folder, m.brass, 0.17, 0.065, -0.28, 0.07, 0.02, 0.16);
  return { root, pageTurn, opening, pile, folder, chair, nextMaterialPosition, deposit };
}
