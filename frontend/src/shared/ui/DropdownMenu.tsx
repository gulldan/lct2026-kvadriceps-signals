import * as DropdownMenuPrimitive from "@radix-ui/react-dropdown-menu";
import { Check } from "lucide-react";
import type { ComponentProps } from "react";

import { cn } from "@/shared/lib/cn";

export const DropdownMenu = DropdownMenuPrimitive.Root;
export const DropdownMenuTrigger = DropdownMenuPrimitive.Trigger;

export function DropdownMenuContent({
  className,
  sideOffset = 4,
  semantics = "menu",
  children,
  ...props
}: ComponentProps<typeof DropdownMenuPrimitive.Content> & { semantics?: "menu" | "dialog" }) {
  return (
    <DropdownMenuPrimitive.Portal>
      <DropdownMenuPrimitive.Content
        sideOffset={sideOffset}
        className={cn(
          "z-(--layer-overlay) min-w-32 overflow-hidden rounded-2xl border bg-popover p-1 text-popover-foreground shadow-md",
          className,
        )}
        {...props}
        asChild={semantics === "dialog" || props.asChild}
        role={semantics === "dialog" ? undefined : "menu"}
        aria-orientation={semantics === "dialog" ? undefined : "vertical"}
      >
        {semantics === "dialog" ? (
          <dialog open className="static m-0">
            {children}
          </dialog>
        ) : (
          children
        )}
      </DropdownMenuPrimitive.Content>
    </DropdownMenuPrimitive.Portal>
  );
}

export function DropdownMenuItem({
  className,
  variant = "default",
  ...props
}: ComponentProps<typeof DropdownMenuPrimitive.Item> & { variant?: "default" | "destructive" }) {
  return (
    <DropdownMenuPrimitive.Item
      className={cn(
        "relative flex cursor-default items-center gap-2 rounded-sm px-2 py-1.5 text-sm transition-colors outline-none select-none focus:bg-accent focus:text-accent-foreground data-[disabled]:pointer-events-none data-[disabled]:opacity-50 [&_svg]:size-4 [&_svg]:shrink-0",
        variant === "destructive" &&
          "text-destructive focus:bg-destructive/10 focus:text-destructive",
        className,
      )}
      {...props}
    />
  );
}

export function DropdownMenuCheckboxItem({
  className,
  children,
  ...props
}: ComponentProps<typeof DropdownMenuPrimitive.CheckboxItem>) {
  return (
    <DropdownMenuPrimitive.CheckboxItem
      className={cn(
        "relative flex cursor-default items-center gap-2 rounded-sm py-1.5 pr-2 pl-7 text-sm transition-colors outline-none select-none focus:bg-accent focus:text-accent-foreground data-[disabled]:pointer-events-none data-[disabled]:opacity-50",
        className,
      )}
      {...props}
    >
      <span className="absolute left-1.5 flex size-4 items-center justify-center">
        <DropdownMenuPrimitive.ItemIndicator>
          <Check className="size-4" aria-hidden />
        </DropdownMenuPrimitive.ItemIndicator>
      </span>
      {children}
    </DropdownMenuPrimitive.CheckboxItem>
  );
}

export function DropdownMenuLabel({
  className,
  ...props
}: ComponentProps<typeof DropdownMenuPrimitive.Label>) {
  return (
    <DropdownMenuPrimitive.Label
      className={cn("px-2 py-1.5 text-sm font-semibold", className)}
      {...props}
    />
  );
}

export function DropdownMenuSeparator({
  className,
  ...props
}: ComponentProps<typeof DropdownMenuPrimitive.Separator>) {
  return (
    <DropdownMenuPrimitive.Separator
      className={cn("-mx-1 my-1 h-px bg-muted", className)}
      {...props}
    />
  );
}
