import { useRef } from "react";
import { useRequest } from "@/shared/lib/useRequest";
import { readDescription, type DescriptionJob } from "./descriptionApi";
import type { AnalysisJob, TrendCandidate } from "./trendApi";

/** Хеш подтверждений в ключе: описание не подменяет факты другого прогона. Первый запрос ставит
 *  работу, опросы только читают. */
export function useDescription(job?: AnalysisJob, candidate?: TrendCandidate) {
  const key =
    job?.result && candidate ? `${job.id}/${candidate.id}/${candidate.brief.evidence_sha256}` : "";
  const started = useRef("");
  const { data, status } = useRequest<DescriptionJob>(
    key || null,
    (signal) => {
      const start = started.current !== key;
      started.current = key;
      return readDescription(job!, candidate!, start, signal);
    },
    { pollMs: (description) => (description.status === "prepared" ? 2000 : 0) },
  );
  return { key, description: data, failed: status === "error" };
}
