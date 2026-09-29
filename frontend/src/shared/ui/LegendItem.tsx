import type { ReactNode } from "react";

import { cn } from "@/shared/lib/cn";

const SIZES = { xs: "size-1.5", sm: "size-2", md: "size-3" } as const;
const OUTLINES = {
  dashed: "border border-dashed border-foreground",
  solid: "border-2 border-foreground",
} as const;

export function LegendItem({
  marker,
  outline,
  color,
  size = "sm",
  shape = "round",
  className,
  children,
}: {
  marker?: string;
  outline?: keyof typeof OUTLINES;
  color?: string;
  size?: keyof typeof SIZES;
  shape?: "round" | "square";
  className?: string;
  children: ReactNode;
}) {
  return (
    <span className={cn("inline-flex items-center gap-1.5", className)}>
      <span
        aria-hidden
        className={cn(
          SIZES[size],
          shape === "round" && "rounded-full",
          marker,
          outline && OUTLINES[outline],
        )}
        style={color ? { background: color } : undefined}
      />
      {children}
    </span>
  );
}
