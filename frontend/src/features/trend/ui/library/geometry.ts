import {
  Box3,
  BoxGeometry,
  CylinderGeometry,
  Group,
  Matrix4,
  Mesh,
  SphereGeometry,
  TorusGeometry,
  Vector3,
  type BufferGeometry,
  type Material,
  type Object3D,
} from "three";
import { mergeGeometries } from "three/addons/utils/BufferGeometryUtils.js";

const UP = new Vector3(0, 1, 0);

const createMesh = (
  parent: Object3D,
  geometry: BufferGeometry,
  material: Material,
  x: number,
  y: number,
  z: number,
) => {
  const object = new Mesh(geometry, material);
  object.position.set(x, y, z);
  object.castShadow = true;
  object.receiveShadow = true;
  parent.add(object);
  return object;
};

/** Геометрия разделяется внутри одной сцены и освобождается вместе с ней. */
export function libraryGeometry() {
  const cube = new BoxGeometry(1, 1, 1);
  const sphere = new SphereGeometry(1, 20, 14);
  const bead = new SphereGeometry(1, 10, 8);
  const cylinder = new CylinderGeometry(1, 1, 1, 16);
  const wire = new CylinderGeometry(1, 1, 1, 8);
  const ring = new TorusGeometry(1, 0.065, 6, 32);
  const eyelet = new TorusGeometry(1, 0.065, 4, 16);
  const box = (
    parent: Object3D,
    material: Material,
    x: number,
    y: number,
    z: number,
    w: number,
    h: number,
    d: number,
  ) => {
    const object = createMesh(parent, cube, material, x, y, z);
    object.scale.set(w, h, d);
    return object;
  };
  const ball = (
    parent: Object3D,
    material: Material,
    x: number,
    y: number,
    z: number,
    w: number,
    h = w,
    d = w,
  ) => {
    // У мелкой резьбы тот же силуэт, но она не требует геометрии крупного персонажа.
    const object = createMesh(parent, Math.max(w, h, d) < 0.25 ? bead : sphere, material, x, y, z);
    object.scale.set(w, h, d);
    return object;
  };
  const rod = (
    parent: Object3D,
    material: Material,
    from: [number, number, number],
    to: [number, number, number],
    radius: number,
  ) => {
    const start = new Vector3(...from);
    const end = new Vector3(...to);
    const direction = end.sub(start);
    const object = createMesh(parent, radius < 0.06 ? wire : cylinder, material, 0, 0, 0);
    object.position.copy(start.addScaledVector(direction, 0.5));
    object.quaternion.setFromUnitVectors(UP, direction.clone().normalize());
    object.scale.set(radius, direction.length(), radius);
    return object;
  };
  const hoop = (
    parent: Object3D,
    material: Material,
    x: number,
    y: number,
    z: number,
    radius: number,
  ) => {
    const object = createMesh(parent, radius < 0.25 ? eyelet : ring, material, x, y, z);
    object.scale.setScalar(radius);
    return object;
  };
  return {
    box,
    ball,
    rod,
    hoop,
    dispose: () =>
      [cube, sphere, bead, cylinder, wire, ring, eyelet].forEach((geometry) => geometry.dispose()),
  };
}

export type LibraryGeometry = ReturnType<typeof libraryGeometry>;

export interface LibrarySlot {
  kind: string;
  /** Положение заглушки в мире; модель ставится в её локальную рамку. */
  matrix: Matrix4;
  /** Габариты заглушки в её собственных осях. */
  box: Box3;
}

function mergeByMaterial(batches: Map<Material, BufferGeometry[]>, target: Group) {
  for (const [material, geometries] of batches) {
    const merged = mergeGeometries(geometries);
    geometries.forEach((geometry) => geometry.dispose());
    if (!merged) continue;
    const mesh = new Mesh(merged, material);
    mesh.castShadow = true;
    mesh.receiveShadow = true;
    target.add(mesh);
  }
}

/**
 * Сотни книг и деталей превращаются в один вызов отрисовки на материал.
 * Источники света запоминают положение до слияния: по ним сцена рисует ореолы и пятна света.
 * Объекты с `userData.slot` сливаются отдельно: это заглушки, которые заменят готовые модели.
 */
export function batchArchitecture(root: Group) {
  root.updateMatrixWorld(true);
  const batches = new Map<string, Map<Material, BufferGeometry[]>>();
  const emitters: { kind: string; position: Vector3 }[] = [];
  const slots: LibrarySlot[] = [];
  // Вложенный слот уходит вместе с внешним: подсвечник на шкафу исчезает вместе со шкафом.
  const outermost = (object: Object3D) => {
    let found: Object3D | undefined;
    for (let item: Object3D | null = object; item && item !== root; item = item.parent)
      if (item.userData.slot) found = item;
    return found;
  };
  root.traverse((object) => {
    if (object.userData.emitter)
      emitters.push({
        kind: object.userData.emitter,
        position: object.getWorldPosition(new Vector3()),
      });
    const slot = outermost(object);
    if (slot === object) {
      const inverse = object.matrixWorld.clone().invert();
      const box = new Box3();
      object.traverse((part) => {
        if (!(part instanceof Mesh)) return;
        part.geometry.computeBoundingBox();
        box.union(
          part.geometry
            .boundingBox!.clone()
            .applyMatrix4(inverse.clone().multiply(part.matrixWorld)),
        );
      });
      slots.push({ kind: object.userData.slot, matrix: object.matrixWorld.clone(), box });
    }
    if (!(object instanceof Mesh) || Array.isArray(object.material)) return;
    const key = (slot?.userData.slot as string | undefined) ?? "";
    const group = batches.get(key) ?? new Map<Material, BufferGeometry[]>();
    const geometries = group.get(object.material) ?? [];
    const geometry = object.geometry.index
      ? object.geometry.toNonIndexed()
      : object.geometry.clone();
    geometries.push(geometry.applyMatrix4(object.matrixWorld));
    group.set(object.material, geometries);
    batches.set(key, group);
  });
  const result = new Group();
  result.userData.emitters = emitters;
  result.userData.slots = slots;
  const standins: Record<string, Group> = {};
  for (const [key, group] of batches) {
    if (!key) {
      mergeByMaterial(group, result);
      continue;
    }
    const standin = new Group();
    standin.name = `standin-${key}`;
    mergeByMaterial(group, standin);
    result.add(standin);
    standins[key] = standin;
  }
  result.userData.standins = standins;
  return result;
}
