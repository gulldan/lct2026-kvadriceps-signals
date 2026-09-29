import type { Root } from "mdast";
import type { Plugin } from "unified";
import { visit } from "unist-util-visit";

// Знак перед ценой не закрывает формулу. Повторный разбор сохраняет формулы после цены,
// а также обычные правила экранирования и блоков кода.
export const remarkCurrency: Plugin<[], Root> = function () {
  return (initial, file) => {
    let source = String(file);
    let tree = initial;
    for (;;) {
      const prices: number[] = [];
      visit(tree, "inlineMath", (node) => {
        const start = node.position?.start.offset;
        const end = node.position?.end.offset;
        if (start === undefined || end === undefined) return;
        const raw = source.slice(start, end);
        const amount = /^\$\d+(?:[.,]\d+)*(?=\s|[^\p{L}\d])/u.test(raw);
        const prose = /^\$\d+(?:[.,]\d+)*[\s.,;:!?–—]+\p{L}{2,}/u.test(raw);
        if (amount && ((prose && /\s\$$/.test(raw)) || /^\d/.test(source.slice(end)))) {
          prices.push(start);
        }
      });
      if (prices.length === 0) return tree;
      for (const offset of prices.toReversed()) {
        source = `${source.slice(0, offset)}\\${source.slice(offset)}`;
      }
      tree = this.parse(source) as Root;
    }
  };
};
