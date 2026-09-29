import type { ComponentProps } from "react";

import { cn } from "@/shared/lib/cn";
import { useTranslation } from "react-i18next";
import { ScrollArea } from "@/shared/ui/ScrollArea";

/** caption называет таблицу для экранного диктора (визуально скрыт) и её область прокрутки. */
export function Table({
  className,
  caption,
  children,
  ...props
}: ComponentProps<"table"> & { caption?: string }) {
  const { t } = useTranslation();
  return (
    <ScrollArea
      aria-label={props["aria-label"] ?? caption ?? t("ui.table")}
      className="relative w-full print:overflow-visible"
    >
      <table className={cn("w-full caption-bottom text-sm", className)} {...props}>
        {caption && <caption className="sr-only">{caption}</caption>}
        {children}
      </table>
    </ScrollArea>
  );
}

export function TableHeader({ className, ...props }: ComponentProps<"thead">) {
  return <thead className={cn("[&_tr]:border-b", className)} {...props} />;
}

export function TableBody({ className, ...props }: ComponentProps<"tbody">) {
  return <tbody className={cn("[&_tr:last-child]:border-0", className)} {...props} />;
}

export function TableRow({ className, ...props }: ComponentProps<"tr">) {
  return (
    <tr
      className={cn(
        "border-b transition-colors hover:bg-muted/50 data-[state=selected]:bg-muted",
        className,
      )}
      {...props}
    />
  );
}

export function TableHead({ className, scope = "col", ...props }: ComponentProps<"th">) {
  return (
    <th
      scope={scope}
      className={cn(
        "h-10 px-2 text-left align-middle font-medium text-muted-foreground [&:has([role=checkbox])]:pr-0",
        className,
      )}
      {...props}
    />
  );
}

export function TableCell({ className, ...props }: ComponentProps<"td">) {
  return (
    <td className={cn("p-2 align-middle [&:has([role=checkbox])]:pr-0", className)} {...props} />
  );
}
