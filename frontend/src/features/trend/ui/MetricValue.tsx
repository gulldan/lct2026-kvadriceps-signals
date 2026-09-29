import { useTranslation } from "react-i18next";
import { InfoHint } from "@/shared/ui/InfoHint";
import { cn } from "@/shared/lib/cn";
import { metricReasons, metricState } from "../metricState";
import type { MetricEnvelope } from "../technologyApi";

export function MetricValue({
  metric,
  format,
  className,
}: {
  metric: MetricEnvelope;
  format?: (value: number) => string;
  className?: string;
}) {
  const { t } = useTranslation("trend");
  const state = metricState(metric);
  if (state === "measured") {
    const value = metric.value as number;
    return (
      <span className={cn("inline-flex items-baseline gap-1 tabular-nums", className)}>
        <span className="font-mono">{format ? format(value) : String(value)}</span>
        {metric.unit && <span className="text-xs text-muted-foreground">{metric.unit}</span>}
        {value === 0 && (
          <InfoHint label={t("metrics.state.zeroHint")}>{t("metrics.state.zero")}</InfoHint>
        )}
      </span>
    );
  }
  const reasons = metricReasons(metric);
  return (
    <span className={cn("inline-flex items-center gap-1 text-muted-foreground", className)}>
      <span>{t(`metrics.state.${state}`)}</span>
      {reasons.length > 0 && (
        <InfoHint label={t("metrics.state.whyLabel")}>
          <ul className="list-disc space-y-1 pl-4">
            {reasons.map((code) => (
              <li key={code}>{t(`metrics.reasons.${code}`, { defaultValue: code })}</li>
            ))}
          </ul>
        </InfoHint>
      )}
    </span>
  );
}
