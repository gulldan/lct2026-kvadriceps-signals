import type { ApiCluster } from "@/features/cluster";
import { cleanInternalTitle, itcFactorValue } from "@/features/hypothesis";

export interface ThemePoint {
  id: string;
  label: string;
  freshness: number;
  novelty: number;
  documents: number;
}

export const THEME_REGIONS = [
  "earlierDistinct",
  "recentDistinct",
  "earlierSimilar",
  "recentSimilar",
] as const;
export type ThemeRegion = (typeof THEME_REGIONS)[number];

export type ThemeSelection = (
  | { kind: "point"; id: string }
  | { kind: "region"; id: ThemeRegion }
) & { points: ThemePoint[] };

/** Постоянные середины держат принадлежность к области при поиске и сортировке. */
export function themeRegion(point: ThemePoint): ThemeRegion {
  const column = point.freshness >= 0.5 ? 1 : 0;
  const row = point.novelty >= 0.5 ? 0 : 2;
  return THEME_REGIONS[row + column]!;
}

export function nearbyThemes(
  points: ThemePoint[],
  selected: ThemePoint,
  width: number,
  height: number,
): ThemePoint[] {
  return points.filter(
    (point) =>
      Math.hypot(
        (point.freshness - selected.freshness) * width,
        (point.novelty - selected.novelty) * height,
      ) <= 28,
  );
}

/** Фильтрация не должна двигать другие темы. */
export function themePoints(clusters: ApiCluster[]): ThemePoint[] {
  return clusters
    .flatMap((cluster) => {
      const itc = cluster.params?.itc;
      const label = cleanInternalTitle(cluster.label);
      const freshness = itcFactorValue(itc, "SM");
      const novelty = itcFactorValue(itc, "NV");
      if (!itc || !label || freshness === null || novelty === null) return [];
      return [
        {
          id: cluster.id,
          label,
          freshness: freshness / 100,
          novelty: novelty / 100,
          documents: cluster.document_count,
        },
      ];
    })
    .toSorted((a, b) => b.documents - a.documents);
}

export const THEME_SORTS = {
  tech: {
    labelKey: "sort.tech",
    cmp: (a: ApiCluster, b: ApiCluster) =>
      (b.params?.itc?.techscore ?? -1) - (a.params?.itc?.techscore ?? -1) ||
      b.document_count - a.document_count,
  },
  size: {
    labelKey: "sort.size",
    cmp: (a: ApiCluster, b: ApiCluster) =>
      b.document_count - a.document_count || b.chunk_count - a.chunk_count,
  },
  fresh: {
    labelKey: "sort.fresh",
    cmp: (a: ApiCluster, b: ApiCluster) =>
      (itcFactorValue(b.params?.itc, "SM") ?? -1) - (itcFactorValue(a.params?.itc, "SM") ?? -1),
  },
  novelty: {
    labelKey: "sort.novelty",
    cmp: (a: ApiCluster, b: ApiCluster) =>
      (itcFactorValue(b.params?.itc, "NV") ?? -1) - (itcFactorValue(a.params?.itc, "NV") ?? -1),
  },
} as const;
