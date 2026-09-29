import type { ComponentProps } from "react";

import { cn } from "@/shared/lib/cn";
import { inputStyles } from "@/shared/ui/Input";

export function Textarea({ className, ...props }: ComponentProps<"textarea">) {
  return <textarea className={cn(inputStyles, "min-h-20 resize-y py-2", className)} {...props} />;
}
