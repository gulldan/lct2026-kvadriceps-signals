import { expect, test } from "bun:test";
import { assertInitialBoundary } from "./initial-boundary";

test("a lazy facade cannot hide deferred modules in a shared initial chunk", () => {
  const card = "/app/src/features/trend/ui/LiveDetail.tsx";
  const chunks = new Map([
    ["index.js", { imports: ["map.js"], moduleIds: ["/app/src/app/App.tsx"] }],
    ["map.js", { imports: ["shared.js"], moduleIds: ["/app/src/features/trend/ui/LiveMap.tsx"] }],
    ["shared.js", { imports: [], moduleIds: ["/app/src/shared/ui/Button.tsx"] }],
    ["detail.js", { imports: ["shared.js"], moduleIds: [card] }],
    ["docx.js", { imports: [], moduleIds: ["/app/node_modules/docx/dist/index.mjs"] }],
    ["pdf.js", { imports: [], moduleIds: ["/app/node_modules/pdfjs-dist/build/pdf.mjs"] }],
  ]);
  expect(() => assertInitialBoundary(chunks, "index.js")).not.toThrow();

  // Имена файлов и фасад сохраняются; меняется только состав общего чанка.
  chunks.get("detail.js")!.moduleIds = [];
  chunks.get("shared.js")!.moduleIds.push(card);
  expect(() => assertInitialBoundary(chunks, "index.js")).toThrow(
    "index.js → map.js → shared.js\nDeferred modules: " + card,
  );
  chunks.get("shared.js")!.moduleIds.pop();
  for (const deferred of ["docx.js", "pdf.js"]) {
    chunks.get("shared.js")!.imports = [deferred];
    expect(() => assertInitialBoundary(chunks, "index.js")).toThrow(
      `index.js → map.js → shared.js → ${deferred}`,
    );
  }
});
