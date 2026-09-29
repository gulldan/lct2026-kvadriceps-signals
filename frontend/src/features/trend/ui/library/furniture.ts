import { Group, type Object3D } from "three";
import type { LibraryGeometry } from "./geometry";
import type { LibraryMaterials } from "./materials";

export function libraryFurniture(g: LibraryGeometry, m: LibraryMaterials) {
  function book(parent: Object3D, x: number, y: number, z: number, index: number, scale = 1) {
    const cover = [m.navy, m.claret, m.sage, m.oak][index % 4]!;
    const group = new Group();
    group.position.set(x, y, z);
    group.scale.setScalar(scale);
    group.rotation.y = ((index % 3) - 1) * 0.14;
    parent.add(group);
    g.box(group, cover, 0, 0, 0, 0.5, 0.1, 0.64);
    g.box(group, m.paper, 0.025, 0, 0, 0.44, 0.061, 0.59);
    g.box(group, m.brass, 0, 0.052, 0, 0.25, 0.005, 0.35);
    g.box(group, cover, 0, 0.056, 0, 0.22, 0.005, 0.32);
    return group;
  }
  function plant(parent: Object3D, x: number, z: number, scale = 1, y = 0) {
    const group = new Group();
    group.userData.slot = "plant";
    group.position.set(x, y, z);
    group.scale.setScalar(scale);
    parent.add(group);
    g.ball(group, m.oak, 0, 0.23, 0, 0.23, 0.29, 0.23);
    g.rod(group, m.brass, [0, 0.43, 0], [0, 0.5, 0], 0.24);
    const canopy = new Group();
    canopy.position.y = 0.44;
    group.add(canopy);
    for (let i = 0; i < 9; i++) {
      const angle = i * 2.4;
      const height = 0.6 + (i % 4) * 0.16;
      const leafX = Math.cos(angle) * 0.27;
      const leafZ = Math.sin(angle) * 0.27;
      g.rod(canopy, m.wood, [0, 0, 0], [leafX, height - 0.44, leafZ], 0.018);
      const leaf = g.ball(canopy, m.sage, leafX, height - 0.44, leafZ, 0.12, 0.25, 0.055);
      leaf.rotation.set(Math.sin(angle) * 0.7, angle, Math.cos(angle) * 0.7);
    }
    return { root: group, canopy };
  }
  function lamp(parent: Object3D, x: number, y: number, z: number) {
    const group = new Group();
    group.userData.slot = "lamp";
    parent.add(group);
    g.ball(group, m.brass, x, y + 0.03, z, 0.21, 0.04, 0.21);
    g.rod(group, m.brass, [x, y, z], [x, y + 0.68, z], 0.032);
    g.ball(group, m.brass, x, y + 0.69, z, 0.25, 0.14, 0.25);
    g.ball(group, m.light, x, y + 0.64, z, 0.21, 0.028, 0.21).userData.emitter = "lamp";
    g.ball(group, m.brass, x, y + 0.84, z, 0.045);
  }
  function chair(parent: Object3D, x: number, z: number, rotation: number) {
    const group = new Group();
    group.userData.slot = "chair";
    group.position.set(x, 0, z);
    group.rotation.y = rotation;
    parent.add(group);
    g.box(group, m.wood, 0, 0.53, 0, 0.6, 0.13, 0.57);
    g.box(group, m.navy, 0, 0.62, 0, 0.53, 0.09, 0.49);
    for (const legX of [-0.24, 0.24]) {
      for (const legZ of [-0.22, 0.22]) g.box(group, m.wood, legX, 0.26, legZ, 0.055, 0.5, 0.055);
      g.box(group, m.wood, legX, 0.9, 0.25, 0.055, 0.7, 0.055);
    }
    g.box(group, m.wood, 0, 1.24, 0.25, 0.6, 0.1, 0.07);
    g.box(group, m.navy, 0, 1.02, 0.25, 0.43, 0.29, 0.08);
    return group;
  }
  function globe(parent: Object3D, x: number, y: number, z: number, scale = 1) {
    const group = new Group();
    group.userData.slot = "globe";
    group.position.set(x, y, z);
    group.scale.setScalar(scale);
    parent.add(group);
    g.ball(group, m.brass, 0, 0.04, 0, 0.23, 0.04, 0.23);
    g.rod(group, m.brass, [0, 0.04, 0], [0, 0.3, 0], 0.045);
    const orb = new Group();
    orb.position.y = 0.65;
    orb.rotation.z = -0.35;
    group.add(orb);
    g.ball(orb, m.sage, 0, 0, 0, 0.32);
    const hoop = g.hoop(group, m.brass, 0, 0.65, 0, 0.38);
    hoop.rotation.z = -0.35;
    const equator = g.hoop(orb, m.brass, 0, 0, 0, 0.325);
    equator.rotation.x = Math.PI / 2;
    for (let i = 0; i < 6; i++) {
      const angle = i * 1.8;
      g.ball(
        orb,
        m.paper,
        Math.cos(angle) * 0.24,
        0.07 + Math.sin(i) * 0.13,
        Math.sin(angle) * 0.23,
        0.095,
        0.07,
        0.06,
      );
    }
    return { root: group, orb };
  }
  return { book, plant, lamp, chair, globe };
}
