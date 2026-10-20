import { expect, test } from "bun:test";
import { tokenCycles, validateDesignSource } from "./design-rules";

const known = new Set(["--brand", "--card", "--color-brand", "--color-card"]);
test("design gate rejects literal colors and unknown tokens but ignores comments", () => {
  const bad = validateDesignSource(
    "src/features/demo/page.tsx",
    'const page = <textarea className="bg-red-500" style={{ color: "#fff", background: "var(--missing)" }} />;',
    known,
  );
  expect(bad.map((v) => v.rule).toSorted()).toEqual(
    ["known-token", "semantic-colors", "semantic-utilities"].toSorted(),
  );
  expect(
    validateDesignSource(
      "src/features/demo/page.tsx",
      '// #fff is mentioned in a comment\nconst page = <><Input className="w-full" /><input type="file" hidden /><span className="bg-card" style={{ color: "var(--brand)" }} /></>;',
      known,
    ),
  ).toEqual([]);
  expect(
    validateDesignSource("src/shared/ui/Textarea.tsx", "const field = <textarea />;", known),
  ).toEqual([]);
  expect(
    validateDesignSource(
      "src/shared/design-system/tokens.css",
      ":root { --brand: #336699; }",
      known,
    ).map((v) => v.rule),
  ).toEqual(["palette-derived"]);
  expect(
    validateDesignSource(
      "src/shared/design-system/tokens.css",
      ":root { --palette-blue: #336699; --brand: var(--palette-blue); }",
      new Set(["--palette-blue"]),
    ),
  ).toEqual([]);
});
test("token aliases cannot become circular", () => {
  expect(tokenCycles(":root { --a: var(--b); --b: var(--a); }")).toHaveLength(1);
  expect(tokenCycles(":root { --a: var(--b); --b: oklch(1 0 0); }")).toEqual([]);
});

test("a CSS color token cannot be used as a utility until it has a Tailwind color alias", () => {
  const tokens = new Set([...known, "--category-1", "--chart-1", "--color-chart-1"]);
  expect(
    validateDesignSource(
      "src/features/demo/page.tsx",
      'const colors = "hover:bg-category-1 border-category-1 text-category-1";',
      tokens,
    ).map((item) => item.rule),
  ).toEqual(["mapped-color-utility", "mapped-color-utility", "mapped-color-utility"]);
  expect(
    validateDesignSource(
      "src/features/demo/page.tsx",
      'const colors = "hover:bg-chart-1 border-chart-1 text-chart-1 text-xs border-t ring-2";',
      tokens,
    ),
  ).toEqual([]);
  expect(
    validateDesignSource(
      "src/index.css",
      ".map { border-radius: var(--radius); @apply bg-category-1; }",
      new Set([...tokens, "--radius"]),
    ).map((item) => item.rule),
  ).toEqual(["mapped-color-utility"]);
});

test("product controls inherit kit recipes, including conditional and template classes", () => {
  const rules = (source: string) =>
    validateDesignSource("src/features/demo/page.tsx", source, known).map(({ rule }) => rule);
  expect(rules('const page = <Button className="bg-brand h-12 text-xs" />;')).toEqual([
    "control-size",
    "control-type",
    "control-color",
  ]);
  expect(rules('const page = <Input className={cn("w-full", active && "bg-card")} />;')).toEqual([
    "control-color",
  ]);
  expect(
    rules('const page = <Button className={`w-full ${active ? "text-brand" : "bg-card"}`} />;'),
  ).toEqual(["control-color", "control-color"]);
  expect(rules('const page = <><h1>Title</h1><div className="bg-card/40" /></>;')).toEqual([
    "page-title",
    "token-shades",
  ]);
  expect(
    rules(
      'const page = <><PageHeader title="Title" /><Button variant="outline" size="sm" className="w-full justify-start" /><Input controlSize="lg" /><p className="text-brand">Text</p></>;',
    ),
  ).toEqual([]);
  expect(
    validateDesignSource(
      "src/shared/ui/Button.tsx",
      'const button = <button className="bg-brand text-sm h-9" />;',
      known,
    ),
  ).toEqual([]);
});
