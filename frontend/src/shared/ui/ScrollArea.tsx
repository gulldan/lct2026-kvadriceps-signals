/* oxlint-disable jsx-a11y/no-noninteractive-tabindex -- Native scroll containers need keyboard focus; the required region label supplies screen-reader context (MDN overflow accessibility). */
import type { ComponentProps } from "react";
import { cn } from "@/shared/lib/cn";

export function ScrollArea({
  className,
  ...props
}: ComponentProps<"section"> & { "aria-label": string }) {
  return (
    <section
      tabIndex={0}
      className={cn(
        "min-h-0 overflow-auto focus-visible:outline-2 focus-visible:-outline-offset-2 focus-visible:outline-ring",
        className,
      )}
      {...props}
    />
  );
}
