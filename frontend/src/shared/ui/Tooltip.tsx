import * as TooltipPrimitive from "@radix-ui/react-tooltip";
import type { ComponentProps } from "react";

import { cn } from "@/shared/lib/cn";

export const TooltipProvider = TooltipPrimitive.Provider;
export const Tooltip = TooltipPrimitive.Root;
export const TooltipTrigger = TooltipPrimitive.Trigger;

export function TooltipContent({
  className,
  sideOffset = 4,
  variant = "default",
  ...props
}: ComponentProps<typeof TooltipPrimitive.Content> & { variant?: "default" | "card" }) {
  return (
    <TooltipPrimitive.Portal>
      <TooltipPrimitive.Content
        sideOffset={sideOffset}
        className={cn(
          "z-(--layer-overlay) overflow-hidden text-xs shadow-md",
          variant === "card"
            ? "rounded-2xl bg-popover p-4 text-popover-foreground shadow-lg"
            : "rounded-md bg-primary px-3 py-1.5 text-primary-foreground",
          className,
        )}
        {...props}
      />
    </TooltipPrimitive.Portal>
  );
}
