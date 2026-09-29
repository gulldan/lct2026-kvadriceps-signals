import {
  Bone,
  Box3,
  BufferAttribute,
  CanvasTexture,
  CircleGeometry,
  Group,
  InstancedMesh,
  Matrix4,
  Mesh,
  MeshStandardMaterial,
  PlaneGeometry,
  RepeatWrapping,
  Skeleton,
  SkinnedMesh,
  SRGBColorSpace,
  TextureLoader,
  Vector3,
  type Texture,
  type BufferGeometry,
  type Material,
  type Object3D,
} from "three";
import { GLTFLoader } from "three/addons/loaders/GLTFLoader.js";
import { MeshoptDecoder } from "three/addons/libs/meshopt_decoder.module.js";
import type { LibrarySlot } from "./geometry";
import { libraryActors } from "./actors";
import archUrl from "./art/arch.glb?url";
import bookcaseUrl from "./art/bookcase.glb?url";
import chairUrl from "./art/chair.glb?url";
import chandelierUrl from "./art/chandelier.glb?url";
import globeUrl from "./art/globe.glb?url";
import lampUrl from "./art/lamp.glb?url";
import ladderUrl from "./art/ladder.glb?url";
import plantUrl from "./art/plant.glb?url";
import tableUrl from "./art/table.glb?url";
import catalogUrl from "./art/catalog.glb?url";
import clockUrl from "./art/clock.glb?url";
import lecternUrl from "./art/lectern.glb?url";
import telescopeUrl from "./art/telescope.glb?url";
import wallUrl from "./art/wall.glb?url";
import pressUrl from "./art/press.glb?url";
import roundUrl from "./art/round.glb?url";
import catUrl from "./art/cat.glb?url";
import clerkUrl from "./art/clerk.glb?url";
import readerUrl from "./art/reader.glb?url";
import plinthUrl from "./art/plinth.webp?url";
import balustradeUrl from "./art/balustrade.glb?url";
import deskUrl from "./art/desk.glb?url";
import sideUrl from "./art/side.glb?url";
import { ROOM_DEPTH, ROOM_WIDTH } from "./layout";
import marbleUrl from "./art/marble.webp?url";
import mosaicUrl from "./art/mosaic.webp?url";
import parquetUrl from "./art/parquet.webp?url";
import rugUrl from "./art/rug.webp?url";
import runnerUrl from "./art/runner.webp?url";
import terracottaUrl from "./art/terracotta.webp?url";

// Сколько единиц зала покрывает один повтор текстуры пола; UV пола заданы с шагом 3,2.
// Оттенок приглушает сгенерированную текстуру до общей гаммы зала.
const FLOORS: Record<
  string,
  { url: string; span: number; relief: number; roughness: number; tint: [number, number, number] }
> = {
  parquet: { url: parquetUrl, span: 3.4, relief: 0.25, roughness: 0.7, tint: [1, 1, 1] },
  stone: { url: marbleUrl, span: 4.6, relief: 2.4, roughness: 0.28, tint: [1, 1, 1] },
  workshop: { url: parquetUrl, span: 2.6, relief: 0.25, roughness: 0.6, tint: [1.3, 1.15, 1] },
  clay: { url: terracottaUrl, span: 3.2, relief: 3, roughness: 0.75, tint: [0.8, 0.68, 0.62] },
};

type Fit = "stretch" | "height" | "width";
/**
 * Модели сгенерированы по концептам и сжаты (WebP, meshopt). Генератор ставит «лицо» модели
 * на +X; поворот приводит его к лицу заглушки, способ подгонки задаёт, можно ли тянуть пропорции.
 */
