import { expect, test } from "bun:test";
import { createData } from "../../../../tests/product/data";
import { loadHypothesisRuntimeSettings } from "@/shared/appSettings";
import { matchesQueue } from "./model";

test("очередь гипотез не скрывает непроверенные направления и черновики", () => {
  const hypothesis = createData().hypotheses[0]!;
  const rows = [
    hypothesis,
    { ...hypothesis, id: "pending", status: "generated" as const, assessment: {} },
    { ...hypothesis, id: "closed", status: "archived" as const, assessment: {} },
  ];
  const settings = loadHypothesisRuntimeSettings();
  expect(rows.filter((row) => matchesQueue(row, "ready", settings, "hypotheses"))).toEqual([
    hypothesis,
  ]);
  for (const kind of ["directions", "drafts"] as const) {
    expect(rows.filter((row) => matchesQueue(row, "ready", settings, kind))).toEqual(rows);
    expect(rows.filter((row) => matchesQueue(row, "risk", settings, kind))).toEqual(rows);
  }
});
