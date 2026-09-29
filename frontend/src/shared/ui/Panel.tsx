import { useId, useState } from "react";
import type { ComponentType, ReactNode } from "react";
import { ChevronDown } from "lucide-react";

import { cn } from "@/shared/lib/cn";
import { Button } from "@/shared/ui/Button";
import { InfoHint } from "@/shared/ui/InfoHint";

export function Panel({
  title,
  titleHint,
  icon: Icon,
  action,
  count,
  collapsible = false,
  defaultOpen = true,
  children,
}: {
  title?: string;
  titleHint?: string;
  icon?: ComponentType<{ className?: string }>;
  action?: ReactNode;
  count?: number;
  collapsible?: boolean;
  defaultOpen?: boolean;
  children: ReactNode;
}) {
  const [open, setOpen] = useState(defaultOpen);
  const bodyId = useId();
  const showBody = !collapsible || open;
  const titleInner = (
    <>
      {Icon && <Icon className="size-4 shrink-0 text-muted-foreground" aria-hidden />}
      <span className="text-sm font-medium text-foreground">{title}</span>
      {count !== undefined && count > 0 && (
        <span className="rounded-full bg-muted px-1.5 font-mono text-xs text-muted-foreground tabular-nums">
          {count}
        </span>
      )}
    </>
  );
  return (
    <section className="min-w-0 space-y-3 border-t pt-5">
      {(title || action) && (
        <div className="flex flex-wrap items-center justify-between gap-3">
          <div className="flex min-w-0 items-center gap-1.5">
            {collapsible ? (
              <Button
                variant="ghost"
                size="content"
                onClick={() => setOpen((o) => !o)}
                className="flex-1 gap-2 p-0 text-left"
                aria-expanded={open}
                aria-controls={bodyId}
              >
                <ChevronDown
                  className={cn(
                    "size-4 shrink-0 text-muted-foreground transition-transform",
                    open && "rotate-180",
                  )}
                />
                {titleInner}
              </Button>
            ) : (
              <h3 className="flex items-center gap-2">{titleInner}</h3>
            )}
            {titleHint && <InfoHint label={title}>{titleHint}</InfoHint>}
          </div>
          {action}
        </div>
      )}
      <div id={bodyId} hidden={!showBody} className="space-y-3">
        {showBody && children}
      </div>
    </section>
  );
}

export function FieldLabel({ children }: { children: ReactNode }) {
  return <p className="kicker text-muted-foreground">{children}</p>;
}

export function Field({ label, children }: { label: string; children: ReactNode }) {
  return (
    <div>
      <FieldLabel>{label}</FieldLabel>
      <div className="mt-1 text-sm">{children}</div>
    </div>
  );
}

export function BulletList({ items, small = false }: { items: string[]; small?: boolean }) {
  return (
    <ul
      className={cn(
        "list-disc space-y-0.5 pl-4 text-muted-foreground",
        small ? "text-xs" : "text-sm",
      )}
    >
      {items.map((x) => (
        <li key={x}>{x}</li>
      ))}
    </ul>
  );
}