const KINDS: Record<string, { url: string; yaw: number; fit: Fit }> = {
  stack: { url: bookcaseUrl, yaw: -Math.PI / 2, fit: "stretch" },
  bookcase: { url: bookcaseUrl, yaw: -Math.PI / 2, fit: "stretch" },
  arch: { url: archUrl, yaw: Math.PI / 2, fit: "stretch" },
  chandelier: { url: chandelierUrl, yaw: 0, fit: "width" },
  table: { url: tableUrl, yaw: 0, fit: "stretch" },
  chair: { url: chairUrl, yaw: Math.PI / 2, fit: "stretch" },
  lamp: { url: lampUrl, yaw: 0, fit: "height" },
  plant: { url: plantUrl, yaw: 0, fit: "height" },
  globe: { url: globeUrl, yaw: 0, fit: "height" },
  ladder: { url: ladderUrl, yaw: Math.PI / 2, fit: "stretch" },
  catalog: { url: catalogUrl, yaw: -Math.PI / 2, fit: "stretch" },
  clock: { url: clockUrl, yaw: -Math.PI / 2, fit: "stretch" },
  lectern: { url: lecternUrl, yaw: -Math.PI / 2, fit: "stretch" },
  telescope: { url: telescopeUrl, yaw: Math.PI / 4, fit: "height" },
  wall: { url: wallUrl, yaw: -Math.PI / 2, fit: "stretch" },
  press: { url: pressUrl, yaw: -Math.PI / 2, fit: "height" },
  round: { url: roundUrl, yaw: 0, fit: "stretch" },
  desk: { url: deskUrl, yaw: 0, fit: "stretch" },
  side: { url: sideUrl, yaw: 0, fit: "stretch" },
  balustrade: { url: balustradeUrl, yaw: -Math.PI / 2, fit: "stretch" },
};
// Длинная стена-срез делится на пролёты балюстрады примерно по три единицы.
const RAIL = 3;

/**
 * Модули стены идут вдоль задней и левой стен пролётами между окнами, круглым окном
 * и картой: эти проёмы остаются процедурными и продолжают светиться.
 */
function wallPlacements() {
  const back = -ROOM_DEPTH / 2 + 0.25;
  const left = -ROOM_WIDTH / 2 + 0.25;
  const depth = 0.3;
  const height = 5.55;
  const result: { box: Box3; yaw: number }[] = [];
  const spans = (
    edges: readonly (readonly [number, number])[],
    place: (from: number, to: number) => Box3,
    yaw: number,
  ) => {
    for (const [from, to] of edges) {
      const count = Math.max(1, Math.round((to - from) / 5));
      const width = (to - from) / count;
      for (let i = 0; i < count; i++)
        result.push({ box: place(from + i * width, from + (i + 1) * width), yaw });
    }
  };
  spans(
    [
      [-ROOM_WIDTH / 2 + 0.25, -8.95],
      [-6.25, 4.2],
      [5.85, ROOM_WIDTH / 2],
    ],
    (from, to) => new Box3(new Vector3(from, 0, back), new Vector3(to, height, back + depth)),
    -Math.PI / 2,
  );
  spans(
    [
      [-ROOM_DEPTH / 2 + 0.25, -15.35],
      [-13.25, -7.85],
      [-5.75, -1.55],
      [0.55, 10.25],
      [12.35, ROOM_DEPTH / 2],
    ],
    (from, to) => new Box3(new Vector3(left, 0, from), new Vector3(left + depth, height, to)),
    0,
  );
  return result;
}
// Имя корневой группы персонажа указывает, какая модель ляжет на его скелет.
const CHARACTERS: Record<string, string> = {
  "library-clerk": clerkUrl,
  "library-reader": readerUrl,
  "reading-room-cat": catUrl,
};
// Двусторонний стеллаж делится на отсеки той же ширины, что и процедурная заглушка.
const BAY = 1.8;

interface Part {
  geometry: BufferGeometry;
  material: Material;
  matrix: Matrix4;
}
interface Model {
  parts: Part[];
  box: Box3;
}

async function load(url: string, loader: GLTFLoader): Promise<Model> {
  const { scene } = await loader.loadAsync(url);
  scene.updateMatrixWorld(true);
  const parts: Part[] = [];
  const box = new Box3();
  scene.traverse((object) => {
    if (!(object instanceof Mesh)) return;
    object.geometry.computeBoundingBox();
    box.union(object.geometry.boundingBox!.clone().applyMatrix4(object.matrixWorld));
    parts.push({
      geometry: object.geometry,
      material: object.material,
      matrix: object.matrixWorld,
    });
  });
  return { parts, box };
}

