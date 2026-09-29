import type { MetricEnvelope } from "./technologyApi";

/** Ни одно состояние не рисуется как 0 и не выводится только из цвета. */
type MetricState = "measured" | "unknown" | "not_observed";

const NOT_OBSERVED = new Set(["not_observed", "not_observed_in_scope", "none_observed"]);

export function metricState(
  metric: Pick<MetricEnvelope, "value" | "status" | "reason_codes">,
): MetricState {
  if (typeof metric.value === "number" && Number.isFinite(metric.value)) return "measured";
  const codes = [metric.status, ...(metric.reason_codes ?? [])];
  return codes.some((code) => NOT_OBSERVED.has(code)) ? "not_observed" : "unknown";
}

export function metricReasons(metric: Pick<MetricEnvelope, "status" | "reason_codes">) {
  const reasons = [...(metric.reason_codes ?? [])];
  if (metric.status && metric.status !== "measured" && !reasons.includes(metric.status))
    reasons.unshift(metric.status);
  return reasons;
}
