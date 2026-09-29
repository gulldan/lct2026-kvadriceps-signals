import type { ComponentProps } from "react";
import { cn } from "@/shared/lib/cn";

/** Нативный ползунок сохраняет клавиатуру платформы (стрелки, Home/End, PageUp/Down). */
export function Range({ className, ...props }: Omit<ComponentProps<"input">, "type">) {
  return (
    <input
      type="range"
      className={cn(
        "h-control w-full cursor-pointer accent-primary disabled:cursor-not-allowed disabled:opacity-50",
        className,
      )}
      {...props}
    />
  );
}
