import { memo } from "react";
import Markdown, { type Components } from "react-markdown";
import remarkGfm from "remark-gfm";
import remarkMath from "remark-math";
import rehypeKatex from "rehype-katex";
// mhchem добавляет в KaTeX \ce{…} и \pu{…} для химии; импорт ради побочного эффекта.
import "katex/contrib/mhchem";
import "katex/dist/katex.min.css";

import { cn } from "@/shared/lib/cn";
import { remarkCurrency } from "@/shared/lib/remarkCurrency";

interface RichTextInnerProps {
  children: string;
  className?: string;
  components?: Components;
}

// Лениво: ~726 КБ markdown и формул вне стартового бандла. Мемо: перерисовка KaTeX — самая дорогая
// работа длинных тредов.
function RichTextInner({ children, className, components }: RichTextInnerProps) {
  return (
    <div className={cn("rich-text", className)}>
      <Markdown
        remarkPlugins={[remarkGfm, remarkMath, remarkCurrency]}
        rehypePlugins={[rehypeKatex]}
        components={components}
      >
        {children}
      </Markdown>
    </div>
  );
}

export default memo(RichTextInner);
