import { useTranslation } from "react-i18next";
import { Notice } from "@/shared/ui/Notice";
import { formatDateTime } from "@/shared/lib/format";
import { liveExamined, STALE_HOURS } from "../liveCoverage";
import type { LiveOverview } from "../liveApi";

export function LiveStale({ overview }: { overview: LiveOverview }) {
  const { t } = useTranslation("trend");
  const { staleHours } = liveExamined(overview);
  if (staleHours === null || staleHours < STALE_HOURS) return null;
  return (
    <Notice tone="risk">
      {t("study.live.examined.stale", {
        date: formatDateTime(overview.observed_at),
        hours: Math.round(staleHours),
      })}
    </Notice>
  );
}
