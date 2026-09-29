import { Loader2 } from "lucide-react";
import { useTranslation } from "react-i18next";

import { cn } from "@/shared/lib/cn";

const SIZE_CLASS = {
  sm: "size-3.5",
  default: "size-4",
  lg: "size-6",
} as const;

interface SpinnerProps {
  size?: keyof typeof SIZE_CLASS;
  className?: string;
  label?: string;
}

export function Spinner({ size = "default", className, label }: SpinnerProps) {
  const { t } = useTranslation();
  return (
    <output className="inline-flex" aria-label={label ?? t("ui.loading")}>
      <Loader2 className={cn("animate-spin", SIZE_CLASS[size], className)} aria-hidden />
    </output>
  );
}
