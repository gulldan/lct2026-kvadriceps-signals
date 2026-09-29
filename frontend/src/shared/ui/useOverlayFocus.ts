import { useRef } from "react";

/** Управляемые оверлеи открываются и без Trigger из Radix (действия строки, данные API). */
export function useOverlayFocus({
  onOpenAutoFocus,
  onCloseAutoFocus,
}: {
  onOpenAutoFocus?: (event: Event) => void;
  onCloseAutoFocus?: (event: Event) => void;
}) {
  const opener = useRef<HTMLElement | SVGElement | null>(null);
  return {
    onOpenAutoFocus(event: Event) {
      opener.current =
        document.activeElement instanceof HTMLElement ||
        document.activeElement instanceof SVGElement
          ? document.activeElement
          : null;
      onOpenAutoFocus?.(event);
    },
    onCloseAutoFocus(event: Event) {
      onCloseAutoFocus?.(event);
      if (!event.defaultPrevented && opener.current?.isConnected) {
        event.preventDefault();
        opener.current.focus({ preventScroll: true });
      }
    },
  };
}
