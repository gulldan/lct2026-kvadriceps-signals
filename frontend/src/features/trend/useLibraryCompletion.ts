import { useCallback, useState } from "react";
import type { LiveSearchState } from "./liveApi";

interface LibrarySession {
  key: string;
  running?: LiveSearchState;
  released: boolean;
}

/** Завершаем только ту сцену, которую пользователь видел во время текущего поиска. */
export function useLibraryCompletion(key: string, state?: LiveSearchState) {
  const [session, setSession] = useState<LibrarySession>({ key, released: false });
  let current = session;
  if (session.key !== key) current = { key, released: false };
  if (state?.status === "running" && current.running !== state) {
    current = { key, running: state.reopening ? undefined : state, released: false };
    // Сохранённый ответ не должен вызывать обновление на каждом рендере.
    if (state.reopening && !session.running && session.key === key) current = session;
  } else if (state?.status === "failed" && current.running) {
    current = { key, released: true };
  }
  if (current !== session) setSession(current);

  const running = current.running;
  const finishing = !!(
    key &&
    state?.status === "ready" &&
    !state.reopening &&
    running &&
    !current.released
  );
  const onFinished = useCallback(() => {
    // Запоздавший кадр прежней сцены не может закрыть новый поиск.
    setSession((value) =>
      value.key === key && value.running === running ? { ...value, released: true } : value,
    );
  }, [key, running]);

  return {
    finishing,
    onFinished,
    progressState:
      key && state?.status === "running"
        ? state
        : finishing && running && state
          ? {
              ...running,
              status: "ready" as const,
              // Готовый ответ из кэша не содержит длительность текущего поиска.
              elapsed_seconds: Math.max(state.elapsed_seconds, running.elapsed_seconds),
              found: state.found ?? running.found,
              visited_steps: state.visited_steps ?? running.visited_steps,
              wait_seconds: 0,
            }
          : undefined,
  };
}
