import type { ComponentProps } from "react";
import { cn } from "@/shared/lib/cn";

export function PageLayout({ className, ...props }: ComponentProps<"div">) {
  return (
    <div
      data-slot="page-layout"
      className={cn("mx-auto w-full max-w-7xl px-4 py-6 md:px-6 md:py-8", className)}
      {...props}
    />
  );
}
