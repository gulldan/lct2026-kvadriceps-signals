import type { ComponentProps } from "react";

import { cn } from "@/shared/lib/cn";

export function Kicker({ className, ...props }: ComponentProps<"div">) {
  return <div className={cn("kicker text-muted-foreground", className)} {...props} />;
}
