import type { TFunction } from "i18next";
import { ApiError } from "@/shared/api/client";

const KNOWN = [400, 401, 403, 404, 409, 413, 429, 500, 503, 507] as const;

/** Не сырой текст сервера: он приходит на двух языках. */
export function describeError(error: unknown, t: TFunction<"common">): string {
  if (error instanceof ApiError) {
    const code = KNOWN.find((status) => status === error.status);
    if (code) return t(`errors.http.${code}`);
    if (error.status === 0) return t("errors.http.network");
    return t("errors.http.other", { status: error.status });
  }
  if (error instanceof TypeError) return t("errors.http.network");
  return t("errors.http.other", { status: "" });
}
