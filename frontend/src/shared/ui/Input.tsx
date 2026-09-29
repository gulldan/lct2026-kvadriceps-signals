import type { ComponentProps } from "react";

import { cn } from "@/shared/lib/cn";
import { useFilterBarSize } from "@/shared/ui/FilterBar";

export const inputStyles =
  "flex w-full min-w-0 rounded-lg border border-input bg-background px-3 text-sm transition-colors placeholder:text-muted-foreground focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-1 focus-visible:ring-offset-background disabled:cursor-not-allowed disabled:opacity-50 aria-invalid:border-risk aria-invalid:focus-visible:ring-risk";

export function Input({
  className,
  type,
  controlSize: requestedSize = "default",
  ...props
}: ComponentProps<"input"> & {
  controlSize?: "sm" | "default" | "lg";
}) {
  const controlSize = useFilterBarSize() ?? requestedSize;
  return (
    <input
      data-slot="input"
      data-size={controlSize}
      type={type}
      className={cn(
        inputStyles,
        "py-1 file:border-0 file:bg-transparent file:text-sm file:font-medium",
        { sm: "h-control-sm", default: "h-control", lg: "h-control-lg" }[controlSize],
        className,
      )}
      {...props}
    />
  );
}
