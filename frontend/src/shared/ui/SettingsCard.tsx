import type { LucideIcon } from "lucide-react";
import type { ReactNode } from "react";
import { cn } from "@/shared/lib/cn";

export function SettingsCard({
  icon: Icon,
  title,
  titleExtra,
  description,
  children,
  footer,
  className,
}: {
  icon: LucideIcon;
  title: string;
  titleExtra?: ReactNode;
  description?: string;
  children: ReactNode;
  footer?: ReactNode;
  className?: string;
}) {
  return (
    <section className={cn("min-w-0 border-t pt-5", className)}>
      <header>
        <h2 className="flex items-center gap-2 text-base font-medium">
          <Icon className="size-4 text-muted-foreground" aria-hidden />
          {title}
          {titleExtra}
        </h2>
        {description && (
          <p className="mt-1 max-w-2xl text-sm text-muted-foreground">{description}</p>
        )}
      </header>
      <div className="mt-5 space-y-5">
        {children}
        {footer && (
          <div className="flex flex-wrap items-center justify-between gap-2 border-t pt-3">
            {footer}
          </div>
        )}
      </div>
    </section>
  );
}