/** Матрица, которая ставит модель в рамку заглушки: поворот, масштаб и опора на пол. */
function fitMatrix(model: Model, yaw: number, target: Box3, fit: Fit) {
  const turn = new Matrix4().makeRotationY(yaw);
  const box = model.box.clone().applyMatrix4(turn);
  const size = box.getSize(new Vector3());
  const goal = target.getSize(new Vector3());
  const scale =
    fit === "stretch"
      ? new Vector3(goal.x / size.x, goal.y / size.y, goal.z / size.z)
      : new Vector3().setScalar(
          fit === "height" ? goal.y / size.y : Math.max(goal.x, goal.z) / Math.max(size.x, size.z),
        );
  const center = box.getCenter(new Vector3());
  const anchor = target.getCenter(new Vector3());
  // Люстра висит от потолочного крепления, остальное стоит на полу.
  const top = fit === "width";
  return new Matrix4()
    .makeTranslation(anchor.x, top ? target.max.y : target.min.y, anchor.z)
    .multiply(new Matrix4().makeScale(scale.x, scale.y, scale.z))
    .multiply(new Matrix4().makeTranslation(-center.x, -(top ? box.max.y : box.min.y), -center.z))
    .multiply(turn);
}

/** Отсеки стеллажа: по модели шкафа на каждую сторону, спинками друг к другу. */
function stackPlacements(slot: LibrarySlot, yaw: number) {
  const { box } = slot;
  const count = Math.max(1, Math.round((box.max.x - box.min.x) / BAY));
  const width = (box.max.x - box.min.x) / count;
  const result: { matrix: Matrix4; box: Box3; yaw: number }[] = [];
  for (let bay = 0; bay < count; bay++) {
    const left = box.min.x + bay * width;
    for (const side of [-1, 1]) {
      const cell = new Box3(
        new Vector3(left, box.min.y, side > 0 ? 0 : box.min.z),
        new Vector3(left + width, box.max.y, side > 0 ? box.max.z : 0),
      );
      result.push({ matrix: slot.matrix, box: cell, yaw: yaw + (side > 0 ? 0 : Math.PI) });
    }
  }
  return result;
}

function railPlacements(slot: LibrarySlot, yaw: number) {
  const { box } = slot;
  const count = Math.max(1, Math.round((box.max.x - box.min.x) / RAIL));
  const width = (box.max.x - box.min.x) / count;
  return Array.from({ length: count }, (_, index) => ({
    matrix: slot.matrix,
    box: new Box3(
      new Vector3(box.min.x + index * width, 0, box.min.z),
      new Vector3(box.min.x + (index + 1) * width, box.max.y, box.max.z),
    ),
    yaw,
  }));
}

/**
 * Верхушки свечей — самые высокие точки на внешнем кольце люстры. По ним встают ореолы,
 * потому что у модели другое число рожков, чем у заглушки.
 */
function candleTips(model: Model) {
  const radius = Math.max(model.box.max.x - model.box.min.x, model.box.max.z - model.box.min.z) / 2;
  const center = model.box.getCenter(new Vector3());
  const sectors = new Map<number, Vector3>();
  const point = new Vector3();
  for (const { geometry, matrix } of model.parts) {
    const position = geometry.getAttribute("position");
    for (let i = 0; i < position.count; i++) {
      point.fromBufferAttribute(position, i).applyMatrix4(matrix);
      const dx = point.x - center.x;
      const dz = point.z - center.z;
      if (Math.hypot(dx, dz) < radius * 0.4) continue;
      const sector = Math.round(((Math.atan2(dz, dx) + Math.PI) / (Math.PI * 2)) * 16) % 16;
      const best = sectors.get(sector);
      if (!best || point.y > best.y) sectors.set(sector, point.clone());
    }
  }
  const tips = [...sectors.values()].toSorted((a, b) => b.y - a.y);
  const high = tips[0]?.y ?? 0;
  const unique: Vector3[] = [];
  for (const tip of tips) {
    if (tip.y < high - (model.box.max.y - model.box.min.y) * 0.25) continue;
    if (unique.some((item) => Math.hypot(item.x - tip.x, item.z - tip.z) < radius * 0.25)) continue;
    unique.push(tip);
  }
  return unique;
}

export interface LibraryModelHooks {
  emitters: () => { kind: string; position: Vector3 }[];
  setEmitters: (next: { kind: string; position: Vector3 }[]) => void;
}
/**
 * Карта нормалей из яркости: швы и прожилки темнее поверхности, поэтому низкое солнце
 * проявляет их рельефом без отдельной карты высот.
 */
