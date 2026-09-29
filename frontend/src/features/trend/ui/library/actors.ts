import {
  AnimationMixer,
  Box3,
  Group,
  LoopRepeat,
  Mesh,
  Quaternion,
  SkinnedMesh,
  Vector3,
  type AnimationAction,
  type AnimationClip,
  type Bone,
  type Object3D,
} from "three";
import type { GLTFLoader } from "three/addons/loaders/GLTFLoader.js";
import archivistUrl from "./art/archivist-rig.glb?url";
import binderUrl from "./art/binder-rig.glb?url";

/**
 * Какой клип играет в какой позе. Позы задаёт прежняя логика сцены (`userData.pose`
 * у жителей, результат `motion.advance` у архивариуса), поэтому маршруты и эпизоды
 * не меняются: клипы только заменяют жёсткое вращение частей тела.
 */
interface Role {
  url: string;
  poses: Record<string, string>;
  /** Клип для всех поз, которых нет в `poses`. */
  idle: string;
  /** Позы за столом: модель опускается на длину ног, стол закрывает их от камеры. */
  seated?: string[];
}
const CAST: Record<string, Role> = {
  // Сова собрана из A-позы с руками-крыльями: ходьба и лестница из библиотеки движений,
  // стойка — кадр ходьбы. Клипы сидения и подбора ломали лицо и остались неиспользованными.
  "library-archivist": {
    url: archivistUrl,
    poses: {
      walk: "walk",
      deliver: "walk",
      chair: "walk",
      climb: "climb",
      descend: "climb",
      reach: "climb",
    },
    idle: "stand",
    seated: ["sit", "open", "settle"],
  },
  // Клерк и читатель после автоматического риггинга теряли кисти и ноги: у них процедурные позы.
  "library-binder": { url: binderUrl, poses: { walk: "walk" }, idle: "idle" },
};
// Движущиеся позы: голова смотрит по ходу, в остальных персонаж оглядывается и читает.
const MOVING = new Set(["walk", "climb"]);
const up = new Vector3(0, 1, 0);
const axis = new Vector3();
const turn = new Quaternion();
const parent = new Quaternion();

interface Cast {
  owner: Object3D;
  role: Role;
  mixer: AnimationMixer;
  actions: Map<string, AnimationAction>;
  wrapper: Group;
  base: number;
  drop: number;
  head?: Bone;
  current?: AnimationAction;
}

function play(member: Cast, pose: string) {
  const { poses, idle } = member.role;
  const next = member.actions.get(poses[pose] ?? idle) ?? member.actions.get(idle);
  if (!next || next === member.current) return;
  next.reset().play();
  if (member.current) next.crossFadeFrom(member.current, 0.35, true);
  member.current = next;
}

/** Габариты процедурной фигуры в её собственных осях: по ним встаёт модель. */
function bounds(owner: Object3D, keep: Set<Object3D>) {
  const box = new Box3();
  owner.updateMatrixWorld(true);
  const inverse = owner.matrixWorld.clone().invert();
  owner.traverse((item) => {
    if (!(item instanceof Mesh) || keep.has(item)) return;
    item.geometry.computeBoundingBox();
    box.union(
      item.geometry.boundingBox!.clone().applyMatrix4(inverse.clone().multiply(item.matrixWorld)),
    );
  });
  return box;
}

/**
 * Персонажи со скелетом и клипами из библиотеки движений. Модель встаёт вместо процедурной
 * фигуры, а её группы продолжают двигаться невидимыми: к ним привязаны книги, письма и свет.
 */
