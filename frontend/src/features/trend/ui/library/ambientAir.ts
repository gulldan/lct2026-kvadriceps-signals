import {
  BufferAttribute,
  BufferGeometry,
  DataTexture,
  DoubleSide,
  Group,
  LinearFilter,
  Mesh,
  MeshBasicMaterial,
  PlaneGeometry,
  Points,
  PointsMaterial,
} from "three";
import type { LibraryGeometry } from "./geometry";
import type { LibraryMaterials } from "./materials";
import type { AmbientRandom, AmbientScheduler } from "./ambientScheduler";

export function ambientAir(
  g: LibraryGeometry,
  m: LibraryMaterials,
  root: Group,
  fixed: Group,
  scheduler: AmbientScheduler,
  random: AmbientRandom,
) {
  const fabric = m.navy.clone();
  fabric.color.lerp(m.claret.color, 0.13);
  fabric.side = DoubleSide;
  fabric.roughness = 1;
  const curtains: { mesh: Mesh<PlaneGeometry>; rest: Float32Array; z: number }[] = [];
  for (const center of [-6.8, -0.5]) {
    g.rod(fixed, m.brass, [-22.78, 4.72, center - 1.46], [-22.78, 4.72, center + 1.46], 0.045);
    g.box(fixed, m.wood, -22.77, 4.85, center, 0.22, 0.2, 3.05);
    for (const side of [-1, 1]) {
      const geometry = new PlaneGeometry(0.74, 3.5, 4, 14);
      const mesh = new Mesh(geometry, fabric);
      mesh.name = "window-curtain";
      mesh.rotation.y = Math.PI / 2;
      mesh.position.set(-22.72, 2.9, center + side * 1.04);
      mesh.castShadow = true;
      root.add(mesh);
      curtains.push({
        mesh,
        rest: new Float32Array(geometry.attributes.position!.array),
        z: center,
      });
      for (let ring = 0; ring < 5; ring++) {
        const hoop = g.hoop(
          fixed,
          m.brass,
          -22.77,
          4.66,
          center + side * 1.04 - 0.29 + ring * 0.145,
          0.06,
        );
        hoop.rotation.y = Math.PI / 2;
      }
      g.rod(
        fixed,
        m.brass,
        [-22.82, 2.05, center + side * 1.08],
        [-22.56, 1.92, center + side * 1.08],
        0.024,
      );
      g.ball(fixed, m.brass, -22.56, 1.83, center + side * 1.08, 0.054, 0.13, 0.054);
    }
  }
  const bookmarks: Mesh[] = [];
  for (let index = 0; index < 3; index++) {
    const slip = g.box(
      root,
      index === 1 ? m.claret : m.paper,
      -7.03 + index * 0.13,
      1.39,
      5.47,
      0.18,
      0.01,
      0.46,
    );
    slip.name = "loose-bookmark";
    bookmarks.push(slip);
  }
  const pixels = new Uint8Array(16 * 16 * 4);
  for (let index = 0; index < 256; index++) {
    const x = ((index % 16) - 7.5) / 7.5;
    const y = (Math.floor(index / 16) - 7.5) / 7.5;
    const alpha = Math.max(0, 1 - x * x - y * y) ** 2;
    pixels[index * 4] = 255;
    pixels[index * 4 + 1] = 255;
    pixels[index * 4 + 2] = 255;
    pixels[index * 4 + 3] = Math.round(alpha * 255);
  }
  const soft = new DataTexture(pixels, 16, 16);
  soft.magFilter = LinearFilter;
  soft.minFilter = LinearFilter;
  soft.needsUpdate = true;
  const particles = (name: string, count: number, size: number, opacity: number) => {
    const geometry = new BufferGeometry();
    const positions = new Float32Array(count * 3);
    geometry.setAttribute("position", new BufferAttribute(positions, 3));
    const material = new PointsMaterial({
      color: m.paper.color,
      size,
      sizeAttenuation: false,
      transparent: true,
      opacity,
      depthWrite: false,
      map: soft,
    });
    const points = new Points(geometry, material);
    points.name = name;
    points.frustumCulled = false;
    root.add(points);
    return { geometry, positions, material, points };
  };
  const dust = particles("window-dust", 48, 2.1, 0.3);
  const burst = particles("folio-dust", 25, 2.5, 0);
  const steam = particles("tea-steam", 11, 8, 0);
  const beamMaterial = new MeshBasicMaterial({
    color: m.paper.color,
    transparent: true,
    opacity: 0.15,
    depthWrite: false,
    map: soft,
  });
  const beam = new Mesh(new PlaneGeometry(7.1, 2.5), beamMaterial);
  beam.name = "window-light";
  beam.position.set(-19.05, 0.076, -6.8);
  beam.rotation.x = -Math.PI / 2;
  root.add(beam);
  const seeds = Array.from({ length: 48 }, () => ({
    x: random(),
    y: random(),
    z: random(),
    phase: random() * Math.PI * 2,
  }));
  let burstSerial = 0;
  let burstDirection = 1;
  let quiet = true;
  const windAt = (x: number, z: number) => (quiet ? 0 : scheduler.wind(x, z));
  function advance(still: boolean) {
    quiet = still;
    const time = still ? 0 : scheduler.time();
    for (const curtain of curtains) {
      const wind = windAt(-22.72, curtain.z);
      const positions = curtain.mesh.geometry.attributes.position!;
      for (let vertex = 0; vertex < positions.count; vertex++) {
        const offset = vertex * 3;
        const x = curtain.rest[offset]!;
        const y = curtain.rest[offset + 1]!;
        const free = Math.max(0, (1.75 - y) / 3.5);
        positions.setXYZ(
          vertex,
          x + Math.sin(time * 1.5 + y * 2.3) * wind * free * 0.1,
          y + wind * free * 0.08,
          Math.sin(x * 29) * 0.049 +
            wind * free ** 1.5 * (0.8 + Math.sin(time * 1.9 + y * 1.4) * 0.2),
        );
      }
      positions.needsUpdate = true;
    }
    for (let index = 0; index < bookmarks.length; index++) {
      const slip = bookmarks[index]!;
      const wind = windAt(-7.1 - index * 0.8, 5.4);
      const flutter = Math.sin(time * 4.2 + index * 1.8) * wind;
      slip.position.set(
        -7.03 + index * 0.13 + wind * (0.45 + index * 0.19),
        1.39 + index * 0.009 + wind * (0.5 + index * 0.25),
        5.47 + wind * 0.28 + flutter * 0.1,
      );
      slip.rotation.set(flutter * 0.6, 0.1 + wind * 0.5, wind * 0.27 + flutter * 0.12);
    }
    for (let index = 0; index < seeds.length; index++) {
      const seed = seeds[index]!;
      const wind = windAt(-22.2 + seed.x * 5.2, -6.8);
      dust.positions[index * 3] =
        -22.25 + seed.x * 5.2 + wind * 0.32 + Math.sin(time * 0.17 + seed.phase) * 0.18;
      dust.positions[index * 3 + 1] =
        0.7 + seed.y * 3.1 + Math.sin(time * 0.29 + seed.phase) * 0.13;
      dust.positions[index * 3 + 2] =
        -7.75 + seed.z * 1.9 + Math.cos(time * 0.21 + seed.phase) * 0.13;
    }
    dust.geometry.attributes.position!.needsUpdate = true;
    dust.material.opacity = still ? 0.18 : 0.25 + windAt(-22, -6.8) * 0.14;
    beamMaterial.opacity = 0.13 + windAt(-22, -6.8) * 0.045;
    const episode = still ? undefined : scheduler.event("dust");
    if (episode && episode.serial !== burstSerial) {
      burstSerial = episode.serial;
      burstDirection = episode.variant % 2 ? 1 : -1;
    }
    const age = episode ? (time - episode.start) / episode.duration : 0;
    burst.points.visible = Boolean(episode);
    burst.material.opacity = episode ? Math.sin(age * Math.PI) * 0.52 : 0;
    for (let index = 0; index < 25; index++) {
      const seed = seeds[index]!;
      const spread = Math.sqrt(age);
      burst.positions[index * 3] =
        -7.1 + (seed.x - 0.5) * spread * 2.2 + spread * 0.5 * burstDirection;
      burst.positions[index * 3 + 1] = 1.4 + spread * (0.2 + seed.y * 1.2);
      burst.positions[index * 3 + 2] = 5.4 + (seed.z - 0.5) * spread * 1.5;
    }
    burst.geometry.attributes.position!.needsUpdate = true;
    const tea = still ? undefined : scheduler.event("tea");
    const teaAge = tea ? (time - tea.start) / tea.duration : 0;
    steam.points.visible = Boolean(tea);
    steam.material.opacity = tea ? Math.sin(teaAge * Math.PI) * 0.22 : 0;
    for (let index = 0; index < 11; index++) {
      const rise = (time * 0.14 + seeds[index]!.phase) % 1;
      const wind = windAt(-14.48, 10.38);
      steam.positions[index * 3] =
        -14.48 + Math.sin(rise * 5 + index * 0.4) * rise * 0.17 + wind * rise * 0.4;
      steam.positions[index * 3 + 1] = 1.48 + rise * 0.92;
      steam.positions[index * 3 + 2] = 10.38 + Math.cos(rise * 4 + index * 0.6) * rise * 0.09;
    }
    steam.geometry.attributes.position!.needsUpdate = true;
  }
  function dispose() {
    soft.dispose();
    for (const system of [dust, burst, steam]) {
      system.geometry.dispose();
      system.material.dispose();
    }
  }
  advance(true);
  return { advance, dispose };
}
