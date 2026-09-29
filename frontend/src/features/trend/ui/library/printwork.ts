import {
  CanvasTexture,
  Mesh,
  MeshStandardMaterial,
  PlaneGeometry,
  SRGBColorSpace,
  type Group,
} from "three";
import type { LibraryMaterials } from "./materials";

const INSCRIPTIONS = [
  ["ГАЗПРОМБАНК", "БИБЛИОТЕКА ИССЛЕДОВАНИЙ"],
  ["квадрицепс", "ИЩЕМ ГЛУБЖЕ"],
  ["ЕСТЕСТВЕННЫЕ НАУКИ", "ОБСЕРВАТОРИЯ"],
  ["МАСТЕРСКАЯ", "ИДЕИ ПРИОБРЕТАЮТ ФОРМУ"],
  ["АТЛАСЫ И ОТКРЫТИЯ", "ЗА ПРЕДЕЛАМИ ИЗВЕСТНОГО"],
  ["АРХИВ", "У КАЖДОЙ ИСТОРИИ ЕСТЬ ИСТОЧНИК"],
  ["42", "НЕ ПАНИКОВАТЬ"],
  ["В–612", "БЕРЕГИТЕ СВОЮ РОЗУ"],
  ["ЭТО НЕ ЯБЛОКО", "КАБИНЕТ НЕОЧЕВИДНОГО"],
  ["СВОИМ ПУТЁМ", "ДАЖЕ ЕСЛИ ВСЕ ПОШЛИ В ДРУГОЙ ЗАЛ"],
  ["ТИШИНА", "ИДЁТ ПОИСК"],
  ["КОМНАТА 404", "КНИГА ЕЩЁ НЕ НАЙДЕНА"],
  ["РУКОПИСИ НЕ ГОРЯТ", "НО ЧАЙ ОСТАВЛЯЕТ СЛЕДЫ"],
  ["ВРЕМЯ НАЙДЁТСЯ", "ОТДЕЛ ДЛИТЕЛЬНЫХ ИССЛЕДОВАНИЙ"],
  ["ВАВИЛОНСКАЯ БИБЛИОТЕКА", "СЛЕДУЮЩАЯ ПОЛКА — НЕ ПОСЛЕДНЯЯ"],
  ["ЧИТАЛЬНЫЙ ЗАЛ", "ВОЗВРАЩАЙТЕСЬ С ОТКРЫТИЯМИ"],
] as const;

/** Все надписи — один атлас; кириллица остаётся чёткой при приближении камеры. */
export function libraryPrintwork(m: LibraryMaterials) {
  const canvas = document.createElement("canvas");
  canvas.width = 2048;
  canvas.height = 1024;
  const context = canvas.getContext("2d")!;
  const typeface = getComputedStyle(document.documentElement).fontFamily;
  INSCRIPTIONS.forEach(([title, subtitle], index) => {
    const x = (index % 4) * 512;
    const y = Math.floor(index / 4) * 256;
    context.fillStyle = (index === 1 ? m.claret : index === 0 ? m.navy : m.paper).color.getStyle();
    context.fillRect(x, y, 512, 256);
    context.strokeStyle = m.brass.color.getStyle();
    context.lineWidth = 2;
    context.strokeRect(x + 15, y + 15, 482, 226);
    context.strokeRect(x + 23, y + 23, 466, 210);
    context.fillStyle = (index < 2 ? m.paper : m.wood).color.getStyle();
    context.textAlign = "center";
    context.textBaseline = "middle";
    let size = index === 6 ? 98 : index === 1 ? 66 : 40;
    context.font = `600 ${size}px ${typeface}`;
    while (context.measureText(title).width > 454) {
      context.font = `600 ${--size}px ${typeface}`;
    }
    context.fillText(title, x + 256, y + 112);
    context.font = `500 14px ${typeface}`;
    context.fillText(subtitle, x + 256, y + 179, 446);
    for (const side of [-1, 1]) {
      context.beginPath();
      context.arc(x + 256 + side * 182, y + 60, 3, 0, Math.PI * 2);
      context.fill();
    }
  });
  const texture = new CanvasTexture(canvas);
  texture.colorSpace = SRGBColorSpace;
  texture.anisotropy = 4;
  const material = new MeshStandardMaterial({ map: texture, roughness: 0.68 });
  const geometries: PlaneGeometry[] = [];
  function label(parent: Group, index: number, width: number, height = width / 2, segments = 1) {
    const geometry = new PlaneGeometry(width, height, segments, segments);
    const uv = geometry.getAttribute("uv");
    for (let i = 0; i < uv.count; i++) {
      uv.setXY(
        i,
        ((index % 4) + 0.005 + uv.getX(i) * 0.99) / 4,
        (3 - Math.floor(index / 4) + 0.01 + uv.getY(i) * 0.98) / 4,
      );
    }
    geometries.push(geometry);
    const mesh = new Mesh(geometry, material);
    parent.add(mesh);
    return mesh;
  }
  return {
    label,
    dispose: () => {
      geometries.forEach((geometry) => geometry.dispose());
      texture.dispose();
      material.dispose();
    },
  };
}
