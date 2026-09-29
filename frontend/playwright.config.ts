import { defineConfig } from "@playwright/test";

// E2E и a11y гоняются против живого стенда с работающим бэкендом:
//   E2E_BASE_URL=http://localhost:5173 E2E_PASS=... bun run a11y
export default defineConfig({
  testDir: "tests/e2e",
  timeout: 30_000,
  retries: 0,
  workers: 4,
  use: {
    baseURL: process.env.E2E_BASE_URL ?? "http://localhost:5173",
    viewport: { width: 1440, height: 1000 },
  },
});
