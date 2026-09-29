import { createInstance } from "i18next";
import { initReactI18next } from "react-i18next";

import {
  BUNDLES,
  defaultNS,
  EAGER_NAMESPACES,
  type LanguageCode,
  type LazyNamespace,
  type Namespace,
} from "./resources";

export type { LazyNamespace, LanguageCode };

export const LANGUAGES = [
  { code: "ru", nativeLabel: "Русский", bcp47: "ru-RU" },
  { code: "en", nativeLabel: "English", bcp47: "en-US" },
  { code: "zh", nativeLabel: "中文", bcp47: "zh-CN" },
] as const satisfies readonly { code: LanguageCode; nativeLabel: string; bcp47: string }[];

const STORAGE_KEY = "rag.lang";
const FALLBACK: LanguageCode = "ru";

export const i18n = createInstance();

function isSupported(code: string | null | undefined): code is LanguageCode {
  return LANGUAGES.some((l) => l.code === code);
}

// Без автоопределения: англоязычный браузер не должен молча переключать интерфейс.
function detectLanguage(): LanguageCode {
  try {
    const saved = localStorage.getItem(STORAGE_KEY);
    if (isSupported(saved)) return saved;
  } catch {
    /* приватный режим без localStorage — используем язык по умолчанию */
  }
  return FALLBACK;
}

export function currentLanguage(): LanguageCode {
  const base = (i18n.language ?? FALLBACK).slice(0, 2);
  return isSupported(base) ? base : FALLBACK;
}

export function currentLocale(): string {
  return LANGUAGES.find((l) => l.code === currentLanguage())?.bcp47 ?? "ru-RU";
}

const loaded = new Map<string, Promise<void>>();
const opened = new Set<Namespace>(EAGER_NAMESPACES);

function ensure(lang: LanguageCode, namespaces: Iterable<Namespace>): Promise<void> {
  const jobs: Promise<void>[] = [];
  for (const ns of namespaces) {
    const key = `${lang}/${ns}`;
    let job = loaded.get(key);
    if (!job) {
      job = BUNDLES[ns][lang]().then((bundle) => {
        i18n.addResourceBundle(lang, ns, bundle.default, true, true);
      });
      loaded.set(key, job);
    }
    jobs.push(job);
  }
  return Promise.all(jobs).then(() => undefined);
}

/** Плюс эталонный русский: недостающий перевод падает на него, а не на сырой ключ. */
function ensureLanguage(lang: LanguageCode, namespaces: Iterable<Namespace>): Promise<void> {
  const list = [...namespaces];
  return lang === FALLBACK
    ? ensure(lang, list)
    : Promise.all([ensure(lang, list), ensure(FALLBACK, list)]).then(() => undefined);
}

// Словари едут по сети: ранний выбор, догрузившись позже нового, возвращал прежний язык.
let requested: LanguageCode | undefined;

export function setLanguage(code: LanguageCode): void {
  requested = code;
  void ensureLanguage(code, opened).then(() => {
    if (requested !== code) return;
    // lang меняется вместе с текстом: иначе меню, открытое во время загрузки, пропадало.
    document.documentElement.lang = code;
    return i18n.changeLanguage(code);
  });
  try {
    localStorage.setItem(STORAGE_KEY, code);
  } catch {
    /* без localStorage выбор живёт до перезагрузки */
  }
}

let initialization: Promise<void> | undefined;

/** Ждать обязательно: до завершения t() отдаёт сырые ключи. */
export function initI18n(): Promise<void> {
  initialization ??= (async () => {
    const lng = detectLanguage();
    await i18n.use(initReactI18next).init({
      resources: {},
      defaultNS,
      ns: Object.keys(BUNDLES),
      lng,
      fallbackLng: FALLBACK,
      interpolation: { escapeValue: false }, // React сам экранирует
      returnEmptyString: false,
      // Незнакомое значение перечисления печатается как есть, а не путём ключа.
      parseMissingKeyHandler: (key, fallback) => fallback ?? key.split(".").pop() ?? key,
    });
    await ensureLanguage(lng, EAGER_NAMESPACES);
    document.documentElement.lang = currentLanguage();
  })();
  return initialization;
}

export function loadNamespace(ns: LazyNamespace): Promise<void> {
  opened.add(ns);
  return ensureLanguage(currentLanguage(), [ns]);
}
