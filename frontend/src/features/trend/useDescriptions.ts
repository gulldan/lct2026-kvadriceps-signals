import { useRef } from "react";
import { ApiError } from "@/shared/api/client";
import { useRequest } from "@/shared/lib/useRequest";
import { readDescription, type DescriptionJob } from "./descriptionApi";
import type { AnalysisJob, TrendCandidate } from "./trendApi";

/** Сервер держит немного описаний в работе на человека, поэтому запросы идут по очереди. */
const MAX_PENDING = 12;
const MAX_MISSES = 2;
const POLL_MS = 6000;
const DEADLINE_MS = 15 * 60 * 1000;

export function useDescriptions(job: AnalysisJob | undefined, candidates: TrendCandidate[]) {
  const key =
    job && candidates.length ? `${job.id}/${candidates.map((c) => c.id).join(",")}` : null;
  const state = useRef({
    key: "",
    since: 0,
    started: new Set<string>(),
    failed: new Set<string>(),
    misses: new Map<string, number>(),
    have: {} as Record<string, DescriptionJob>,
  });
  const { data } = useRequest<Record<string, DescriptionJob>>(
    key,
    async (signal) => {
      if (state.current.key !== key)
        state.current = {
          key: key ?? "",
          since: Date.now(),
          started: new Set(),
          failed: new Set(),
          misses: new Map(),
          have: {},
        };
      const { started, failed, misses } = state.current;
      const out = { ...state.current.have };
      let changed = false;
      let pending = Object.values(out).filter((d) => d.status === "prepared").length;
      for (const candidate of candidates) {
        const current = out[candidate.id];
        if ((current && current.status !== "prepared") || failed.has(candidate.id)) continue;
        const fresh = !started.has(candidate.id);
        if (fresh && pending >= MAX_PENDING) continue;
        try {
          const description = await readDescription(job!, candidate, fresh, signal);
          started.add(candidate.id);
          if (
            !current ||
            current.status !== description.status ||
            current.attempt !== description.attempt
          ) {
            out[candidate.id] = description;
            changed = true;
          }
          if (fresh && description.status === "prepared") pending++;
        } catch (error) {
          if (signal.aborted) throw error;
          const count = (misses.get(candidate.id) ?? 0) + 1;
          misses.set(candidate.id, count);
          if (
            error instanceof ApiError &&
            (error.status === 404 || error.status === 429) &&
            count < MAX_MISSES
          )
            started.delete(candidate.id);
          else failed.add(candidate.id);
        }
      }
      // Прежняя ссылка: опрос без новостей не перерисовывает радар.
      if (!changed) return state.current.have;
      state.current.have = out;
      return out;
    },
    {
      keepPrevious: true,
      pollMs: (descriptions) => {
        if (Date.now() - state.current.since > DEADLINE_MS) return 0;
        const unresolved = candidates.some(
          (c) =>
            !state.current.failed.has(c.id) &&
            descriptions[c.id]?.status !== "validated" &&
            descriptions[c.id]?.status !== "template",
        );
        return unresolved ? POLL_MS : 0;
      },
    },
  );
  return data ?? {};
}