function relief(image: CanvasImageSource & { width: number; height: number }, strength: number) {
  const size = 512;
  const canvas = document.createElement("canvas");
  canvas.width = canvas.height = size;
  const context = canvas.getContext("2d", { willReadFrequently: true })!;
  context.drawImage(image, 0, 0, size, size);
  const source = context.getImageData(0, 0, size, size);
  const height = new Float32Array(size * size);
  for (let i = 0; i < height.length; i++)
    height[i] =
      (source.data[i * 4]! * 0.3 +
        source.data[i * 4 + 1]! * 0.59 +
        source.data[i * 4 + 2]! * 0.11) /
      255;
  const at = (x: number, y: number) => height[((y + size) % size) * size + ((x + size) % size)]!;
  const target = context.createImageData(size, size);
  for (let y = 0; y < size; y++) {
    for (let x = 0; x < size; x++) {
      const dx = (at(x + 1, y) - at(x - 1, y)) * strength;
      const dy = (at(x, y + 1) - at(x, y - 1)) * strength;
      const length = Math.hypot(dx, dy, 1);
      const offset = (y * size + x) * 4;
      target.data[offset] = (-dx / length) * 127.5 + 127.5;
      target.data[offset + 1] = (dy / length) * 127.5 + 127.5;
      target.data[offset + 2] = (1 / length) * 127.5 + 127.5;
      target.data[offset + 3] = 255;
    }
  }
  context.putImageData(target, 0, 0);
  return new CanvasTexture(canvas);
}

function tiled<T extends Texture>(texture: T, repeat: number) {
  texture.wrapS = texture.wrapT = RepeatWrapping;
  texture.repeat.set(repeat, repeat);
  texture.anisotropy = 8;
  return texture;
}

/** meshopt хранит координаты целыми числами; перенос в другое пространство требует float. */
function unpacked(source: BufferGeometry) {
  const geometry = source.clone();
  for (const name of ["position", "normal"]) {
    const attribute = geometry.getAttribute(name);
    if (!attribute) continue;
    const values = new Float32Array(attribute.count * 3);
    for (let i = 0; i < attribute.count; i++)
      values.set([attribute.getX(i), attribute.getY(i), attribute.getZ(i)], i * 3);
    geometry.setAttribute(name, new BufferAttribute(values, 3));
  }
  return geometry;
}

const smooth = (from: number, to: number, value: number) => {
  const t = Math.min(1, Math.max(0, (value - from) / (to - from)));
  return t * t * (3 - 2 * t);
};

interface Rig {
  body: Group;
  head: Group;
  limbs?: Group[];
  feet?: Group[];
  tail?: Group;
  keep?: Object3D[];
}

/**
 * Персонаж получает скелет из тех же групп, что двигают процедурную фигуру: кость — дочерний
 * узел группы тела, головы, руки или лапы. Веса вершин считаются по их месту в позе покоя,
 * поэтому все позы и жесты работают без изменений.
 */
