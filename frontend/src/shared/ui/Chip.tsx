import type { ComponentProps } from "react";

import { cn } from "@/shared/lib/cn";
import { badgeVariants } from "@/shared/ui/Badge";

export function Chip({ className, type, ...props }: ComponentProps<"button">) {
  return (
    <button
      data-slot="chip"
      type={type ?? "button"}
      className={cn(
        badgeVariants({ variant: "outline" }),
        "min-h-control-sm max-w-full cursor-pointer gap-1.5 rounded-full px-3 py-1 text-sm font-normal transition-colors hover:border-brand-border hover:bg-brand-wash hover:text-accent-foreground focus-visible:ring-2 focus-visible:ring-ring disabled:cursor-not-allowed disabled:opacity-50 aria-pressed:border-brand-border aria-pressed:bg-brand-wash aria-pressed:text-accent-foreground",
        className,
      )}
      {...props}
    />
  );
}
