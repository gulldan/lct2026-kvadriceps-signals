import { Group } from "three";
import { batchArchitecture, type LibraryGeometry } from "./geometry";
import type { LibraryMaterials } from "./materials";
import { libraryPrintwork } from "./printwork";

/** Материальные следы жизни не занимают проходы: ковры, витражи, вывески и вещи на полках. */
export function libraryDecor(g: LibraryGeometry, m: LibraryMaterials) {
  const fixed = new Group();
  const prints = libraryPrintwork(m);
  function plaque(
    index: number,
    x: number,
    y: number,
    z: number,
    width: number,
    rotation = 0,
    height = width / 2,
  ) {
    const frame = new Group();
    frame.position.set(x, y, z);
    frame.rotation.y = rotation;
    fixed.add(frame);
    g.box(frame, m.wood, 0, 0, -0.06, width + 0.15, height + 0.14, 0.13);
    g.box(frame, m.brass, 0, 0, 0.01, width + 0.055, height + 0.055, 0.045);
    prints.label(frame, index, width, height).position.z = 0.037;
    return frame;
  }
  plaque(0, 6.8, 0.77, 16.16, 2.4, 0, 0.64);
  g.rod(fixed, m.brass, [-2.14, 4.98, -18.02], [1.84, 4.98, -18.02], 0.045);
  for (const x of [-2.14, 1.84]) g.ball(fixed, m.brass, x, 4.98, -18.02, 0.075);
  plaque(2, -4.92, 3.78, -6.9, 2.15, Math.PI / 2, 0.48);
  plaque(3, 6.58, 3.78, -6.3, 2.1, Math.PI / 2, 0.48);
  plaque(4, 17.08, 3.02, -2.3, 2.24, Math.PI / 2, 0.5);
  plaque(5, -16.12, 3.03, 8.6, 2.18, Math.PI / 2, 0.5);
  plaque(10, -22.82, 2.4, -3.3, 1.5, Math.PI / 2);
  plaque(11, -22.82, 0.55, 5.1, 0.63, Math.PI / 2);
  plaque(14, -22.82, 4.8, 15.3, 3.05, Math.PI / 2, 0.55);
  plaque(15, 6.8, 1.57, 15.5, 1.15, 0, 0.35);
  plaque(7, 21.5, 0.74, 12.44, 0.91, 0, 0.43);
  plaque(9, -21, 0.74, 5.59, 0.99, 0, 0.46);
  plaque(8, 7.25, 1.51, -18.13, 1.23, 0, 0.33);
  plaque(13, 20.05, 3.8, -17.42, 0.97, 0, 0.26);
  // Книга-путеводитель и полотенце лежат среди обычных исследовательских материалов.
  const guide = plaque(6, -9.1, 1.275, 3.72, 0.51, 0, 0.73);
  guide.rotation.x = -Math.PI / 2;
  for (let fold = 0; fold < 7; fold++)
    g.box(
      fixed,
      m.paper,
      -8.15 + fold * 0.055,
      1.28 + Math.sin(fold * 2) * 0.008,
      3.75,
      0.065,
      0.065,
      0.57,
    );
  const manuscript = plaque(12, -18.2, 1.23, 2.28, 0.68, 0, 0.39);
  manuscript.rotation.x = -Math.PI / 2;

  function carpet(x: number, z: number, width: number, depth: number, color: typeof m.navy) {
    const rug = new Group();
    rug.userData.slot = "rug";
    fixed.add(rug);
    g.box(rug, color, x, 0.063, z, width, 0.015, depth);
    for (const side of [-1, 1]) {
      g.box(rug, m.brass, x + side * (width / 2 - 0.16), 0.073, z, 0.03, 0.004, depth - 0.22);
      g.box(rug, m.brass, x, 0.073, z + side * (depth / 2 - 0.16), width - 0.32, 0.004, 0.03);
      for (let stitch = 0; stitch < width / 0.14; stitch++)
        g.box(
          rug,
          m.paper,
          x - width / 2 + stitch * 0.14,
          0.065,
          z + side * (depth / 2 + 0.075),
          0.027,
          0.018,
          0.16,
        );
    }
    for (let row = 0; row < Math.floor(depth / 0.55); row++) {
      for (let col = 0; col < Math.floor(width / 0.6); col++) {
        const diamond = g.box(
          rug,
          (row + col) % 3 ? m.oak : m.paper,
          x - width / 2 + 0.35 + col * 0.6,
          0.075,
          z - depth / 2 + 0.32 + row * 0.55,
          0.095,
          0.005,
          0.16,
        );
        diamond.rotation.y = Math.PI / 4;
      }
    }
  }
  carpet(-10, -7.1, 4.8, 3.5, m.navy);
  carpet(10, -7.2, 4.7, 3, m.claret);
  carpet(-9, 3.8, 5.1, 2.5, m.sage);
  carpet(0.25, -3.5, 2.3, 14, m.claret);
  carpet(6.8, 15.5, 4.3, 2.7, m.navy);

  // Цветные стёкла превращают одинаковые окна в небольшие розетки.
  const glass = [m.navy, m.sage, m.claret, m.brass].map((material) => {
    const tint = material.clone();
    tint.color.lerp(m.paper.color, 0.32);
    tint.emissive.copy(tint.color);
    tint.emissiveIntensity = 0.22;
    return tint;
  });
  for (const z of [-14.3, -6.8, -0.5, 11.3]) {
    for (let row = 0; row < 3; row++) {
      for (let col = 0; col < 4; col++) {
        const color = glass[(row + col) % glass.length]!;
        g.box(fixed, color, -22.74, 1.1 + row * 0.85, z - 0.6 + col * 0.4, 0.025, 0.77, 0.35);
      }
    }
    const medallion = g.hoop(fixed, m.brass, -22.77, 3.28, z, 0.52);
    medallion.rotation.y = Math.PI / 2;
    for (let petal = 0; petal < 6; petal++) {
      const angle = (petal / 6) * Math.PI * 2;
      g.ball(
        fixed,
        glass[petal % 4]!,
        -22.76,
        3.28 + Math.sin(angle) * 0.29,
        z + Math.cos(angle) * 0.29,
        0.015,
        0.18,
        0.14,
      );
    }
  }
  // Плющ обвивает только стойки, оставляя проёмы открытыми.
  for (const [x, z] of [
    [-16.6, 10.3],
    [16.6, 10.3],
    [6.1, 3.8],
  ] as const) {
    for (let leaf = 0; leaf < 22; leaf++) {
      const angle = leaf * 2.4;
      const y = 0.25 + leaf * 0.13;
      g.rod(
        fixed,
        m.sage,
        [x + Math.sin(angle) * 0.2, y, z + Math.cos(angle) * 0.2],
        [x + Math.sin(angle + 2.4) * 0.2, y + 0.13, z + Math.cos(angle + 2.4) * 0.2],
        0.013,
      );
      const foliage = g.ball(
        fixed,
        leaf % 3 ? m.sage : m.oak,
        x + Math.sin(angle) * 0.31,
        y,
        z + Math.cos(angle) * 0.31,
        0.065,
        0.14,
        0.035,
      );
      foliage.rotation.z = angle;
    }
  }
  const root = batchArchitecture(fixed);
  root.name = "library-crafted-details";
  const banner = prints.label(root, 1, 3.55, 1.3, 12);
  banner.position.set(-0.15, 4.27, -17.49);
  const vertices = banner.geometry.getAttribute("position");
  const rest = new Float32Array(vertices.array);
  banner.castShadow = true;
  let time = 0;
  return {
    root,
    advance(dt: number, still = false) {
      if (!still) time += dt;
      for (let index = 0; index < vertices.count; index++) {
        const x = rest[index * 3]!;
        const y = rest[index * 3 + 1]!;
        const freedom = (0.65 - y) / 1.3;
        vertices.setZ(
          index,
          Math.sin(x * 5) * 0.028 +
            (still ? 0 : Math.sin(time * 1.1 + x * 2.4)) * freedom * freedom * 0.085,
        );
      }
      vertices.needsUpdate = true;
      banner.geometry.computeVertexNormals();
    },
    dispose: prints.dispose,
  };
}
