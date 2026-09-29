import type { ComponentProps, ReactNode } from "react";

import { Kicker } from "@/shared/ui/Kicker";
import { Button } from "@/shared/ui/Button";
import { cn } from "@/shared/lib/cn";

export function PageTitle({
  children,
  as: Tag = "h1",
  variant = "page",
  className,
  ...props
}: ComponentProps<"h1"> & { as?: "h1" | "h2"; variant?: "page" | "welcome" | "object" }) {
  return (
    <Tag
      data-slot="page-title"
      data-variant={variant}
      className={cn(
        "font-display min-w-0 text-balance wrap-anywhere",
        variant === "welcome"
          ? "text-4xl md:text-5xl"
          : variant === "object"
            ? "text-2xl"
            : "text-3xl",
        className,
      )}
      {...props}
    >
      {children}
    </Tag>
  );
}

export function PageToolbar({ children }: { children: ReactNode }) {
  return (
    <div
      data-slot="page-toolbar"
      className="mb-6 flex min-w-0 flex-wrap items-center justify-between gap-3 print:hidden"
    >
      {children}
    </div>
  );
}

export function BackButton({
  children,
  ...props
}: Omit<ComponentProps<typeof Button>, "variant" | "size" | "className">) {
  return (
    <Button {...props} variant="ghost" className="-ml-4">
      {children}
    </Button>
  );
}

interface PageHeaderProps {
  title: string;
  /** Вложенные примеры каталога и документа сохраняют типографику без второго h1. */
  titleAs?: "h1" | "h2";
  description?: string;
  descriptionWidth?: "reading" | "full";
  kicker?: string;
  badge?: ReactNode;
  actions?: ReactNode;
}

export function PageHeader({
  title,
  titleAs = "h1",
  description,
  descriptionWidth = "reading",
  kicker,
  badge,
  actions,
}: PageHeaderProps) {
  return (
    <div
      data-slot="page-header"
      className="mb-6 flex min-w-0 flex-wrap items-start justify-between gap-3"
    >
      <div className="min-w-0">
        {kicker && <Kicker className="mb-1.5">{kicker}</Kicker>}
        <div className="flex min-w-0 items-center gap-2.5">
          <PageTitle as={titleAs}>{title}</PageTitle>
          {badge}
        </div>
        {description && (
          <p
            data-slot="page-description"
            className={cn(
              "mt-2 text-sm leading-relaxed text-pretty text-muted-foreground",
              descriptionWidth === "reading" && "max-w-prose",
            )}
          >
            {description}
          </p>
        )}
      </div>
      {actions && (
        <div data-slot="page-actions" className="flex min-w-0 flex-wrap items-center gap-2">
          {actions}
        </div>
      )}
    </div>
  );
}