function attachRig(root: Object3D, rig: Rig, model: Model) {
  const parts = [
    rig.body,
    rig.head,
    ...(rig.limbs ?? []),
    ...(rig.feet ?? []),
    ...(rig.tail ? [rig.tail] : []),
  ];
  const saved = parts.map((part) => ({
    position: part.position.clone(),
    rotation: part.rotation.clone(),
    scale: part.scale.clone(),
  }));
  parts.forEach((part) => {
    part.position.copy(part.userData.rest as Vector3);
    part.rotation.set(0, 0, 0);
    part.scale.set(1, 1, 1);
  });
  const keep = new Set<Object3D>();
  for (const held of rig.keep ?? []) held.traverse((item) => keep.add(item));
  const box = new Box3();
  root.updateMatrixWorld(true);
  const inverse = root.matrixWorld.clone().invert();
  root.traverse((item) => {
    if (!(item instanceof Mesh) || keep.has(item)) return;
    item.geometry.computeBoundingBox();
    box.union(
      item.geometry.boundingBox!.clone().applyMatrix4(inverse.clone().multiply(item.matrixWorld)),
    );
    item.visible = false;
  });
  const fitted = fitMatrix(model, -Math.PI / 2, box, "height");
  const bones = parts.map((part) => {
    const bone = new Bone();
    part.add(bone);
    return bone;
  });
  const skeleton = new Skeleton(bones);
  const weigh = rig.tail ? animalWeights(rig) : figureWeights(rig);
  for (const part of model.parts) {
    const geometry = unpacked(part.geometry).applyMatrix4(fitted.clone().multiply(part.matrix));
    const position = geometry.getAttribute("position");
    const indices = new Uint16Array(position.count * 4);
    const weights = new Float32Array(position.count * 4);
    const point = new Vector3();
    for (let i = 0; i < position.count; i++) {
      const [index, weight] = weigh(point.fromBufferAttribute(position, i));
      indices.set(index, i * 4);
      weights.set(weight, i * 4);
    }
    geometry.setAttribute("skinIndex", new BufferAttribute(indices, 4));
    geometry.setAttribute("skinWeight", new BufferAttribute(weights, 4));
    const mesh = new SkinnedMesh(geometry, part.material);
    mesh.castShadow = mesh.receiveShadow = true;
    mesh.frustumCulled = false;
    root.add(mesh);
    root.updateMatrixWorld(true);
    // Без явной матрицы привязки three пересчитывает обратные матрицы костей в позе покоя.
    mesh.bind(skeleton);
  }
  parts.forEach((part, index) => {
    part.position.copy(saved[index]!.position);
    part.rotation.copy(saved[index]!.rotation);
    part.scale.copy(saved[index]!.scale);
  });
}

type Weigh = (point: Vector3) => [number[], number[]];

const rest = (part: Group) => part.userData.rest as Vector3;

/** Прямоходящий: голова выше шеи, руки по бокам ниже плеча, ступни у пола. */
function figureWeights(rig: Rig): Weigh {
  const neck = rest(rig.head).y - 0.17;
  const shoulder = rest(rig.limbs![0]!).y + 0.1;
  const torso = Math.abs(rest(rig.limbs![0]!).x) - 0.06;
  const ankle = rest(rig.feet![0]!).y + 0.02;
  return ({ x, y }) => {
    const head = smooth(neck - 0.05, neck + 0.05, y);
    const limb =
      (1 - head) *
      smooth(torso - 0.04, torso + 0.05, Math.abs(x)) *
      smooth(shoulder - 0.62, shoulder - 0.5, y) *
      (1 - smooth(shoulder, shoulder + 0.08, y));
    const foot = (1 - limb) * (1 - smooth(ankle - 0.08, ankle + 0.04, y));
    const body = Math.max(0, 1 - head - limb - foot);
    const side = x < 0 ? 0 : 1;
    return [
      [0, 1, 2 + side, 4 + side],
      [body, head, limb, foot],
    ];
  };
}

/**
 * Кот лежит вдоль оси Z, голова впереди и выше спины. Хвост модели обвит вокруг лап,
 * поэтому движется вместе с телом, а не с костью хвоста.
 */
function animalWeights(rig: Rig): Weigh {
  const head = (rig.head.userData.rest as Vector3).z - 0.14;
  const chin = (rig.head.userData.rest as Vector3).y - 0.16;
  return ({ y, z }) => {
    const front = smooth(head - 0.06, head + 0.06, z) * smooth(chin - 0.05, chin + 0.05, y);
    return [
      [0, 1, 0, 0],
      [1 - front, front, 0, 0],
    ];
  };
}

/**
 * Заменяет процедурные заглушки готовыми моделями. Слитые заглушки уступают место
 * инстансам, а живые объекты (стул архивариуса, отзывчивое растение, лестница) получают
 * модель дочерним узлом и продолжают двигаться по своей логике.
 */
