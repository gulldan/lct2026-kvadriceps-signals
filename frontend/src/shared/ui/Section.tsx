import { useId, type ReactNode } from "react";

import { cn } from "@/shared/lib/cn";

export function Section({
  title,
  description,
  action,
  children,
  className,
  reading = false,
  id,
}: {
  title: string;
  description?: ReactNode;
  action?: ReactNode;
  children: ReactNode;
  className?: string;
  reading?: boolean;
  id?: string;
}) {
  const headingId = useId();
  return (
    <section
      id={id}
      aria-labelledby={headingId}
      data-slot="section"
      className={cn("min-w-0", className)}
    >
      <div className="flex flex-wrap items-baseline justify-between gap-2">
        <h2
          id={headingId}
          className={cn("font-medium wrap-anywhere", reading ? "text-xl" : "text-base")}
        >
          {title}
        </h2>
        {action}
      </div>
      {description && (
        <p
          className={cn("mt-1 text-muted-foreground", reading ? "text-base leading-7" : "text-sm")}
        >
          {description}
        </p>
      )}
      <div
        className={cn(
          reading ? "mt-3 space-y-3 text-base leading-7" : description ? "mt-3" : "mt-2",
        )}
      >
        {children}
      </div>
    </section>
  );
}
