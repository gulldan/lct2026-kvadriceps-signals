import { i18n } from "@/shared/i18n";
import { saveFile } from "@/shared/lib/download";

export const BASE = "/api/v1";
const STORAGE_KEY = "rag.auth";

export class ApiError extends Error {
  readonly status: number;
  constructor(status: number, message: string) {
    super(message);
    this.status = status;
  }
}

interface ApiUser {
  id: string;
  username: string;
  roles: string[];
}

export interface TokenResponse {
  access_token: string;
  refresh_token?: string;
  token_type: string;
  expires_at: string;
  user: ApiUser;
}

let currentAuth: TokenResponse | null = null;
let authGeneration = 0;

export function loadAuth(): TokenResponse | null {
  return currentAuth;
}

export function saveAuth(auth: TokenResponse | null): void {
  currentAuth = auth;
  if (!auth) authGeneration += 1;
  // Старый формат хранил секреты в localStorage, до перехода на HttpOnly cookie.
  try {
    localStorage.removeItem(STORAGE_KEY);
  } catch {
    /* Хранилище может быть запрещено браузером. */
  }
}

let onUnauthorized: (() => void) | null = null;
let onAuthChanged: ((auth: TokenResponse) => void) | null = null;

export function setUnauthorizedHandler(fn: () => void): void {
  onUnauthorized = fn;
}

/** Иначе WebSocket и всё, что читает токен из контекста, живёт с протухшим. */
export function setAuthChangedHandler(fn: ((auth: TokenResponse) => void) | null): void {
  onAuthChanged = fn;
}

let refreshInFlight: Promise<boolean> | null = null;

async function sessionMutation<T>(run: () => Promise<T>): Promise<T> {
  if (typeof navigator !== "undefined" && navigator.locks) {
    return navigator.locks.request("rag-session", run);
  }
  return run();
}

async function tryRefresh(): Promise<boolean> {
  const generation = authGeneration;
  refreshInFlight ??= sessionMutation(async () => {
    try {
      const resp = await fetch(`${BASE}/auth/refresh`, {
        method: "POST",
        credentials: "same-origin",
        headers: { "Content-Type": "application/json", "X-Auth-Mode": "browser" },
        body: "{}",
        signal: AbortSignal.timeout(8000),
      });
      // 204 — сервер ответил «сессии нет» без ошибки в консоли у того, кто ещё не вошёл.
      if (resp.status === 204 || !resp.ok || generation !== authGeneration) return false;
      const fresh = (await resp.json()) as TokenResponse;
      if (!fresh.access_token || !fresh.user) return false;
      saveAuth(fresh);
      onAuthChanged?.(fresh);
      return true;
    } catch {
      return false;
    }
  }).finally(() => {
    refreshInFlight = null;
  });
  return refreshInFlight;
}

let restoring: Promise<TokenResponse | null> | undefined;

/** Первым зовёт index.tsx, параллельно словарям; провайдер получает тот же ответ, а не второй
 *  обмен токена. */
export function restoreAuth(): Promise<TokenResponse | null> {
  restoring ??= tryRefresh().then((ok) => (ok ? loadAuth() : null));
  return restoring;
}

export async function endSession(): Promise<void> {
  await sessionMutation(async () => {
    const resp = await fetch(`${BASE}/auth/logout`, {
      method: "POST",
      credentials: "same-origin",
      headers: { "Content-Type": "application/json", "X-Auth-Mode": "browser" },
      body: "{}",
      signal: AbortSignal.timeout(8000),
    });
    if (!resp.ok) throw await failure(resp);
    saveAuth(null);
  });
}

export async function authFetch(path: string, init: RequestInit = {}): Promise<Response> {
  const doFetch = async (): Promise<Response> => {
    const auth = loadAuth();
    const headers = new Headers(init.headers);
    if (auth) headers.set("Authorization", `Bearer ${auth.access_token}`);
    return fetch(BASE + path, { ...init, headers });
  };

  let resp = await doFetch();
  if (resp.status === 401 && !path.startsWith("/auth/") && (await tryRefresh())) {
    resp = await doFetch();
  }
  if (resp.status === 401) {
    onUnauthorized?.();
    throw new ApiError(401, i18n.t("errors.sessionExpired"));
  }
  return resp;
}

async function failure(resp: Response): Promise<ApiError> {
  let msg = `${resp.status} ${resp.statusText}`;
  try {
    const body = (await resp.json()) as { error?: string };
    if (body.error) msg = body.error;
  } catch {
    // тело ошибки не JSON — остаётся текст статуса
  }
  return new ApiError(resp.status, msg);
}

export async function request<T>(path: string, init: RequestInit = {}): Promise<T> {
  const resp = await authFetch(path, init);
  if (!resp.ok) throw await failure(resp);
  if (resp.status === 204) return undefined as T;
  return (await resp.json()) as T;
}

/** Ссылка <a download> токен не несёт, поэтому файл забирается fetch-ом и отдаётся как blob. */
export async function downloadFile(path: string, filename: string): Promise<void> {
  const resp = await authFetch(path);
  if (!resp.ok) throw await failure(resp);
  saveFile(filename, await resp.blob());
}

export function postJSON<T>(path: string, body: unknown): Promise<T> {
  return request<T>(path, {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(body),
  });
}

export function putJSON<T>(path: string, body: unknown): Promise<T> {
  return request<T>(path, {
    method: "PUT",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(body),
  });
}

/** В dev `__GATEWAY__` указывает на шлюз; в образе Docker он пуст, и upgrade проксирует nginx. */
export async function wsURL(): Promise<string> {
  const { ticket } = await postJSON<{ ticket: string }>("/ws-ticket", {});
  const gateway = __GATEWAY__ || window.location.origin;
  const gw = gateway.replace(/^http/, "ws").replace(/\/$/, "");
  return `${gw}${BASE}/ws?ticket=${encodeURIComponent(ticket)}`;
}
