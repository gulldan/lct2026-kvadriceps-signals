import { Group } from "three";
import { batchArchitecture, type LibraryGeometry } from "./geometry";
import { libraryFurniture } from "./furniture";
import type { LibraryMaterials } from "./materials";

export type ResidentKind = "clerk" | "binder" | "reader";

export function worldResident(g: LibraryGeometry, m: LibraryMaterials, kind: ResidentKind) {
  const root = new Group();
  root.name = `library-${kind}`;
  const body = new Group();
  body.position.y = 0.7;
  root.add(body);
  const coat = new Group();
  const clerk = kind === "clerk";
  const reader = kind === "reader";
  g.ball(
    coat,
    clerk ? m.ink : reader ? m.claret : m.sage,
    0,
    0.1,
    0,
    clerk ? 0.235 : 0.33,
    clerk ? 0.46 : 0.38,
    0.26,
  );
  if (clerk) {
    g.box(coat, m.paper, 0, 0.38, 0.19, 0.19, 0.24, 0.04);
    g.box(coat, m.claret, 0, 0.32, 0.224, 0.047, 0.17, 0.023);
    g.box(coat, m.wood, 0.25, -0.1, 0.02, 0.15, 0.35, 0.27);
    g.rod(coat, m.oak, [-0.16, 0.46, 0.15], [0.26, -0.2, 0.17], 0.027);
  } else if (!reader) {
    g.box(coat, m.paper, 0, -0.02, 0.235, 0.44, 0.52, 0.035);
    g.rod(coat, m.oak, [-0.2, 0.21, 0.263], [0.2, 0.21, 0.263], 0.018);
    g.box(coat, m.oak, 0.09, -0.13, 0.265, 0.17, 0.14, 0.023);
  } else g.ball(coat, m.sage, 0, 0.03, -0.15, 0.34, 0.4, 0.21);
  for (let button = 0; button < 3; button++)
    g.ball(coat, m.brass, 0, 0.21 - button * 0.12, 0.261, 0.025, 0.025, 0.012);
  body.add(batchArchitecture(coat));
  const head = new Group();
  head.position.set(0, clerk ? 1.48 : 1.31, clerk ? 0.02 : 0.07);
  root.add(head);
  const face = new Group();
  if (clerk) {
    g.ball(face, m.navy, 0, 0, 0, 0.235, 0.26, 0.23);
    g.ball(face, m.oak, 0, -0.045, 0.268, 0.072, 0.062, 0.19);
    g.ball(face, m.navy, 0, 0.21, -0.06, 0.17, 0.1, 0.12);
    g.hoop(face, m.brass, 0.125, 0.05, 0.192, 0.077);
    g.rod(face, m.brass, [0.19, 0.07, 0.19], [0.21, 0.05, 0.04], 0.011);
  } else {
    g.ball(face, reader ? m.paper : m.oak, 0, 0, 0, 0.295, 0.265, 0.25);
    g.ball(face, m.paper, 0, -0.085, 0.2, 0.19, 0.105, 0.16);
    g.ball(face, m.ink, 0, -0.049, 0.344, 0.054, 0.042, 0.04);
    for (const side of [-1, 1]) {
      g.ball(face, reader ? m.paper : m.oak, side * 0.24, 0.15, -0.02, 0.09, 0.11, 0.08);
      if (!reader) {
        const stripe = g.ball(face, m.navy, side * 0.12, 0.065, 0.212, 0.077, 0.186, 0.037);
        stripe.rotation.z = side * -0.19;
      }
      if (reader) g.hoop(face, m.brass, side * 0.12, 0.04, 0.248, 0.108);
    }
    if (reader) {
      g.ball(face, m.paper, 0, -0.225, 0.21, 0.12, 0.21, 0.075);
      g.rod(face, m.brass, [-0.02, 0.04, 0.253], [0.02, 0.04, 0.253], 0.016);
    }
  }
  for (const side of [-1, 1]) {
    g.ball(
      face,
      m.ink,
      side * (clerk ? 0.137 : 0.12),
      0.05,
      clerk ? 0.184 : 0.259,
      0.033,
      0.043,
      0.025,
    );
    g.ball(
      face,
      m.light,
      side * (clerk ? 0.137 : 0.12) - 0.009,
      0.065,
      clerk ? 0.208 : 0.281,
      0.009,
    );
  }
  head.add(batchArchitecture(face));
  const arms: Group[] = [];
  const feet: Group[] = [];
  for (const side of [-1, 1]) {
    const arm = new Group();
    arm.position.set(side * (clerk ? 0.23 : 0.3), 1.01, 0);
    root.add(arm);
    const sleeve = new Group();
    g.rod(
      sleeve,
      clerk ? m.paper : reader ? m.claret : m.sage,
      [0, 0, 0],
      [side * 0.015, -0.36, 0.025],
      clerk ? 0.061 : 0.078,
    );
    g.ball(sleeve, clerk ? m.oak : m.paper, side * 0.015, -0.38, 0.028, 0.074, 0.084, 0.068);
    arm.add(batchArchitecture(sleeve));
    arms.push(arm);
    const foot = new Group();
    foot.position.set(side * 0.14, 0.32, 0);
    root.add(foot);
    const boot = new Group();
    g.rod(boot, m.wood, [0, 0, 0], [0, -0.21, 0], 0.064);
    g.ball(boot, m.ink, 0, -0.225, 0.072, 0.102, 0.065, 0.17);
    foot.add(batchArchitecture(boot));
    feet.push(foot);
  }
  const held = new Group();
  held.position.set(0, 0.85, 0.39);
  root.add(held);
  const book = libraryFurniture(g, m).book(held, 0, 0, 0, reader ? 1 : 2, 1.1);
  book.rotation.x = -0.14;
  book.visible = reader;
  const letter = new Group();
  held.add(letter);
  g.box(letter, m.paper, 0, 0.08, 0.03, 0.38, 0.025, 0.27);
  g.ball(letter, m.claret, 0, 0.1, 0.03, 0.046, 0.011, 0.046);
  letter.visible = false;
  const brush = new Group();
  held.add(brush);
  g.rod(brush, m.oak, [0.16, -0.2, 0], [0.16, 0.3, 0], 0.018);
  g.ball(brush, m.paper, 0.16, 0.34, 0, 0.12, 0.11, 0.085);
  brush.visible = false;
  root.scale.setScalar(clerk ? 1.14 : 1.08);
  // Готовая модель жителя привязывается скелетом к этим же группам в позе покоя.
  for (const part of [body, head, ...arms, ...feet]) part.userData.rest = part.position.clone();
  root.userData.rig = { body, head, limbs: arms, feet, keep: [held] };
  return { root, body, head, arms, feet, held, book, letter, brush };
}
export type WorldResident = ReturnType<typeof worldResident>;

