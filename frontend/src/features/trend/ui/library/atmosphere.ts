import {
  AdditiveBlending,
  BufferAttribute,
  BufferGeometry,
  Color,
  DoubleSide,
  DynamicDrawUsage,
  Group,
  InstancedBufferAttribute,
  InstancedMesh,
  Matrix4,
  Mesh,
  PlaneGeometry,
  Points,
  ShaderMaterial,
  Vector3,
  type Object3D,
} from "three";
import type { libraryLight } from "./materials";
import { libraryRandom } from "./random";

type Emitter = { kind: string; position: Vector3 };
// Огонь свечи мерцает; лампа и тёплые окошки в цоколе светят ровно и слабее.
const HALO: Record<string, { size: number; strength: number }> = {
  lamp: { size: 1.4, strength: 2 },
  ember: { size: 0.45, strength: 1.4 },
};
type Window = { x: number; z: number; rotation: number };

// Прозрачный холст: яркость свечения становится его непрозрачностью, иначе квадрат ореола
// над страницей стал бы тёмным.
const OUTPUT = `gl_FragColor.a = min(1.0, max(gl_FragColor.r, max(gl_FragColor.g, gl_FragColor.b)));
#include <tonemapping_fragment>
#include <colorspace_fragment>`;
const additive = (vertexShader: string, fragmentShader: string, uniforms = {}) =>
  new ShaderMaterial({
    uniforms: { time: { value: 0 }, ...uniforms },
    vertexShader,
    fragmentShader,
    transparent: true,
    depthWrite: false,
    blending: AdditiveBlending,
    premultipliedAlpha: true,
    side: DoubleSide,
  });

// Ореол всегда смотрит в ортографическую камеру и чуть выдвинут к ней, чтобы не резаться о стену.
const HALO_VERTEX = `attribute vec3 instanceColor;
varying vec2 vUv;
varying vec3 vColor;
void main() {
  vUv = uv - 0.5;
  vColor = instanceColor;
  vec4 center = modelViewMatrix * instanceMatrix * vec4(0.0, 0.0, 0.0, 1.0);
  center.z += 0.6;
  center.xy += position.xy * length(instanceMatrix[0].xyz);
  gl_Position = projectionMatrix * center;
}`;
const HALO_FRAGMENT = `varying vec2 vUv;
varying vec3 vColor;
void main() {
  float d = length(vUv) * 2.0;
  float glow = exp(-d * d * 16.0) + exp(-d * d * 3.2) * 0.28;
  gl_FragColor = vec4(vColor * glow * smoothstep(1.0, 0.6, d), 1.0);
  ${OUTPUT}
}`;
const POOL_VERTEX = `attribute vec3 instanceColor;
varying vec2 vUv;
varying vec3 vColor;
void main() {
  vUv = uv - 0.5;
  vColor = instanceColor;
  gl_Position = projectionMatrix * modelViewMatrix * instanceMatrix * vec4(position, 1.0);
}`;
const POOL_FRAGMENT = `varying vec2 vUv;
varying vec3 vColor;
void main() {
  float d = length(vUv) * 2.0;
  gl_FragColor = vec4(vColor * pow(max(0.0, 1.0 - d), 2.2), 1.0);
  ${OUTPUT}
}`;
// u идёт поперёк луча, v — от окна к полу; мягкие края и медленные полосы дают объём.
const BEAM_VERTEX = `varying vec2 vUv;
void main() {
  vUv = uv;
  gl_Position = projectionMatrix * modelViewMatrix * vec4(position, 1.0);
}`;
const BEAM_FRAGMENT = `uniform float time;
uniform vec3 color;
varying vec2 vUv;
void main() {
  float edge = smoothstep(0.0, 0.22, vUv.x) * smoothstep(1.0, 0.78, vUv.x);
  float fade = smoothstep(0.0, 0.08, vUv.y) * mix(1.0, 0.25, vUv.y);
  float streak = 0.72 + 0.28 * sin(vUv.x * 19.0 + time * 0.23) * sin(vUv.x * 7.0 - time * 0.11);
  gl_FragColor = vec4(color * edge * fade * streak, 1.0);
  ${OUTPUT}
}`;
// Пятно на полу повторяет арку окна с переплётом.
const PATCH_FRAGMENT = `uniform float time;
uniform vec3 color;
varying vec2 vUv;
void main() {
  vec2 p = vec2((vUv.x - 0.5) * 1.66, vUv.y * 3.37);
  float arch = p.y < 2.54 ? 1.0 - smoothstep(0.73, 0.83, abs(p.x)) : 1.0 - smoothstep(0.73, 0.83, length(p - vec2(0.0, 2.54)));
  float bars = smoothstep(0.02, 0.05, abs(p.x)) * smoothstep(0.02, 0.05, abs(abs(p.x) - 0.4));
  float rails = smoothstep(0.02, 0.05, abs(p.y - 0.83)) * smoothstep(0.02, 0.05, abs(p.y - 1.7));
  float soft = smoothstep(0.0, 0.1, vUv.y);
  gl_FragColor = vec4(color * arch * bars * rails * soft * (0.94 + 0.06 * sin(time * 0.4)), 1.0);
  ${OUTPUT}
}`;
const DUST_VERTEX = `uniform float time;
uniform float scale;
uniform float size;
uniform float blink;
attribute float phase;
varying float vAlpha;
void main() {
  vAlpha = pow(0.5 + 0.5 * sin(time * (0.6 + fract(phase * 7.1)) + phase * 6.28), blink);
  vec4 view = modelViewMatrix * vec4(position, 1.0);
  gl_PointSize = max(1.5, scale * size * (0.6 + fract(phase * 3.7) * 0.8));
  gl_Position = projectionMatrix * view;
}`;
const DUST_FRAGMENT = `uniform vec3 color;
varying float vAlpha;
void main() {
  float d = length(gl_PointCoord - 0.5) * 2.0;
  gl_FragColor = vec4(color * vAlpha * smoothstep(1.0, 0.1, d), 1.0);
  ${OUTPUT}
}`;

