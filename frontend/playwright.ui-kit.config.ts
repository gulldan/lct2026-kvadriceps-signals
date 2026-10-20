import { defineConfig, devices } from "@playwright/test";

const port = Number(process.env.UI_TEST_PORT ?? 4174);
if (!Number.isInteger(port) || port < 1 || port > 65535) {
  throw new Error("UI_TEST_PORT must be an integer between 1 and 65535");
}
const baseURL = `http://127.0.0.1:${port}`;
// Сцену библиотеки рисует программный WebGL (SwiftShader): около 2,5 с процессора на каждую.
// Рядом с остальными воркерами запуск сцены уходил за пять секунд ожидания даже по одной, поэтому
// сцены идут отдельной очередью в один воркер после основной массы проверок.
const scenes = ["product/research-library.spec.ts", "product/service-states.spec.ts"];

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
    // На Linux без локали Chromium не пишет кириллическое имя выгрузки, а обычная полоса прокрутки
    // съедает 15 px ширины: одинаковый результат на сервере и на Маке даёт накладная полоса и UTF-8.
    launchOptions: {
      args: ["--enable-features=OverlayScrollbar"],
      env: { ...process.env, LANG: process.env.LANG || "C.UTF-8" },
    },
  },
  projects: [
    { name: "chromium", use: { ...devices["Desktop Chrome"] }, testIgnore: scenes },
    // Последним в списке: очередь сцен начинается, когда остальные проверки уже разобраны воркерами.
    { name: "scenes", use: { ...devices["Desktop Chrome"] }, testMatch: scenes, workers: 1 },
  ],
  webServer: {
    command: `bun run dev --host 127.0.0.1 --port ${port} --strictPort`,
    url: `${baseURL}/ui-kit.html`,
    // Чужой Vite из соседнего worktree проверил бы старое приложение вместо текущих изменений.
    reuseExistingServer: false,
  },
});
