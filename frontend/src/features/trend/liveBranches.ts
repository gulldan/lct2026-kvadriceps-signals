import type { LiveTopic } from "./liveApi";

const BRANCHES = [
  "computing",
  "quantum",
  "materials",
  "physics",
  "space",
  "life",
  "earth",
  "math",
  "engineering",
  "economics",
] as const;
export type Branch = (typeof BRANCHES)[number];

export function topicBranch(topic: Pick<LiveTopic, "branch">): Branch | null {
  return BRANCHES.includes(topic.branch as Branch) ? (topic.branch as Branch) : null;
}
