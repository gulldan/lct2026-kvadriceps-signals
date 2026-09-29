import { Group } from "three";
import { batchArchitecture, type LibraryGeometry } from "./geometry";
import { libraryFurniture } from "./furniture";
import type { LibraryMaterials } from "./materials";
import type { AmbientScheduler } from "./ambientScheduler";

const ease = (value: number) => value * value * (3 - 2 * value);
const openEnvelope = (progress: number) =>
  progress < 0.25
    ? ease(progress / 0.25)
    : progress < 0.68
      ? 1
      : 1 - ease((progress - 0.68) / 0.32);

export function ambientVignettes(
  g: LibraryGeometry,
  m: LibraryMaterials,
  root: Group,
  fixed: Group,
  scheduler: AmbientScheduler,
) {
  const f = libraryFurniture(g, m);
  for (const [x, z] of [
    [-14.8, 10.2],
    [15.1, 9],
  ] as const) {
    // Столешница и ножки — заглушка под модель столика; лампа и книга остаются.
    const stand = new Group();
    stand.userData.slot = "side";
    fixed.add(stand);
    g.box(stand, m.wood, x, 1.08, z, 1.16, 0.15, 1.1);
    g.box(stand, m.oak, x, 1.18, z, 1.23, 0.06, 1.16);
    for (const side of [-1, 1])
      for (const end of [-1, 1])
        g.rod(
          stand,
          m.wood,
          [x + side * 0.42, 0.1, z + end * 0.39],
          [x + side * 0.42, 1.05, z + end * 0.39],
          0.055,
        );
    f.lamp(fixed, x, 1.22, z);
    f.book(fixed, x - 0.23, 1.31, z - 0.27, 2, 0.66);
  }
  g.rod(fixed, m.paper, [-14.48, 1.23, 10.38], [-14.48, 1.48, 10.38], 0.15);
  g.ball(fixed, m.wood, -14.48, 1.48, 10.38, 0.128, 0.012, 0.128);
  g.ball(fixed, m.paper, -14.48, 1.24, 10.38, 0.23, 0.022, 0.23);
  g.hoop(fixed, m.paper, -14.3, 1.37, 10.38, 0.092);

  const cabinet = new Group();
  cabinet.position.set(-15.1, 0, 4.8);
  fixed.add(cabinet);
  g.box(cabinet, m.wood, 0, 0.71, 0, 1.25, 1.38, 0.8);
  g.box(cabinet, m.oak, 0, 1.45, 0, 1.37, 0.1, 0.88);
  for (const x of [-0.3, 0.3])
    for (const y of [0.29, 0.61, 0.93, 1.25]) {
      if (x > 0.1 && y > 0.8 && y < 1.1) continue;
      g.box(cabinet, m.oak, x, y, 0.425, 0.55, 0.27, 0.06);
      g.box(cabinet, m.brass, x, y + 0.035, 0.466, 0.18, 0.07, 0.025);
      g.box(cabinet, m.paper, x, y + 0.035, 0.482, 0.13, 0.037, 0.006);
      g.rod(cabinet, m.brass, [x - 0.085, y - 0.055, 0.493], [x + 0.085, y - 0.055, 0.493], 0.018);
    }
  g.box(cabinet, m.recess, 0.3, 0.93, 0.43, 0.56, 0.28, 0.06);
  const drawer = new Group();
  drawer.name = "restless-catalogue-drawer";
  drawer.position.set(-14.8, 0.93, 5.22);
  root.add(drawer);
  const tray = new Group();
  g.box(tray, m.oak, 0, 0, 0, 0.55, 0.27, 0.075);
  g.box(tray, m.brass, 0, 0.035, 0.051, 0.18, 0.07, 0.024);
  g.rod(tray, m.brass, [-0.09, -0.055, 0.078], [0.09, -0.055, 0.078], 0.022);
  g.box(tray, m.wood, 0, -0.112, -0.25, 0.49, 0.035, 0.55);
  for (const side of [-1, 1]) g.box(tray, m.wood, side * 0.24, -0.025, -0.25, 0.035, 0.19, 0.55);
  for (let sheet = 0; sheet < 5; sheet++)
    g.box(tray, m.paper, 0.015 * Math.sin(sheet), -0.06 + sheet * 0.015, -0.25, 0.4, 0.012, 0.42);
  drawer.add(batchArchitecture(tray));

  const scrollTable = new Group();
  scrollTable.position.set(9.15, 0, 7.15);
  fixed.add(scrollTable);
  g.box(scrollTable, m.wood, 0, 1.08, 0, 1.84, 0.13, 0.89);
  g.box(scrollTable, m.oak, 0, 1.18, 0, 1.9, 0.065, 0.95);
  for (const side of [-1, 1])
    for (const end of [-1, 1])
      g.rod(
        scrollTable,
        m.wood,
        [side * 0.72, 0.1, end * 0.32],
        [side * 0.72, 1.05, end * 0.32],
        0.048,
      );
  const scroll = new Group();
  scroll.name = "unrolling-atlas";
  scroll.position.set(9.15, 1.24, 7.15);
  root.add(scroll);
  const paper = g.box(scroll, m.paper, 0, 0, 0, 1, 0.013, 0.67);
  const scrollLeft = new Group();
  const scrollRight = new Group();
  scroll.add(scrollLeft, scrollRight);
  for (const end of [scrollLeft, scrollRight]) {
    g.rod(end, m.paper, [0, 0.055, -0.37], [0, 0.055, 0.37], 0.1);
    g.rod(end, m.oak, [0, 0.055, -0.47], [0, 0.055, 0.47], 0.035);
    for (const sign of [-1, 1]) g.ball(end, m.brass, 0, 0.055, sign * 0.45, 0.067, 0.067, 0.035);
  }
  const mapMark = g.hoop(scroll, m.sage, 0, 0.012, 0, 0.2);
  mapMark.rotation.x = -Math.PI / 2;
  for (const side of [-1, 1])
    g.rod(scroll, m.oak, [side * 0.16, 0.016, -0.18], [side * 0.1, 0.016, 0.16], 0.006);

  const feather = new Group();
  feather.name = "wandering-quill";
  root.add(feather);
  const quill = new Group();
  g.rod(quill, m.oak, [0, 0, -0.31], [0, 0, 0.29], 0.012);
  for (let barb = 0; barb < 7; barb++) {
    const z = -0.12 + barb * 0.061;
    const width = 0.075 + Math.sin((barb / 7) * Math.PI) * 0.07;
    for (const side of [-1, 1]) {
      const vane = g.ball(quill, m.paper, side * width * 0.45, 0.008, z, width * 0.68, 0.017, 0.09);
      vane.rotation.y = side * 0.48;
    }
  }
  feather.add(batchArchitecture(quill));
  const quillRest = [
    [-7.39, 1.42, 5.59],
    [-6.87, 1.42, 5.38],
    [-7.26, 1.42, 5.16],
    [-7.2, 1.42, 5.51],
  ] as const;
  let featherHome = 0;
  let featherSerial = 0;
  let featherFrom = 0;
  let featherDestination = 1;
  function advance(still: boolean) {
    const time = scheduler.time();
    const drawerEvent = still ? undefined : scheduler.event("drawer");
    const drawerProgress = drawerEvent
      ? Math.min(1, (time - drawerEvent.start) / drawerEvent.duration)
      : 0;
    const opening = drawerEvent ? openEnvelope(drawerProgress) : 0;
    drawer.position.z = 5.22 + opening * (0.31 + (drawerEvent?.variant ?? 0) * 0.045);
    drawer.rotation.z = drawerEvent ? Math.sin(drawerProgress * Math.PI * 2) * opening * 0.021 : 0;
    const scrollEvent = still ? undefined : scheduler.event("scroll");
    const scrollProgress = scrollEvent
      ? Math.min(1, (time - scrollEvent.start) / scrollEvent.duration)
      : 0;
    const extension = scrollEvent ? openEnvelope(scrollProgress) : 0;
    const span = 0.49 + extension * 1.06;
    paper.scale.x = span;
    scrollLeft.position.x = -span / 2;
    scrollRight.position.x = span / 2;
    scrollRight.rotation.z = -extension * 6;
    scrollLeft.rotation.z = extension * 3;
    mapMark.scale.setScalar(0.09 + extension * 0.11);
    const event = still ? undefined : scheduler.event("feather");
    if (event && event.serial !== featherSerial) {
      featherSerial = event.serial;
      featherFrom = featherHome;
      featherDestination = (featherHome + 1 + event.variant) % quillRest.length;
      if (featherDestination === featherHome)
        featherDestination = (featherDestination + 1) % quillRest.length;
      featherHome = featherDestination;
    }
    const start = quillRest[featherFrom]!;
    const end = quillRest[featherHome]!;
    const progress = event ? Math.min(1, (time - event.start) / event.duration) : 1;
    const blend = ease(progress);
    const lift = event ? Math.sin(Math.PI * progress) : 0;
    feather.position.set(
      start[0] + (end[0] - start[0]) * blend + lift * 0.85 * (event?.variant === 1 ? -1 : 1),
      start[1] + lift * (0.8 + (event?.strength ?? 0) * 0.35),
      start[2] + (end[2] - start[2]) * blend + Math.sin(progress * Math.PI * 2) * lift * 0.36,
    );
    feather.rotation.set(
      lift * 0.38,
      0.28 + lift * (event?.variant ?? 0) * 0.7,
      Math.sin(progress * Math.PI * 3) * lift * 0.24,
    );
  }
  advance(true);
  return { advance };
}
