import { lazy, Suspense } from "react";
import type { Components } from "react-markdown";

export type { Components as RichTextComponents } from "react-markdown";

import { cn } from "@/shared/lib/cn";

const RichTextInner = lazy(() => import("./RichTextInner"));

interface RichTextProps {
  children: string | null | undefined;
  className?: string;
  components?: Components;
}

/** Сырой HTML выключен: вывод корпуса и модели не может внедрить разметку. */
export function RichText({ children, className, components }: RichTextProps) {
  const text = (children ?? "").trim();
  if (text === "") {
    return null;
  }
  return (
    <Suspense
      fallback={<div className={cn("rich-text whitespace-pre-wrap", className)}>{text}</div>}
    >
      <RichTextInner className={className} components={components}>
        {text}
      </RichTextInner>
    </Suspense>
  );
}
