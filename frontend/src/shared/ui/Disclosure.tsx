import { useState, type ComponentProps, type ReactNode } from "react";
import { ChevronDown } from "lucide-react";
import { cn } from "@/shared/lib/cn";

export function Disclosure({
  title,
  children,
  className,
  summaryClassName,
  reading = false,
  lazy = false,
  onToggle,
  ...props
}: Omit<ComponentProps<"details">, "title"> & {
  title: ReactNode;
  summaryClassName?: string;
  reading?: boolean;
  lazy?: boolean;
}) {
  const [expanded, setExpanded] = useState(false);
  // По умолчанию содержимое в разметке: на нём держатся проверка формы и возврат фокуса по якорю.
  const rendered = !lazy || expanded || props.open === true || window.location.hash !== "";
  return (
    <details
      className={cn("min-w-0 border-t [&[open]>summary>svg]:rotate-180", className)}
      onToggle={(event) => {
        setExpanded(event.currentTarget.open);
        onToggle?.(event);
      }}
      {...props}
    >
      <summary
        className={cn(
          "flex min-w-0 cursor-pointer list-none items-center justify-between gap-3 py-4 text-sm font-medium wrap-anywhere focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-ring [&::-webkit-details-marker]:hidden",
          reading && "text-base",
          summaryClassName,
        )}
      >
        {title}
        <ChevronDown
          className="size-4 shrink-0 text-muted-foreground transition-transform"
          aria-hidden
        />
      </summary>
      {rendered && <div className="pb-5">{children}</div>}
    </details>
  );
}
