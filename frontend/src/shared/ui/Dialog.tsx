import * as DialogPrimitive from "@radix-ui/react-dialog";
import { X } from "lucide-react";
import type { ComponentProps } from "react";
import { useTranslation } from "react-i18next";

import { cn } from "@/shared/lib/cn";
import { Button } from "@/shared/ui/Button";
import { useOverlayFocus } from "@/shared/ui/useOverlayFocus";

export const Dialog = DialogPrimitive.Root;

export function DialogContent({
  className,
  children,
  ...props
}: ComponentProps<typeof DialogPrimitive.Content>) {
  const { t } = useTranslation();
  const focus = useOverlayFocus(props);
  return (
    <DialogPrimitive.Portal>
      <DialogPrimitive.Overlay className="fixed inset-0 z-(--layer-overlay) bg-overlay" />
      {/* Иначе длинная строка растягивала форму шире окна. */}
      <DialogPrimitive.Content
        className={cn(
          "fixed top-1/2 left-1/2 z-(--layer-overlay) grid w-full max-w-lg -translate-x-1/2 -translate-y-1/2 gap-4 bg-popover p-6 shadow-xl sm:rounded-2xl [&>*]:min-w-0",
          className,
        )}
        {...props}
        {...focus}
      >
        {children}
        <DialogPrimitive.Close asChild>
          <Button
            variant="ghost"
            size="icon-sm"
            className="absolute top-3 right-3 z-(--layer-sticky)"
            aria-label={t("actions.close")}
          >
            <X className="size-4" aria-hidden />
          </Button>
        </DialogPrimitive.Close>
      </DialogPrimitive.Content>
    </DialogPrimitive.Portal>
  );
}

export function DialogHeader({ className, ...props }: ComponentProps<"div">) {
  return <div className={cn("flex flex-col space-y-1.5 pr-8 text-left", className)} {...props} />;
}

export function DialogFooter({ className, ...props }: ComponentProps<"div">) {
  return (
    <div
      data-slot="dialog-footer"
      className={cn("flex flex-col-reverse gap-2 sm:flex-row sm:justify-end", className)}
      {...props}
    />
  );
}

export function DialogTitle({ className, ...props }: ComponentProps<typeof DialogPrimitive.Title>) {
  return (
    <DialogPrimitive.Title
      className={cn("text-lg leading-none font-semibold tracking-tight", className)}
      {...props}
    />
  );
}

export function DialogDescription({
  className,
  ...props
}: ComponentProps<typeof DialogPrimitive.Description>) {
  return (
    <DialogPrimitive.Description
      className={cn("text-sm text-muted-foreground", className)}
      {...props}
    />
  );
}