export function worldCart(g: LibraryGeometry, m: LibraryMaterials) {
  const root = new Group();
  root.name = "bookbinders-cart";
  const frame = new Group();
  g.box(frame, m.wood, 0, 0.39, 0, 0.66, 0.09, 0.84);
  for (const side of [-1, 1]) {
    g.box(frame, m.oak, side * 0.33, 0.54, 0, 0.037, 0.31, 0.89);
    g.rod(frame, m.brass, [side * 0.26, 0.41, -0.39], [side * 0.26, 0.96, -0.47], 0.025);
  }
  g.rod(frame, m.brass, [-0.26, 0.96, -0.47], [0.26, 0.96, -0.47], 0.026);
  root.add(batchArchitecture(frame));
  const wheels: Group[] = [];
  for (const x of [-0.35, 0.35])
    for (const z of [-0.28, 0.28]) {
      const wheel = new Group();
      wheel.position.set(x, 0.18, z);
      root.add(wheel);
      const ring = g.hoop(wheel, m.ink, 0, 0, 0, 0.14);
      ring.rotation.y = Math.PI / 2;
      g.rod(wheel, m.brass, [0, -0.13, 0], [0, 0.13, 0], 0.012);
      wheels.push(wheel);
    }
  const books: Group[] = [];
  const f = libraryFurniture(g, m);
  for (let index = 0; index < 8; index++)
    books.push(
      f.book(
        root,
        (index % 2 ? 1 : -1) * 0.155,
        0.49 + Math.floor(index / 2) * 0.077,
        index % 2 ? 0.12 : -0.13,
        index,
        0.72,
      ),
    );
  return { root, wheels, books };
}

export function worldCat(g: LibraryGeometry, m: LibraryMaterials) {
  const root = new Group();
  root.name = "reading-room-cat";
  const body = new Group();
  root.add(body);
  const fur = new Group();
  g.ball(fur, m.oak, 0, 0.24, 0, 0.27, 0.21, 0.42);
  for (const side of [-1, 1]) g.ball(fur, m.paper, side * 0.17, 0.1, 0.25, 0.075, 0.08, 0.16);
  body.add(batchArchitecture(fur));
  const head = new Group();
  head.position.set(0, 0.43, 0.3);
  root.add(head);
  const face = new Group();
  g.ball(face, m.oak, 0, 0, 0, 0.2, 0.195, 0.18);
  for (const side of [-1, 1]) {
    const ear = g.ball(face, m.oak, side * 0.137, 0.166, -0.015, 0.066, 0.145, 0.053);
    ear.rotation.z = -side * 0.22;
    g.ball(face, m.brass, side * 0.08, 0.005, 0.166, 0.048, 0.033, 0.018);
    g.ball(face, m.ink, side * 0.08, 0.004, 0.182, 0.012, 0.028, 0.008);
    g.rod(face, m.paper, [side * 0.04, -0.062, 0.18], [side * 0.2, -0.055, 0.232], 0.007);
  }
  g.ball(face, m.claret, 0, -0.06, 0.192, 0.026, 0.021, 0.021);
  head.add(batchArchitecture(face));
  const tail = new Group();
  tail.position.set(0, 0.2, -0.35);
  root.add(tail);
  const plume = new Group();
  for (let part = 0; part < 5; part++) {
    const a = part / 5,
      b = (part + 1) / 5;
    g.rod(
      plume,
      m.oak,
      [Math.sin(a * 2.8) * 0.2, a * 0.1, -a * 0.44],
      [Math.sin(b * 2.8) * 0.2, b * 0.1, -b * 0.44],
      0.06 - a * 0.027,
    );
  }
  tail.add(batchArchitecture(plume));
  for (const part of [body, head, tail]) part.userData.rest = part.position.clone();
  root.userData.rig = { body, head, tail };
  return { root, body, head, tail };
}
