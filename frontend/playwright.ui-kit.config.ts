import { defineConfig, devices } from "@playwright/test";

const port = Number(process.env.UI_TEST_PORT ?? 4174);
if (!Number.isInteger(port) || port < 1 || port > 65535) {
  throw new Error("UI_TEST_PORT must be an integer between 1 and 65535");
}
const baseURL = `http://127.0.0.1:${port}`;

export default defineConfig({
  testDir: "tests",
  testMatch: ["ui-kit/**/*.spec.ts", "product/**/*.spec.ts"],
  timeout: 30_000,
  expect: { timeout: 5_000 },
  fullyParallel: true,
  forbidOnly: !!process.env.CI,
  retries: process.env.CI ? 1 : 0,
  workers: process.env.CI ? 2 : undefined,
  reporter: [["list"], ["html", { open: "never" }]],
  use: {
    baseURL,
    trace: "retain-on-failure",
    screenshot: "only-on-failure",
  },
  projects: [{ name: "chromium", use: { ...devices["Desktop Chrome"] } }],
  webServer: {
    command: `bun run dev --host 127.0.0.1 --port ${port} --strictPort`,
    url: `${baseURL}/ui-kit.html`,
    // Чужой Vite из соседнего worktree проверил бы старое приложение вместо текущих изменений.
    reuseExistingServer: false,
  },
});
