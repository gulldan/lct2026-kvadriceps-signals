// Несуществующий ключ в t() — ошибка компиляции, а не молчаливый фолбэк в рантайме.
import type { defaultNS, Resources } from "./resources";

declare module "i18next" {
  interface CustomTypeOptions {
    defaultNS: typeof defaultNS;
    resources: Resources;
  }
}