export async function libraryModels(scene: Object3D, hooks: LibraryModelHooks) {
  const loader = new GLTFLoader().setMeshoptDecoder(MeshoptDecoder);
  const urls = [
    ...new Set([...Object.values(KINDS).map(({ url }) => url), ...Object.values(CHARACTERS)]),
  ];
  const loaded = new Map(
    await Promise.all(urls.map(async (url) => [url, await load(url, loader)] as const)),
  );
  const model = (kind: string) => loaded.get(KINDS[kind]!.url)!;
  const placements = new Map<string, Matrix4[]>();
  const place = (kind: string, matrix: Matrix4) =>
    placements.set(kind, [...(placements.get(kind) ?? []), matrix]);
  const chandeliers: Matrix4[] = [];
  const live: Object3D[] = [];
  scene.traverse((object) => {
    const slots = object.userData.slots as LibrarySlot[] | undefined;
    for (const slot of slots ?? []) {
      const config = KINDS[slot.kind];
      if (!config || slot.kind === "wall") continue;
      const cells =
        slot.kind === "stack"
          ? stackPlacements(slot, config.yaw)
          : slot.kind === "balustrade"
            ? railPlacements(slot, config.yaw)
            : [{ matrix: slot.matrix, box: slot.box, yaw: config.yaw }];
      for (const cell of cells) {
        const matrix = cell.matrix
          .clone()
          .multiply(fitMatrix(model(slot.kind), cell.yaw, cell.box, config.fit));
        place(slot.kind === "stack" ? "bookcase" : slot.kind, matrix);
        if (slot.kind === "chandelier") chandeliers.push(matrix);
      }
    }
    const standins = object.userData.standins as Record<string, Group> | undefined;
    for (const [kind, standin] of Object.entries(standins ?? {}))
      if (KINDS[kind] || kind === "rug" || kind === "medallion") standin.visible = false;
    if (object.userData.slot && KINDS[object.userData.slot as string]) live.push(object);
  });

  for (const { box, yaw } of wallPlacements())
    place("wall", fitMatrix(model("wall"), yaw, box, "stretch"));
  // Модель часов со своим циферблатом и маятником заменяет процедурные стрелки.
  for (const name of ["library-pendulum", "library-clock-hands"]) {
    const part = scene.getObjectByName(name);
    if (part) part.visible = false;
  }

  const root = new Group();
  root.name = "library-models";
  for (const [kind, matrices] of placements) {
    for (const part of model(kind).parts) {
      const mesh = new InstancedMesh(part.geometry, part.material, matrices.length);
      matrices.forEach((matrix, index) =>
        mesh.setMatrixAt(index, matrix.clone().multiply(part.matrix)),
      );
      mesh.castShadow = mesh.receiveShadow = true;
      mesh.computeBoundingSphere();
      root.add(mesh);
    }
  }
  scene.add(root);

  for (const object of live) {
    const kind = object.userData.slot as string;
    object.updateMatrixWorld(true);
    const inverse = object.matrixWorld.clone().invert();
    const box = new Box3();
    object.traverse((part) => {
      if (!(part instanceof Mesh)) return;
      part.geometry.computeBoundingBox();
      box.union(
        part.geometry.boundingBox!.clone().applyMatrix4(inverse.clone().multiply(part.matrixWorld)),
      );
      part.visible = false;
    });
    const fitted = fitMatrix(model(kind), KINDS[kind]!.yaw, box, KINDS[kind]!.fit);
    for (const part of model(kind).parts) {
      const mesh = new Mesh(part.geometry, part.material);
      mesh.matrixAutoUpdate = false;
      mesh.matrix.copy(fitted).multiply(part.matrix);
      mesh.castShadow = mesh.receiveShadow = true;
      object.add(mesh);
    }
  }

  const actors = await libraryActors(scene, loader);
  scene.traverse((object) => {
    const rig = object.userData.rig as Rig | undefined;
    const url = rig && CHARACTERS[object.name];
    if (url && !actors.cast.has(object.name)) attachRig(object, rig, loaded.get(url)!);
  });

  const textures = new TextureLoader();
  const images = new Map(
    await Promise.all(
      [...Object.values(FLOORS).map(({ url }) => url), rugUrl, runnerUrl, mosaicUrl, plinthUrl].map(
        async (url) => {
          const texture = await textures.loadAsync(url);
          texture.colorSpace = SRGBColorSpace;
          return [url, texture] as const;
        },
      ),
    ),
  );
  const owned: Texture[] = [...images.values()];
  scene.traverse((object) => {
    if (!(object instanceof Mesh)) return;
    const material = object.material as MeshStandardMaterial;
    if (material.userData.plinth && !material.userData.painted) {
      // Грань цоколя — один длинный бокс; повтор по длине держит блоки кладки квадратными.
      material.userData.painted = true;
      const map = images.get(plinthUrl)!;
      map.wrapS = map.wrapT = RepeatWrapping;
      map.repeat.set(28, 1);
      map.anisotropy = 8;
      material.map = map;
      material.normalMap = relief(map.image as HTMLImageElement, 2.5);
      material.normalMap.wrapS = material.normalMap.wrapT = RepeatWrapping;
      material.normalMap.repeat.set(28, 1);
      owned.push(material.normalMap);
      material.color.setScalar(1);
      material.needsUpdate = true;
      return;
    }
    const floor = FLOORS[material.userData.floor as string];
    if (!floor || material.userData.painted) return;
    material.userData.painted = true;
    const repeat = 3.2 / floor.span;
    material.map?.dispose();
    material.map = tiled(images.get(floor.url)!, repeat);
    material.normalMap = tiled(
      relief(material.map.image as HTMLImageElement, floor.relief),
      repeat,
    );
    material.roughness = floor.roughness;
    material.color.setRGB(...floor.tint);
    owned.push(material.normalMap!);
    material.needsUpdate = true;
  });

  // Ковры: узор ложится на плоскость по габаритам заглушки; дорожка повторяет узор вдоль.
  const woven = (url: string) =>
    new MeshStandardMaterial({ map: tiled(images.get(url)!, 1), roughness: 0.95 });
  const persian = woven(rugUrl);
  const runner = woven(runnerUrl);
  scene.traverse((object) => {
    for (const slot of (object.userData.slots as LibrarySlot[] | undefined) ?? []) {
      if (slot.kind !== "rug") continue;
      const size = slot.box.getSize(new Vector3());
      const center = slot.box.getCenter(new Vector3());
      const long = Math.max(size.x, size.z);
      const short = Math.min(size.x, size.z);
      const isRunner = long / short > 2.5;
      const plane = new PlaneGeometry(size.x, size.z).rotateX(-Math.PI / 2);
      const uv = plane.getAttribute("uv");
      const across = size.x < size.z;
      for (let i = 0; i < uv.count; i++) {
        const u = uv.getX(i);
        const v = uv.getY(i);
        const [along, side] = across ? [v, u] : [u, v];
        uv.setXY(i, isRunner ? along * Math.round(long / (short * 1.5)) : along, side);
      }
      const mesh = new Mesh(plane, isRunner ? runner : persian);
      mesh.position.set(center.x, slot.box.max.y + 0.004, center.z);
      mesh.applyMatrix4(slot.matrix);
      mesh.receiveShadow = true;
      root.add(mesh);
    }
  });
  const mosaic = new Mesh(
    new CircleGeometry(2.3, 72).rotateX(-Math.PI / 2),
    new MeshStandardMaterial({ map: images.get(mosaicUrl)!, roughness: 0.32, metalness: 0.1 }),
  );
  mosaic.position.set(0.25, 0.07, 14.2);
  mosaic.receiveShadow = true;
  root.add(mosaic);

  // Свечи заглушек убираются, их место занимают вершины свечей модели.
  const tips = candleTips(model("chandelier"));
  const centers = chandeliers.map((matrix) => new Vector3().setFromMatrixPosition(matrix));
  hooks.setEmitters([
    ...hooks
      .emitters()
      .filter(
        ({ kind, position }) =>
          kind !== "flame" ||
          !centers.some((center) => Math.hypot(center.x - position.x, center.z - position.z) < 1.6),
      ),
    ...chandeliers.flatMap((matrix) =>
      tips.map((tip) => ({
        kind: "flame",
        position: tip
          .clone()
          .applyMatrix4(matrix)
          .add(new Vector3(0, -0.12, 0)),
      })),
    ),
  ]);
  return {
    root,
    advance: actors.advance,
    dispose() {
      actors.dispose();
      owned.forEach((texture) => texture.dispose());
      root.traverse((object) => {
        if (object instanceof InstancedMesh) object.dispose();
        else if (object instanceof Mesh) {
          object.geometry.dispose();
          (object.material as MeshStandardMaterial).dispose();
        }
      });
      for (const { parts } of loaded.values())
        for (const { geometry, material } of parts) {
          geometry.dispose();
          for (const value of Object.values(material))
            if (value && typeof value === "object" && "isTexture" in value)
              (value as { dispose: () => void }).dispose();
          material.dispose();
        }
    },
  };
}
