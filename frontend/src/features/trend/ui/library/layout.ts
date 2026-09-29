import type { LibrarySection } from "../../liveSearchProgress";

export type FloorPoint = readonly [x: number, z: number];
export interface ShelfTarget {
  point: FloorPoint;
  heading: number;
}

export const ROOM_WIDTH = 46.4;
export const ROOM_DEPTH = 37.4;
export const DESK: FloorPoint = [0.25, 7.22];
export const DESK_BOOK: FloorPoint = [0.25, 8];
export const SEAT: FloorPoint = [0.25, 6.93];
export const LADDER_PARK: FloorPoint = [1.3, 6.55];

// Геометрия арок и маршруты используют одни границы проёмов.
export const LIBRARY_PARTITIONS = [
  {
    axis: "x",
    at: -5.4,
    from: -18.3,
    to: 4.3,
    gaps: [
      [-16.6, -13.6],
      [-8.4, -5.4],
      [-0.7, 2.3],
    ],
  },
  {
    axis: "x",
    at: 6.1,
    from: -18.3,
    to: 6.5,
    gaps: [
      [-16.6, -13.6],
      [-7.8, -4.8],
      [0.6, 3.6],
    ],
  },
  { axis: "z", at: -3.5, from: -15.9, to: -5.4, gaps: [[-12.4, -9.4]] },
  { axis: "z", at: -1.9, from: 6.1, to: 15.9, gaps: [[10.1, 13.1]] },
  {
    axis: "x",
    at: -16.6,
    from: -18.3,
    to: 13.4,
    gaps: [
      [-12.3, -9.3],
      [-3.8, -0.8],
      [7.1, 10.1],
    ],
  },
  {
    axis: "x",
    at: 16.6,
    from: -18.3,
    to: 12.4,
    gaps: [
      [-12.3, -9.3],
      [-3.8, -0.8],
      [7.1, 10.1],
    ],
  },
  { axis: "z", at: -12, from: -16.6, to: -5.4, gaps: [[-12.7, -9.7]] },
  { axis: "z", at: -10.4, from: 6.1, to: 16.6, gaps: [[10.1, 13.1]] },
] as const;
export const SHELF_ISLANDS = [
  { axis: "x", at: -2.3, from: -11.5, to: -7.9, section: "world" },
  { axis: "z", at: -13.5, from: 8.7, to: 11.9, section: "engineering" },
  { axis: "z", at: 12, from: -6, to: -2.8, section: "corpus" },
  { axis: "x", at: 20.1, from: -10.8, to: -7.2, section: "world" },
] as const;

// Направление задано для каждой рабочей стороны: лестница подходит к лицу стеллажа.
export const SHELF_TARGETS: Record<LibrarySection, readonly ShelfTarget[]> = {
  science: [
    { point: [-12.3, -16.8], heading: Math.PI },
    { point: [-21.3, -8.8], heading: -Math.PI / 2 },
    { point: [-8.8, -13.7], heading: 0 },
    { point: [-8.8, -10.3], heading: Math.PI },
    { point: [-14.9, -6.8], heading: -Math.PI / 2 },
    { point: [-7.1, -1.8], heading: Math.PI / 2 },
  ],
  engineering: [
    { point: [10.1, -16.8], heading: Math.PI },
    { point: [13.4, -16.8], heading: Math.PI },
    { point: [10.3, -15.2], heading: 0 },
    { point: [10.3, -11.8], heading: Math.PI },
    { point: [4.4, -2.7], heading: Math.PI / 2 },
    { point: [14.9, -5.4], heading: Math.PI / 2 },
  ],
  world: [
    { point: [-2.8, -16.8], heading: Math.PI },
    { point: [2.5, -16.8], heading: Math.PI },
    { point: [-0.6, -9.7], heading: -Math.PI / 2 },
    { point: [-4, -9.7], heading: Math.PI / 2 },
    { point: [18.4, -9], heading: Math.PI / 2 },
    { point: [21.8, -9], heading: -Math.PI / 2 },
  ],
  corpus: [
    { point: [-21.3, 2.3], heading: -Math.PI / 2 },
    { point: [-21.3, 8.2], heading: -Math.PI / 2 },
    { point: [-4.4, 10.3], heading: 0 },
    { point: [-4.4, 13.7], heading: Math.PI },
    { point: [-14.9, 12], heading: -Math.PI / 2 },
    { point: [-18.3, 4.4], heading: Math.PI / 2 },
  ],
};
