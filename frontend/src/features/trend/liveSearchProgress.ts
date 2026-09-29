import { LIVE_SEARCH_STEPS, type LiveSearchState, type LiveSearchStep } from "./liveApi";

export const LIBRARY_SECTIONS = ["science", "engineering", "world", "corpus"] as const;
export type LibrarySection = (typeof LIBRARY_SECTIONS)[number];
export type LibraryActivity =
  | "prepare"
  | "collect"
  | "analyze"
  | "write"
  | "wait"
  | "reconnect"
  | "finish";

const SHELVES: Partial<Record<NonNullable<LiveSearchState["step"]>, LibrarySection>> = {
  arxiv: "science",
  scholar: "science",
  github: "engineering",
  hn: "engineering",
  patents: "world",
  web: "world",
  registry: "world",
  corpus: "corpus",
};

export function libraryProgress(state: LiveSearchState, waiting: boolean, reconnecting: boolean) {
  const shelf = state.step ? SHELVES[state.step] : undefined;
  const activity: LibraryActivity = reconnecting
    ? "reconnect"
    : waiting
      ? "wait"
      : shelf
        ? "collect"
        : state.step === "analysis" || state.step === "rescore"
          ? "analyze"
          : state.step === "cards"
            ? "write"
            : "prepare";
  // Старый сервер не присылает историю: порядок плана не доказывает, что источник опрашивался.
  const visited = new Set<LiveSearchStep>(state.visited_steps ?? []);
  if (state.step && state.step !== "rescore") visited.add(state.step);
  const consulted = LIVE_SEARCH_STEPS.filter(
    (step) => visited.has(step) && step !== state.step && SHELVES[step],
  );
  return { activity, shelf, visited, consulted };
}
