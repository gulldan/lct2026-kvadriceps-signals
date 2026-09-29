import { Group } from "three";
import type { LibraryGeometry } from "./geometry";
import type { LibraryMaterials } from "./materials";
import { libraryFurniture } from "./furniture";

/** У каждого нового крыла своя работа и силуэт: места для сценок, а не копии стеллажей. */
export function libraryWings(room: Group, g: LibraryGeometry, m: LibraryMaterials) {
  const furniture = libraryFurniture(g, m);

  function frame(parent: Group, width: number, depth: number, height: number) {
    g.box(parent, m.wood, 0, height - 0.08, 0, width, 0.16, depth);
    g.box(parent, m.oak, 0, height + 0.015, 0, width + 0.07, 0.035, depth + 0.07);
    for (const x of [-width / 2 + 0.17, width / 2 - 0.17]) {
      for (const z of [-depth / 2 + 0.17, depth / 2 - 0.17]) {
        g.box(parent, m.wood, x, height / 2, z, 0.16, height, 0.16);
        g.box(parent, m.brass, x, 0.13, z, 0.18, 0.075, 0.18);
      }
    }
  }

  const press = new Group();
  press.userData.slot = "press";
  press.position.set(-19.5, 0, -14.5);
  room.add(press);
  frame(press, 2.6, 1.7, 1.1);
  g.box(press, m.paper, 0, 1.15, 0.24, 1.1, 0.04, 1.15);
  for (const x of [-0.88, 0.88]) {
    g.box(press, m.wood, x, 1.75, -0.34, 0.22, 3.2, 0.28);
    g.box(press, m.brass, x, 2.99, -0.34, 0.24, 0.14, 0.3);
    g.box(press, m.brass, x, 0.47, -0.34, 0.25, 0.18, 0.31);
  }
  g.box(press, m.wood, 0, 3.04, -0.34, 2.24, 0.3, 0.38);
  g.box(press, m.oak, 0, 3.23, -0.34, 2.44, 0.12, 0.51);
  g.rod(press, m.ink, [0, 1.69, -0.34], [0, 3.45, -0.34], 0.083);
  for (let thread = 0; thread < 15; thread++) {
    const ring = g.hoop(press, m.brass, 0, 2.05 + thread * 0.08, -0.34, 0.105);
    ring.rotation.x = Math.PI / 2;
  }
  g.box(press, m.wood, 0, 1.68, -0.12, 1.36, 0.2, 1.05);
  g.box(press, m.ink, 0, 1.555, -0.12, 1.31, 0.045, 1);
  const handwheel = g.hoop(press, m.brass, 0, 3.4, -0.34, 0.6);
  handwheel.rotation.x = Math.PI / 2;
  for (let spoke = 0; spoke < 6; spoke++) {
    const angle = (spoke * Math.PI) / 3;
    g.rod(
      press,
      m.brass,
      [0, 3.4, -0.34],
      [Math.cos(angle) * 0.6, 3.4, -0.34 + Math.sin(angle) * 0.6],
      0.026,
    );
  }
  for (let line = 0; line < 7; line++) {
    g.box(press, m.ink, -0.08, 1.175, 0.03 + line * 0.092, 0.66, 0.007, 0.022);
  }
  for (let book = 0; book < 4; book++)
    furniture.book(press, 0.92, 1.2 + book * 0.11, 0.48, book, 0.7);
  g.ball(press, m.ink, -1, 1.19, 0.56, 0.1, 0.06, 0.12);
  g.rod(press, m.oak, [-1.16, 1.2, 0.15], [-0.9, 1.2, 0.15], 0.04);
  g.rod(press, m.ink, [-1.03, 1.21, 0.04], [-1.03, 1.21, 0.26], 0.07);

  const bindery = new Group();
  bindery.position.set(-18.1, 0, 2.1);
  bindery.rotation.y = -Math.PI / 2;
  room.add(bindery);
  frame(bindery, 2.35, 0.75, 1.08);
  g.box(bindery, m.claret, 0, 1.12, 0, 1.63, 0.018, 0.59);
  g.box(bindery, m.oak, 0, 0.225, 0, 2.18, 0.075, 0.61);
  for (let bundle = 0; bundle < 7; bundle++) {
    furniture.book(
      bindery,
      -0.92 + (bundle % 4) * 0.53,
      0.3 + Math.floor(bundle / 4) * 0.078,
      0,
      bundle,
      0.72,
    );
  }
  g.box(bindery, m.paper, 0.18, 1.17, -0.02, 0.64, 0.08, 0.49);
  for (const x of [-0.14, 0.51]) {
    g.rod(bindery, m.brass, [x, 1.14, -0.2], [x, 1.93, -0.2], 0.03);
  }
  g.rod(bindery, m.brass, [-0.14, 1.94, -0.2], [0.51, 1.94, -0.2], 0.035);
  for (let stitch = 0; stitch < 5; stitch++)
    g.rod(
      bindery,
      m.paper,
      [-0.06 + stitch * 0.12, 1.18, 0.04],
      [-0.06 + stitch * 0.12, 1.9, -0.2],
      0.005,
    );
  furniture.lamp(bindery, -0.86, 1.11, -0.13);

  const garden = new Group();
  garden.position.set(20.2, 0, 2.5);
  room.add(garden);
  g.box(garden, m.paper, 0, 0.19, 0, 3.2, 0.36, 2.8);
  g.box(garden, m.wood, 0, 0.39, 0, 3.07, 0.05, 2.66);
  g.box(garden, m.sage, 0, 0.424, 0, 2.82, 0.018, 2.4);
  for (const x of [-1.5, 1.5]) {
    for (const z of [-1.3, 1.3]) {
      g.rod(garden, m.brass, [x, 0.42, z], [x, 1.21, z], 0.035);
      g.ball(garden, m.brass, x, 1.24, z, 0.058);
    }
    g.rod(garden, m.brass, [x, 0.79, -1.3], [x, 0.79, 1.3], 0.023);
  }
  for (const x of [-0.89, 0.87]) {
    furniture.plant(garden, x, 0.45, 1.6, 0.42);
    g.rod(garden, m.sage, [x, 0.47, -0.79], [x, 2.4, -0.79], 0.043);
    for (let leaf = 0; leaf < 9; leaf++) {
      const angle = leaf * 2.4;
      const branch = g.ball(
        garden,
        m.sage,
        x + Math.cos(angle) * 0.35,
        1.76 + (leaf % 3) * 0.19,
        -0.79 + Math.sin(angle) * 0.28,
        0.12,
        0.44,
        0.08,
      );
      branch.rotation.set(Math.sin(angle) * 0.7, angle, Math.cos(angle) * 0.8);
    }
  }
  for (const z of [-1.19, 1.19]) {
    for (const x of [-1.38, 1.38]) g.rod(garden, m.brass, [x, 0.43, z], [x, 2.35, z], 0.04);
    for (let segment = 0; segment < 18; segment++) {
      const a = (segment * Math.PI) / 18;
      const b = ((segment + 1) * Math.PI) / 18;
      g.rod(
        garden,
        m.brass,
        [Math.cos(a) * 1.38, 2.35 + Math.sin(a) * 1.04, z],
        [Math.cos(b) * 1.38, 2.35 + Math.sin(b) * 1.04, z],
        0.032,
      );
    }
  }
  g.rod(garden, m.brass, [0, 3.39, -1.19], [0, 3.39, 1.19], 0.03);
  g.box(garden, m.paper, 0, 0.81, 1.1, 0.5, 0.28, 0.07);
  g.box(garden, m.brass, 0, 0.81, 1.145, 0.35, 0.018, 0.01);

  const reception = new Group();
  reception.position.set(6.8, 0, 15.5);
  room.add(reception);
  frame(reception, 3.25, 1.36, 1.23);
  g.box(reception, m.navy, 0, 0.7, 0.54, 2.94, 0.89, 0.14);
  for (const x of [-1.05, 0, 1.05]) {
    g.box(reception, m.brass, x, 0.68, 0.62, 0.86, 0.58, 0.017);
    g.box(reception, m.navy, x, 0.68, 0.635, 0.78, 0.5, 0.02);
  }
  g.hoop(reception, m.brass, 0, 0.72, 0.66, 0.14);
  furniture.lamp(reception, -1.19, 1.26, -0.05);
  for (let stack = 0; stack < 5; stack++) {
    const slip = g.box(reception, m.paper, 0.63, 1.29 + stack * 0.013, 0.1, 0.64, 0.011, 0.45);
    slip.rotation.y = stack * 0.07;
  }
  g.ball(reception, m.brass, -0.24, 1.29, 0.27, 0.13, 0.04, 0.13);
  g.ball(reception, m.brass, -0.24, 1.38, 0.27, 0.1, 0.1, 0.1);
  g.ball(reception, m.wood, -0.24, 1.49, 0.27, 0.034);
  g.box(reception, m.wood, 1.18, 1.36, -0.11, 0.62, 0.18, 0.42);
  g.box(reception, m.recess, 1.18, 1.461, -0.11, 0.52, 0.024, 0.31);

  const reading = new Group();
  reading.position.set(-10, 0, 14.9);
  room.add(reading);
  g.rod(reading, m.claret, [0, 0.055, 0], [0, 0.08, 0], 2.09);
  const carpet = g.hoop(reading, m.brass, 0, 0.088, 0, 1.96);
  carpet.rotation.x = Math.PI / 2;
  const pedestal = new Group();
  pedestal.userData.slot = "round";
  reading.add(pedestal);
  g.rod(pedestal, m.wood, [0, 0.16, 0], [0, 1.16, 0], 0.18);
  g.rod(pedestal, m.oak, [0, 1.12, 0], [0, 1.22, 0], 0.96);
  for (let foot = 0; foot < 3; foot++) {
    const angle = (foot * Math.PI * 2) / 3;
    g.rod(
      pedestal,
      m.wood,
      [0, 0.36, 0],
      [Math.cos(angle) * 0.8, 0.12, Math.sin(angle) * 0.8],
      0.076,
    );
  }
  furniture.chair(reading, -1.24, 0.16, -Math.PI / 2);
  furniture.chair(reading, 1.24, 0.16, Math.PI / 2);
  furniture.chair(reading, 0, 1.24, 0);
  furniture.lamp(reading, 0, 1.24, -0.23);
  furniture.book(reading, -0.45, 1.28, 0.17, 2, 0.79);
  furniture.book(reading, 0.39, 1.28, 0.2, 3, 0.72);
  g.box(reading, m.paper, 0.24, 1.242, -0.56, 0.51, 0.016, 0.27);

  // Открытая южная галерея замыкает длинные маршруты и сохраняет обзор на стол архивариуса.
  for (const x of [-21.4, -15.3, 14.6, 21.2]) {
    const potZ = x < 0 ? 17.0 : 15.8;
    furniture.plant(room, x, potZ, 1.8);
  }
}
