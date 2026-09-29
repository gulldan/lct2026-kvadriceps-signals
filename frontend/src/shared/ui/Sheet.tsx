import * as SheetPrimitive from "@radix-ui/react-dialog";
import { cva, type VariantProps } from "class-variance-authority";
import { X } from "lucide-react";
import type { ComponentProps } from "react";
import { useTranslation } from "react-i18next";

import { cn } from "@/shared/lib/cn";
import { Button } from "@/shared/ui/Button";
import { useOverlayFocus } from "@/shared/ui/useOverlayFocus";

export const Sheet = SheetPrimitive.Root;

const sheetVariants = cva("fixed z-(--layer-overlay) gap-4 bg-background p-6 shadow-lg", {
  variants: {
    side: {
      top: "inset-x-0 top-0 border-b",
      bottom: "inset-x-0 bottom-0 border-t",
      left: "inset-y-0 left-0 h-full w-3/4 border-r sm:max-w-sm",
      right: "inset-y-0 right-0 h-full w-3/4 border-l sm:max-w-sm",
    },
  },
  defaultVariants: { side: "right" },
});

export function SheetContent({
  className,
  children,
  side = "right",
  ...props
}: ComponentProps<typeof SheetPrimitive.Content> & VariantProps<typeof sheetVariants>) {
  const { t } = useTranslation();
  const focus = useOverlayFocus(props);
  return (
    <SheetPrimitive.Portal>
      <SheetPrimitive.Overlay className="fixed inset-0 z-(--layer-overlay) bg-overlay" />
      <SheetPrimitive.Content
        className={cn(sheetVariants({ side }), className)}
        {...props}
        {...focus}
      >
        {children}
        <SheetPrimitive.Close asChild>
          <Button
            variant="ghost"
            size="icon-sm"
            className="absolute top-3 right-3 z-(--layer-sticky)"
            aria-label={t("actions.close")}
          >
            <X className="size-4" aria-hidden />
          </Button>
        </SheetPrimitive.Close>
      </SheetPrimitive.Content>
    </SheetPrimitive.Portal>
  );
}

export function SheetHeader({ className, ...props }: ComponentProps<"div">) {
  return <div className={cn("flex flex-col space-y-2 pr-8 text-left", className)} {...props} />;
}

export function SheetTitle({ className, ...props }: ComponentProps<typeof SheetPrimitive.Title>) {
  return (
    <SheetPrimitive.Title
      className={cn("text-lg font-semibold text-foreground", className)}
      {...props}
    />
  );
}

export function SheetDescription({
  className,
  ...props
}: ComponentProps<typeof SheetPrimitive.Description>) {
  return (
    <SheetPrimitive.Description
      className={cn("text-sm text-muted-foreground", className)}
      {...props}
    />
  );
}
