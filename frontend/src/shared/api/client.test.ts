import { afterEach, beforeEach, describe, expect, test } from "bun:test";
import {
  loadAuth,
  request,
  saveAuth,
  setAuthChangedHandler,
  setUnauthorizedHandler,
} from "./client";

const store = new Map<string, string>();
const calls: { url: string; auth: string | null }[] = [];
let responses: (() => Response)[] = [];

beforeEach(() => {
  store.clear();
  saveAuth(null);
  calls.length = 0;
  responses = [];
  (globalThis as { localStorage?: unknown }).localStorage = {
    getItem: (key: string) => store.get(key) ?? null,
    setItem: (key: string, value: string) => void store.set(key, value),
    removeItem: (key: string) => void store.delete(key),
  };
  globalThis.fetch = (async (input: RequestInfo | URL, init?: RequestInit) => {
    const headers = new Headers(init?.headers);
    calls.push({ url: String(input), auth: headers.get("Authorization") });
    const next = responses.shift();
    return next ? next() : new Response("{}", { status: 200 });
  }) as typeof fetch;
});

afterEach(() => {
  setAuthChangedHandler(null);
  setUnauthorizedHandler(() => {});
});

const token = (access: string) => ({
  access_token: access,
  refresh_token: "refresh-1",
  token_type: "Bearer",
  expires_at: "2099-01-01T00:00:00Z",
  user: { id: "u1", username: "u", roles: ["user"] },
});

describe("request", () => {
  test("a 401 refreshes once, retries with the new token and tells the context", async () => {
    saveAuth(token("old"));
    let announced = "";
    setAuthChangedHandler((auth) => {
      announced = auth.access_token;
    });
    responses = [
      () => new Response("", { status: 401 }),
      () => new Response(JSON.stringify(token("new")), { status: 200 }),
      () => new Response(JSON.stringify({ ok: true }), { status: 200 }),
    ];
    const result = await request<{ ok: boolean }>("/things");
    expect(result).toEqual({ ok: true });
    expect(calls.map((call) => call.auth)).toEqual(["Bearer old", null, "Bearer new"]);
    expect(calls[1]!.url).toContain("/auth/refresh");
    expect(announced).toBe("new");
    expect(loadAuth()?.access_token).toBe("new");
    expect(store.has("rag.auth")).toBe(false);
  });

  test("a failed refresh ends the session exactly once", async () => {
    saveAuth(token("old"));
    let ended = 0;
    setUnauthorizedHandler(() => {
      ended += 1;
    });
    responses = [() => new Response("", { status: 401 }), () => new Response("", { status: 401 })];
    await expect(request("/things")).rejects.toMatchObject({ status: 401 });
    expect(ended).toBe(1);
    expect(calls).toHaveLength(2);
  });

  test("a server error surfaces its status and message", async () => {
    saveAuth(token("old"));
    responses = [() => new Response(JSON.stringify({ error: "boom" }), { status: 503 })];
    await expect(request("/things")).rejects.toMatchObject({ status: 503, message: "boom" });
  });
});