export async function libraryActors(scene: Object3D, loader: GLTFLoader) {
  const owners = new Map<string, Object3D>();
  scene.traverse((object) => {
    if (CAST[object.name]) owners.set(object.name, object);
  });
  const cast: Cast[] = [];
  await Promise.all(
    [...owners].map(async ([name, owner]) => {
      const config = CAST[name]!;
      const gltf = await loader.loadAsync(config.url);
      const rig = owner.userData.rig as { keep?: Object3D[] } | undefined;
      const keep = new Set<Object3D>();
      for (const held of rig?.keep ?? []) held.traverse((item) => keep.add(item));
      const target = bounds(owner, keep);
      const model = gltf.scene;
      model.updateMatrixWorld(true);
      const own = new Box3().setFromObject(model);
      const size = own.getSize(new Vector3());
      const scale = target.getSize(new Vector3()).y / size.y;
      const wrapper = new Group();
      wrapper.name = `${name}-model`;
      wrapper.scale.setScalar(scale);
      const center = own.getCenter(new Vector3());
      model.position.set(-center.x, -own.min.y, -center.z);
      wrapper.position.set(
        (target.min.x + target.max.x) / 2,
        target.min.y,
        (target.min.z + target.max.z) / 2,
      );
      wrapper.add(model);
      owner.traverse((item) => {
        if (item instanceof Mesh && !keep.has(item)) item.visible = false;
      });
      owner.add(wrapper);
      let head: Bone | undefined;
      model.traverse((item) => {
        if (item instanceof SkinnedMesh) {
          item.castShadow = item.receiveShadow = true;
          item.frustumCulled = false;
        }
        if ((item as Bone).isBone && item.name === "Head") head = item as Bone;
      });
      // Клипы играют на месте: вперёд персонажа ведёт маршрут сцены, у корня остаётся покачивание.
      for (const clip of gltf.animations as AnimationClip[])
        for (const track of clip.tracks) {
          if (!track.name.endsWith("Hips.position")) continue;
          const [x, , z] = track.values;
          for (let i = 0; i < track.values.length; i += 3) {
            track.values[i] = x!;
            track.values[i + 2] = z!;
          }
        }
      const mixer = new AnimationMixer(model);
      const actions = new Map<string, AnimationAction>();
      for (const clip of gltf.animations as AnimationClip[])
        actions.set(clip.name, mixer.clipAction(clip).setLoop(LoopRepeat, Infinity));
      const height = target.max.y - target.min.y;
      cast.push({
        owner,
        role: config,
        mixer,
        actions,
        wrapper,
        base: wrapper.position.y,
        drop: height * 0.4,
        head,
      });
    }),
  );

  return {
    cast: new Set(cast.map(({ owner }) => owner.name)),
    /** `pose` архивариуса приходит из движения, у жителей — из их `userData.pose`. */
    advance(dt: number, archivistPose: string, time: number) {
      for (const member of cast) {
        const pose =
          member.owner.name === "library-archivist"
            ? archivistPose
            : String(member.owner.userData.pose ?? "idle");
        play(member, pose);
        member.mixer.update(dt);
        const seated = member.role.seated?.includes(pose) ?? false;
        const goal = member.base - (seated ? member.drop : 0);
        member.wrapper.position.y += (goal - member.wrapper.position.y) * Math.min(1, dt * 5);
        const clip = member.role.poses[pose];
        if (!member.head?.parent || (clip && MOVING.has(clip))) continue;
        // Поверх неподвижной стойки голова медленно оглядывается и склоняется к книге.
        const reading = ["read", "write", "open", "settle", "prepare"].includes(pose);
        member.head.parent.updateWorldMatrix(true, false);
        member.head.parent.getWorldQuaternion(parent).invert();
        const look = Math.sin(time * 0.55) * (reading ? 0.12 : 0.35);
        member.head.quaternion.premultiply(
          turn.setFromAxisAngle(axis.copy(up).applyQuaternion(parent), look),
        );
        const right = axis.set(1, 0, 0).applyQuaternion(member.owner.getWorldQuaternion(turn));
        const nod = reading ? 0.32 + Math.sin(time * 1.3) * 0.04 : Math.sin(time * 0.8) * 0.03;
        member.head.quaternion.premultiply(
          turn.setFromAxisAngle(right.applyQuaternion(parent), nod),
        );
      }
    },
    dispose() {
      for (const { mixer } of cast) mixer.stopAllAction();
    },
  };
}
