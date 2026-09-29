/** Стартовый скрипт index.html читает тот же ключ, чтобы страница не вспыхивала светлым. */
export const THEMES = ["system", "light", "dark"] as const;
export type Theme = (typeof THEMES)[number];
const STORAGE_KEY = "rag.theme";
export const THEME_EVENT = "rag:theme";

export function currentTheme(): Theme {
  try {
    const stored = localStorage.getItem(STORAGE_KEY);
    return THEMES.find((theme) => theme === stored) ?? "system";
  } catch {
    return "system";
  }
}

function resolvedTheme(theme = currentTheme()): "light" | "dark" {
  if (theme !== "system") return theme;
  return typeof matchMedia === "function" && matchMedia("(prefers-color-scheme: dark)").matches
    ? "dark"
    : "light";
}

function applyTheme(theme: Theme) {
  const root = document.documentElement;
  if (theme === "system") delete root.dataset.theme;
  else root.dataset.theme = theme;
  window.dispatchEvent(new CustomEvent(THEME_EVENT, { detail: resolvedTheme(theme) }));
}

/** Строгая CSP запрещает inline-скрипты, и стартовый скрипт index.html мог не выполниться. */
export function bootTheme() {
  applyTheme(currentTheme());
}

export function setTheme(theme: Theme) {
  try {
    localStorage.setItem(STORAGE_KEY, theme);
  } catch {
    // Без хранилища тема живёт до перезагрузки — это лучше, чем ошибка.
  }
  applyTheme(theme);
}

export function watchSystemTheme() {
  if (typeof matchMedia !== "function") return () => {};
  const media = matchMedia("(prefers-color-scheme: dark)");
  const notify = () => {
    if (currentTheme() === "system") applyTheme("system");
  };
  media.addEventListener("change", notify);
  return () => media.removeEventListener("change", notify);
}
