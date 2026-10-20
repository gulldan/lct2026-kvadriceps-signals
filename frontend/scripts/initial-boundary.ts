import type { Rolldown } from "vite";

type Chunk = Pick<Rolldown.OutputChunk, "imports" | "moduleIds">;

const DEFERRED_MODULES = [
  /\/src\/ui-kit\//,
  /\/src\/features\/trend\/ui\/(LiveDetail|LiveSignalReport)\.tsx$/,
  /\/src\/features\/trend\/ui\/library\/scene\.ts$/,
  /\/src\/features\/trend\/pages\/(TrendPage|TrendRadar|TechnologyCardPage)\.tsx$/,
  /\/src\/shared\/(ui\/(RichTextInner|PdfViewerInner)\.tsx|report\/docx\.ts)$/,
  /\/node_modules\/(docx|pdfjs-dist|react-pdf|react-markdown|katex|three)\//,
];

/** Проверяем модули внутри чанков: ленивый фасад может скрывать общий стартовый чанк. */
export function assertInitialBoundary(chunks: ReadonlyMap<string, Chunk>, entry: string) {
  const visited = new Set<string>();
  const visit = (file: string, chain: string[]) => {
    if (visited.has(file)) return;
    const chunk = chunks.get(file);
    if (!chunk) throw new Error(`Missing build chunk: ${file}`);
    visited.add(file);
    const path = [...chain, file];
    const deferred = chunk.moduleIds.filter((id) =>
      DEFERRED_MODULES.some((pattern) => pattern.test(id.replaceAll("\\", "/"))),
    );
    if (deferred.length) {
      throw new Error(
        `Product initial boundary violated: ${path.join(" → ")}\nDeferred modules: ${deferred.join(", ")}`,
      );
    }
    for (const dependency of chunk.imports) visit(dependency, path);
  };
  visit(entry, []);
}
