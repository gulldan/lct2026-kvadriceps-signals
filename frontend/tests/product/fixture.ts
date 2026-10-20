import { expect, test as base } from "@playwright/test";
import { createTestApi, type TestApi } from "./api";
import type { Role } from "../../src/shared/types";

export { expect } from "@playwright/test";
export const test = base.extend<{ api: TestApi; roles: Role[] | null }>({
  roles: [["admin", "operator", "user"], { option: true }],
  api: [
    async ({ page, roles }, use) => {
      const api = createTestApi();
      const errors: string[] = [];
      page.on("pageerror", (error) => errors.push(error.message));
      if (roles) api.auth.user.roles = roles;
      let signedIn = roles !== null;
      await page.addInitScript(() => {
        if (!localStorage.getItem("rag.lang")) localStorage.setItem("rag.lang", "ru");
      });
      await page.routeWebSocket(/\/api\/v1\/ws/, () => {
        /* открытый поток загрузки без событий */
      });
      await page.route("**/api/v1/**", async (route) => {
        const request = route.request();
        const path = new URL(request.url()).pathname;
        if (path === "/api/v1/auth/refresh") {
          // Без cookie сервер отвечает 204: «сессии нет» — не ошибка.
          await route.fulfill(signedIn ? { json: api.auth } : { status: 204 });
          return;
        }
        if (path === "/api/v1/auth/logout") {
          signedIn = false;
          await route.fulfill({ status: 204 });
          return;
        }
        if (path === "/api/v1/ws-ticket") {
          await route.fulfill({ json: { ticket: "test-ticket" } });
          return;
        }
        const contentType = request.headers()["content-type"] ?? "";
        const body = contentType.includes("application/json")
          ? (request.postDataJSON() as Record<string, unknown>)
          : {};
        if (contentType.includes("multipart/form-data")) {
          const data = await new Response(new Uint8Array(request.postDataBuffer() ?? []), {
            headers: { "content-type": contentType },
          }).formData();
          const file = data.get("file");
          if (file instanceof File) body.filename = file.name;
        }
        const reply = api.dispatch(request.method(), request.url(), body);
        if (path === "/api/v1/auth/login" && reply.status === 200) signedIn = true;
        if (reply.pdf)
          await route.fulfill({
            status: reply.status,
            path: new URL("../fixtures/one-page.pdf", import.meta.url).pathname,
            contentType: "application/pdf",
          });
        else if (reply.text)
          await route.fulfill({
            status: reply.status,
            body: reply.text,
            contentType: "text/plain",
          });
        else
          await route.fulfill({
            status: reply.status,
            ...(reply.status === 204 ? {} : { json: reply.json }),
          });
      });
      await use(api);
      expect(
        api.unhandled,
        "Every API operation must be explicitly exercised against a fixture",
      ).toEqual([]);
      expect(errors, "No uncaught errors in page interactions").toEqual([]);
    },
    { auto: true },
  ],
});
