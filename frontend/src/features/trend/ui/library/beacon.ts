import {
  AdditiveBlending,
  BufferAttribute,
  BufferGeometry,
  CanvasTexture,
  Color,
  DoubleSide,
  DynamicDrawUsage,
  Group,
  Mesh,
  MeshBasicMaterial,
  PlaneGeometry,
  Points,
  PointsMaterial,
  SRGBColorSpace,
} from "three";

/** Руна рисуется один раз: кольца, деления и знаки по кругу, как у метки цели в игре. */
function runeTexture(color: Color) {
  const canvas = document.createElement("canvas");
  canvas.width = canvas.height = 256;
  const context = canvas.getContext("2d")!;
  const style = color.getStyle();
  context.translate(128, 128);
  context.strokeStyle = context.fillStyle = style;
  context.shadowColor = style;
  context.shadowBlur = 10;
  for (const [radius, width] of [
    [118, 3],
    [104, 1.5],
    [62, 2],
  ] as const) {
    context.lineWidth = width;
    context.beginPath();
    context.arc(0, 0, radius, 0, Math.PI * 2);
    context.stroke();
  }
  for (let tick = 0; tick < 48; tick++) {
    const angle = (tick / 48) * Math.PI * 2;
    const inner = tick % 4 ? 106 : 96;
    context.lineWidth = tick % 4 ? 1 : 2;
    context.beginPath();
    context.moveTo(Math.cos(angle) * inner, Math.sin(angle) * inner);
    context.lineTo(Math.cos(angle) * 116, Math.sin(angle) * 116);
    context.stroke();
  }
  for (let glyph = 0; glyph < 8; glyph++) {
    context.save();
    context.rotate((glyph / 8) * Math.PI * 2);
    context.translate(0, -83);
    context.lineWidth = 2;
    context.beginPath();
    context.moveTo(-7, 6);
    context.lineTo(0, -8);
    context.lineTo(7, 6);
    if (glyph % 2) context.moveTo(-5, 0);
    if (glyph % 2) context.lineTo(5, 0);
    context.stroke();
    context.restore();
  }
  const core = context.createRadialGradient(0, 0, 0, 0, 0, 60);
  core.addColorStop(0, style);
  core.addColorStop(1, "transparent");
  context.globalAlpha = 0.35;
  context.fillStyle = core;
  context.fillRect(-60, -60, 120, 120);
  const texture = new CanvasTexture(canvas);
  texture.colorSpace = SRGBColorSpace;
  return texture;
}

/**
 * Метка цели у полки: светящаяся руна на полу медленно вращается и дышит, над ней
 * поднимаются искры. Видна издалека и показывает, куда идёт архивариус.
 */
export function libraryBeacon(color: Color) {
  const texture = runeTexture(color);
  const plane = new PlaneGeometry(1.7, 1.7).rotateX(-Math.PI / 2);
  const glow = new MeshBasicMaterial({
    map: texture,
    color: color.clone().multiplyScalar(2.2),
    transparent: true,
    depthWrite: false,
    blending: AdditiveBlending,
    side: DoubleSide,
  });
  const count = 14;
  const sparkPositions = new Float32Array(count * 3);
  const sparkGeometry = new BufferGeometry();
  sparkGeometry.setAttribute(
    "position",
    new BufferAttribute(sparkPositions, 3).setUsage(DynamicDrawUsage),
  );
  const sparkMaterial = new PointsMaterial({
    color: color.clone().multiplyScalar(2.5),
    size: 0.07,
    transparent: true,
    depthWrite: false,
    blending: AdditiveBlending,
  });
  const runes: Mesh[] = [];
  let time = 0;
  return {
    attach(parent: Group) {
      const root = new Group();
      root.userData.glow = true;
      parent.add(root);
      const rune = new Mesh(plane, glow);
      rune.position.set(0, 0.02, 0.55);
      rune.userData.glow = true;
      const sparks = new Points(sparkGeometry, sparkMaterial);
      sparks.position.copy(rune.position);
      sparks.userData.glow = true;
      sparks.frustumCulled = false;
      root.add(rune, sparks);
      runes.push(rune);
    },
    advance(dt: number) {
      time += dt;
      for (let index = 0; index < count; index++) {
        const seed = index * 7.13;
        const life = (time * 0.35 + index / count) % 1;
        const angle = seed + life * 1.5;
        const radius = 0.25 + ((index * 0.37) % 0.5);
        sparkPositions[index * 3] = Math.cos(angle) * radius;
        sparkPositions[index * 3 + 1] = life * 2.1;
        sparkPositions[index * 3 + 2] = Math.sin(angle) * radius;
      }
      sparkGeometry.attributes.position!.needsUpdate = true;
      sparkMaterial.opacity = 0.9;
      const pulse = 0.75 + Math.sin(time * 2.4) * 0.25;
      glow.opacity = pulse;
      for (const rune of runes) rune.rotation.y = time * 0.35;
    },
    dispose() {
      texture.dispose();
      plane.dispose();
      glow.dispose();
      sparkGeometry.dispose();
      sparkMaterial.dispose();
    },
  };
}
