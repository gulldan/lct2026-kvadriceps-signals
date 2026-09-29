import type { ComponentProps } from "react";
import { badgeTones } from "@/shared/design-system/tones";
import { cn } from "@/shared/lib/cn";

/** role="alert" — только для новой срочной ошибки. */
export function Notice({
  tone = "warn",
  className,
  ...props
}: ComponentProps<"div"> & { tone?: "brand" | "ok" | "warn" | "risk" | "muted" }) {
  return (
    <div className={cn("rounded-md px-3 py-2 text-sm", badgeTones[tone], className)} {...props} />
  );
}
