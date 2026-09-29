import { useTranslation } from "react-i18next";
import { ApiError } from "@/shared/api/client";
import { ErrorState } from "@/shared/ui/ErrorState";
import { Spinner } from "@/shared/ui/Spinner";
import { Notice } from "@/shared/ui/Notice";
import type { AnalysisJob } from "../trendApi";

export function AnalysisState({
  job,
  error,
  retry,
  refreshing = false,
}: {
  job?: AnalysisJob;
  error?: unknown;
  retry: () => void;
  refreshing?: boolean;
}) {
  const { t } = useTranslation("trend");
  if (error)
    return (
      <ErrorState
        message={t(
          error instanceof ApiError && [403, 404].includes(error.status)
            ? "unavailable"
            : "loadError",
        )}
        onRetry={retry}
      />
    );
  if (job?.status === "failed")
    return (
      <Notice tone="risk" role="alert">
        {t("failed")}
      </Notice>
    );
  return (
    <output className="flex items-center gap-3 py-10">
      <span aria-hidden="true">
        <Spinner />
      </span>
      {t(job?.status === "validated" ? "validating" : refreshing ? "refreshing" : "loading")}
    </output>
  );
}
