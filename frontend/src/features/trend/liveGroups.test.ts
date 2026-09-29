import { describe, expect, test } from "bun:test";
import { groupTopics, liveSubgroup } from "./liveGroups";

const topic = (stage: string, delta: number | null, prior: number, current: number) =>
  ({ stage, growth: { delta_pp: delta }, comparison: { prior, current } }) as never;

describe("live groups", () => {
  test("growth from zero and an undefined change are separate subgroups", () => {
    expect(liveSubgroup(topic("established", 0.4, 0, 12))).toBe("new_from_zero");
    expect(liveSubgroup(topic("established", null, 3, 5))).toBe("delta_unknown");
    expect(liveSubgroup(topic("established", 0.4, 3, 5))).toBe("measured");
    expect(liveSubgroup({ growth: { delta_pp: 0 }, comparison: null } as never)).toBe("measured");
  });
  test("groups keep stage order and drop empty stages", () => {
    const groups = groupTopics([
      topic("unassessed", null, 0, 0),
      topic("emerging_signal", 1, 2, 9),
      topic("emerging_signal", 2, 0, 4),
    ]);
    expect(groups.map((group) => group.stage)).toEqual(["emerging_signal", "unassessed"]);
    expect(groups[0]!.subgroups.measured).toHaveLength(1);
    expect(groups[0]!.subgroups.new_from_zero).toHaveLength(1);
    expect(groups[1]!.subgroups.delta_unknown).toHaveLength(1);
  });
});
