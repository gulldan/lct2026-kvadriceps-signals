import "../index.css";
import { lazy, StrictMode, Suspense } from "react";
import { createRoot } from "react-dom/client";
import { ArrowUpRight } from "lucide-react";

import { initI18n, loadNamespace } from "@/shared/i18n";
import { Button } from "@/shared/ui/Button";
import { LanguageSwitcher } from "@/shared/ui/LanguageSwitcher";
import { ThemeSwitcher } from "@/shared/ui/ThemeSwitcher";
import { PageHeader } from "@/shared/ui/PageHeader";
import { Toaster } from "@/shared/ui/Sonner";
import { TooltipProvider } from "@/shared/ui/Tooltip";
import { Foundations } from "./Foundations";
import { Controls } from "./Controls";
import { OverlaysFeedback } from "./OverlaysFeedback";
import { ServiceStates } from "./ServiceStates";
const DataPatterns = lazy(() =>
  import("./DataPatterns").then((module) => ({ default: module.DataPatterns })),
);
const TrendRadarFixtures = lazy(() =>
  import("./TrendRadarFixtures").then((module) => ({ default: module.TrendRadarFixtures })),
);
const LiveSignalFixtures = lazy(() =>
  import("./LiveSignalFixtures").then((module) => ({ default: module.LiveSignalFixtures })),
);

await initI18n();
// Словари экранов выдачи и просмотрщика ленивые: без них вместо подписей видны ключи.
await loadNamespace("document");

const sections = [
  ["tokens", "Палитра"],
  ["typography", "Типографика"],
  ["buttons", "Действия"],
  ["badges", "Статусы"],
  ["forms", "Поля"],
  ["filter-bars", "Панели фильтров"],
  ["navigation", "Навигация"],
  ["surfaces", "Композиция"],
  ["data", "Данные"],
  ["content", "Текст"],
  ["export", "Выгрузка"],
  ["overlays", "Слои"],
  ["feedback", "Состояния"],
  ["service-states", "Ошибки"],
  ["search-progress", "Длительный поиск"],
  ["trend-fixtures", "Источники трендов"],
  ["live-signals", "Слабые сигналы"],
];

function UiKit() {
  return (
    <TooltipProvider>
      <a
        href="#kit-main"
        className="sr-only focus:not-sr-only focus:fixed focus:top-4 focus:left-4 focus:z-(--layer-overlay) focus:bg-card focus:p-3"
      >
        К компонентам
      </a>
      <header className="border-b bg-card">
        <div className="mx-auto flex max-w-7xl items-center justify-between gap-4 px-4 py-4 sm:px-8">
          <a className="text-lg font-semibold tracking-tight" href="#kit-main">
            RAG <span className="font-normal text-muted-foreground">/ UI kit</span>
          </a>
          <div className="flex items-center gap-2">
            <ThemeSwitcher />
            <LanguageSwitcher />
            <Button asChild variant="ghost" size="sm">
              <a href="/">
                Продукт
                <ArrowUpRight className="size-3.5" aria-hidden />
              </a>
            </Button>
          </div>
        </div>
      </header>
      <div className="mx-auto max-w-7xl px-4 sm:px-8">
        <div className="py-9 sm:py-12">
          <PageHeader
            title="Общий язык интерфейса"
            description="Токены, компоненты и состояния исследовательской платформы."
          />
          <p className="mt-4 max-w-2xl text-sm leading-relaxed text-muted-foreground">
            Живой каталог для разработки. Все образцы используют компоненты продукта. Изменение
            общего компонента или токена сразу отражается здесь и в приложении.
          </p>
        </div>
        <nav
          aria-label="Разделы UI kit"
          className="sticky top-0 z-(--layer-sticky) -mx-4 overflow-x-auto border-y bg-background px-4 sm:-mx-8 sm:px-8"
        >
          <div className="flex w-max gap-6">
            {sections.map(([id, label]) => (
              <a
                key={id}
                href={`#${id}`}
                className="inline-flex h-12 items-center text-sm text-muted-foreground underline-offset-8 hover:text-foreground hover:underline"
              >
                {label}
              </a>
            ))}
          </div>
        </nav>
        <main id="kit-main" tabIndex={-1}>
          <Foundations />
          <Controls />
          <Suspense
            fallback={<p className="py-6 text-muted-foreground">Загрузка примеров данных…</p>}
          >
            <DataPatterns />
          </Suspense>
          <OverlaysFeedback />
          <ServiceStates />
          <Suspense
            fallback={<p className="py-6 text-muted-foreground">Загрузка примеров источников…</p>}
          >
            <TrendRadarFixtures />
          </Suspense>
          <Suspense
            fallback={<p className="py-6 text-muted-foreground">Загрузка примеров выдачи…</p>}
          >
            <LiveSignalFixtures />
          </Suspense>
        </main>
        <footer className="border-t py-6 text-xs text-muted-foreground">
          Развивайте общие компоненты в shared/ui и токены в shared/design-system. Правила и API — в
          DESIGN.md и frontend/docs/UI_KIT.md.
        </footer>
      </div>
      <Toaster />
    </TooltipProvider>
  );
}

const container = document.getElementById("root");
if (!container) throw new Error("UI kit root not found");
createRoot(container).render(
  <StrictMode>
    <UiKit />
  </StrictMode>,
);
