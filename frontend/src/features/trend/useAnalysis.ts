import { useRequest } from "@/shared/lib/useRequest";
import { getAnalysis, type AnalysisJob } from "./trendApi";

/** Поздний ответ никогда не рендерится под ссылкой другого прогона. */
export function useAnalysis(id: string) {
  const { data, error, retry } = useRequest<AnalysisJob>(
    id ? `analysis:${id}` : null,
    (signal) => getAnalysis(id, signal),
    {
      pollMs: (job) => (job.status === "prepared" || job.status === "validated" ? 2000 : 0),
      cacheMs: 10 * 60_000,
    },
  );
  return { job: data, error, retry };
}
