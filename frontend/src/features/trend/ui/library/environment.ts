import { Group, PointLight, Vector3, type MeshStandardMaterial } from "three";
import { batchArchitecture, type LibraryGeometry } from "./geometry";
import { libraryFurniture } from "./furniture";
import type { LibraryMaterials } from "./materials";
import { ambientScheduler } from "./ambientScheduler";
import { ambientAir } from "./ambientAir";
import { ambientVisitors } from "./ambientVisitors";
import { ambientVignettes } from "./ambientVignettes";
import { libraryRandom } from "./random";
import { worldResidents } from "./worldResidents";
import { ambientCuriosities } from "./ambientCuriosities";
import { libraryExhibits } from "./exhibits";
import { libraryGallery } from "./gallery";

type Candle = {
  flame: Group;
  material: MeshStandardMaterial;
  light?: PointLight;
  x: number;
  z: number;
  phase: number;
  leanX: number;
  leanZ: number;
};
function influence(x: number, z: number, position: Vector3, radius = 2.15) {
  const distance = Math.hypot(x - position.x, z - position.z);
  const proximity = Math.max(0, 1 - distance / radius);
  return proximity * proximity;
}

/** Один генератор на сессию меняет очередность эпизодов, маршруты гостей и силу порывов. */
export function libraryEnvironment(g: LibraryGeometry, m: LibraryMaterials, seed?: number) {
  const root = new Group();
  root.name = "library-environment";
  const fixed = new Group();
  const f = libraryFurniture(g, m);
  const random = libraryRandom(seed);
  const scheduler = ambientScheduler(random);
  const candles: Candle[] = [];
  function sconce(x: number, z: number, rotation: number, lit = false, heightOffset = 0) {
    const bracket = new Group();
    bracket.position.set(x, heightOffset, z);
    bracket.rotation.y = rotation;
    fixed.add(bracket);
    g.box(bracket, m.brass, 0, 3.12, 0, 0.28, 0.47, 0.09);
    g.ball(bracket, m.oak, 0, 3.15, 0.051, 0.089, 0.16, 0.031);
    g.rod(bracket, m.brass, [0, 3.03, 0], [0, 2.94, 0.3], 0.032);
    g.rod(bracket, m.brass, [0, 2.94, 0.3], [0, 3.21, 0.56], 0.035);
    g.ball(bracket, m.brass, 0, 3.23, 0.56, 0.2, 0.037, 0.2);
    g.rod(bracket, m.paper, [0, 3.26, 0.56], [0, 3.65, 0.56], 0.092);
    g.rod(bracket, m.ink, [0, 3.64, 0.56], [0, 3.73, 0.56], 0.014);
    const worldX = x + Math.sin(rotation) * 0.56;
    const worldZ = z + Math.cos(rotation) * 0.56;
    const material = m.active.clone();
    material.color.lerp(m.paper.color, 0.35);
    material.emissive.copy(material.color);
    material.emissiveIntensity = 1.1;
    const flame = new Group();
    flame.name = "candle-flame";
    flame.position.set(worldX, 3.69 + heightOffset, worldZ);
    root.add(flame);
    const outer = g.ball(flame, material, 0, 0.15, 0, 0.095, 0.21, 0.085);
    const tip = g.ball(flame, material, 0.016, 0.31, 0, 0.042, 0.13, 0.037);
    const core = g.ball(flame, m.light, 0, 0.075, 0, 0.05, 0.1, 0.045);
    for (const mesh of [outer, tip, core]) {
      mesh.castShadow = false;
      mesh.receiveShadow = false;
    }
    const light = lit ? new PointLight(material.color, 0.95, 4.5, 2) : undefined;
    if (light) {
      light.position.set(worldX, 3.78 + heightOffset, worldZ);
      root.add(light);
    }
    candles.push({
      flame,
      material,
      light,
      x: worldX,
      z: worldZ,
      phase: random() * 6.28,
      leanX: 0,
      leanZ: 0,
    });
  }
  sconce(-4.85, -10, Math.PI / 2);
  sconce(-4.85, 3.8, Math.PI / 2, true);
  sconce(5.55, -9.6, -Math.PI / 2, true);
  sconce(6.65, 5.2, Math.PI / 2);
  for (const z of [-6, 4.8]) {
    sconce(-17.15, z, -Math.PI / 2, false, -0.57);
    sconce(17.15, z, Math.PI / 2, false, -0.57);
  }
  const plant = f.plant(root, 14.9, 3.8, 1.55);
  plant.canopy.name = "responsive-leaves";
  g.box(fixed, m.wood, 12, 0.52, 7.1, 1.07, 0.95, 0.8);
  g.box(fixed, m.oak, 12, 1.03, 7.1, 1.19, 0.09, 0.9);
  const globe = f.globe(root, 12, 1.09, 7.1, 1.25);
  globe.orb.name = "responsive-globe";
  const lectern = new Group();
  lectern.userData.slot = "lectern";
  lectern.position.set(-7.1, 0, 5.4);
  fixed.add(lectern);
  g.box(lectern, m.wood, 0, 0.12, 0, 0.76, 0.16, 0.6);
  g.rod(lectern, m.oak, [0, 0.16, 0], [0, 1.21, 0], 0.075);
  g.ball(lectern, m.brass, 0, 0.97, 0, 0.1, 0.085, 0.1);
  const lecternTop = new Group();
  lecternTop.position.y = 1.25;
  lecternTop.rotation.x = -0.18;
  lectern.add(lecternTop);
  g.box(lecternTop, m.wood, 0, 0, 0, 1.05, 0.09, 0.85);
  g.box(lecternTop, m.brass, 0, 0.075, 0.35, 1.06, 0.055, 0.04);
  for (const side of [-1, 1]) {
    g.box(lecternTop, m.claret, side * 0.237, 0.065, 0, 0.47, 0.035, 0.66);
    g.box(lecternTop, m.paper, side * 0.228, 0.091, 0, 0.43, 0.027, 0.61);
    for (let line = 0; line < 8; line++) {
      g.box(lecternTop, m.oak, side * 0.23, 0.108, -0.225 + line * 0.06, 0.28, 0.004, 0.007);
    }
  }
  const leaf = new Group();
  leaf.name = "responsive-page";
  leaf.position.set(-7.1, 1.367, 5.42);
  leaf.rotation.x = -0.18;
  root.add(leaf);
  g.box(leaf, m.paper, 0.219, 0, 0, 0.433, 0.008, 0.603);
  for (let line = 0; line < 8; line++)
    g.box(leaf, m.oak, 0.23, 0.006, -0.225 + line * 0.06, 0.28, 0.002, 0.007);
  g.box(lecternTop, m.claret, 0.06, 0.112, 0.27, 0.045, 0.006, 0.21);
  const air = ambientAir(g, m, root, fixed, scheduler, random);
  const visitors = ambientVisitors(g, m, root, fixed, scheduler, random);
  const vignettes = ambientVignettes(g, m, root, fixed, scheduler);
  const clock = libraryClock(g, m, root, fixed);
  const curiosities = ambientCuriosities(g, m, root, scheduler);
  const residents = worldResidents(g, m, root, fixed, scheduler, random);
  const exhibits = libraryExhibits(g, m, root, scheduler);
  const gallery = libraryGallery(g, m, root, scheduler);
  root.add(batchArchitecture(fixed));
  const previous = new Vector3();
  let initialized = false;
  let plantLeanX = 0;
  let plantLeanZ = 0;
  let pageLift = 0;
  let globeSpeed = 0;
  let globeTurn = 0;
  function advance(dt: number, position: Vector3, still = false) {
    const delta = Math.min(0.12, Math.max(0, dt));
    let velocityX = 0;
    let velocityZ = 0;
    if (initialized && delta > 0 && !still) {
      velocityX = Math.max(-3, Math.min(3, (position.x - previous.x) / delta));
      velocityZ = Math.max(-3, Math.min(3, (position.z - previous.z) / delta));
    }
    initialized = true;
    previous.copy(position);
    if (!still) scheduler.advance(delta);
    const time = scheduler.time();
    const response = still ? 1 : 1 - Math.exp(-delta * 4.8);
    const speed = Math.hypot(velocityX, velocityZ);
    for (const station of candles) {
      const proximity = still ? 0 : influence(station.x, station.z, position, 2.55);
      const wind = still ? 0 : scheduler.wind(station.x, station.z);
      station.leanX += (velocityZ * proximity * 0.37 + wind * 0.08 - station.leanX) * response;
      station.leanZ += (-velocityX * proximity * 0.37 - wind * 0.38 - station.leanZ) * response;
      const flicker = still
        ? 0
        : (Math.sin(time * 2.3 + station.phase) + Math.sin(time * 3.7 + station.phase * 1.4)) *
          0.013;
      station.flame.rotation.set(station.leanX + flicker * 0.45, 0, station.leanZ + flicker);
      station.flame.scale.y = 1 + flicker - proximity * speed * 0.04 - wind * 0.1;
      station.material.emissiveIntensity = 1.1 + flicker - wind * 0.09;
      if (station.light) station.light.intensity = 0.95 + flicker * 0.3 - wind * 0.07;
    }
    const plantWake = still ? 0 : influence(14.9, 3.8, position, 2.5);
    const plantWind = still ? 0 : scheduler.wind(14.9, 3.8);
    plantLeanX += (velocityZ * plantWake * 0.24 - plantLeanX) * response;
    plantLeanZ += (-velocityX * plantWake * 0.24 - plantWind * 0.23 - plantLeanZ) * response;
    plant.canopy.rotation.set(plantLeanX, 0, plantLeanZ);
    const pageWake = still ? 0 : influence(-7.1, 5.4, position, 2.4) * Math.min(1.5, speed);
    const pageWind = still ? 0 : scheduler.wind(-7.1, 5.4);
    const dust = scheduler.event("dust");
    const dustGesture =
      !still && dust
        ? Math.sin(Math.min(1, (time - dust.start) / dust.duration) * Math.PI) * 0.55
        : 0;
    pageLift +=
      (pageWake * 1.5 + pageWind * 1.0 + dustGesture - pageLift) *
      (still ? 1 : 1 - Math.exp(-delta * 3.2));
    leaf.rotation.z = -pageLift;
    const globeWake = still ? 0 : influence(12, 7.1, position, 2.6) * speed;
    globeSpeed += (globeWake * 0.38 - globeSpeed) * (still ? 1 : 1 - Math.exp(-delta * 1.6));
    if (!still) globeTurn += globeSpeed * delta;
    curiosities.advance(still);
    globe.orb.rotation.y = globeTurn + curiosities.globeTurn();
    air.advance(still);
    visitors.advance(delta, position, still);
    residents.advance(delta, position, still);
    vignettes.advance(still);
    clock.advance(time, still);
    exhibits.advance(still, position);
    gallery.advance(still, position);
  }
  return { root, advance, dispose: air.dispose };
}

