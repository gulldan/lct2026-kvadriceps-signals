import { expect, test } from "bun:test";
import type { LiveChannel } from "./liveApi";
import { mergeChannels } from "./liveMetrics";

const channel = (over: Partial<LiveChannel>): LiveChannel => ({
  source: "USPTO",
  observed_at: "2026-09-20T00:00:00Z",
  counts: [],
  total: 0,
  partial: false,
  limits: [],
  records: [],
  ...over,
});

test("патенты двух ведомств складываются, несобранная история не обнуляет собранную", () => {
  const us = channel({ counts: [null, 2, 3], total: 5 });
  const cn = channel({
    source: "CNIPA",
    observed_at: "2026-09-18T00:00:00Z",
    counts: [null, null, 4],
    total: 4,
    partial: true,
    records: [{ id: "cn", title: "", date: "2026-09-01", url: "", source: "CNIPA" }],
  });
  const merged = mergeChannels(us, cn)!;
  expect(merged.counts).toEqual([null, 2, 7]);
  expect(merged.total).toBe(9);
  expect(merged.partial).toBe(true);
  expect(merged.observed_at).toBe("2026-09-18T00:00:00Z");
  expect(merged.records!.map((row) => row.source)).toEqual(["CNIPA"]);
  expect(mergeChannels(null, cn)).toBe(cn);
  expect(mergeChannels(undefined, null)).toBeNull();
});
