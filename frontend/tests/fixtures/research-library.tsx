import "../../src/index.css";
import { useEffect, useId, useState } from "react";
import { createRoot } from "react-dom/client";
import { BrowserRouter, useSearchParams } from "react-router-dom";
import { SunMoon } from "lucide-react";
import { useTranslation } from "react-i18next";
import { initI18n } from "@/shared/i18n";
import { loadable } from "@/shared/lib/loadable";
import { Button } from "@/shared/ui/Button";
import { Switch } from "@/shared/ui/CSwitch";
import { FilterBar } from "@/shared/ui/FilterBar";
import { Label } from "@/shared/ui/Label";
import { PageLayout } from "@/shared/ui/PageLayout";
import { PageTitle } from "@/shared/ui/PageHeader";
import { TooltipProvider } from "@/shared/ui/Tooltip";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/shared/ui/Select";
import { LiveSearchProgress } from "@/features/trend/ui/LiveSearchProgress";
import { useLibraryCompletion } from "@/features/trend/useLibraryCompletion";
import {
  LIVE_SEARCH_STEPS,
  type LiveDetail,
  type LiveSearchState,
  type LiveSearchStep,
} from "@/features/trend/liveApi";
import result from "./trend-live-topic.json";

const LiveDetailView = loadable(() =>
  import("@/features/trend/ui/LiveDetail").then((module) => module.LiveDetailView),
);
const INITIAL: LiveSearchState = {
  query: "Новые технологии хранения энергии",
  status: "running",
  step: "scholar",
  elapsed_seconds: 134,
  visited_steps: ["arxiv", "scholar"],
  found: [],
};

await initI18n();
document.documentElement.dataset.theme = "light";

function Preview() {
  const { t } = useTranslation("trend");
  const [params] = useSearchParams();
  const debug = params.get("debug") === "1";
  const animationId = useId();
  const [state, setState] = useState<LiveSearchState>(INITIAL);
  const [run, setRun] = useState(0);
  const [reconnecting, setReconnecting] = useState(false);
  const [animationPaused, setAnimationPaused] = useState(false);
  const completion = useLibraryCompletion(`${state.query}:${run}`, state);
  useEffect(() => {
    if (completion.finishing) void LiveDetailView.warm();
  }, [completion.finishing]);
  const restart = () => {
    setRun((value) => value + 1);
    setState({ ...INITIAL });
    setReconnecting(false);
    setAnimationPaused(false);
  };
  const pick = (step: LiveSearchStep) => {
    if (state.status !== "running") setRun((value) => value + 1);
    setReconnecting(false);
    setState((value) => ({
      ...value,
      status: "running",
      step,
      wait_seconds: 0,
      visited_steps: [...new Set([...(value.visited_steps ?? []), step])],
      found:
        step === "analysis" || step === "cards"
          ? ["Твердотельные аккумуляторы", "Натрий-ионные батареи"]
          : value.found,
    }));
  };
  return (
    <main>
      <PageLayout className="space-y-3">
        <header>
          <PageTitle>Слабые сигналы</PageTitle>
          {debug && (
            <p className="mt-1 text-xs text-muted-foreground">
              Предпросмотр · демонстрационные данные
            </p>
          )}
        </header>
        {debug && (
          <section className="border-b pb-3" aria-label="Управление предпросмотром">
            <FilterBar size="sm" className="items-center">
              <Select value={state.step} onValueChange={(step) => pick(step as LiveSearchStep)}>
                <SelectTrigger size="sm" className="w-56" aria-label="Этап поиска">
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  {LIVE_SEARCH_STEPS.map((step) => (
                    <SelectItem key={step} value={step}>
                      {t(`study.live.progress.sources.${step}`)}
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
              <Button
                variant="outline"
                size="sm"
                disabled={state.status !== "running"}
                onClick={() => setState((value) => ({ ...value, wait_seconds: 30 }))}
              >
                Пауза источника
              </Button>
              <Button
                variant="outline"
                size="sm"
                disabled={state.status !== "running"}
                aria-pressed={reconnecting}
                onClick={() => setReconnecting(!reconnecting)}
              >
                Потеря связи
              </Button>
              <Button
                size="sm"
                disabled={state.status !== "running"}
                onClick={() => {
                  setReconnecting(false);
                  setState((value) => ({ ...value, status: "ready", wait_seconds: 0 }));
                }}
              >
                Завершить поиск
              </Button>
              <Button variant="outline" size="sm" onClick={restart}>
                Начать заново
              </Button>
              <Button
                variant="outline"
                size="sm"
                onClick={() => {
                  document.documentElement.dataset.theme =
                    document.documentElement.dataset.theme === "dark" ? "light" : "dark";
                }}
              >
                <SunMoon aria-hidden />
                Светлая / тёмная тема
              </Button>
              <div className="flex min-h-control-sm shrink-0 items-center gap-3">
                <Label htmlFor={animationId}>{t("study.live.progress.library.animation")}</Label>
                <Switch
                  id={animationId}
                  checked={!animationPaused}
                  onCheckedChange={(enabled) => setAnimationPaused(!enabled)}
                  aria-describedby={`${animationId}-hint`}
                />
                <span id={`${animationId}-hint`} className="sr-only">
                  {t("study.live.progress.library.animationHint")}
                </span>
              </div>
            </FilterBar>
          </section>
        )}
        {completion.progressState ? (
          <LiveSearchProgress
            key={run}
            state={completion.progressState}
            reconnecting={reconnecting}
            finishing={completion.finishing}
            animationPaused={debug && animationPaused}
            onFinished={completion.onFinished}
          />
        ) : (
          <div className="library-result-reveal py-6">
            <LiveDetailView detail={result as unknown as LiveDetail} onBack={restart} />
          </div>
        )}
      </PageLayout>
    </main>
  );
}

createRoot(document.getElementById("root")!).render(
  <BrowserRouter>
    <TooltipProvider>
      <Preview />
    </TooltipProvider>
  </BrowserRouter>,
);
