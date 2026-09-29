import { Color, MeshStandardMaterial } from "three";

function tokenColors(element: HTMLElement) {
  const style = getComputedStyle(element);
  return (token: string) => new Color(style.getPropertyValue(token).trim());
}

/** Цвета света не являются материалами и не освобождаются вместе с ними. */
export function libraryLight(element: HTMLElement) {
  const color = tokenColors(element);
  return {
    ember: color("--library-ember"),
    sky: color("--library-sky"),
  };
}

export function libraryMaterials(element: HTMLElement) {
  const color = tokenColors(element);
  const material = (token: string, roughness = 0.75, metalness = 0) =>
    new MeshStandardMaterial({ color: color(token), roughness, metalness });
  const wood = material("--library-walnut");
  const oak = material("--library-oak");
  const paper = material("--library-paper");
  const brass = material("--library-brass", 0.32, 0.6);
  const navy = material("--library-navy");
  const sage = material("--library-sage");
  const claret = material("--library-claret");
  const ink = material("--media-ink");
  // Дерево и штукатурка получают процедурную фактуру в surfaces.ts; копии наследуют пометку.
  wood.userData.surface = oak.userData.surface = "wood";
  const plaster = paper.clone();
  plaster.userData.surface = "plaster";
  plaster.color.lerp(wood.color, 0.12);
  const floor = paper.clone();
  floor.color.lerp(oak.color, 0.23);
  const floorAlt = floor.clone();
  floorAlt.color.lerp(oak.color, 0.16);
  const recess = wood.clone();
  recess.color.multiplyScalar(0.52);
  const light = paper.clone();
  light.emissive.copy(paper.color);
  light.emissiveIntensity = 1.6;
  const active = brass.clone();
  active.emissive.copy(brass.color);
  active.emissiveIntensity = 0.9;
  return {
    wood,
    oak,
    paper,
    brass,
    navy,
    sage,
    claret,
    ink,
    plaster,
    floor,
    floorAlt,
    recess,
    light,
    active,
  };
}

export type LibraryMaterials = ReturnType<typeof libraryMaterials>;
