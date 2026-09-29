import { expect, test } from "./fixture";

// Проверяет CSP стенда, CORS хранилища и открытый ETag в настоящем браузере. Сессия прерывается
// сама, чтобы крупный синтетический документ не ушёл в индексацию.
test("multipart uploads can reach storage from the deployed browser origin", async ({ page }) => {
  await page.goto("/operator/documents");
  await expect(page.getByRole("button", { name: /Загрузить|Upload/ }).first()).toBeVisible();
  const refreshed = await page.request.post("/api/v1/auth/refresh", {
    headers: { "X-Auth-Mode": "browser" },
    data: {},
  });
  expect(refreshed.ok()).toBe(true);
  const { access_token: token } = (await refreshed.json()) as { access_token: string };
  const headers = { Authorization: `Bearer ${token}` };
  const created = await page.request.post("/api/v1/uploads", {
    headers,
    data: { filename: "qa-multipart-abort.txt", size: 52 * 1024 * 1024, mime_type: "text/plain" },
  });
  expect(created.ok()).toBe(true);
  const session = (await created.json()) as { upload_id: string; part_size: number };
  try {
    const response = await page.request.get(
      `/api/v1/uploads/${session.upload_id}/parts?from=1&count=1`,
      { headers },
    );
    expect(response.ok()).toBe(true);
    const { urls } = (await response.json()) as { urls: { url: string }[] };
    const part = await page.evaluate(
      async ({ url, size }) => {
        const uploaded = await fetch(url, { method: "PUT", body: new Uint8Array(size) });
        return { ok: uploaded.ok, etag: uploaded.headers.get("ETag") };
      },
      { url: urls[0]!.url, size: session.part_size },
    );
    expect(part.ok).toBe(true);
    expect(part.etag).toBeTruthy();
  } finally {
    const aborted = await page.request.delete(`/api/v1/uploads/${session.upload_id}`, { headers });
    expect(aborted.ok()).toBe(true);
  }
});
