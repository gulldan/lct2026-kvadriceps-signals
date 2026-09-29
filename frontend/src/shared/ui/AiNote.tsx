import { Sparkles } from "lucide-react";
import { useTranslation } from "react-i18next";

import { cn } from "@/shared/lib/cn";

export function AiNote({ children, className }: { children?: string; className?: string }) {
  const { t } = useTranslation();
  return (
    <p className={cn("flex items-start gap-1 text-caption text-muted-foreground", className)}>
      <Sparkles className="mt-0.5 size-3 shrink-0" aria-hidden />
      {children ?? t("ui.aiNote")}
    </p>
  );
}