/**
 * Воздух зала: вечерние лучи из окон с пылью, ореолы огня и тёплые пятна от ламп.
 * Всё рисуется аддитивно и не участвует в тенях, затенении и навигации.
 */
export function libraryAtmosphere(
  scene: Object3D,
  sun: Vector3,
  light: ReturnType<typeof libraryLight>,
) {
  const root = new Group();
  root.name = "library-atmosphere";
  const random = libraryRandom(7);
  const emitters: Emitter[] = [];
  const windows: Window[] = [];
  scene.updateMatrixWorld(true);
  scene.traverse((object) => {
    emitters.push(...((object.userData.emitters as Emitter[] | undefined) ?? []));
    windows.push(...((object.userData.windows as Window[] | undefined) ?? []));
    if (object.name === "candle-flame")
      emitters.push({ kind: "flame", position: object.getWorldPosition(new Vector3()) });
  });
  const ember = light.ember;
  const matrix = new Matrix4();

  const haloMaterial = additive(HALO_VERTEX, HALO_FRAGMENT);
  const haloPlane = new PlaneGeometry(1, 1);
  let glowing: Emitter[] = [];
  let phases: number[] = [];
  let haloColors = new InstancedBufferAttribute(new Float32Array(0), 3);
  let halos = new InstancedMesh(haloPlane, haloMaterial, 0);
  // Список огней меняется, когда модели заменяют заглушки: свечи люстр стоят в другом месте.
  function setEmitters(next: Emitter[]) {
    root.remove(halos);
    halos.dispose();
    glowing = next.filter(({ kind }) => kind !== "chandelier");
    phases = glowing.map(() => random() * 6.28);
    halos = new InstancedMesh(haloPlane.clone(), haloMaterial, glowing.length);
    halos.userData.glow = true;
    haloColors = new InstancedBufferAttribute(new Float32Array(glowing.length * 3), 3);
    haloColors.setUsage(DynamicDrawUsage);
    halos.geometry.setAttribute("instanceColor", haloColors);
    glowing.forEach(({ kind, position }, index) => {
      const size = HALO[kind]?.size ?? 0.72;
      halos.setMatrixAt(
        index,
        matrix.makeScale(size, size, size).setPosition(position.x, position.y + 0.05, position.z),
      );
    });
    root.add(halos);
  }
  setEmitters(emitters);

  const poolSpots = emitters.flatMap(({ kind, position }) =>
    kind === "chandelier"
      ? [{ at: new Vector3(position.x, 0.085, position.z), size: 7, strength: 0.5 }]
      : kind === "lamp"
        ? [{ at: new Vector3(position.x, position.y - 0.62, position.z), size: 2.2, strength: 0.5 }]
        : [],
  );
  const poolPlane = new PlaneGeometry(1, 1).rotateX(-Math.PI / 2);
  const pools = new InstancedMesh(
    poolPlane,
    additive(POOL_VERTEX, POOL_FRAGMENT),
    poolSpots.length,
  );
  const poolColors = new InstancedBufferAttribute(new Float32Array(poolSpots.length * 3), 3);
  pools.geometry.setAttribute("instanceColor", poolColors);
  poolSpots.forEach(({ at, size, strength }, index) => {
    pools.setMatrixAt(index, matrix.makeScale(size, 1, size).setPosition(at));
    new Color()
      .copy(ember)
      .multiplyScalar(strength)
      .toArray(poolColors.array, index * 3);
  });
  root.add(pools);

  // Лучи идут от проёма окна по направлению солнца до пола.
  const ray = sun.clone().normalize().negate();
  const beamColor = {
    value: ember
      .clone()
      .lerp(new Color(1, 1, 1), 0.35)
      .multiplyScalar(0.34),
  };
  const patchColor = {
    value: ember
      .clone()
      .lerp(new Color(1, 1, 1), 0.45)
      .multiplyScalar(0.55),
  };
  const beamMaterial = additive(BEAM_VERTEX, BEAM_FRAGMENT, { color: beamColor });
  const patchMaterial = additive(BEAM_VERTEX, PATCH_FRAGMENT, { color: patchColor });
  const beamVolumes: { origin: Vector3; across: Vector3; up: Vector3 }[] = [];
  const toFloor = (point: Vector3) => point.clone().addScaledVector(ray, (point.y - 0.07) / -ray.y);
  for (const { x, z, rotation } of windows) {
    const across = new Vector3(Math.cos(rotation), 0, -Math.sin(rotation));
    const inward = new Vector3(Math.sin(rotation), 0, Math.cos(rotation));
    // Свет входит только через окна, обращённые к солнцу.
    if (inward.dot(ray) <= 0) continue;
    const origin = new Vector3(x, 0.7, z)
      .addScaledVector(inward, 0.1)
      .addScaledVector(across, -0.83);
    const up = new Vector3(0, 3.37, 0);
    const width = across.clone().multiplyScalar(1.66);
    const corners = [origin, origin.clone().add(width), origin.clone().add(width).add(up)];
    corners.push(origin.clone().add(up));
    const floor = corners.map(toFloor);
    const beam = new BufferGeometry();
    const positions: number[] = [];
    const uvs: number[] = [];
    // Четыре грани призмы; у каждой u поперёк, v вдоль луча.
    for (let side = 0; side < 4; side++) {
      const a = corners[side]!;
      const b = corners[(side + 1) % 4]!;
      const fa = floor[side]!;
      const fb = floor[(side + 1) % 4]!;
      positions.push(...a.toArray(), ...b.toArray(), ...fb.toArray());
      positions.push(...a.toArray(), ...fb.toArray(), ...fa.toArray());
      uvs.push(0, 0, 1, 0, 1, 1, 0, 0, 1, 1, 0, 1);
    }
    beam.setAttribute("position", new BufferAttribute(new Float32Array(positions), 3));
    beam.setAttribute("uv", new BufferAttribute(new Float32Array(uvs), 2));
    const patch = new BufferGeometry();
    patch.setAttribute(
      "position",
      new BufferAttribute(
        new Float32Array([
          ...floor[0]!.toArray(),
          ...floor[1]!.toArray(),
          ...floor[2]!.toArray(),
          ...floor[3]!.toArray(),
        ]),
        3,
      ),
    );
    patch.setAttribute("uv", new BufferAttribute(new Float32Array([0, 0, 1, 0, 1, 1, 0, 1]), 2));
    patch.setIndex([0, 1, 2, 0, 2, 3]);
    root.add(new Mesh(beam, beamMaterial), new Mesh(patch, patchMaterial));
    beamVolumes.push({ origin, across: width, up });
  }

  // Пылинки живут внутри лучей: всплывают, дрейфуют и возвращаются к окну.
  const motesPerBeam = 70;
  const count = beamVolumes.length * motesPerBeam;
  const dust = new BufferGeometry();
  const dustPositions = new Float32Array(count * 3);
  const phase = new Float32Array(count);
  const motes = Array.from({ length: count }, (_, index) => {
    phase[index] = random();
    return {
      beam: beamVolumes[Math.floor(index / motesPerBeam)]!,
      u: random(),
      v: random(),
      t: random(),
      drift: 0.012 + random() * 0.02,
      sway: random() * 6.28,
    };
  });
  dust.setAttribute("position", new BufferAttribute(dustPositions, 3).setUsage(DynamicDrawUsage));
  dust.setAttribute("phase", new BufferAttribute(phase, 1));
  const dustMaterial = additive(DUST_VERTEX, DUST_FRAGMENT, {
    scale: { value: 40 },
    size: { value: 0.05 },
    blink: { value: 1 },
    color: {
      value: ember
        .clone()
        .lerp(new Color(1, 1, 1), 0.5)
        .multiplyScalar(1.6),
    },
  });
  const points = new Points(dust, dustMaterial);
  points.frustumCulled = false;
  root.add(points);
  const point = new Vector3();

  // Светлячки в оранжерее восточного крыла заметны только при приближении.
  const fireflyCount = 11;
  const fireflyPositions = new Float32Array(fireflyCount * 3);
  const fireflyPhase = Float32Array.from({ length: fireflyCount }, () => random());
  const fireflyGeometry = new BufferGeometry();
  fireflyGeometry.setAttribute(
    "position",
    new BufferAttribute(fireflyPositions, 3).setUsage(DynamicDrawUsage),
  );
  fireflyGeometry.setAttribute("phase", new BufferAttribute(fireflyPhase, 1));
  const fireflyMaterial = additive(DUST_VERTEX, DUST_FRAGMENT, {
    scale: dustMaterial.uniforms.scale,
    size: { value: 0.13 },
    blink: { value: 5 },
    color: {
      value: light.ember
        .clone()
        .lerp(new Color(0.75, 1, 0.35), 0.6)
        .multiplyScalar(4),
    },
  });
  const fireflies = new Points(fireflyGeometry, fireflyMaterial);
  fireflies.frustumCulled = false;
  root.add(fireflies);

  // Пыль висит во всём зале, а не только в лучах: воздух заметен у ламп и в тени.
  const roomCount = 900;
  const roomPositions = new Float32Array(roomCount * 3);
  const roomPhase = Float32Array.from({ length: roomCount }, () => random());
  const roomSeeds = Array.from({ length: roomCount }, () => [
    (random() - 0.5) * 44,
    0.3 + random() * 4.6,
    (random() - 0.5) * 35,
  ]);
  const roomGeometry = new BufferGeometry();
  roomGeometry.setAttribute(
    "position",
    new BufferAttribute(roomPositions, 3).setUsage(DynamicDrawUsage),
  );
  roomGeometry.setAttribute("phase", new BufferAttribute(roomPhase, 1));
  const roomMaterial = additive(DUST_VERTEX, DUST_FRAGMENT, {
    scale: dustMaterial.uniforms.scale,
    size: { value: 0.035 },
    blink: { value: 1.5 },
    color: {
      value: ember
        .clone()
        .lerp(new Color(1, 1, 1), 0.4)
        .multiplyScalar(0.55),
    },
  });
  const roomDust = new Points(roomGeometry, roomMaterial);
  roomDust.frustumCulled = false;
  root.add(roomDust);

  // Искры над свечами: редкие, поднимаются и гаснут, у каждой свечи свой ритм.
  const emberCount = 90;
  const emberPositions = new Float32Array(emberCount * 3);
  const emberPhase = Float32Array.from({ length: emberCount }, () => random());
  const emberGeometry = new BufferGeometry();
  emberGeometry.setAttribute(
    "position",
    new BufferAttribute(emberPositions, 3).setUsage(DynamicDrawUsage),
  );
  emberGeometry.setAttribute("phase", new BufferAttribute(emberPhase, 1));
  const emberMaterial = additive(DUST_VERTEX, DUST_FRAGMENT, {
    scale: dustMaterial.uniforms.scale,
    size: { value: 0.045 },
    blink: { value: 3 },
    color: { value: ember.clone().multiplyScalar(3.2) },
  });
  const embers = new Points(emberGeometry, emberMaterial);
  embers.frustumCulled = false;
  root.add(embers);

  root.traverse((object) => {
    object.userData.glow = true;
    object.castShadow = false;
    object.receiveShadow = false;
  });
  let time = 0;
  function advance(dt: number) {
    time += dt;
    for (const material of [
      beamMaterial,
      patchMaterial,
      dustMaterial,
      fireflyMaterial,
      roomMaterial,
      emberMaterial,
    ])
      material.uniforms.time!.value = time;
    glowing.forEach(({ kind }, index) => {
      const flicker =
        kind !== "flame"
          ? 1
          : 0.86 +
            Math.sin(time * 7.3 + phases[index]!) * 0.07 +
            Math.sin(time * 13.1 + phases[index]! * 2.3) * 0.05;
      const strength = (HALO[kind]?.strength ?? 2.7) * flicker;
      haloColors.setXYZ(index, ember.r * strength, ember.g * strength, ember.b * strength);
    });
    haloColors.needsUpdate = true;
    motes.forEach((mote, index) => {
      mote.t = (mote.t + dt * mote.drift) % 1;
      const wobble = Math.sin(time * 0.5 + mote.sway) * 0.03;
      point
        .copy(mote.beam.origin)
        .addScaledVector(mote.beam.across, Math.min(0.95, Math.max(0.05, mote.u + wobble)))
        .addScaledVector(mote.beam.up, mote.v);
      point.addScaledVector(ray, ((point.y - 0.07) / -ray.y) * mote.t);
      point.y += Math.sin(time * 0.3 + mote.sway) * 0.05;
      point.toArray(dustPositions, index * 3);
    });
    dust.attributes.position!.needsUpdate = true;
    fireflyPhase.forEach((seed, index) => {
      const t = time * (0.18 + seed * 0.12) + seed * 40;
      fireflyPositions[index * 3] = 20.2 + Math.sin(t * 1.3) * 1.1;
      fireflyPositions[index * 3 + 1] = 1.7 + Math.sin(t * 2.1 + seed * 9) * 0.75;
      fireflyPositions[index * 3 + 2] = 2.5 + Math.cos(t * 0.9 + seed * 5) * 0.95;
    });
    fireflyGeometry.attributes.position!.needsUpdate = true;
    roomSeeds.forEach(([x, y, z], index) => {
      const seed = roomPhase[index]! * 40;
      roomPositions[index * 3] = x! + Math.sin(time * 0.07 + seed) * 0.6;
      roomPositions[index * 3 + 1] = y! + Math.sin(time * 0.11 + seed * 1.3) * 0.35;
      roomPositions[index * 3 + 2] = z! + Math.cos(time * 0.06 + seed * 0.7) * 0.6;
    });
    roomGeometry.attributes.position!.needsUpdate = true;
    const flames = glowing.filter(({ kind }) => kind === "flame");
    for (let index = 0; index < emberCount && flames.length; index++) {
      const source = flames[(index * 7) % flames.length]!.position;
      const life = (time * (0.28 + emberPhase[index]! * 0.2) + emberPhase[index]! * 9) % 1;
      const drift = emberPhase[index]! * 12;
      emberPositions[index * 3] = source.x + Math.sin(life * 5 + drift) * 0.08 * life;
      emberPositions[index * 3 + 1] = source.y + 0.12 + life * 0.9;
      emberPositions[index * 3 + 2] = source.z + Math.cos(life * 4 + drift) * 0.08 * life;
    }
    emberGeometry.attributes.position!.needsUpdate = true;
  }
  advance(0);
  return {
    root,
    advance,
    emitters: () => [...glowing],
    setEmitters,
    /** Размер пылинок задаётся в пикселях и следует за приближением камеры. */
    setScale(pixelsPerUnit: number) {
      dustMaterial.uniforms.scale!.value = pixelsPerUnit;
    },
    dispose() {
      haloPlane.dispose();
      root.traverse((object) => {
        if (object instanceof Mesh || object instanceof Points) {
          object.geometry.dispose();
          (object.material as ShaderMaterial).dispose();
        }
      });
    },
  };
}
