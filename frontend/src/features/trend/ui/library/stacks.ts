import { Group } from "three";
import type { LibraryGeometry } from "./geometry";
import type { LibraryMaterials } from "./materials";
import { LIBRARY_PARTITIONS, SHELF_ISLANDS } from "./layout";

/** Полки открыты с обеих сторон, чтобы маршруты читались сквозь весь книжный лабиринт. */
export function libraryStacks(room: Group, g: LibraryGeometry, m: LibraryMaterials) {
  const bindings = [m.navy, m.sage, m.claret, m.oak, m.paper, m.wood];

  function segment(line: Group, from: number, to: number, height: number, section: number) {
    const length = to - from;
    if (length < 0.3) return;
    const parent = new Group();
    parent.userData.slot = "stack";
    line.add(parent);
    const x = (from + to) / 2;
    const count = Math.max(1, Math.round(length / 1.8));
    const width = length / count;
    const interval = (height - 0.36) / 4;
    const frame = bindings[(section + 5) % bindings.length]!;
    g.box(parent, m.recess, x, height / 2, 0, length, height, 0.09);
    g.box(parent, m.wood, x, 0.15, 0, length + 0.06, 0.3, 1.04);
    g.box(parent, m.oak, x, height + 0.01, 0, length + 0.12, 0.13, 1.1);
    g.box(parent, frame, x, height + 0.13, 0, length + 0.18, 0.12, 1.12);
    for (const side of [-1, 1]) {
      g.box(parent, m.brass, x, 0.25, side * 0.531, length, 0.025, 0.015);
      g.box(parent, m.brass, x, height + 0.081, side * 0.559, length + 0.16, 0.026, 0.015);
    }

    for (let bay = 0; bay < count; bay++) {
      const center = from + (bay + 0.5) * width;
      for (const side of [-1, 1]) {
        // Два отсека оставлены для живых сценок; их опоры совпадают с окружением.
        const niche =
          section === 0 && side === -1 && [3.3, -11].some((z) => Math.abs(z - center) < 0.1);
        for (let row = 0; row <= 4; row++) {
          if (niche && row === 3) continue;
          const y = 0.32 + row * interval;
          g.box(parent, m.oak, center, y, side * 0.264, width - 0.07, 0.075, 0.51);
          g.box(parent, m.brass, center, y - 0.02, side * 0.523, width - 0.09, 0.022, 0.018);
          if (row === 4 || (niche && row >= 2)) continue;
          const seed = section * 97 + bay * 31 + row * 17 + Math.round(from * 11) + side * 5;
          const hash = (value: number) =>
            Math.abs(Math.sin(seed + value * 12.9898) * 43758.5453) % 1;
          const innerWidth = width - 0.22;
          let cursor = center - innerWidth / 2;
          let volume = 0;
          while (cursor < center + innerWidth / 2 - 0.12) {
            const variation = hash(volume);
            const thickness = 0.105 + variation * 0.095;
            const available = center + innerWidth / 2 - cursor;
            if (thickness > available) break;
            const cover = bindings[Math.floor(hash(volume + 53) * bindings.length)]!;
            const bookHeight = interval * (0.64 + hash(volume + 37) * 0.26);
            const book = new Group();
            book.position.set(cursor + thickness / 2, y + 0.042, side * 0.285);
            parent.add(book);
            if (volume === 0 && row % 2 === 1 && width > 1.3) {
              book.rotation.z = side * 0.12;
              cursor += 0.11;
            }
            g.box(book, cover, 0, bookHeight / 2, 0, thickness, bookHeight, 0.4);
            for (const band of [0.16, 0.82]) {
              g.box(
                book,
                m.brass,
                0,
                bookHeight * band,
                side * 0.204,
                thickness * 0.86,
                0.018,
                0.01,
              );
            }
            if (variation > 0.68)
              g.box(
                book,
                m.paper,
                0,
                bookHeight * 0.57,
                side * 0.207,
                thickness * 0.66,
                bookHeight * 0.2,
                0.012,
              );
            cursor += thickness + 0.025;
            volume++;
            if (volume === 3 && hash(row + 83) > 0.63 && center + innerWidth / 2 - cursor > 0.62) {
              for (let pile = 0; pile < 3; pile++) {
                g.box(
                  parent,
                  bindings[(row + pile + section) % bindings.length]!,
                  cursor + 0.24,
                  y + 0.08 + pile * 0.11,
                  side * 0.28,
                  0.45,
                  0.105,
                  0.4,
                );
                g.box(
                  parent,
                  m.paper,
                  cursor + 0.24,
                  y + 0.083 + pile * 0.11,
                  side * 0.486,
                  0.39,
                  0.061,
                  0.01,
                );
              }
              cursor += 0.53;
            }
          }
        }
        // Латунные держатели и сигнатуры различают отделы даже с дальней камеры.
        g.box(
          parent,
          m.brass,
          center,
          height + 0.125,
          side * 0.573,
          Math.min(0.45, width * 0.4),
          0.16,
          0.025,
        );
        g.box(
          parent,
          m.paper,
          center,
          height + 0.125,
          side * 0.592,
          Math.min(0.35, width * 0.3),
          0.097,
          0.014,
        );
        for (let mark = 0; mark < (section % 4) + 1; mark++)
          g.box(
            parent,
            m.ink,
            center - 0.09 + mark * 0.053,
            height + 0.125,
            side * 0.602,
            0.012,
            0.045,
            0.007,
          );
      }
    }
    for (let upright = 0; upright <= count; upright++) {
      const px = from + upright * width;
      g.box(parent, frame, px, height / 2, 0, 0.105, height, 1.03);
      for (const side of [-1, 1]) {
        g.box(parent, m.oak, px, height / 2, side * 0.536, 0.061, height - 0.2, 0.037);
        g.box(parent, m.brass, px, height / 2, side * 0.558, 0.017, height - 0.45, 0.012);
      }
    }
    for (const end of [from, to]) {
      g.box(parent, m.wood, end, height / 2, 0, 0.14, height, 1.035);
      g.box(
        parent,
        m.brass,
        end + (end === from ? -0.073 : 0.073),
        height * 0.62,
        0,
        0.013,
        0.88,
        0.64,
      );
      g.box(
        parent,
        frame,
        end + (end === from ? -0.083 : 0.083),
        height * 0.62,
        0,
        0.012,
        0.81,
        0.57,
      );
      for (const z of [-0.38, 0.38])
        g.ball(parent, m.brass, end, height + 0.23, z, 0.075, 0.11, 0.075);
    }
  }

  LIBRARY_PARTITIONS.forEach((partition, section) => {
    const group = new Group();
    if (partition.axis === "x") {
      group.position.x = partition.at;
      group.rotation.y = -Math.PI / 2;
    } else group.position.z = partition.at;
    room.add(group);
    let start: number = partition.from;
    for (const [from, to] of [...partition.gaps, [partition.to, partition.to] as const]) {
      segment(
        group,
        start,
        from,
        section === 0 ? 3.24 : partition.axis === "z" ? 2.8 : 3.04 + (section % 3) * 0.14,
        section,
      );
      start = to;
    }
  });

  for (const island of SHELF_ISLANDS) {
    const group = new Group();
    if (island.axis === "x") {
      group.position.x = island.at;
      group.rotation.y = -Math.PI / 2;
    } else group.position.z = island.at;
    room.add(group);
    const section = { science: 7, engineering: 10, world: 8, corpus: 9 }[island.section];
    const height = { science: 3.24, engineering: 3.28, world: 3.05, corpus: 2.65 }[island.section];
    segment(group, island.from, island.to, height, section);
    const center = (island.from + island.to) / 2;
    for (const side of [-1, 1]) {
      g.rod(
        group,
        m.brass,
        [island.from + 0.22, height + 0.24, side * 0.42],
        [center, height + 0.48, side * 0.42],
        0.023,
      );
      g.rod(
        group,
        m.brass,
        [center, height + 0.48, side * 0.42],
        [island.to - 0.22, height + 0.24, side * 0.42],
        0.023,
      );
    }
    g.ball(group, m.brass, center, height + 0.46, 0, 0.13, 0.17, 0.13);
  }
}
