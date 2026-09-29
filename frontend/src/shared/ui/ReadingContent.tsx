import type { ComponentProps } from "react";
import { cn } from "@/shared/lib/cn";

export function ReadingContent({
  className,
  flow = "sections",
  ...props
}: ComponentProps<"article"> & { flow?: "sections" | "text" }) {
  return (
    <article
      data-slot="reading-content"
      className={cn(
        "max-w-4xl min-w-0 text-base leading-7 wrap-break-word",
        flow === "sections" ? "space-y-8" : "space-y-3",
        className,
      )}
      {...props}
    />
  );
}
