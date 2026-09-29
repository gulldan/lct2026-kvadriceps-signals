import {
  CanvasTexture,
  Color,
  Mesh,
  MeshStandardMaterial,
  PlaneGeometry,
  RepeatWrapping,
  SRGBColorSpace,
  type Group,
} from "three";
import type { LibraryMaterials } from "./materials";
import { ROOM_DEPTH, ROOM_WIDTH } from "./layout";

const SIZE = 1024;
// Одна текстура покрывает 3,2 единицы зала: доска остаётся резкой при четырёхкратном приближении.
const REPEAT = 3.2;

/** Детерминированный шум от номера детали: узор одинаков на каждом повторе текстуры. */
const hash = (a: number, b: number) => {
  const value = Math.sin(a * 127.1 + b * 311.7) * 43758.5453;
  return value - Math.floor(value);
};
const shade = (color: Color, amount: number) =>
  color
    .clone()
    .multiplyScalar(1 + amount)
    .getStyle();

function texture(draw: (context: CanvasRenderingContext2D) => void) {
  const canvas = document.createElement("canvas");
  canvas.width = canvas.height = SIZE;
  draw(canvas.getContext("2d")!);
  const result = new CanvasTexture(canvas);
  result.colorSpace = SRGBColorSpace;
  result.wrapS = result.wrapT = RepeatWrapping;
  result.anisotropy = 8;
  return result;
}

function plank(
  context: CanvasRenderingContext2D,
  x: number,
  y: number,
  width: number,
  height: number,
  tone: Color,
  seam: Color,
  seed: number,
) {
  const along = width > height;
  context.fillStyle = shade(tone, (hash(seed, 1) - 0.5) * 0.34);
  context.fillRect(x, y, width, height);
  // Волокна идут вдоль доски; редкие сучки не дают рисунку стать обоями.
  context.strokeStyle = shade(tone, -0.28);
  context.globalAlpha = 0.28;
  context.lineWidth = 1;
  for (let line = 0; line < 7; line++) {
    const offset = hash(seed, line + 3) * (along ? height : width);
    const wave = (hash(seed, line + 9) - 0.5) * 6;
    context.beginPath();
    if (along) {
      context.moveTo(x, y + offset);
      context.bezierCurveTo(
        x + width / 3,
        y + offset + wave,
        x + width / 1.5,
        y + offset - wave,
        x + width,
        y + offset,
      );
    } else {
      context.moveTo(x + offset, y);
      context.bezierCurveTo(
        x + offset + wave,
        y + height / 3,
        x + offset - wave,
        y + height / 1.5,
        x + offset,
        y + height,
      );
    }
    context.stroke();
  }
  context.globalAlpha = 1;
  // Светлая кромка сверху и тень снизу читаются как фаска лакированной доски.
  const bevel = context.createLinearGradient(x, y, along ? x : x + width, along ? y + height : y);
  bevel.addColorStop(0, new Color(1, 1, 1).getStyle());
  bevel.addColorStop(0.5, tone.getStyle());
  bevel.addColorStop(1, new Color(0, 0, 0).getStyle());
  context.globalAlpha = 0.1;
  context.fillStyle = bevel;
  context.fillRect(x, y, width, height);
  context.globalAlpha = 1;
  context.strokeStyle = seam.getStyle();
  context.lineWidth = 2;
  context.strokeRect(x + 1, y + 1, width - 2, height - 2);
}

/**
 * Ёлочка 4:1: полоса из горизонтальной и вертикальной доски покрывает восемь диагоналей
 * клеток, соседняя полоса сдвинута на восемь клеток. Период 16 клеток кратен сдвигу.
 */
function herringbone(tone: Color, seam: Color) {
  return texture((context) => {
    const cells = 16;
    const cell = SIZE / cells;
    const ratio = 4;
    const wrap = (value: number) => ((value % cells) + cells) % cells;
    for (let strip = -3 * cells; strip <= 3 * cells; strip += 2 * ratio) {
      for (let k = -cells; k <= 2 * cells; k++) {
        const seed = wrap(strip + k) * 31 + wrap(k);
        plank(context, (strip + k) * cell, k * cell, cell * ratio, cell, tone, seam, seed);
        plank(
          context,
          (strip + k) * cell,
          (k + 1) * cell,
          cell,
          cell * ratio,
          tone,
          seam,
          seed + 997,
        );
      }
    }
  });
}

function boards(tone: Color, seam: Color) {
  return texture((context) => {
    const rows = 8;
    const height = SIZE / rows;
    for (let row = 0; row < rows; row++) {
      const offset = ((row * 3) % 4) * (SIZE / 4);
      for (let piece = -2; piece < 2; piece++)
        plank(
          context,
          offset + piece * (SIZE / 2),
          row * height,
          SIZE / 2,
          height,
          tone,
          seam,
          row * 7 + ((piece + 2) % 2),
        );
    }
  });
}

