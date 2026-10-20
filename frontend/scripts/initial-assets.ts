import type { Manifest } from "vite";

/** Только статические импорты: общие файлы считаются один раз, ленивые маршруты не входят. */
export function initialAssets(manifest: Manifest, entry: string): string[] {
  const seen = new Set<string>();
  const visit = (key: string) => {
    if (seen.has(key)) return;
    const chunk = manifest[key];
    if (!chunk) throw new Error(`Missing build manifest entry: ${key}`);
    seen.add(key);
    for (const dependency of chunk.imports ?? []) visit(dependency);
  };
  visit(entry);
  return [...new Set([...seen].map((key) => `dist/${manifest[key]!.file}`))];
}
