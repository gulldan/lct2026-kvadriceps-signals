import * as LabelPrimitive from "@radix-ui/react-label";
import type { ComponentProps } from "react";

import { cn } from "@/shared/lib/cn";

export function Label({ className, ...props }: ComponentProps<typeof LabelPrimitive.Root>) {
  return (
    <LabelPrimitive.Root
      className={cn(
        "inline-flex min-h-6 items-center text-sm leading-5 font-medium peer-disabled:cursor-not-allowed peer-disabled:opacity-70",
        className,
      )}
      {...props}
    />
  );
}
