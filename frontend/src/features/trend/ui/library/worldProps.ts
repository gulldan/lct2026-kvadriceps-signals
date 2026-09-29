import { Group, Vector3 } from "three";
import { type LibraryGeometry } from "./geometry";
import { libraryFurniture } from "./furniture";
import type { LibraryMaterials } from "./materials";

const RECEIVERS = [
  [2.27, 1.24, 8.37],
  [-9.2, 1.25, 15],
  [6.8, 1.3, 15.5],
] as const;

export function worldProps(g: LibraryGeometry, m: LibraryMaterials, root: Group, fixed: Group) {
  const state = new Group();
  state.name = "library-world-deliveries";
  state.userData.letters = 0;
  state.userData.books = 0;
  root.add(state);
  const table = new Group();
  table.position.set(-9.8, 0, 6.5);
  fixed.add(table);
  const desk = new Group();
  desk.userData.slot = "desk";
  table.add(desk);
  g.box(desk, m.wood, 0, 1.09, 0, 2.1, 0.14, 1);
  g.box(desk, m.oak, 0, 1.19, 0, 2.17, 0.06, 1.07);
  for (const side of [-1, 1])
    for (const end of [-1, 1])
      g.rod(desk, m.wood, [side * 0.86, 0.1, end * 0.37], [side * 0.86, 1.05, end * 0.37], 0.052);
  for (let index = 0; index < 7; index++) {
    g.box(table, m.paper, -0.62, 1.24 + index * 0.019, -0.16, 0.49, 0.015, 0.37);
    g.box(table, m.paper, 0.49, 1.24 + index * 0.017, 0.16, 0.46, 0.013, 0.34);
  }
  const sortingSheet = g.box(root, m.paper, -10.42, 1.39, 6.34, 0.51, 0.014, 0.38);
  sortingSheet.name = "clerk-sorting-sheet";
  const stamp = new Group();
  stamp.position.set(-9.62, 1.3, 6.56);
  root.add(stamp);
  g.box(stamp, m.wood, 0, 0, 0, 0.19, 0.08, 0.16);
  g.rod(stamp, m.brass, [0, 0.035, 0], [0, 0.2, 0], 0.032);
  g.ball(stamp, m.oak, 0, 0.24, 0, 0.077, 0.054, 0.065);

  const letterTrays: Group[][] = [];
  const letterCounts = [0, 0, 0];
  for (const [x, y, z] of RECEIVERS) {
    const tray = new Group();
    tray.position.set(x, y, z);
    fixed.add(tray);
    g.box(tray, m.wood, 0, 0, 0, 0.42, 0.035, 0.31);
    for (const side of [-1, 1]) g.box(tray, m.brass, side * 0.2, 0.045, 0, 0.022, 0.08, 0.32);
    const pile: Group[] = [];
    for (let index = 0; index < 7; index++) {
      const envelope = new Group();
      envelope.position.set(x, y + 0.035 + index * 0.023, z);
      envelope.rotation.y = ((index % 3) - 1) * 0.05;
      envelope.visible = false;
      state.add(envelope);
      g.box(envelope, m.paper, 0, 0, 0, 0.34, 0.019, 0.24);
      g.ball(envelope, m.claret, 0, 0.013, 0, 0.035, 0.008, 0.035);
      pile.push(envelope);
    }
    letterTrays.push(pile);
  }
  const transferLetter = new Group();
  transferLetter.name = "delivered-envelope";
  root.add(transferLetter);
  transferLetter.visible = false;
  g.box(transferLetter, m.paper, 0, 0, 0, 0.36, 0.025, 0.25);
  g.ball(transferLetter, m.claret, 0, 0.019, 0, 0.041, 0.01, 0.041);
  const f = libraryFurniture(g, m);
  const archiveBooks: Group[] = [];
  for (let index = 0; index < 8; index++) {
    const book = f.book(state, -10.43, 1.24 + index * 0.083, 6.73, index, 0.83);
    book.visible = false;
    archiveBooks.push(book);
  }
  const transferBook = f.book(root, -10.5, 1.3, 6.5, 2, 0.9);
  transferBook.visible = false;

  const glass = m.paper.clone();
  glass.transparent = true;
  glass.opacity = 0.19;
  glass.depthWrite = false;
  glass.roughness = 0.18;
  const pipePoints = [
    [-10.65, 1.35, 6.94],
    [-10.65, 2.28, 6.94],
    [-9.2, 2.28, 6.94],
    [-9.2, 1.37, 6.94],
  ] as const;
  for (let index = 1; index < pipePoints.length; index++)
    g.rod(fixed, glass, [...pipePoints[index - 1]!], [...pipePoints[index]!], 0.103);
  for (const x of [-10.65, -9.2]) {
    g.rod(fixed, m.brass, [x, 1.22, 6.94], [x, 1.43, 6.94], 0.14);
    g.rod(fixed, m.brass, [x, 2.17, 6.94], [x, 2.31, 6.94], 0.128);
  }
  const capsule = new Group();
  capsule.name = "pneumatic-letter-capsule";
  capsule.visible = false;
  root.add(capsule);
  g.rod(capsule, m.oak, [0, -0.14, 0], [0, 0.14, 0], 0.082);
  g.rod(capsule, m.brass, [0, -0.16, 0], [0, -0.09, 0], 0.09);
  g.rod(capsule, m.brass, [0, 0.09, 0], [0, 0.16, 0], 0.09);
  const outgoing = g.box(root, m.paper, -9.2, 1.32, 6.79, 0.4, 0.036, 0.26);
  outgoing.visible = false;
  outgoing.name = "received-pneumatic-letter";

  const bell = g.ball(fixed, m.brass, 15.1, 2.69, -7.3, 0.205, 0.16, 0.205);
  g.hoop(fixed, m.brass, 15.1, 2.59, -7.3, 0.212).rotation.x = Math.PI / 2;
  g.rod(fixed, m.brass, [15.1, 2.78, -7.3], [15.1, 2.9, -7.3], 0.03);
  const hammer = new Group();
  hammer.name = "clock-chime-hammer";
  hammer.position.set(15.43, 2.75, -7.3);
  root.add(hammer);
  g.rod(hammer, m.oak, [0, 0, 0], [-0.23, -0.045, 0], 0.018);
  g.ball(hammer, m.brass, -0.24, -0.045, 0, 0.065, 0.065, 0.075);
  bell.castShadow = true;
  const lampWipe = new Group();
  lampWipe.name = "lamp-polishing-cloth";
  lampWipe.visible = false;
  root.add(lampWipe);
  g.box(lampWipe, m.paper, 0, 0, 0, 0.3, 0.03, 0.24);
  g.rod(lampWipe, m.oak, [0, 0, 0], [0, -0.7, -0.85], 0.023);

  function sort(progress: number, active: boolean) {
    const cycle = (progress * 3) % 1;
    const lift = active ? Math.sin(cycle * Math.PI) : 0;
    sortingSheet.position.set(
      -10.42 + (active ? cycle * 0.95 : 0),
      1.39 + lift * 0.4,
      6.34 + (active ? cycle * 0.24 : 0),
    );
    sortingSheet.rotation.z = lift * 0.14;
    stamp.position.y =
      1.3 +
      (active && progress > 0.65 ? Math.abs(Math.sin((progress - 0.65) * Math.PI * 7)) * 0.12 : 0);
  }
  function carryLetter(progress: number, from: Vector3, receiver: number) {
    transferLetter.visible = progress > 0 && progress < 1;
    const to = RECEIVERS[receiver]!;
    const blend = progress * progress * (3 - 2 * progress);
    transferLetter.position.set(
      from.x + (to[0] - from.x) * blend,
      from.y + (to[1] + 0.07 - from.y) * blend + Math.sin(progress * Math.PI) * 0.22,
      from.z + (to[2] - from.z) * blend,
    );
  }
  function depositLetter(receiver: number) {
    letterCounts[receiver] = Math.min(7, letterCounts[receiver]! + 1);
    letterTrays[receiver]!.forEach((letter, index) => {
      letter.visible = index < letterCounts[receiver]!;
    });
    state.userData.letters = Number(state.userData.letters) + 1;
    transferLetter.visible = false;
    outgoing.visible = false;
  }
  function carryBook(progress: number, from: Vector3) {
    transferBook.visible = progress > 0 && progress < 1;
    const height = 1.24 + Math.min(7, Number(state.userData.books)) * 0.083;
    transferBook.position.set(
      from.x + (-10.43 - from.x) * progress,
      from.y + (height - from.y) * progress + Math.sin(progress * Math.PI) * 0.24,
      from.z + (6.73 - from.z) * progress,
    );
  }
  function depositBook() {
    state.userData.books = Number(state.userData.books) + 1;
    archiveBooks.forEach((book, index) => {
      book.visible = index < Math.min(8, Number(state.userData.books));
    });
    transferBook.visible = false;
  }
  function pneumatic(progress: number, active: boolean) {
    capsule.visible = active && progress > 0.1 && progress < 0.91;
    const travel = Math.max(0, Math.min(0.999, (progress - 0.1) / 0.8)) * 3;
    const segment = Math.floor(travel);
    const blend = travel - segment;
    const a = pipePoints[segment]!;
    const b = pipePoints[segment + 1]!;
    capsule.position.set(a[0] + (b[0] - a[0]) * blend, a[1] + (b[1] - a[1]) * blend, a[2]);
    capsule.rotation.z = segment === 1 ? -Math.PI / 2 : 0;
    if (active && progress > 0.9) outgoing.visible = true;
  }
  function chime(progress: number, active: boolean) {
    const pulse = active
      ? Math.max(0, Math.sin(progress * Math.PI * 6)) * Math.sin(progress * Math.PI)
      : 0;
    hammer.rotation.z = -pulse * 0.75;
  }
  function quiet() {
    sort(0, false);
    pneumatic(0, false);
    chime(0, false);
    transferLetter.visible = false;
    transferBook.visible = false;
    lampWipe.visible = false;
  }
  return {
    state,
    sort,
    carryLetter,
    depositLetter,
    carryBook,
    depositBook,
    pneumatic,
    chime,
    quiet,
    lampWipe,
  };
}