function libraryClock(g: LibraryGeometry, m: LibraryMaterials, root: Group, fixed: Group) {
  const casework = new Group();
  casework.userData.slot = "clock";
  casework.position.set(15.1, 0, -7.3);
  fixed.add(casework);
  g.box(casework, m.wood, 0, 0.14, 0, 1.18, 0.25, 0.83);
  g.box(casework, m.wood, 0, 1.26, -0.2, 0.95, 2.05, 0.23);
  g.box(casework, m.recess, 0, 1.1, -0.058, 0.7, 1.6, 0.045);
  for (const side of [-1, 1]) {
    g.box(casework, m.oak, side * 0.45, 1.28, 0.1, 0.15, 2.15, 0.51);
    g.box(casework, m.brass, side * 0.45, 1.28, 0.36, 0.023, 2.08, 0.014);
  }
  g.box(casework, m.wood, 0, 2.39, 0.03, 1.12, 0.15, 0.77);
  g.box(casework, m.brass, 0, 2.485, 0.03, 1.2, 0.045, 0.8);
  g.ball(casework, m.paper, 0, 1.98, 0.35, 0.37, 0.37, 0.027);
  g.hoop(casework, m.brass, 0, 1.98, 0.38, 0.39);
  for (let hour = 0; hour < 12; hour++) {
    const angle = (hour / 12) * Math.PI * 2;
    g.rod(
      casework,
      m.wood,
      [Math.sin(angle) * 0.27, 1.98 + Math.cos(angle) * 0.27, 0.386],
      [Math.sin(angle) * 0.318, 1.98 + Math.cos(angle) * 0.318, 0.386],
      0.012,
    );
  }
  const pendulum = new Group();
  pendulum.name = "library-pendulum";
  pendulum.position.set(15.1, 1.56, -6.96);
  root.add(pendulum);
  const bob = new Group();
  g.rod(bob, m.brass, [0, 0, 0], [0, -0.83, 0], 0.027);
  g.ball(bob, m.brass, 0, -0.81, 0, 0.19, 0.2, 0.051);
  g.hoop(bob, m.oak, 0, -0.81, 0.052, 0.13);
  pendulum.add(batchArchitecture(bob));
  const hands = new Group();
  hands.name = "library-clock-hands";
  hands.position.set(15.1, 1.98, -6.885);
  root.add(hands);
  g.rod(hands, m.wood, [0, -0.035, 0], [0.045, 0.285, 0], 0.018);
  g.rod(hands, m.wood, [0, 0, 0.01], [-0.16, 0.1, 0.01], 0.023);
  g.ball(hands, m.brass, 0, 0, 0.018, 0.04, 0.04, 0.017);
  function advance(time: number, still: boolean) {
    pendulum.rotation.z = still ? 0 : Math.sin(time * 2.8) * 0.27;
    hands.rotation.z = still ? 0 : -time * 0.013;
  }
  return { advance };
}
