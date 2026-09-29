import { useRequest } from "@/shared/lib/useRequest";
import { listLiveSearches, type LiveSearchEntry } from "./liveApi";

export function useLiveSearches(): LiveSearchEntry[] {
  const history = useRequest<{ entries: LiveSearchEntry[] }>("live-searches", listLiveSearches, {
    cacheMs: 30_000,
  });
  return history.data?.entries ?? [];
}
