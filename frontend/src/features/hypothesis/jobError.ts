import { i18n } from "@/shared/i18n";

export function hypothesisJobErrorMessage(code?: string): string {
  if (code === "insufficient_context") return i18n.t("task:toasts.insufficientContext");
  if (code === "unparsable_model_output") return i18n.t("task:toasts.unparsableModelOutput");
  return i18n.t("task:toasts.failed");
}
