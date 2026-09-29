import { i18n } from "@/shared/i18n";

export function hypothesisJobErrorMessage(code?: string): string {
  return code === "insufficient_context"
    ? i18n.t("task:toasts.insufficientContext")
    : i18n.t("task:toasts.failed");
}
