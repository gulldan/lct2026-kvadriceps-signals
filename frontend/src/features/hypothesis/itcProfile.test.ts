import { expect, test } from "bun:test";
import { createData } from "../../../tests/product/data";
import { itcFactorValue, itcIndex } from "./itcProfile";

test("the research index uses the weighted aggregate, independently of the legacy band", () => {
  const itc = structuredClone(createData().clusters[0]!.params!.itc!);
  expect(itcIndex(itc)).toBe(64);
  itc.score = 1;
  expect(itcIndex(itc)).toBe(64);
  itc.axes.impact = 0;
  expect(itcFactorValue(itc, "IP")).toBe(0);
  itc.signals!.freshness_available = false;
  itc.axes.momentum = 0.5;
  expect(itcFactorValue(itc, "SM")).toBeNull();
  itc.signals!.novelty_distance = null;
  expect(itcFactorValue(itc, "NV")).toBeNull();
});
