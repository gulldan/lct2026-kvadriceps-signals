import { memo, useCallback, useEffect, useId, useRef, useState, type KeyboardEvent } from "react";
import { Maximize, Minus, Plus } from "lucide-react";
import { useTranslation } from "react-i18next";
import { Button } from "@/shared/ui/Button";
import type { LibraryActivity, LibrarySection } from "../liveSearchProgress";
import type { createLibraryScene } from "./library/scene";
import libraryPoster from "./library/poster.png";
import "./research-library.css";

const FINISH_LIMIT_MS = 9000;
const FOLLOW_ZOOM = 3.25;

export const ResearchLibrary = memo(function ResearchLibrary({
  activity,
  shelf,
  found,
  consulted,
  paused,
  finishing = false,
  onFinished,
}: {
  activity: LibraryActivity;
  shelf?: LibrarySection;
  found: number;
  consulted: number;
  paused: boolean;
  finishing?: boolean;
  onFinished?: () => void;
}) {
  const { t } = useTranslation("trend");
  const hintId = useId();
  // Игровой вид по умолчанию: камера ведёт архивариуса, весь зал — по кнопке.
  const [zoom, setZoom] = useState(FOLLOW_ZOOM);
  const pose = finishing ? "finish" : activity;
  const host = useRef<HTMLDivElement>(null);
  const scene = useRef<ReturnType<typeof createLibraryScene>>(null);
  const latest = useRef({ activity: pose, shelf, found, consulted });
  const playback = useRef({ paused, reduced: false });
  const finishCallback = useRef(onFinished);
  const finishDelivered = useRef(false);
  const [status, setStatus] = useState("loading");
  const [hidden, setHidden] = useState(() => document.hidden);
  const [reduced, setReduced] = useState(
    () => matchMedia("(prefers-reduced-motion: reduce)").matches,
  );
  const lastActivity = useRef(pose);
  if (pose !== "reconnect") lastActivity.current = pose;
  latest.current = { activity: pose, shelf, found, consulted };
  playback.current = { paused: paused || hidden || pose === "reconnect", reduced };
  finishCallback.current = onFinished;
  const finish = useCallback(() => {
    if (latest.current.activity !== "finish" || finishDelivered.current) return;
    finishDelivered.current = true;
    finishCallback.current?.();
  }, []);

  useEffect(() => {
    let cancelled = false;
    // Тяжёлый движок нужен только во время исследования и не входит в начальную загрузку страницы.
    void import("./library/scene")
      .then(({ createLibraryScene }) => {
        if (cancelled || !host.current) return;
        scene.current = createLibraryScene(
          host.current,
          latest.current,
          () => !cancelled && setStatus("fallback"),
          () => !cancelled && finish(),
        );
        scene.current.setPaused(playback.current.paused, playback.current.reduced);
        setStatus("ready");
      })
      .catch(() => {
        if (!cancelled) setStatus("fallback");
      });
    return () => {
      cancelled = true;
      scene.current?.dispose();
      scene.current = null;
    };
  }, [finish]);
  useEffect(() => {
    scene.current?.update(latest.current);
  }, [pose, shelf, found, consulted]);
  useEffect(() => {
    scene.current?.setZoom(zoom);
  }, [zoom, status]);
  useEffect(() => {
    scene.current?.setPaused(playback.current.paused, reduced);
  }, [paused, hidden, pose, reduced]);
  useEffect(() => {
    if (!finishing) {
      finishDelivered.current = false;
      return;
    }
    // Готовая выдача доступна даже при остановившемся движке или медленной загрузке сцены.
    const timer = setTimeout(finish, FINISH_LIMIT_MS);
    return () => clearTimeout(timer);
  }, [finishing, finish]);
  useEffect(() => {
    if (finishing && (paused || hidden || reduced || status === "fallback")) finish();
  }, [finishing, paused, hidden, reduced, status, finish]);
  useEffect(() => {
    const media = matchMedia("(prefers-reduced-motion: reduce)");
    const onMotion = () => setReduced(media.matches);
    const onVisibility = () => setHidden(document.hidden);
    media.addEventListener("change", onMotion);
    document.addEventListener("visibilitychange", onVisibility);
    return () => {
      media.removeEventListener("change", onMotion);
      document.removeEventListener("visibilitychange", onVisibility);
    };
  }, []);

  const moveCamera = (event: KeyboardEvent<HTMLButtonElement>) => {
    if (finishing) return;
    const shifts: Record<string, [number, number]> = {
      ArrowLeft: [64, 0],
      ArrowRight: [-64, 0],
      ArrowUp: [0, 64],
      ArrowDown: [0, -64],
    };
    const shift = shifts[event.key];
    if (shift) {
      event.preventDefault();
      scene.current?.pan(...shift);
    } else if (event.key === "Escape" || event.key === "0") setZoom(1);
    else if (event.key === "+" || event.key === "=") setZoom((value) => Math.min(4, value + 0.75));
    else if (event.key === "-") setZoom((value) => Math.max(1, value - 0.75));
  };

  return (
    <div
      className="research-library"
      data-activity={pose}
      data-pose={lastActivity.current}
      data-paused={playback.current.paused}
      data-reduced-motion={reduced}
      data-shelf={finishing ? "desk" : (shelf ?? "desk")}
      data-renderer={status}
      data-exploring={zoom > 1 && !finishing}
    >
      <img className="library-poster" src={libraryPoster} alt="" />
      <div className="library-scene" ref={host} aria-hidden="true" />
      <span id={hintId} className="sr-only">
        {t("study.live.progress.library.exploreHint")}
      </span>
      {status === "ready" && !finishing && (
        <fieldset
          className="library-camera-tools"
          aria-label={t("study.live.progress.library.camera")}
        >
          <Button
            variant="outline"
            aria-describedby={hintId}
            onKeyDown={moveCamera}
            size="icon"
            aria-label={t("study.live.progress.library.zoomOut")}
            title={t("study.live.progress.library.zoomOut")}
            disabled={zoom === 1}
            onClick={() => setZoom((value) => Math.max(1, value - 0.75))}
          >
            <Minus aria-hidden />
          </Button>
          <Button
            variant="outline"
            aria-describedby={hintId}
            onKeyDown={moveCamera}
            size="icon"
            aria-label={t("study.live.progress.library.zoomIn")}
            title={t("study.live.progress.library.zoomIn")}
            disabled={zoom === 4}
            onClick={() => setZoom((value) => Math.min(4, value + 0.75))}
          >
            <Plus aria-hidden />
          </Button>
          <Button
            variant="outline"
            aria-describedby={hintId}
            onKeyDown={moveCamera}
            size="icon"
            aria-label={t("study.live.progress.library.resetView")}
            title={t("study.live.progress.library.resetView")}
            disabled={zoom === 1}
            onClick={() => setZoom(1)}
          >
            <Maximize aria-hidden />
          </Button>
        </fieldset>
      )}
    </div>
  );
});
