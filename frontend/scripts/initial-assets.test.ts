import { expect, test } from "bun:test";
import { initialAssets } from "./initial-assets";

test("initial assets include shared static dependencies once and exclude lazy imports", () => {
  const manifest = {
    "index.html": { file: "entry.js", imports: ["a", "b"], dynamicImports: ["pdf"] },
    a: { file: "a.js", imports: ["shared"] },
    b: { file: "b.js", imports: ["shared"] },
    shared: { file: "shared.js", imports: ["a"] },
    pdf: { file: "pdf.js" },
  };
  expect(initialAssets(manifest, "index.html")).toEqual([
    "dist/entry.js",
    "dist/a.js",
    "dist/shared.js",
    "dist/b.js",
  ]);
  expect(() => initialAssets(manifest, "missing")).toThrow("Missing build manifest entry");
});
