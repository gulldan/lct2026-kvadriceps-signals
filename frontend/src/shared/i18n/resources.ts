// Русский — эталон: из него выводится типизация ключей t(), и на него падает недостающий перевод.
import type ruAdmin from "./locales/ru/admin.json";
import type ruApp from "./locales/ru/app.json";
import type ruAuth from "./locales/ru/auth.json";
import type ruCluster from "./locales/ru/cluster.json";
import type ruCommon from "./locales/ru/common.json";
import type ruDocument from "./locales/ru/document.json";
import type ruHypothesis from "./locales/ru/hypothesis.json";
import type ruHypothesisDetail from "./locales/ru/hypothesisDetail.json";
import type ruKpi from "./locales/ru/kpi.json";
import type ruSearch from "./locales/ru/search.json";
import type ruSummary from "./locales/ru/summary.json";
import type ruTask from "./locales/ru/task.json";
import type ruTrend from "./locales/ru/trend.json";

export const defaultNS = "common";

export type LanguageCode = "ru" | "en" | "zh";
// Без них не нарисовать оболочку, вход, колокольчик и карту направлений; остальное едет с чанком.
export type EagerNamespace = "app" | "auth" | "common" | "task" | "trend";
export type LazyNamespace =
  | "admin"
  | "cluster"
  | "document"
  | "hypothesis"
  | "hypothesisDetail"
  | "kpi"
  | "search"
  | "summary";
export type Namespace = EagerNamespace | LazyNamespace;

export const EAGER_NAMESPACES: EagerNamespace[] = ["app", "auth", "common", "task", "trend"];

type Loader = () => Promise<{ default: object }>;

/** Импорты явные: Vite режет их на чанки, а TS следит за полнотой по языкам и неймспейсам. */
export const BUNDLES: Record<Namespace, Record<LanguageCode, Loader>> = {
  admin: {
    ru: () => import("./locales/ru/admin.json"),
    en: () => import("./locales/en/admin.json"),
    zh: () => import("./locales/zh/admin.json"),
  },
  app: {
    ru: () => import("./locales/ru/app.json"),
    en: () => import("./locales/en/app.json"),
    zh: () => import("./locales/zh/app.json"),
  },
  auth: {
    ru: () => import("./locales/ru/auth.json"),
    en: () => import("./locales/en/auth.json"),
    zh: () => import("./locales/zh/auth.json"),
  },
  common: {
    ru: () => import("./locales/ru/common.json"),
    en: () => import("./locales/en/common.json"),
    zh: () => import("./locales/zh/common.json"),
  },
  document: {
    ru: () => import("./locales/ru/document.json"),
    en: () => import("./locales/en/document.json"),
    zh: () => import("./locales/zh/document.json"),
  },
  hypothesis: {
    ru: () => import("./locales/ru/hypothesis.json"),
    en: () => import("./locales/en/hypothesis.json"),
    zh: () => import("./locales/zh/hypothesis.json"),
  },
  kpi: {
    ru: () => import("./locales/ru/kpi.json"),
    en: () => import("./locales/en/kpi.json"),
    zh: () => import("./locales/zh/kpi.json"),
  },
  search: {
    ru: () => import("./locales/ru/search.json"),
    en: () => import("./locales/en/search.json"),
    zh: () => import("./locales/zh/search.json"),
  },
  task: {
    ru: () => import("./locales/ru/task.json"),
    en: () => import("./locales/en/task.json"),
    zh: () => import("./locales/zh/task.json"),
  },
  cluster: {
    ru: () => import("./locales/ru/cluster.json"),
    en: () => import("./locales/en/cluster.json"),
    zh: () => import("./locales/zh/cluster.json"),
  },
  hypothesisDetail: {
    ru: () => import("./locales/ru/hypothesisDetail.json"),
    en: () => import("./locales/en/hypothesisDetail.json"),
    zh: () => import("./locales/zh/hypothesisDetail.json"),
  },
  summary: {
    ru: () => import("./locales/ru/summary.json"),
    en: () => import("./locales/en/summary.json"),
    zh: () => import("./locales/zh/summary.json"),
  },
  trend: {
    ru: () => import("./locales/ru/trend.json"),
    en: () => import("./locales/en/trend.json"),
    zh: () => import("./locales/zh/trend.json"),
  },
};

export interface Resources {
  admin: typeof ruAdmin;
  app: typeof ruApp;
  auth: typeof ruAuth;
  common: typeof ruCommon;
  document: typeof ruDocument;
  hypothesis: typeof ruHypothesis;
  kpi: typeof ruKpi;
  search: typeof ruSearch;
  task: typeof ruTask;
  cluster: typeof ruCluster;
  hypothesisDetail: typeof ruHypothesisDetail;
  summary: typeof ruSummary;
  trend: typeof ruTrend;
}
