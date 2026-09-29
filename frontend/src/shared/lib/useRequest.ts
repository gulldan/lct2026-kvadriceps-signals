import { useCallback, useEffect, useRef, useState } from "react";

type RequestStatus = "idle" | "loading" | "ready" | "error";

export interface RequestState<T> {
  status: RequestStatus;
  data?: T;
  error?: unknown;
  retry: () => void;
}

interface RequestOptions<T> {
  pollMs?: number | ((data: T) => number);
  keepPrevious?: boolean;
  /** Окончательный ответ переживает размонтирование: возврат на страницу не тянет JSON заново. */
  cacheMs?: number;
  /** Кто ждёт ответа в другом окне: иначе готовый на сервере ответ не приходил до возврата. */
  background?: boolean;
}

const cache = new Map<string, { data: unknown; at: number }>();

// Один ключ — один запрос в полёте: компоненты делят ответ; отмена — когда отписался последний.
interface Inflight {
  promise: Promise<unknown>;
  controller: AbortController;
  refs: number;
}
const inflight = new Map<string, Inflight>();

function share<T>(key: string, fetcher: (signal: AbortSignal) => Promise<T>): Inflight {
  let entry = inflight.get(key);
  if (!entry) {
    const controller = new AbortController();
    const created: Inflight = { controller, refs: 0, promise: Promise.resolve() };
    created.promise = fetcher(controller.signal).finally(() => {
      if (inflight.get(key) === created) inflight.delete(key);
    });
    inflight.set(key, created);
    entry = created;
  }
  entry.refs += 1;
  return entry;
}

function release(entry: Inflight) {
  entry.refs -= 1;
  if (entry.refs <= 0) entry.controller.abort();
}

/** Ссылка намеренно не отпускается: иначе размонтирование оборвало бы прогрев. */
export function prefetch<T>(key: string, fetcher: (signal: AbortSignal) => Promise<T>): void {
  if (cache.has(key) || inflight.has(key)) return;
  void share(key, fetcher)
    .promise.then((data) => cache.set(key, { data, at: Date.now() }))
    .catch(() => undefined);
}

const MAX_RETRY_MS = 30_000;

const FIRST_TRY_RETRIES = 3;
const FIRST_TRY_MS = 2000;

export function retryDelay(delay: number, failures: number): number {
  return failures > 0 ? Math.min(delay * 2 ** failures, MAX_RETRY_MS) : delay;
}

export function useRequest<T>(
  key: string | null,
  fetcher: (signal: AbortSignal) => Promise<T>,
  options: RequestOptions<T> = {},
): RequestState<T> {
  const [state, setState] = useState<{
    key: string | null;
    status: RequestStatus;
    data?: T;
    error?: unknown;
  }>({
    key: null,
    status: "idle",
  });
  const [attempt, setAttempt] = useState(0);
  const fetcherRef = useRef(fetcher);
  fetcherRef.current = fetcher;
  const optionsRef = useRef(options);
  optionsRef.current = options;

  useEffect(() => {
    if (key === null) return;
    let cancelled = false;
    let flight: Inflight | undefined;
    let timer: ReturnType<typeof setTimeout> | undefined;
    let last: T | undefined;
    let lastJson: string | undefined;
    let failures = 0;
    const delayFor = (data: T) => {
      const poll = optionsRef.current.pollMs;
      return typeof poll === "function" ? poll(data) : (poll ?? 0);
    };
    const schedule = () => {
      // Первый опрос мог не дойти, пока поиск на сервере идёт: число таких попыток ограничено.
      const delay =
        last === undefined
          ? failures > 0 && failures <= FIRST_TRY_RETRIES && optionsRef.current.pollMs !== undefined
            ? FIRST_TRY_MS
            : 0
          : delayFor(last);
      if (delay > 0 && (!document.hidden || optionsRef.current.background))
        timer = setTimeout(() => void run(), retryDelay(delay, failures));
    };
    const run = async () => {
      clearTimeout(timer);
      try {
        flight = share(key, fetcherRef.current);
        const data = (await flight.promise) as T;
        if (cancelled) return;
        // Каждый ответ — новый объект: по ссылке ни один тик опроса не совпадал с прошлым.
        const json = JSON.stringify(data);
        const unchanged = last !== undefined && json === lastJson && failures === 0;
        failures = 0;
        last = data;
        lastJson = json;
        const ttl = optionsRef.current.cacheMs ?? 0;
        if (ttl > 0) cache.set(key, { data, at: Date.now() });
        if (!unchanged) setState({ key, status: "ready", data });
        schedule();
      } catch (error) {
        if (cancelled) return;
        failures += 1;
        // Обрыв сети не заканчивает опрос: работа на сервере идёт дальше.
        setState({
          key,
          status: "error",
          error,
          data: optionsRef.current.keepPrevious ? last : undefined,
        });
        schedule();
      } finally {
        if (flight) release(flight);
        flight = undefined;
      }
    };
    // Только опрашиваемое: иначе возврат на вкладку давал залп запросов по всем хукам страницы.
    const onVisible = () => {
      if (!document.hidden && last !== undefined && delayFor(last) > 0) void run();
    };
    document.addEventListener("visibilitychange", onVisible);
    const hit = cache.get(key);
    if (hit && Date.now() - hit.at < (optionsRef.current.cacheMs ?? 0)) {
      last = hit.data as T;
      setState({ key, status: "ready", data: last });
      // Снимок из кэша виден сразу, а опрашиваемый ключ тут же уходит за свежим ответом.
      if (delayFor(last) > 0) void run();
    } else {
      setState((current) => ({
        key,
        status: "loading",
        data: optionsRef.current.keepPrevious ? current.data : undefined,
      }));
      void run();
    }
    return () => {
      cancelled = true;
      if (flight) release(flight);
      flight = undefined;
      clearTimeout(timer);
      document.removeEventListener("visibilitychange", onVisible);
    };
  }, [key, attempt]);

  const retry = useCallback(() => setAttempt((value) => value + 1), []);
  if (key === null) return { status: "idle", retry };
  if (state.key !== key)
    return { status: "loading", data: options.keepPrevious ? state.data : undefined, retry };
  return { status: state.status, data: state.data, error: state.error, retry };
}
