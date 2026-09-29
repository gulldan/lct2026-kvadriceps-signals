import { ExtrudeGeometry, Group, Mesh, Shape, type Material } from "three";
import type { LibraryGeometry } from "./geometry";
import type { LibraryMaterials } from "./materials";
import { LIBRARY_PARTITIONS, ROOM_DEPTH, ROOM_WIDTH } from "./layout";

/** Деревянные порталы продолжают карнизы стеллажей, оставляя проходы открытыми. */
export function libraryOrnaments(room: Group, g: LibraryGeometry, m: LibraryMaterials) {
  const temporary: ExtrudeGeometry[] = [];
  const marble = m.paper.clone();
  marble.color.lerp(m.sage.color, 0.13);
  marble.roughness = 0.43;
  const fresco = m.paper.clone();
  fresco.color.lerp(m.claret.color, 0.31);

  function column(parent: Group, x: number, z: number, height: number, radius = 0.17) {
    g.box(parent, m.wood, x, 0.12, z, radius * 3.6, 0.24, radius * 3.6);
    g.box(parent, m.oak, x, 0.31, z, radius * 3.0, 0.14, radius * 3.0);
    g.rod(parent, m.wood, [x, 0.36, z], [x, height - 0.25, z], radius);
    for (let flute = 0; flute < 8; flute++) {
      const a = (flute * Math.PI) / 4;
      g.rod(
        parent,
        m.oak,
        [x + Math.sin(a) * radius, 0.55, z + Math.cos(a) * radius],
        [x + Math.sin(a) * radius, height - 0.42, z + Math.cos(a) * radius],
        0.017,
      );
    }
    for (const y of [0.43, height - 0.28, height - 0.13]) {
      g.rod(parent, m.brass, [x, y, z], [x, y + 0.055, z], radius * 1.4);
    }
    g.box(parent, m.oak, x, height, z, radius * 3.6, 0.15, radius * 3.6);
    for (const side of [-1, 1]) {
      g.hoop(parent, m.brass, x + side * radius * 0.83, height - 0.08, z + radius * 1.65, 0.075);
    }
  }

  function arch(parent: Group, center: number, width: number) {
    const portal = new Group();
    portal.userData.slot = "arch";
    portal.position.x = center;
    parent.add(portal);
    const radius = width / 2;
    const spring = 2.28;
    column(portal, -radius - 0.22, 0, spring, 0.18);
    column(portal, radius + 0.22, 0, spring, 0.18);
    const shape = new Shape();
    shape.moveTo(-radius - 0.34, spring);
    shape.absarc(0, spring, radius + 0.34, Math.PI, 0, true);
    shape.lineTo(radius, spring);
    shape.absarc(0, spring, radius, 0, Math.PI, false);
    shape.closePath();
    const geometry = new ExtrudeGeometry(shape, {
      depth: 0.52,
      bevelEnabled: true,
      bevelThickness: 0.024,
      bevelSize: 0.024,
      bevelSegments: 1,
      curveSegments: 24,
    });
    temporary.push(geometry);
    const archway = new Mesh(geometry, m.wood);
    archway.position.z = -0.26;
    portal.add(archway);
    for (const side of [-1, 1]) {
      for (let i = 0; i < 24; i++) {
        const a = (i * Math.PI) / 24;
        const b = ((i + 1) * Math.PI) / 24;
        g.rod(
          portal,
          m.brass,
          [Math.cos(a) * (radius + 0.12), spring + Math.sin(a) * (radius + 0.12), side * 0.292],
          [Math.cos(b) * (radius + 0.12), spring + Math.sin(b) * (radius + 0.12), side * 0.292],
          0.026,
        );
        if (i % 3 === 0)
          g.rod(
            portal,
            m.oak,
            [Math.cos(a) * (radius + 0.02), spring + Math.sin(a) * (radius + 0.02), side * 0.29],
            [Math.cos(a) * (radius + 0.33), spring + Math.sin(a) * (radius + 0.33), side * 0.29],
            0.009,
          );
      }
      g.box(portal, m.brass, 0, spring + radius + 0.2, side * 0.3, 0.27, 0.42, 0.08);
      g.ball(portal, m.claret, 0, spring + radius + 0.24, side * 0.365, 0.083, 0.12, 0.023);
    }
  }

  function relief(
    parent: Group,
    x: number,
    y: number,
    z: number,
    width: number,
    tone: Material,
    motif = 0,
  ) {
    g.box(parent, m.oak, x, y, z, width, 1.17, 0.045);
    g.box(parent, m.brass, x, y, z + 0.027, width - 0.08, 1.09, 0.018);
    g.box(parent, tone, x, y, z + 0.04, width - 0.15, 1.02, 0.016);
    const radius = Math.min(0.35, width * 0.24);
    g.hoop(parent, m.brass, x, y, z + 0.06, radius);
    if (motif % 3 === 0) {
      for (let petal = 0; petal < 8; petal++) {
        const angle = (petal * Math.PI) / 4;
        const leaf = g.ball(
          parent,
          marble,
          x + Math.cos(angle) * radius * 0.61,
          y + Math.sin(angle) * radius * 0.61,
          z + 0.06,
          radius * 0.27,
          radius * 0.43,
          0.014,
        );
        leaf.rotation.z = angle - Math.PI / 2;
      }
    } else if (motif % 3 === 1) {
      for (let spoke = 0; spoke < 8; spoke++) {
        const angle = (spoke * Math.PI) / 4;
        const reach = radius * (spoke % 2 ? 0.67 : 0.93);
        g.rod(
          parent,
          m.brass,
          [x, y, z + 0.063],
          [x + Math.cos(angle) * reach, y + Math.sin(angle) * reach, z + 0.063],
          0.016,
        );
      }
      const core = g.box(parent, marble, x, y, z + 0.066, radius * 0.46, radius * 0.46, 0.015);
      core.rotation.z = Math.PI / 4;
    } else {
      const emblem = g.box(parent, marble, x, y, z + 0.064, radius * 0.84, radius * 1.15, 0.018);
      emblem.rotation.z = Math.PI / 4;
      for (const side of [-1, 1]) {
        const branch = g.ball(
          parent,
          m.brass,
          x + side * radius * 0.64,
          y - radius * 0.07,
          z + 0.067,
          radius * 0.16,
          radius * 0.68,
          0.012,
        );
        branch.rotation.z = side * -0.3;
      }
    }
    g.ball(parent, m.brass, x, y, z + 0.077, 0.043, 0.043, 0.018);
  }

  LIBRARY_PARTITIONS.forEach((partition) => {
    const group = new Group();
    if (partition.axis === "x") {
      group.position.x = partition.at;
      group.rotation.y = -Math.PI / 2;
    } else group.position.z = partition.at;
    room.add(group);
    for (const [from, to] of partition.gaps) arch(group, (from + to) / 2, to - from);
  });

  function chandelier(x: number, y: number, z: number, arms: number) {
    const lamp = new Group();
    lamp.position.set(x, y, z);
    lamp.userData.emitter = lamp.userData.slot = "chandelier";
    room.add(lamp);
    g.rod(lamp, m.brass, [0, 0.1, 0], [0, 1.78, 0], 0.045);
    for (const [height, radius] of [
      [0.3, 0.18],
      [0.63, 0.12],
      [1.04, 0.11],
      [1.72, 0.15],
    ])
      g.ball(lamp, m.brass, 0, height!, 0, radius!, radius! * 1.4, radius!);
    const ring = g.hoop(lamp, m.brass, 0, 0.28, 0, 0.83);
    ring.rotation.x = Math.PI / 2;
    for (let arm = 0; arm < arms; arm++) {
      const angle = (arm * Math.PI * 2) / arms;
      let last: [number, number, number] = [0, 0.57, 0];
      for (let step = 1; step <= 9; step++) {
        const t = step / 9;
        const point: [number, number, number] = [
          Math.cos(angle) * t * 1.09,
          0.5 - Math.sin(t * Math.PI) * 0.34,
          Math.sin(angle) * t * 1.09,
        ];
        g.rod(lamp, m.brass, last, point, 0.031);
        last = point;
      }
      g.rod(lamp, m.brass, [last[0], 0.45, last[2]], [last[0], 0.56, last[2]], 0.13);
      g.rod(lamp, m.paper, [last[0], 0.56, last[2]], [last[0], 0.89, last[2]], 0.057);
      g.ball(lamp, m.light, last[0], 0.99, last[2], 0.055, 0.135, 0.055).userData.emitter = "flame";
      const jewel = g.ball(
        lamp,
        m.light,
        Math.cos(angle) * 0.77,
        -0.03,
        Math.sin(angle) * 0.77,
        0.045,
        0.13,
        0.045,
      );
      jewel.rotation.z = 0.14;
    }
    g.ball(lamp, m.brass, 0, -0.02, 0, 0.07, 0.21, 0.07);
  }
  chandelier(-9.7, 3.35, -6.6, 6);
  chandelier(2.2, 3.8, -6.9, 8);
  chandelier(10.3, 3.25, 5.1, 6);
  chandelier(-19.3, 3.1, -4.9, 5);
  chandelier(0.25, 3.5, -14.9, 8);
  chandelier(20, 3.25, -11.4, 5);
  chandelier(-10, 3.1, 14.9, 6);

  // Инкрустированный путь помогает читать проходы даже в отдалённой камере.
  for (const x of [-1.26, 1.76]) {
    for (const [z, length] of [
      [-5.55, 23.3],
      [14.2, 7.3],
    ] as const) {
      g.box(room, m.wood, x, 0.06, z, 0.1, 0.018, length);
      g.box(room, m.brass, x + 0.09, 0.071, z, 0.026, 0.009, length);
    }
  }
  for (const z of [-14.8, -10.0, -2.0, 3.7, 14.9]) {
    // Медальоны уступают место мозаике и узорной дорожке, когда догружены текстуры.
    const inlay = new Group();
    inlay.userData.slot = "medallion";
    room.add(inlay);
    const medallion = g.hoop(inlay, m.brass, 0.25, 0.069, z, 0.75);
    medallion.rotation.x = Math.PI / 2;
    const inset = g.box(inlay, m.claret, 0.25, 0.065, z, 0.88, 0.021, 0.88);
    inset.rotation.y = Math.PI / 4;
    const core = g.box(inlay, marble, 0.25, 0.08, z, 0.49, 0.009, 0.49);
    core.rotation.y = Math.PI / 4;
  }
  const trim = new Group();
  trim.userData.slot = "wall";
  room.add(trim);
  const back = -ROOM_DEPTH / 2 + 0.44;
  const left = -ROOM_WIDTH / 2 + 0.39;
  for (const [x, z, rotation] of [
    [-7.6, back, 0],
    [7.3, back, 0],
    [left, 5.1, Math.PI / 2],
  ] as const) {
    const niche = new Group();
    niche.position.set(x, 0, z);
    niche.rotation.y = rotation;
    trim.add(niche);
    relief(niche, 0, 4.98, 0.02, 1.45, fresco);
  }
  // Верхняя лента связывает разные отделы; каждый медальон получает свой рисунок.
  const friezeCount = Math.floor((ROOM_WIDTH - 1.2) / 0.765);
  for (let i = 0; i < friezeCount; i++) {
    const x = (-(friezeCount - 1) * 0.765) / 2 + i * 0.765;
    g.ball(trim, i % 2 ? m.claret : m.sage, x, 5.2, back, 0.19, 0.13, 0.022);
    const seal = g.hoop(trim, m.brass, x, 5.2, back + 0.03, 0.205);
    seal.scale.y *= 0.68;
    g.ball(trim, m.brass, x, 5.2, back + 0.05, 0.035);
  }
  const sideCount = Math.floor((ROOM_DEPTH - 1.2) / 0.765);
  for (let i = 0; i < sideCount; i++) {
    const ornament = new Group();
    ornament.position.set(left, 5.2, (-(sideCount - 1) * 0.765) / 2 + i * 0.765);
    ornament.rotation.y = Math.PI / 2;
    trim.add(ornament);
    g.ball(ornament, i % 2 ? m.sage : m.claret, 0, 0, 0, 0.19, 0.13, 0.022);
    const seal = g.hoop(ornament, m.brass, 0, 0, 0.03, 0.205);
    seal.scale.y *= 0.68;
  }
  return () => temporary.forEach((geometry) => geometry.dispose());
}
