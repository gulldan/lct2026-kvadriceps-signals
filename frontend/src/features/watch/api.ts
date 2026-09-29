import { postJSON, request } from "@/shared/api/client";
import { useRequest } from "@/shared/lib/useRequest";

export type WatchKind = "topic" | "query" | "bet";
type WatchStage = "emerging_signal" | "early_signal" | "established" | "unassessed";

interface WatchShift {
  name?: string;
  from: WatchStage;
  to: WatchStage;
}

export interface WatchDigest {
  technologies?: string[];
  stage?: WatchShift;
  stages?: WatchShift[];
  sources?: number;
  market?: number;
  bet?: { status: "confirming" | "unchanged" | "weakening"; changes: string[] };
}

export interface Watch {
  kind: WatchKind;
  target: string;
  title: string;
  created_at: string;
  digest?: WatchDigest;
  digest_at?: string;
}

type WatchSubject = Pick<Watch, "kind" | "target">;

const PATH = "/trends/live/watches";
// Сводка появляется раз в сутки: частый опрос ничего бы не прибавил.
const POLL_MS = 5 * 60_000;

function listWatches(signal?: AbortSignal) {
  return request<{ watches: Watch[] }>(PATH, { signal });
}

export function addWatch(subject: WatchSubject & Pick<Watch, "title">) {
  return postJSON<Watch>(PATH, subject);
}

export function removeWatch({ kind, target }: WatchSubject) {
  return request<void>(`${PATH}?${new URLSearchParams({ kind, target })}`, { method: "DELETE" });
}

export function ackWatch({ kind, target }: WatchSubject) {
  return postJSON<void>(`${PATH}/ack`, { kind, target });
}

export function watchLink({ kind, target }: WatchSubject) {
  if (kind === "bet") return `/hypotheses/${encodeURIComponent(target)}`;
  return `/trends?${new URLSearchParams(kind === "topic" ? { topic: target } : { q: target })}`;
}

/** Запрос радар хранит строчными и через один пробел — так же он сравнивается. */
const norm = (value: string) => value.toLowerCase().split(/\s+/).filter(Boolean).join(" ");

export function sameSubject(watch: WatchSubject, subject: WatchSubject) {
  return (
    watch.kind === subject.kind &&
    watch.target === (subject.kind === "query" ? norm(subject.target) : subject.target)
  );
}

/** Ключ один на колокольчик и кнопку карточки: запрос в полёте они делят. */
export function useWatches(enabled = true) {
  const state = useRequest(enabled ? "live-watches" : null, listWatches, {
    pollMs: POLL_MS,
    keepPrevious: true,
  });
  return {
    watches: state.data?.watches ?? [],
    ready: state.data !== undefined,
    refresh: state.retry,
  };
}
