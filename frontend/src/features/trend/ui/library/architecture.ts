import { Group, Mesh, Shape, ExtrudeGeometry } from "three";
import { batchArchitecture, type LibraryGeometry } from "./geometry";
import { libraryFurniture } from "./furniture";
import type { LibraryMaterials } from "./materials";
import { libraryVignettes } from "./vignettes";
import { libraryOrnaments } from "./ornaments";
import { libraryWings } from "./wings";
import { libraryStacks } from "./stacks";
import { libraryFloors } from "./floors";
import { libraryPlinth } from "./plinth";
import { ROOM_WIDTH, ROOM_DEPTH } from "./layout";

export function libraryArchitecture(g: LibraryGeometry, m: LibraryMaterials) {
  const room = new Group();
  const f = libraryFurniture(g, m);
  const windowGeometry: ExtrudeGeometry[] = [];
  const windows: { x: number; z: number; rotation: number }[] = [];
  const bluePlaster = m.plaster.clone();
  bluePlaster.color.lerp(m.navy.color, 0.34);
  const rosePlaster = m.plaster.clone();
  rosePlaster.color.lerp(m.claret.color, 0.26);
  g.box(room, m.wood, 0, -0.27, 0, ROOM_WIDTH, 0.51, ROOM_DEPTH);
  g.box(room, m.oak, 0, -0.035, 0, ROOM_WIDTH + 0.1, 0.09, ROOM_DEPTH + 0.1);
  libraryFloors(room, m);
  libraryPlinth(room, g, m);
  const back = -ROOM_DEPTH / 2 + 0.13;
  const left = -ROOM_WIDTH / 2 + 0.13;
  g.box(room, m.plaster, 0, 2.85, back, ROOM_WIDTH - 0.15, 5.7, 0.24);
  g.box(room, m.plaster, left, 2.85, 0, 0.24, 5.7, ROOM_DEPTH - 0.35);
  g.box(room, bluePlaster, -10.67, 2.8, back + 0.133, 10.75, 5.5, 0.025);
  g.box(room, bluePlaster, left + 0.14, 2.8, -8.2, 0.025, 5.5, 9.4);
  g.box(room, rosePlaster, 11.2, 2.8, back + 0.133, 9.9, 5.5, 0.025);
  // Отделка стен уступает место готовым модулям стены, когда они догружены.
  const trim = new Group();
  trim.userData.slot = "wall";
  room.add(trim);
  g.box(trim, m.wood, left + 0.147, 0.8, 4.65, 0.027, 1.33, 26.9);
  for (let panel = 0; panel < 28; panel++) {
    g.box(trim, m.oak, left + 0.177, 0.8, -8.35 + panel * 0.94, 0.025, 1.12, 0.05);
  }
  for (const height of [0.22, 4.96, 5.43, 5.65]) {
    g.box(trim, m.oak, 0, height, back + 0.18, ROOM_WIDTH - 0.3, 0.11, 0.18);
    g.box(trim, m.oak, left + 0.18, height, 0, 0.18, 0.11, ROOM_DEPTH - 0.4);
  }
  for (const x of [-22.7, -16.6, -9.5, -5.4, -0.15, 6.1, 8.4, 16.6, 22.7]) {
    g.box(trim, m.plaster, x, 2.7, back + 0.28, 0.38, 5.36, 0.34);
    g.box(trim, m.brass, x, 5.22, back + 0.51, 0.54, 0.13, 0.42);
    g.box(trim, m.oak, x, 0.27, back + 0.41, 0.52, 0.39, 0.52);
  }

  function books(
    parent: Group,
    width: number,
    height: number,
    rows: number,
    seed: number,
    side = 1,
  ) {
    const interval = (height - 0.6) / rows;
    for (let row = 0; row <= rows; row++) {
      const y = 0.36 + row * interval;
      g.box(parent, m.oak, 0, y, side * 0.09, width, 0.075, 0.61);
      g.box(parent, m.brass, 0, y - 0.035, side * 0.404, width, 0.016, 0.013);
      if (row === rows) continue;
      let cursor = -width / 2 + 0.13;
      let index = 0;
      while (cursor < width / 2 - 0.2) {
        const variation = (index * 17 + row * 11 + seed * 7) % 23;
        const thickness = 0.073 + (variation % 5) * 0.021;
        const bookHeight = interval * (0.63 + (variation % 6) * 0.044);
        const cover = [m.navy, m.paper, m.oak, m.sage, m.paper, m.claret, m.wood][variation % 7]!;
        const center = cursor + thickness / 2;
        g.box(
          parent,
          cover,
          center,
          y + bookHeight / 2 + 0.04,
          side * 0.2,
          thickness,
          bookHeight,
          0.35,
        );
        for (const band of [0.14, 0.84]) {
          g.box(
            parent,
            m.brass,
            center,
            y + bookHeight * band + 0.04,
            side * 0.38,
            thickness * 0.82,
            0.015,
            0.009,
          );
        }
        if (variation % 3 === 0) {
          g.box(
            parent,
            m.paper,
            center,
            y + bookHeight * 0.63,
            side * 0.381,
            thickness * 0.53,
            bookHeight * 0.14,
            0.012,
          );
        }
        cursor += thickness + 0.024;
        index++;
      }
    }
  }

  function bookcase(
    x: number,
    z: number,
    width: number,
    height: number,
    rotation = 0,
    seed = 0,
    style: "science" | "engineering" | "world" | "archive" = "science",
  ) {
    const group = new Group();
    group.userData.slot = "bookcase";
    group.position.set(x, 0, z);
    group.rotation.y = rotation;
    room.add(group);
    const depth = 0.7;
    const frame =
      style === "science"
        ? m.navy
        : style === "engineering"
          ? m.ink
          : style === "archive"
            ? m.claret
            : m.wood;
    g.box(group, m.recess, 0, height / 2, -0.22, width, height, 0.12);
    for (const side of [-1, 1]) {
      g.box(group, frame, (side * width) / 2, height / 2, 0, 0.15, height, depth);
      g.box(group, m.oak, (side * width) / 2, height / 2, 0.39, 0.09, height, 0.055);
      g.box(group, m.brass, (side * width) / 2, height / 2, 0.426, 0.025, height - 0.2, 0.01);
    }
    for (const [y, w, h] of [
      [0.12, width + 0.2, 0.24],
      [height, width + 0.22, 0.13],
      [height + 0.11, width + 0.33, 0.1],
    ] as const) {
      g.box(group, frame, 0, y, 0, w, h, depth);
      g.box(group, m.brass, 0, y + h / 2, depth / 2 + 0.008, w, 0.018, 0.015);
    }
    books(group, width, height, 5, seed);
    if (style === "science") {
      for (let i = 0; i < 18; i++) {
        const a = (i * Math.PI) / 18;
        const b = ((i + 1) * Math.PI) / 18;
        g.rod(
          group,
          m.brass,
          [Math.cos(a) * (width / 2 + 0.1), height + 0.7 * Math.sin(a), 0.17],
          [Math.cos(b) * (width / 2 + 0.1), height + 0.7 * Math.sin(b), 0.17],
          0.043,
        );
      }
      g.hoop(group, m.brass, 0, height + 0.33, 0.17, 0.19);
      g.ball(group, m.light, 0, height + 0.33, 0.18, 0.055);
    } else if (style === "engineering") {
      for (const side of [-1, 1]) {
        g.rod(
          group,
          m.brass,
          [side * (width / 2 + 0.15), 0.25, 0.07],
          [side * (width / 2 + 0.15), height + 0.34, 0.07],
          0.047,
        );
      }
      g.rod(
        group,
        m.brass,
        [-width / 2 - 0.15, height + 0.34, 0.07],
        [width / 2 + 0.15, height + 0.34, 0.07],
        0.047,
      );
      g.ball(group, m.paper, 0, height + 0.1, 0.4, 0.19, 0.19, 0.035);
      g.hoop(group, m.brass, 0, height + 0.1, 0.44, 0.2);
      g.rod(group, m.wood, [0, height + 0.1, 0.48], [0.08, height + 0.21, 0.48], 0.013);
    } else if (style === "archive") {
      for (const drawerX of [-width * 0.3, 0, width * 0.3]) {
        for (const y of [0.46, 0.79, 1.12]) {
          g.box(group, m.oak, drawerX, y, 0.37, width * 0.27, 0.28, 0.11);
          g.box(group, m.paper, drawerX, y + 0.03, 0.431, 0.2, 0.07, 0.01);
          g.rod(
            group,
            m.brass,
            [drawerX - 0.085, y - 0.055, 0.46],
            [drawerX + 0.085, y - 0.055, 0.46],
            0.02,
          );
        }
      }
      for (let i = 0; i < 5; i++) {
        g.box(group, i % 2 ? m.paper : m.oak, -0.9 + i * 0.43, height + 0.31, 0, 0.38, 0.31, 0.51);
        g.box(group, m.brass, -0.9 + i * 0.43, height + 0.31, 0.26, 0.14, 0.08, 0.01);
      }
    } else {
      for (let i = 0; i < 4; i++)
        g.rod(
          group,
          m.paper,
          [-0.8 + i * 0.23, height + 0.22, -0.18],
          [-0.8 + i * 0.23, height + 0.22, 0.27],
          0.085,
        );
      f.globe(group, 0.62, height + 0.19, 0.02, 0.64);
    }
    return group;
  }

  bookcase(-12.3, back + 0.54, 3.1, 4.6, 0, 1, "science");
  for (const [index, x] of [-2.8, 2.5].entries())
    bookcase(x, back + 0.54, 3.1, 4.45, 0, index + 2, "world");
  for (const [index, x] of [10.1, 13.4].entries())
    bookcase(x, back + 0.54, 2.72, 4.55, 0, index + 4, "engineering");
  bookcase(left + 0.54, -9.7, 3.1, 4.6, Math.PI / 2, 6, "science");
  for (const [index, z] of [2.3, 8.2].entries())
    bookcase(left + 0.54, z, 3.5, 4.25, Math.PI / 2, index + 7, "archive");
  bookcase(-19.7, back + 0.54, 4.2, 3.45, 0, 12, "archive");
  bookcase(20.1, back + 0.54, 4.3, 4.15, 0, 13, "world");
  bookcase(left + 0.54, 15.3, 4.2, 3.65, Math.PI / 2, 14, "world");

  function window(x: number, z: number, rotation: number) {
    windows.push({ x, z, rotation });
    const group = new Group();
    group.position.set(x, 0, z);
    group.rotation.y = rotation;
    room.add(group);
    const shape = new Shape();
    shape.moveTo(-0.83, 0.7);
    shape.lineTo(0.83, 0.7);
    shape.lineTo(0.83, 3.24);
    shape.absarc(0, 3.24, 0.83, 0, Math.PI, false);
    shape.lineTo(-0.83, 0.7);
    const geometry = new ExtrudeGeometry(shape, {
      depth: 0.045,
      bevelEnabled: false,
      curveSegments: 20,
    });
    windowGeometry.push(geometry);
    group.add(new Mesh(geometry, m.light));
    for (const side of [-1, 1]) g.box(group, m.oak, side * 0.91, 1.98, 0.08, 0.16, 2.72, 0.2);
    for (let i = 0; i < 24; i++) {
      const a = (i / 24) * Math.PI;
      const b = ((i + 1) / 24) * Math.PI;
      g.rod(
        group,
        m.oak,
        [Math.cos(a) * 0.91, 3.24 + Math.sin(a) * 0.91, 0.08],
        [Math.cos(b) * 0.91, 3.24 + Math.sin(b) * 0.91, 0.08],
        0.085,
      );
    }
    for (const y of [0.7, 1.53, 2.4, 3.24]) g.box(group, m.brass, 0, y, 0.12, 1.68, 0.035, 0.07);
    g.box(group, m.brass, 0, 2.35, 0.12, 0.045, 3.32, 0.07);
    for (const dividerX of [-0.4, 0.4]) g.box(group, m.brass, dividerX, 2, 0.12, 0.022, 2.58, 0.05);
    g.box(group, m.oak, 0, 0.64, 0.18, 2, 0.13, 0.56);
    f.plant(group, -0.38, 0.19, 0.45, 0.73);
    f.book(group, 0.38, 0.78, 0.18, 2, 0.7);
  }
  const oculus = new Group();
  oculus.position.set(-7.6, 3.13, back + 0.18);
  room.add(oculus);
  g.ball(oculus, m.light, 0, 0, 0, 1.17, 1.17, 0.03);
  g.hoop(oculus, m.oak, 0, 0, 0.08, 1.24);
  g.hoop(oculus, m.brass, 0, 0, 0.13, 0.91);
  for (let i = 0; i < 16; i++) {
    const a = (i * Math.PI) / 8;
    g.rod(oculus, m.brass, [0, 0, 0.13], [Math.cos(a) * 1.18, Math.sin(a) * 1.18, 0.13], 0.021);
  }
  const map = new Group();
  map.position.set(5.02, 2.55, back + 0.25);
  room.add(map);
  g.box(map, m.wood, 0, 0, 0, 1.42, 2.83, 0.14);
  g.box(map, m.paper, 0, 0, 0.09, 1.24, 2.61, 0.03);
  for (let line = 0; line < 8; line++) {
    g.box(map, m.oak, 0, -1.17 + line * 0.32, 0.112, 1.17, 0.012, 0.007);
    g.box(map, m.oak, -0.54 + line * 0.155, 0, 0.112, 0.011, 2.52, 0.007);
  }
  for (let island = 0; island < 10; island++) {
    const land = g.ball(
      map,
      m.sage,
      Math.sin(island * 2.7) * 0.42,
      Math.cos(island * 1.3) * 0.95,
      0.132,
      0.14 + (island % 3) * 0.04,
      0.19,
      0.014,
    );
    land.rotation.z = island * 0.7;
  }
  window(left + 0.15, -14.3, Math.PI / 2);
  window(left + 0.15, -6.8, Math.PI / 2);
  window(left + 0.15, -0.5, Math.PI / 2);
  window(left + 0.15, 11.3, Math.PI / 2);

  function rug(x: number, z: number, width: number, depth: number) {
    const carpet = new Group();
    carpet.userData.slot = "rug";
    room.add(carpet);
    g.box(carpet, m.navy, x, 0.059, z, width, 0.023, depth);
    for (const side of [-1, 1]) {
      g.box(carpet, m.brass, x, 0.073, z + side * (depth / 2 - 0.13), width - 0.24, 0.006, 0.026);
      g.box(carpet, m.brass, x + side * (width / 2 - 0.13), 0.073, z, 0.026, 0.006, depth - 0.24);
      for (let i = 0; i < Math.floor(width / 0.28); i++) {
        const diamond = g.box(
          carpet,
          m.oak,
          x - width / 2 + 0.25 + i * 0.28,
          0.074,
          z + side * (depth / 2 - 0.3),
          0.06,
          0.006,
          0.06,
        );
        diamond.rotation.y = Math.PI / 4;
      }
    }
  }

  function table(x: number, z: number, width: number, depth: number) {
    const top = new Group();
    top.userData.slot = "table";
    room.add(top);
    g.box(top, m.wood, x, 1.07, z, width - 0.08, 0.14, depth - 0.06);
    g.box(top, m.oak, x, 1.155, z, width, 0.05, depth);
    for (const side of [-1, 1]) {
      g.box(top, m.brass, x, 0.99, z + side * (depth / 2 - 0.12), width - 0.23, 0.025, 0.025);
      for (const end of [-1, 1]) {
        const legX = x + side * (width / 2 - 0.36);
        const legZ = z + end * (depth / 2 - 0.24);
        g.box(top, m.wood, legX, 0.54, legZ, 0.12, 1.06, 0.12);
        g.ball(top, m.oak, legX, 0.87, legZ, 0.11, 0.16, 0.11);
        g.box(top, m.brass, legX, 0.15, legZ, 0.13, 0.075, 0.13);
      }
    }
  }

  rug(0.25, 8.65, 6.9, 4.65);
  table(0.25, 8.4, 4.53, 1.59);
  f.lamp(room, -1.7, 1.19, 9);
  f.lamp(room, 2.05, 1.19, 9);
  f.plant(room, 0.25, 9.05, 0.42, 1.2);
  f.chair(room, -0.9, 9.55, 0);
  f.chair(room, 1.4, 9.55, 0);

  for (const [index, x] of [-10, 10.3].entries()) {
    if (index === 0) {
      rug(x, 9.0, 3.8, 3.1);
      table(x, 9.05, 2.65, 1.32);
    } else {
      const pedestal = new Group();
      pedestal.userData.slot = "round";
      room.add(pedestal);
      g.rod(pedestal, m.wood, [x, 0.08, 9.05], [x, 1.12, 9.05], 0.21);
      g.rod(pedestal, m.oak, [x, 1.11, 9.05], [x, 1.22, 9.05], 1.12);
      for (let side = 0; side < 4; side++) {
        const a = (side * Math.PI) / 2;
        g.rod(
          pedestal,
          m.wood,
          [x, 0.26, 9.05],
          [x + Math.sin(a) * 0.83, 0.1, 9.05 + Math.cos(a) * 0.83],
          0.067,
        );
      }
    }
    f.chair(room, x - 0.62, 10.12, 0);
    f.chair(room, x + 0.65, 10.12, 0);
    f.chair(room, x, 7.93, Math.PI);
    f.lamp(room, x - 0.85, 1.19, 9.1);
    if (index) {
      f.book(room, x + 0.47, 1.28, 8.9, 2);
      f.book(room, x + 0.5, 1.38, 8.89, 3);
    }
    const paper = g.box(room, m.paper, x + 0.45, 1.238, 9.35, 0.55, 0.012, 0.38);
    paper.rotation.y = -0.17;
    for (let line = 0; line < 5; line++) {
      g.box(paper, m.oak, -0.03, 0.65, -0.31 + line * 0.14, 0.68 - (line % 2) * 0.1, 0.02, 0.028);
    }
  }

  function catalogue(x: number, z: number, rotation = 0) {
    const cabinet = new Group();
    cabinet.userData.slot = "catalog";
    cabinet.position.set(x, 0, z);
    cabinet.rotation.y = rotation;
    room.add(cabinet);
    g.box(cabinet, m.wood, 0, 0.69, 0, 1.5, 1.32, 0.72);
    g.box(cabinet, m.oak, 0, 1.4, 0, 1.63, 0.11, 0.82);
    for (let row = 0; row < 4; row++) {
      for (const side of [-1, 1]) {
        g.box(cabinet, m.oak, side * 0.36, 0.25 + row * 0.29, 0.374, 0.65, 0.25, 0.045);
        g.box(cabinet, m.brass, side * 0.36, 0.28 + row * 0.29, 0.404, 0.22, 0.085, 0.021);
        g.box(cabinet, m.paper, side * 0.36, 0.29 + row * 0.29, 0.418, 0.15, 0.039, 0.006);
        g.rod(
          cabinet,
          m.brass,
          [side * 0.36 - 0.09, 0.2 + row * 0.29, 0.43],
          [side * 0.36 + 0.09, 0.2 + row * 0.29, 0.43],
          0.023,
        );
      }
    }
    f.book(cabinet, -0.33, 1.52, 0, 3, 0.9);
    f.book(cabinet, -0.3, 1.61, -0.01, 0, 0.9);
    f.lamp(cabinet, 0.47, 1.46, -0.04);
  }
  catalogue(18.9, -15.8);
  catalogue(15.55, 4.8, -Math.PI / 2);
  catalogue(-15.3, -17.3, Math.PI / 2);
  f.plant(room, 15.1, -14.8, 1.7);
  f.plant(room, -5.8, 15.9, 1.35);
  f.plant(room, -15.1, 14.0, 1.55);
  f.plant(room, 15.3, 11.6, 1.8);
  f.plant(room, 4.7, -11.8, 1.15);
  libraryVignettes(room, g, m);
  libraryWings(room, g, m);
  libraryStacks(room, g, m);
  const disposeOrnaments = libraryOrnaments(room, g, m);
  const batched = batchArchitecture(room);
  batched.userData.windows = windows;
  windowGeometry.forEach((geometry) => geometry.dispose());
  disposeOrnaments();
  return batched;
}
