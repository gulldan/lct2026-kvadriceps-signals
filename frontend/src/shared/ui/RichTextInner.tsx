import { memo, type ReactNode } from "react";
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

// Текст пишут модель и корпус: ссылкой становится только http(s), и новая вкладка не получает
// доступ к нашей через window.opener.
function ExternalLink({ href, children }: { href?: string; children?: ReactNode }) {
  if (!href || !/^https?:\/\//i.test(href)) return <>{children}</>;
  return (
    <a href={href} target="_blank" rel="noopener noreferrer">
      {children}
    </a>
  );
}

// Лениво: ~726 КБ markdown и формул вне стартового бандла. Мемо: перерисовка KaTeX — самая дорогая
// работа длинных тредов.
function RichTextInner({ children, className, components }: RichTextInnerProps) {
  return (
    <div className={cn("rich-text", className)}>
      <Markdown
        remarkPlugins={[remarkGfm, remarkMath, remarkCurrency]}
        rehypePlugins={[rehypeKatex]}
        components={{ a: ExternalLink, ...components }}
      >
        {children}
      </Markdown>
    </div>
  );
}

export default memo(RichTextInner);
