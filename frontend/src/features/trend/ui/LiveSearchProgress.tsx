import { BookOpen, Clock3 } from "lucide-react";
import { useEffect, useLayoutEffect, useRef, useState } from "react";
import { useTranslation } from "react-i18next";

import { Button } from "@/shared/ui/Button";
import type { LiveSearchState } from "../liveApi";
import { libraryProgress } from "../liveSearchProgress";
import { spellAcronyms } from "../signalSources";
import { ResearchLibrary } from "./ResearchLibrary";
import { LibraryStagePanel } from "./LibraryStagePanel";

// Сервер обрывает сбор на 12-й минуте (liveSearchBudget), выдача готова к 12:10–12:41. Предупреждать
// раньше значит пугать обычным поиском, позже — молчать о настоящей задержке.
const LONG_SEARCH_MINUTES = 14;

function duration(seconds: number) {
  const whole = Math.max(0, Math.floor(seconds));
  return `${Math.floor(whole / 60)}:${String(whole % 60).padStart(2, "0")}`;
}

/** Каждый ответ привязывает часы заново: тики не прибавляются к уже обновлённому времени сервера. */
function useProgressClock(state: LiveSearchState) {
  const [clock, setClock] = useState(() => ({ state, at: Date.now(), now: Date.now() }));
  useEffect(() => {
    const now = Date.now();
    setClock({ state, at: now, now });
  }, [state]);
  useEffect(() => {
    // В фоновой вкладке таймер замедляется, поэтому считаем по часам, а не по числу тиков.
    const timer = setInterval(
      () => setClock((previous) => ({ ...previous, now: Date.now() })),
      1000,
    );
    return () => clearInterval(timer);
  }, []);
  const since = clock.state === state ? Math.max(0, Math.floor((clock.now - clock.at) / 1000)) : 0;
  return {
    elapsed: Math.max(0, state.elapsed_seconds + (state.status === "running" ? since : 0)),
    left: Math.max(0, (state.wait_seconds ?? 0) - since),
  };
}

/** Высота учитывает шапку приложения и реальные отступы контейнеров. */
function useRemainingViewport() {
  const ref = useRef<HTMLElement>(null);
  useLayoutEffect(() => {
    const node = ref.current;
    if (!node) return;
    const ancestors: HTMLElement[] = [];
    for (let parent = node.parentElement; parent; parent = parent.parentElement)
      ancestors.push(parent);
    let alive = true;
    const measure = () => {
      if (!alive) return;
      const bottom = ancestors.reduce((space, ancestor) => {
        const style = getComputedStyle(ancestor);
        return (
          space +
          (parseFloat(style.paddingBottom) || 0) +
          (parseFloat(style.borderBottomWidth) || 0) +
          (parseFloat(style.marginBottom) || 0)
        );
      }, 0);
      const top = node.getBoundingClientRect().top + window.scrollY;
      const viewport = window.visualViewport?.height ?? window.innerHeight;
      const height = `${Math.max(0, Math.floor(viewport - top - bottom))}px`;
      if (node.style.blockSize !== height) node.style.blockSize = height;
    };
    const observer = new ResizeObserver(measure);
    for (const ancestor of ancestors) observer.observe(ancestor);
    window.addEventListener("resize", measure);
    window.visualViewport?.addEventListener("resize", measure);
    void document.fonts.ready.then(measure);
    measure();
    return () => {
      alive = false;
      observer.disconnect();
      window.removeEventListener("resize", measure);
      window.visualViewport?.removeEventListener("resize", measure);
    };
  }, []);
  return ref;
}

export function LiveSearchProgress({
  state,
  reconnecting = false,
  finishing = false,
  animationPaused = false,
  onFinished,
}: {
  state: LiveSearchState;
  reconnecting?: boolean;
  finishing?: boolean;
  animationPaused?: boolean;
  onFinished?: () => void;
}) {
  const { t } = useTranslation("trend");
  const viewport = useRemainingViewport();
  const { elapsed, left } = useProgressClock(state);
  const library = libraryProgress(state, left > 0, reconnecting);
  const overdue = !finishing && !state.reopening && elapsed >= LONG_SEARCH_MINUTES * 60;
  const query = spellAcronyms(state.query);
  const stage = state.step
    ? t(`study.live.progress.steps.${state.step}`)
    : t("study.live.progress.preparing");

  return (
    <section ref={viewport} className="library-progress" aria-label={t("study.live.progress.plan")}>
      <header className="library-progress-header">
        {/* Секундомер вынесен из живой области: озвучиваются только смены этапа. */}
        <output aria-live="polite" aria-atomic="true" className="min-w-0">
          <h2
            className="line-clamp-2 text-base leading-snug font-medium wrap-anywhere"
            title={query}
          >
            {finishing
              ? t("study.live.progress.library.completeTitle", { query })
              : state.reopening
                ? t("study.live.progress.reopening", { query })
                : t("study.live.progress.title", { query })}
          </h2>
          <p className="mt-1 text-sm leading-snug text-muted-foreground first-letter:uppercase">
            {finishing
              ? t("study.live.progress.library.ready")
              : reconnecting
                ? t("study.live.progress.reconnectingTitle")
                : stage}
          </p>
        </output>
        <div className="library-progress-tools">
          <p className="flex items-center gap-1.5 text-xs text-muted-foreground" aria-live="off">
            <Clock3 className="size-3.5 shrink-0" aria-hidden />
            <time
              className="font-mono tabular-nums"
              dateTime={`PT${Math.floor(elapsed)}S`}
              aria-label={t("study.live.progress.elapsed", { seconds: Math.floor(elapsed) })}
            >
              {duration(elapsed)}
            </time>
            {!state.reopening && !finishing && (
              <>
                <span aria-hidden>·</span>
                <span>{t("study.live.progress.budgetValue")}</span>
              </>
            )}
          </p>
          {!state.reopening && finishing && (
            <Button onClick={onFinished}>
              <BookOpen aria-hidden />
              {t("study.live.progress.library.showResults")}
            </Button>
          )}
        </div>
        {!finishing && !reconnecting && left > 0 ? (
          <p className="library-progress-note text-xs" aria-live="off">
            {t("study.live.progress.library.waitShort", {
              source: t(`study.live.progress.sources.${state.step ?? "arxiv"}`, {
                defaultValue: stage,
              }),
              seconds: left,
            })}
          </p>
        ) : overdue && !reconnecting ? (
          <output aria-live="polite" className="library-progress-note text-xs">
            {t("study.live.progress.library.overdueShort", { minutes: LONG_SEARCH_MINUTES })}
          </output>
        ) : state.reopening ? (
          <p className="library-progress-note text-xs">
            {t("study.live.progress.library.savedHintShort")}
          </p>
        ) : !finishing && state.found?.length ? (
          // Число прошедших проверку растёт на этапах анализа и карточек, где этап минутами не меняется.
          <p className="library-progress-note text-xs" aria-live="off">
            {t("study.live.progress.foundCount", { count: state.found.length })}.{" "}
            {t("study.live.progress.foundHint")}
          </p>
        ) : null}
      </header>

      {!state.reopening && (
        <div className="library-progress-body">
          <ResearchLibrary
            activity={library.activity}
            shelf={library.shelf}
            found={state.found?.length ?? 0}
            consulted={library.consulted.length}
            paused={animationPaused}
            finishing={finishing}
            onFinished={onFinished}
          />
          <LibraryStagePanel step={state.step} visited={library.visited} finishing={finishing} />
        </div>
      )}
    </section>
  );
}
