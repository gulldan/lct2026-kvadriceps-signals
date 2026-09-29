import type { Itc } from "./api";
import { formatNumber } from "@/shared/lib/format";

export const ITC_FACTORS = [
  { key: "SM", axis: "momentum" },
  { key: "NV", axis: "novelty" },
  { key: "IP", axis: "impact" },
  { key: "HR", axis: "diffusion" },
] as const;

export type ItcFactorKey = (typeof ITC_FACTORS)[number]["key"];

export function itcFactorValue(itc: Itc | undefined, key: ItcFactorKey): number | null {
  if (!itc) return null;
  const signals = itc.signals;
  if (key === "SM" && (signals?.freshness_available === false || signals?.pub_count === 0))
    return null;
  if (key === "NV" && (signals?.novelty_available === false || signals?.novelty_distance === null))
    return null;
  const axis = ITC_FACTORS.find((factor) => factor.key === key)!.axis;
  const value = itc.axes?.[axis];
  return typeof value === "number" && Number.isFinite(value)
    ? Math.max(0, Math.min(100, value * 100))
    : null;
}

export function itcIndex(itc: Itc | undefined): number | null {
  return itc && Number.isFinite(itc.techscore)
    ? Math.max(0, Math.min(100, itc.techscore * 100))
    : null;
}

export function formatItcIndex(itc: Itc | undefined): string {
  return formatNumber(itcIndex(itc) ?? Number.NaN, { maximumFractionDigits: 1 });
}
