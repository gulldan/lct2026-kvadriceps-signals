import { fileURLToPath, URL } from "node:url";

import tailwindcss from "@tailwindcss/vite";
import react from "@vitejs/plugin-react";
import { defineConfig } from "vite";
import { assertInitialBoundary } from "./scripts/initial-boundary.ts";

// Локально RAG_API указывает на шлюз nginx: RAG_API=http://localhost:8080 bun run dev.
// RAG_PUBLIC_API="" при сборке даёт WebSocket того же origin.
const target = process.env.RAG_API ?? "http://localhost:8080";
const publicGateway = process.env.RAG_PUBLIC_API ?? target;

export default defineConfig({
  plugins: [
    react(),
    tailwindcss(),
    {
      name: "product-initial-boundary",
      apply: "build",
      generateBundle(_, bundle) {
        const chunks = new Map(
          Object.values(bundle)
            .filter((chunk) => chunk.type === "chunk")
            .map((chunk) => [chunk.fileName, chunk]),
        );
        const entry = [...chunks.values()].find(
          (chunk) =>
            chunk.facadeModuleId === fileURLToPath(new URL("./index.html", import.meta.url)),
        );
        if (!entry) this.error("Product build entry was not found");
        assertInitialBoundary(chunks, entry.fileName);
      },
    },
  ],
  resolve: {
    alias: { "@": fileURLToPath(new URL("./src", import.meta.url)) },
  },
  define: {
    __GATEWAY__: JSON.stringify(publicGateway),
  },
  build: {
    manifest: true,
    // Карты кода читает тот, кто разбирает отчёт об ошибке, а не браузер пользователя.
    sourcemap: "hidden",
    rolldownOptions: {
      input: {
        index: fileURLToPath(new URL("./index.html", import.meta.url)),
        "ui-kit": fileURLToPath(new URL("./ui-kit.html", import.meta.url)),
      },
      output: {
        hoistTransitiveImports: false,
        codeSplitting: {
          groups: [
            {
              name: "react-vendor",
              test: /\/node_modules\/(react|react-dom|react-router|react-router-dom|scheduler)\//,
            },
            {
              name: "radix",
              test: /\/node_modules\/(@radix-ui|@floating-ui|aria-hidden|react-remove-scroll|react-remove-scroll-bar|react-style-singleton|use-sidecar|use-callback-ref|detect-node-es)/,
            },
            { name: "icons", test: /\/node_modules\/lucide-react\// },
            // Общие помощники карты не должны склеивать её с ленивыми карточками и отчётами.
            {
              name: "feature-trend-map",
              test: /\/src\/features\/trend\//,
              tags: ["$initial"],
            },
          ],
        },
      },
    },
  },
  server: {
    port: 5173,
    proxy: {
      "/api": { target, changeOrigin: true },
    },
  },
});
