import { Group } from "three";
import type { LibraryGeometry } from "./geometry";
import type { LibraryMaterials } from "./materials";
import { libraryFurniture } from "./furniture";

/** У каждого отдела свой силуэт и занятие: наблюдать, конструировать, хранить, изучать карты. */
export function libraryVignettes(room: Group, g: LibraryGeometry, m: LibraryMaterials) {
  const f = libraryFurniture(g, m);

  function compass(x: number, z: number, radius: number) {
    g.rod(room, m.navy, [x, 0.055, z], [x, 0.079, z], radius);
    const border = g.hoop(room, m.brass, x, 0.09, z, radius * 0.96);
    border.rotation.x = Math.PI / 2;
    for (let i = 0; i < 16; i++) {
      const angle = (i / 16) * Math.PI * 2;
      const length = radius * (i % 2 ? 0.53 : 0.81);
      const line = g.box(room, i % 2 ? m.sage : m.paper, x, 0.093, z, 0.024, 0.006, length);
      line.position.x += Math.sin(angle) * length * 0.55;
      line.position.z += Math.cos(angle) * length * 0.55;
      line.rotation.y = angle;
      g.ball(
        room,
        m.brass,
        x + Math.sin(angle) * radius * 0.87,
        0.095,
        z + Math.cos(angle) * radius * 0.87,
        0.03,
        0.007,
        0.03,
      );
    }
  }

  compass(-10, -7.1, 2.4);
  const telescope = new Group();
  telescope.userData.slot = "telescope";
  telescope.position.set(-10, 0, -7.1);
  room.add(telescope);
  for (let i = 0; i < 3; i++) {
    const angle = (i * Math.PI * 2) / 3;
    g.rod(
      telescope,
      m.wood,
      [Math.cos(angle) * 0.85, 0.1, Math.sin(angle) * 0.65],
      [0, 1.53, 0],
      0.064,
    );
    g.ball(telescope, m.brass, Math.cos(angle) * 0.85, 0.12, Math.sin(angle) * 0.65, 0.08);
  }
  g.rod(telescope, m.brass, [0, 1.3, 0], [0, 1.94, 0], 0.09);
  const scope = new Group();
  scope.position.y = 1.95;
  scope.rotation.set(0, 0.4, -0.39);
  telescope.add(scope);
  g.rod(scope, m.brass, [-1.32, 0, 0], [0.9, 0, 0], 0.23);
  g.rod(scope, m.navy, [-1.26, 0, 0], [0.85, 0, 0], 0.203);
  g.rod(scope, m.brass, [0.86, 0, 0], [1.28, 0, 0], 0.09);
  g.ball(scope, m.light, -1.34, 0, 0, 0.025, 0.2, 0.2);
  for (const x of [-1.25, -0.3, 0.82]) {
    const ring = g.hoop(scope, m.brass, x, 0, 0, 0.24);
    ring.rotation.y = Math.PI / 2;
  }
  g.rod(scope, m.brass, [-0.2, 0.25, 0], [0.6, 0.25, 0], 0.045);
  const stars = new Group();
  stars.position.set(-11.9, 0, -7.22);
  stars.rotation.y = 0.2;
  room.add(stars);
  g.box(stars, m.wood, 0, 1.2, 0, 1.07, 1.5, 0.13);
  g.box(stars, m.navy, 0, 1.2, 0.076, 0.94, 1.36, 0.024);
  const constellation = [
    [-0.28, 1.63],
    [-0.1, 1.31],
    [0.21, 1.43],
    [0.28, 0.9],
    [-0.16, 0.74],
  ] as const;
  constellation.forEach(([x, y], index) => {
    g.ball(stars, m.light, x, y, 0.11, 0.035);
    if (index)
      g.rod(
        stars,
        m.brass,
        [constellation[index - 1]![0], constellation[index - 1]![1], 0.102],
        [x, y, 0.102],
        0.009,
      );
  });
  g.rod(stars, m.brass, [-0.45, 0.06, 0.1], [-0.28, 0.6, 0], 0.04);
  g.rod(stars, m.brass, [0.45, 0.06, 0.1], [0.28, 0.6, 0], 0.04);
  f.book(room, -8.45, 0.15, -7, 0, 1.25);
  f.book(room, -8.42, 0.29, -7, 2, 1.15);

  const workshop = new Group();
  workshop.position.set(10, 0, -7.2);
  room.add(workshop);
  g.box(workshop, m.navy, 0, 0.54, 0, 3.7, 1.0, 0.86);
  g.box(workshop, m.oak, 0, 1.13, 0, 3.94, 0.18, 1.0);
  g.box(workshop, m.wood, 0, 1.92, -0.36, 3.75, 1.41, 0.11);
  g.box(workshop, m.sage, 0, 1.92, -0.289, 3.58, 1.23, 0.023);
  for (let i = 0; i < 12; i++) {
    for (let row = 0; row < 3; row++)
      g.ball(workshop, m.brass, -1.55 + i * 0.28, 1.43 + row * 0.43, -0.267, 0.022, 0.022, 0.008);
  }
  for (const x of [-1.5, -0.8, -0.1, 0.6, 1.3]) {
    g.box(workshop, m.wood, x, 0.56, 0.45, 0.61, 0.76, 0.08);
    g.rod(workshop, m.brass, [x - 0.11, 0.87, 0.52], [x + 0.11, 0.87, 0.52], 0.025);
  }
  for (const [x, height] of [
    [-1.2, 0.64],
    [-0.65, 0.47],
    [0.0, 0.7],
  ] as const) {
    g.rod(workshop, m.brass, [x, 1.57, -0.19], [x + 0.08, 1.57 + height, -0.19], 0.035);
    g.hoop(workshop, m.brass, x + 0.08, 1.57 + height, -0.19, 0.11);
  }
  for (const [x, y, radius] of [
    [0.7, 2.13, 0.27],
    [1.28, 1.74, 0.2],
  ] as const) {
    g.hoop(workshop, m.brass, x, y, -0.18, radius);
    for (let i = 0; i < 10; i++) {
      const a = (i * Math.PI) / 5;
      const tooth = g.box(
        workshop,
        m.brass,
        x + Math.sin(a) * radius,
        y + Math.cos(a) * radius,
        -0.18,
        0.075,
        0.12,
        0.075,
      );
      tooth.rotation.z = -a;
    }
    g.rod(workshop, m.brass, [x - radius, y, -0.17], [x + radius, y, -0.17], 0.017);
  }
  const blueprint = g.box(workshop, m.paper, -0.45, 1.229, 0.03, 1.2, 0.012, 0.62);
  blueprint.rotation.y = -0.1;
  for (let i = 0; i < 4; i++)
    g.box(workshop, m.navy, -0.66 + i * 0.16, 1.24, 0.03, 0.016, 0.005, 0.35 - i * 0.055);
  g.rod(workshop, m.paper, [-1.12, 1.29, -0.28], [-1.12, 1.29, 0.32], 0.067);
  f.lamp(workshop, 1.47, 1.23, 0.08);

  const archive = new Group();
  archive.userData.slot = "catalog";
  archive.position.set(-9, 0, 3.8);
  room.add(archive);
  g.box(archive, m.claret, 0, 0.63, 0, 3.87, 1.14, 0.79);
  g.box(archive, m.wood, 0, 1.24, 0, 4.02, 0.14, 0.92);
  for (let column = 0; column < 7; column++) {
    for (let row = 0; row < 3; row++) {
      const x = -1.59 + column * 0.53;
      const y = 0.27 + row * 0.33;
      g.box(archive, m.oak, x, y, 0.425, 0.47, 0.28, 0.058);
      g.box(archive, m.brass, x, y + 0.02, 0.463, 0.16, 0.065, 0.014);
      g.box(archive, m.paper, x, y + 0.028, 0.474, 0.115, 0.032, 0.01);
      g.ball(archive, m.brass, x, y - 0.066, 0.49, 0.025);
    }
  }
  g.box(archive, m.wood, -0.69, 1.87, -0.12, 2.38, 1.16, 0.52);
  for (let column = 0; column < 5; column++) {
    for (let row = 0; row < 3; row++) {
      const x = -1.59 + column * 0.45;
      const y = 1.46 + row * 0.34;
      g.box(archive, m.recess, x, y, 0.159, 0.36, 0.25, 0.021);
      for (let sheet = 0; sheet < 3 + (column % 3); sheet++) {
        const letter = g.box(
          archive,
          m.paper,
          x,
          y - 0.08 + sheet * 0.026,
          0.22,
          0.27,
          0.018,
          0.21,
        );
        letter.rotation.y = ((sheet % 3) - 1) * 0.06;
      }
    }
  }
  for (let i = 0; i < 5; i++) f.book(archive, 1.16, 1.39 + i * 0.105, 0.04, i + 1, 1.04 - i * 0.03);
  g.ball(archive, m.brass, 1.57, 1.38, 0.16, 0.18, 0.1, 0.18);
  g.ball(archive, m.wood, 1.57, 1.48, 0.16, 0.05);

  const atlas = new Group();
  atlas.position.set(10, 0, 4.8);
  room.add(atlas);
  g.box(atlas, m.sage, 0, 0.15, 0, 2.8, 0.2, 0.68);
  for (const x of [-1, 1]) g.rod(atlas, m.wood, [x, 0.2, 0], [x * 0.73, 1.35, 0], 0.11);
  const openAtlas = new Group();
  openAtlas.position.y = 1.3;
  openAtlas.rotation.x = -0.26;
  atlas.add(openAtlas);
  g.box(openAtlas, m.wood, 0, 0, 0, 2.95, 0.14, 1.08);
  for (const side of [-1, 1]) {
    g.box(openAtlas, m.claret, side * 0.57, 0.096, 0, 1.1, 0.045, 0.9);
    g.box(openAtlas, m.paper, side * 0.57, 0.139, 0, 1.05, 0.045, 0.86);
    for (let i = 0; i < 7; i++) {
      const land = g.ball(
        openAtlas,
        i % 3 ? m.sage : m.oak,
        side * (0.27 + (i % 3) * 0.23),
        0.169,
        -0.22 + Math.sin(i * 2) * 0.21,
        0.12,
        0.005,
        0.075,
      );
      land.rotation.y = i * 1.3;
    }
  }
  g.rod(openAtlas, m.brass, [-1.35, 0.19, 0.43], [1.35, 0.19, 0.43], 0.018);
  for (let i = 0; i < 4; i++) {
    g.rod(atlas, m.paper, [-1.65, 0.44 + i * 0.19, -0.29], [-1.65, 0.44 + i * 0.19, 0.4], 0.082);
    g.rod(atlas, m.brass, [-1.75, 0.3, -0.15], [-1.75, 1.29, -0.15], 0.025);
  }
  f.plant(atlas, 1.65, 0, 1.0);

  const typewriter = new Group();
  typewriter.position.set(-9.95, 1.2, 8.9);
  room.add(typewriter);
  g.box(typewriter, m.ink, 0, 0.1, 0, 1.02, 0.16, 0.66);
  const keys = new Group();
  keys.rotation.x = -0.18;
  typewriter.add(keys);
  for (let row = 0; row < 3; row++) {
    for (let i = 0; i < 9; i++)
      g.ball(keys, m.paper, -0.39 + i * 0.095, 0.18, 0.25 - row * 0.115, 0.033, 0.015, 0.033);
  }
  g.rod(typewriter, m.brass, [-0.65, 0.27, -0.22], [0.65, 0.27, -0.22], 0.036);
  g.rod(typewriter, m.ink, [-0.42, 0.32, -0.22], [0.42, 0.32, -0.22], 0.1);
  const page = g.box(typewriter, m.paper, 0, 0.55, -0.31, 0.64, 0.47, 0.012);
  page.rotation.x = -0.15;
  for (let i = 0; i < 5; i++)
    g.box(
      typewriter,
      m.wood,
      -0.04,
      0.55 + i * 0.041,
      -0.263 - i * 0.006,
      0.39 - i * 0.035,
      0.009,
      0.003,
    );
  g.box(room, m.claret, -9.43, 1.21, 9.33, 0.5, 0.025, 0.32);
  g.box(room, m.paper, -9.48, 1.24, 9.31, 0.44, 0.018, 0.29);
  g.ball(room, m.claret, -9.45, 1.255, 9.32, 0.045, 0.008, 0.045);
  compass(10.3, 9, 1.8);

  const manuscript = new Group();
  manuscript.position.set(2.9, 0, -7.4);
  room.add(manuscript);
  g.box(manuscript, m.wood, 0, 0.14, 0, 2.48, 0.24, 1.32);
  for (const side of [-1, 1]) {
    g.rod(manuscript, m.wood, [side * 0.83, 0.24, 0], [side * 0.7, 1.2, 0], 0.17);
    g.rod(manuscript, m.brass, [side * 0.83, 0.25, 0], [side * 0.83, 0.36, 0], 0.2);
  }
  const lectern = new Group();
  lectern.position.y = 1.28;
  lectern.rotation.x = -0.22;
  manuscript.add(lectern);
  g.box(lectern, m.wood, 0, 0, 0, 2.52, 0.17, 1.54);
  g.box(lectern, m.brass, 0, 0.1, 0, 2.31, 0.018, 1.31);
  g.box(lectern, m.claret, 0, 0.12, 0, 2.23, 0.015, 1.23);
  for (const side of [-1, 1]) {
    const leaf = new Group();
    leaf.position.set(side * 0.4, 0.18, 0);
    leaf.rotation.z = -side * 0.06;
    lectern.add(leaf);
    g.box(leaf, m.paper, 0, 0, 0, 0.78, 0.07, 1.05);
    for (const edge of [-1, 1]) {
      g.box(leaf, m.brass, edge * 0.325, 0.04, 0, 0.024, 0.006, 0.88);
      g.box(leaf, m.brass, 0, 0.04, edge * 0.44, 0.67, 0.006, 0.024);
    }
    g.box(leaf, side < 0 ? m.navy : m.claret, -0.16, 0.04, -0.23, 0.16, 0.005, 0.21);
    g.box(leaf, m.brass, -0.16, 0.045, -0.23, 0.055, 0.005, 0.15);
    for (let line = 0; line < 10; line++) {
      const short = line < 3;
      g.box(
        leaf,
        line % 5 ? m.wood : m.claret,
        short ? 0.1 : 0,
        0.043,
        -0.3 + line * 0.066,
        short ? 0.27 : 0.48,
        0.005,
        0.012,
      );
    }
  }
  for (let link = 0; link < 14; link++) {
    const t = link / 13;
    const chain = g.hoop(
      manuscript,
      m.brass,
      1.15,
      1.31 - t * 0.82,
      0.33 + Math.sin(t * Math.PI) * 0.19,
      0.037,
    );
    chain.rotation.y = ((link % 2) * Math.PI) / 2;
  }
  g.ball(manuscript, m.ink, 1.01, 1.53, -0.41, 0.1, 0.1, 0.1);
  g.rod(manuscript, m.brass, [1.01, 1.57, -0.41], [1.12, 2.0, -0.45], 0.011);
  const quill = g.ball(manuscript, m.paper, 1.11, 1.96, -0.45, 0.073, 0.23, 0.016);
  quill.rotation.z = -0.25;

  function scholarBust(x: number, z: number, rotation: number) {
    const statue = new Group();
    statue.position.set(x, 0, z);
    statue.rotation.y = rotation;
    room.add(statue);
    g.box(statue, m.wood, 0, 0.12, 0, 0.88, 0.24, 0.86);
    g.box(statue, m.plaster, 0, 0.72, 0, 0.62, 1.04, 0.62);
    g.box(statue, m.brass, 0, 0.78, 0.32, 0.32, 0.17, 0.015);
    g.box(statue, m.paper, 0, 1.3, 0, 0.82, 0.15, 0.79);
    g.ball(statue, m.plaster, 0, 1.55, 0, 0.43, 0.25, 0.22);
    g.ball(statue, m.paper, 0, 1.87, 0, 0.2, 0.28, 0.2);
    g.ball(statue, m.plaster, 0, 1.88, 0.18, 0.041, 0.066, 0.075);
    g.ball(statue, m.plaster, 0, 1.73, 0.12, 0.14, 0.15, 0.12);
    for (let lock = 0; lock < 7; lock++) {
      const angle = (lock / 6) * Math.PI;
      g.ball(
        statue,
        m.plaster,
        Math.cos(angle) * 0.17,
        2.03 + Math.sin(angle) * 0.07,
        -0.02,
        0.069,
        0.068,
        0.15,
      );
    }
    for (const side of [-1, 1])
      g.rod(statue, m.paper, [side * 0.28, 1.43, 0.17], [side * 0.13, 1.72, 0.08], 0.055);
  }
  scholarBust(-7.05, -10.4, 0.25);
  scholarBust(14.55, 0.4, -0.48);
}