function marble(light: Color, dark: Color, seam: Color, inlay: Color) {
  return texture((context) => {
    const slab = SIZE / 2;
    for (let row = 0; row < 2; row++) {
      for (let column = 0; column < 2; column++) {
        const x = column * slab;
        const y = row * slab;
        const base = (row + column) % 2 ? dark : light;
        context.save();
        context.beginPath();
        context.rect(x, y, slab, slab);
        context.clip();
        context.fillStyle = shade(base, (hash(row, column) - 0.5) * 0.08);
        context.fillRect(x, y, slab, slab);
        // Прожилки ветвятся внутри плиты и не пересекают шов, поэтому повтор незаметен.
        for (let vein = 0; vein < 9; vein++) {
          const seed = row * 17 + column * 5 + vein;
          context.strokeStyle = shade(base, hash(seed, 2) > 0.3 ? -0.32 : 0.25);
          context.globalAlpha = 0.18 + hash(seed, 3) * 0.3;
          context.lineWidth = 0.8 + hash(seed, 4) * 2.4;
          context.beginPath();
          let px = x + hash(seed, 5) * slab;
          let py = y;
          context.moveTo(px, py);
          for (let step = 0; step < 6; step++) {
            const nx = px + (hash(seed, step + 6) - 0.35) * slab * 0.45;
            const ny = py + slab / 5;
            context.quadraticCurveTo(
              px + (hash(seed, step + 20) - 0.5) * 60,
              (py + ny) / 2,
              nx,
              ny,
            );
            px = nx;
            py = ny;
          }
          context.stroke();
        }
        context.globalAlpha = 1;
        context.restore();
        context.strokeStyle = seam.getStyle();
        context.lineWidth = 3;
        context.strokeRect(x + 1.5, y + 1.5, slab - 3, slab - 3);
      }
    }
    for (let row = 0; row <= 2; row++) {
      for (let column = 0; column <= 2; column++) {
        context.save();
        context.translate(column * slab, row * slab);
        context.rotate(Math.PI / 4);
        context.fillStyle = seam.getStyle();
        context.fillRect(-34, -34, 68, 68);
        context.fillStyle = inlay.getStyle();
        context.fillRect(-26, -26, 52, 52);
        context.restore();
      }
    }
  });
}

function terracotta(tone: Color, seam: Color, inlay: Color) {
  return texture((context) => {
    const tiles = 4;
    const tile = SIZE / tiles;
    context.fillStyle = seam.getStyle();
    context.fillRect(0, 0, SIZE, SIZE);
    for (let row = 0; row < tiles; row++) {
      for (let column = 0; column < tiles; column++) {
        const x = column * tile;
        const y = row * tile;
        const glaze = context.createRadialGradient(
          x + tile * 0.4,
          y + tile * 0.35,
          4,
          x + tile / 2,
          y + tile / 2,
          tile * 0.75,
        );
        glaze.addColorStop(0, shade(tone, 0.12 + (hash(row, column) - 0.5) * 0.2));
        glaze.addColorStop(1, shade(tone, -0.14 + (hash(column, row) - 0.5) * 0.2));
        context.fillStyle = glaze;
        context.fillRect(x + 5, y + 5, tile - 10, tile - 10);
      }
    }
    for (let row = 0; row <= tiles; row++) {
      for (let column = 0; column <= tiles; column++) {
        context.save();
        context.translate(column * tile, row * tile);
        context.rotate(Math.PI / 4);
        context.fillStyle = inlay.getStyle();
        context.fillRect(-22, -22, 44, 44);
        context.restore();
      }
    }
  });
}

const surface = (map: CanvasTexture, roughness: number) =>
  new MeshStandardMaterial({ map, roughness, metalness: 0 });

/**
 * Пол каждой зоны — одна плоскость с мировыми координатами текстуры: швов между плитами
 * нет, а дорогая сетка из тысяч плиток больше не нужна.
 */
export function libraryFloors(room: Group, m: LibraryMaterials) {
  const walnut = m.wood.color;
  const seam = walnut.clone().multiplyScalar(0.55);
  const parquet = surface(herringbone(m.oak.color.clone().lerp(walnut, 0.34), seam), 0.38);
  const workshop = surface(boards(m.oak.color.clone().lerp(m.paper.color, 0.08), seam), 0.52);
  const stone = surface(
    marble(
      m.paper.color.clone().lerp(m.oak.color, 0.28).multiplyScalar(0.84),
      m.oak.color.clone().lerp(m.sage.color, 0.4).lerp(m.paper.color, 0.15),
      seam,
      m.brass.color,
    ),
    0.24,
  );
  const clay = surface(
    terracotta(
      m.claret.color.clone().lerp(m.oak.color, 0.45),
      seam,
      m.sage.color.clone().lerp(walnut, 0.25),
    ),
    0.72,
  );
  // Пометка позволяет догруженным текстурам заменить процедурный рисунок без перестройки пола.
  parquet.userData.floor = "parquet";
  stone.userData.floor = "stone";
  clay.userData.floor = "clay";
  workshop.userData.floor = "workshop";
  const halfWidth = ROOM_WIDTH / 2;
  const halfDepth = ROOM_DEPTH / 2;
  const zones = [
    [clay, -halfWidth, -16.6, -halfDepth, halfDepth],
    [stone, 16.6, halfWidth, -halfDepth, halfDepth],
    [stone, -16.6, -5.4, -halfDepth, -3.5],
    [workshop, 6.1, 16.6, -halfDepth, -1.9],
    [parquet, -5.4, 6.1, -halfDepth, halfDepth],
    [parquet, -16.6, -5.4, -3.5, halfDepth],
    [parquet, 6.1, 16.6, -1.9, halfDepth],
  ] as const;
  for (const [material, left, right, north, south] of zones) {
    const plane = new PlaneGeometry(right - left, south - north).rotateX(-Math.PI / 2);
    plane.translate((left + right) / 2, 0.048, (north + south) / 2);
    const position = plane.getAttribute("position");
    const uv = plane.getAttribute("uv");
    for (let i = 0; i < position.count; i++)
      uv.setXY(i, position.getX(i) / REPEAT, -position.getZ(i) / REPEAT);
    const floor = new Mesh(plane, material);
    floor.receiveShadow = true;
    room.add(floor);
  }
}
