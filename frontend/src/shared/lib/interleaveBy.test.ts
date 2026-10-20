import { expect, test } from "bun:test";
import { interleaveBy, interleaveTies } from "./interleaveBy";

test("tie interleaving keeps the ranked key monotonic and mixes groups only among equal values", () => {
  const ranked = [
    { id: "a-16", area: "a", growth: "0.16" },
    { id: "a-09", area: "a", growth: "0.09" },
    { id: "a-0", area: "a", growth: "0" },
    { id: "a-0b", area: "a", growth: "0" },
    { id: "b-0", area: "b", growth: "0" },
  ];
  expect(
    interleaveTies(
      ranked,
      (item) => item.growth,
      (item) => item.area,
    ).map((item) => item.id),
  ).toEqual(["a-16", "a-09", "a-0", "b-0", "a-0b"]);
});

test("diverse ordering preserves every item and rank within known or missing groups", () => {
  const items = [
    { id: "compute-1", area: " Computing " },
    { id: "compute-2", area: "computing" },
    { id: "compute-3", area: "COMPUTING" },
    { id: "medicine-1", area: "medicine" },
    { id: "medicine-2", area: "medicine" },
    { id: "energy-1", area: "energy" },
    { id: "unknown-1", area: "" },
    { id: "unknown-2", area: "" },
  ];
  expect(interleaveBy(items, (item) => item.area).map((item) => item.id)).toEqual([
    "compute-1",
    "medicine-1",
    "energy-1",
    "unknown-1",
    "compute-2",
    "medicine-2",
    "unknown-2",
    "compute-3",
  ]);
  expect(interleaveBy(items.slice(0, 3), (item) => item.area)).toEqual(items.slice(0, 3));
  expect(interleaveBy([], () => "")).toEqual([]);
  expect(items[1]?.id).toBe("compute-2");
});
