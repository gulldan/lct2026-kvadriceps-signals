import type { LiveTopic } from "./liveApi";

export const STAGE_ORDER = [
  "emerging_signal",
  "early_signal",
  "established",
  "unassessed",
] as const;
export type LiveSubgroup = "measured" | "new_from_zero" | "delta_unknown";
export const SUBGROUP_ORDER: LiveSubgroup[] = ["measured", "new_from_zero", "delta_unknown"];

type Grouped = Pick<LiveTopic, "growth"> & {
  comparison?: { prior: number; current: number } | null;
};

export function liveSubgroup(topic: Grouped): LiveSubgroup {
  if (topic.comparison && topic.comparison.prior === 0 && topic.comparison.current > 0)
    return "new_from_zero";
  if (topic.growth.delta_pp === null) return "delta_unknown";
  return "measured";
}

export function groupTopics<T extends Grouped & Pick<LiveTopic, "stage">>(topics: T[]) {
  const groups = new Map<(typeof STAGE_ORDER)[number], Record<LiveSubgroup, T[]>>();
  for (const stage of STAGE_ORDER)
    groups.set(stage, { measured: [], new_from_zero: [], delta_unknown: [] });
  for (const topic of topics) groups.get(topic.stage)![liveSubgroup(topic)].push(topic);
  return STAGE_ORDER.map((stage) => ({ stage, subgroups: groups.get(stage)! })).filter((group) =>
    SUBGROUP_ORDER.some((key) => group.subgroups[key].length > 0),
  );
}
