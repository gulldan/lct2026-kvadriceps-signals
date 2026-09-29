import { Group } from "three";
import type { LibraryGeometry } from "./geometry";
import type { LibraryMaterials } from "./materials";
import { ROOM_DEPTH, ROOM_WIDTH } from "./layout";

/**
 * Каменный цоколь и срезанные передние стены превращают зал в макет на постаменте:
 * срез читается как разрез здания, а не как край пола.
 */
export function libraryPlinth(room: Group, g: LibraryGeometry, m: LibraryMaterials) {
  const stone = m.plaster.clone();
  stone.color.lerp(m.wood.color, 0.5).multiplyScalar(0.62);
  stone.userData.plinth = true;
  const footing = stone.clone();
  delete footing.userData.plinth;
  footing.color.multiplyScalar(0.7);
  const halfWidth = ROOM_WIDTH / 2;
  const halfDepth = ROOM_DEPTH / 2;
  g.box(room, stone, 0, -0.83, 0, ROOM_WIDTH + 0.7, 0.62, ROOM_DEPTH + 0.7);
  g.box(room, m.brass, 0, -0.53, 0, ROOM_WIDTH + 0.74, 0.035, ROOM_DEPTH + 0.74);
  g.box(room, footing, 0, -1.2, 0, ROOM_WIDTH + 1.2, 0.14, ROOM_DEPTH + 1.2);

  const front = halfDepth - 0.12;
  const side = halfWidth - 0.12;
  // Каждая стена — своя группа: локальная X идёт вдоль стены, +Z смотрит наружу, к камере.
  for (const [x, z, rotation, length, posts] of [
    [0, front, 0, ROOM_WIDTH, [-16.6, -5.4, 6.1, 16.6]],
    [side, 0, Math.PI / 2, ROOM_DEPTH, [12, 1.9, -10.1]],
  ] as const) {
    const wall = new Group();
    wall.userData.slot = "balustrade";
    wall.position.set(x, 0, z);
    wall.rotation.y = rotation;
    room.add(wall);
    g.box(wall, m.plaster, 0, 0.42, 0, length, 0.84, 0.24);
    // Тёмная верхняя грань — условный цвет разреза, как на архитектурном макете.
    g.box(wall, m.recess, 0, 0.86, 0, length + 0.02, 0.04, 0.26);
    g.box(wall, m.oak, 0, 0.12, 0, length + 0.06, 0.2, 0.3);
    g.box(wall, m.brass, 0, 0.7, 0, length + 0.03, 0.025, 0.27);
    for (const post of posts) g.box(wall, m.plaster, post, 0.46, 0, 0.5, 0.92, 0.4);
  }

  // Мышиный дом в цоколе: дверь, круглое окно с огоньком, фонарь и почтовый ящик.
  const burrow = new Group();
  burrow.position.set(3.4, -0.83, halfDepth + 0.36);
  room.add(burrow);
  g.box(burrow, m.recess, 0, -0.05, 0.005, 0.26, 0.34, 0.02);
  g.ball(burrow, m.recess, 0, 0.12, 0.005, 0.13, 0.13, 0.02);
  g.box(burrow, m.claret, 0, -0.05, 0.017, 0.21, 0.3, 0.012);
  g.ball(burrow, m.claret, 0, 0.1, 0.017, 0.105, 0.105, 0.012);
  g.ball(burrow, m.brass, 0.06, -0.05, 0.03, 0.014);
  g.box(burrow, m.oak, 0, -0.225, 0.04, 0.34, 0.03, 0.08);
  g.box(burrow, m.sage, 0, -0.205, 0.05, 0.2, 0.01, 0.06);
  g.ball(burrow, m.light, 0.32, 0.06, 0.006, 0.07, 0.07, 0.012).userData.emitter = "ember";
  g.hoop(burrow, m.oak, 0.32, 0.06, 0.012, 0.075);
  g.rod(burrow, m.oak, [0.25, 0.06, 0.015], [0.39, 0.06, 0.015], 0.008);
  g.rod(burrow, m.brass, [-0.2, 0.15, 0.01], [-0.2, 0.15, 0.06], 0.008);
  g.box(burrow, m.brass, -0.2, 0.1, 0.07, 0.045, 0.07, 0.045);
  g.ball(burrow, m.light, -0.2, 0.1, 0.07, 0.022, 0.03, 0.022).userData.emitter = "ember";
  g.box(burrow, m.navy, -0.38, -0.12, 0.025, 0.08, 0.1, 0.05);
  g.box(burrow, m.paper, -0.38, -0.095, 0.052, 0.05, 0.012, 0.004);
  g.ball(burrow, m.oak, 0.5, -0.19, 0.05, 0.045, 0.05, 0.045);
  g.ball(burrow, m.sage, 0.5, -0.12, 0.05, 0.05, 0.06, 0.05);

  // Подвальные решётки на восточной грани: внизу тоже кто-то читает.
  for (const z of [-9, 4.5]) {
    const cellar = new Group();
    cellar.position.set(halfWidth + 0.36, -0.8, z);
    cellar.rotation.y = Math.PI / 2;
    room.add(cellar);
    g.box(cellar, m.recess, 0, 0, 0, 0.9, 0.34, 0.02);
    g.box(cellar, m.light, 0, -0.02, 0.004, 0.8, 0.26, 0.01).userData.emitter = "ember";
    for (let bar = -3; bar <= 3; bar++)
      g.rod(cellar, m.ink, [bar * 0.11, -0.16, 0.02], [bar * 0.11, 0.16, 0.02], 0.012);
    g.box(cellar, m.oak, 0, 0.19, 0.02, 1, 0.05, 0.05);
  }
}
