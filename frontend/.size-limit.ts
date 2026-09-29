import { readFileSync } from "node:fs";
import type { SizeLimitConfig } from "size-limit";
import type { Manifest } from "vite";
import { initialAssets } from "./scripts/initial-assets.ts";

const manifest: Manifest = JSON.parse(
  readFileSync(new URL("./dist/.vite/manifest.json", import.meta.url), "utf8"),
);
const appFiles = initialAssets(manifest, "index.html");
if (appFiles.some((file) => /\/(?:ui-kit|RichTextInner|PdfViewerInner)-/.test(file))) {
  throw new Error("The product entry must not eagerly load the UI catalog, Markdown or PDF.");
}

export default [
  // Файлы берутся из манифеста: второй вход Vite меняет имена общих чанков.
  { name: "Product initial JS (all static imports)", path: appFiles, limit: "260 KB" },
  { name: "UI kit initial JS", path: initialAssets(manifest, "ui-kit.html"), limit: "260 KB" },
  { path: "dist/assets/index-*.js", limit: "20 KB" },
  { path: "dist/assets/react-vendor-*.js", limit: "70 KB" },
  { path: "dist/assets/radix-*.js", limit: "38 KB" },
  { path: "dist/assets/icons-*.js", limit: "12 KB" },
  { path: "dist/assets/RichTextInner-*.js", limit: "130 KB" },
  { path: "dist/assets/PdfViewerInner-*.js", limit: "120 KB" },
  { name: "Research library (loaded on demand)", path: "dist/assets/scene-*.js", limit: "170 KB" },
] satisfies SizeLimitConfig;
