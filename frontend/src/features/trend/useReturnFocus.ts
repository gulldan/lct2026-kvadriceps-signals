import { useEffect } from "react";
import { useLocation } from "react-router-dom";

export function useReturnFocus(ready: boolean) {
  const { hash, key } = useLocation();
  useEffect(() => {
    if (!ready || !hash) return;
    let id: string;
    try {
      id = decodeURIComponent(hash.slice(1));
    } catch {
      return;
    }
    const target = document.getElementById(id);
    if (!target) return;
    const disclosure = target.closest("details");
    if (disclosure) disclosure.open = true;
    target.focus({ preventScroll: true });
    target.scrollIntoView({ block: "center" });
  }, [ready, hash, key]);
}
