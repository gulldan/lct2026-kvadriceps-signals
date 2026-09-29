import { test as base, expect } from "@playwright/test";

// Своё семейство refresh-токенов на сессию: общий storageState повторял бы токен.
export const test = base.extend({
  page: async ({ page, context }, use) => {
    const username = process.env.E2E_USER ?? "admin";
    const password = process.env.E2E_PASS ?? process.env.ADMIN_PASSWORD;
    if (!password) throw new Error("Set E2E_PASS or ADMIN_PASSWORD");
    const response = await context.request.post("/api/v1/auth/login", {
      headers: { "X-Auth-Mode": "browser" },
      data: { username, password },
    });
    expect(response.ok(), `Login returned ${response.status()}`).toBe(true);
    await use(page);
    const revoked = await context.request.post("/api/v1/auth/logout", {
      headers: { "X-Auth-Mode": "browser" },
      data: {},
    });
    expect(revoked.ok()).toBe(true);
  },
});
export { expect };
