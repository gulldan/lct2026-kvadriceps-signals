import { BASE, loadAuth } from "@/shared/api/client";

const seen = new Set<string>();
const LIMIT = 20;

export function reportError(kind: string, error: unknown, extra: Record<string, unknown> = {}) {
  const message = error instanceof Error ? error.message : String(error);
  const stack = error instanceof Error ? (error.stack ?? "") : "";
  const signature = `${kind}:${message}`;
  const auth = loadAuth();
  if (seen.has(signature) || seen.size >= LIMIT || !auth) return;
  seen.add(signature);
  const body = JSON.stringify({
    kind,
    message: message.slice(0, 500),
    stack: stack.slice(0, 4000),
    url: location.pathname + location.search,
    user_agent: navigator.userAgent,
    extra,
  });
  // Мимо authFetch: там 401 завершает сессию, и фоновый отчёт выкидывал бы человека на вход.
  void fetch(`${BASE}/client-errors`, {
    method: "POST",
    headers: {
      "Content-Type": "application/json",
      Authorization: `Bearer ${auth.access_token}`,
    },
    body,
    keepalive: true,
  }).catch(() => {
    // Мониторинг не должен порождать вторую ошибку.
  });
}

export function installMonitor() {
  window.addEventListener("error", (event) =>
    reportError("window.error", event.error ?? event.message),
  );
  window.addEventListener("unhandledrejection", (event) =>
    reportError("unhandledrejection", event.reason),
  );
  // После выкладки открытая вкладка просит чанк, которого больше нет: перезагружаемся один раз.
  window.addEventListener("vite:preloadError", (event) => {
    event.preventDefault();
    if (sessionStorage.getItem("rag.reloaded") === location.href) return;
    sessionStorage.setItem("rag.reloaded", location.href);
    location.reload();
  });
}
