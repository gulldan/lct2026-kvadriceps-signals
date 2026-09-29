import type { ComponentProps, CSSProperties } from "react";
import { Toaster as SonnerToaster } from "sonner";
import { useTranslation } from "react-i18next";

export { toast } from "sonner";

export function Toaster({ style, ...props }: ComponentProps<typeof SonnerToaster>) {
  const { t } = useTranslation("common");
  return (
    <SonnerToaster
      position="top-right"
      offset={{ top: "var(--toast-offset)" }}
      mobileOffset={{ top: "var(--toast-offset)" }}
      closeButton
      className="print:hidden"
      style={
        {
          zIndex: "var(--layer-toast)",
          fontFamily: "var(--font-sans)",
          "--normal-bg": "var(--popover)",
          "--normal-border": "var(--border)",
          "--normal-text": "var(--popover-foreground)",
          "--success-bg": "var(--ok-wash)",
          "--success-border": "var(--ok)",
          "--success-text": "var(--ok)",
          "--warning-bg": "var(--warn-wash)",
          "--warning-border": "var(--warn)",
          "--warning-text": "var(--warn)",
          "--error-bg": "var(--risk-wash)",
          "--error-border": "var(--risk)",
          "--error-text": "var(--risk)",
          "--info-bg": "var(--brand-wash)",
          "--info-border": "var(--brand-border)",
          "--info-text": "var(--accent-foreground)",
          ...style,
        } as CSSProperties
      }
      toastOptions={{
        closeButtonAriaLabel: t("actions.close"),
        classNames: {
          toast: "rounded-2xl border shadow-lg",
          description: "text-muted-foreground",
        },
      }}
      {...props}
    />
  );
}
