import { useEffect, useState } from "react";
import { ApiError } from "@/shared/api/client";
import { useRequest } from "@/shared/lib/useRequest";
import { searchLive, type LiveSearchState } from "./liveApi";

/** Один живой поиск за раз: второй запрос получает 429, и очередь держит клиент. */
const BUSY_RETRY_MS = 5000;

/** Ответ уже на экране и весит мегабайт: дооценку новой моделью забираем реже, чем идёт поиск. */
const RESCORING_POLL_MS = 5000;

export function useLiveSearch(query: string) {
  // Иначе ответ, давно готовый на сервере, висел бы до возврата во вкладку.
  const { data, error, retry, status } = useRequest<LiveSearchState>(
    query || null,
    (signal) => searchLive(query, signal),
    {
      // Открытие сохранённого ответа укладывается в секунды, а поиск идёт минуты.
      pollMs: (value) =>
        value.status === "running"
          ? value.reopening
            ? 1000
            : 2000
          : value.detail?.rescoring || value.detail?.digests_pending
            ? RESCORING_POLL_MS
            : 0,
      background: true,
      keepPrevious: true,
    },
  );
  // Готовый ответ остаётся виден; фоновые повторы ведёт общий опрос без сброса его данных.
  const ready = status !== "loading" && data?.status === "ready" && !!data.detail;
  const busy = error instanceof ApiError && error.status === 429 && !ready;
  const [waited, setWaited] = useState(0);
  useEffect(() => {
    if (!busy) {
      setWaited(0);
      return;
    }
    const timer = setTimeout(() => {
      setWaited((value) => value + BUSY_RETRY_MS / 1000);
      retry();
    }, BUSY_RETRY_MS);
    return () => clearTimeout(timer);
  }, [busy, waited, retry]);
  // При смене запроса прежний ответ не относится к новой строке поиска.
  return {
    state: status === "loading" ? undefined : data,
    error: busy ? undefined : error,
    busy,
    waited,
    retry,
  };
}
