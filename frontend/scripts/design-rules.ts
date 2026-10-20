import { parseSync, Visitor } from "oxc-parser";

export interface DesignViolation {
  line: number;
  rule: string;
  message: string;
}

const literalColor = /#[\da-f]{3,8}\b|\b(?:rgba?|hsla?|oklch|oklab|lab|lch)\(/i;
const paletteClass =
  /\b(?:bg|text|border|ring|outline|fill|stroke|accent|decoration|shadow|from|via|to)(?:-(?:t|r|b|l|x|y|offset))?-(?:slate|gray|zinc|neutral|stone|red|orange|amber|yellow|lime|green|emerald|teal|cyan|sky|blue|indigo|violet|purple|fuchsia|pink|rose|black|white)(?:-\d{2,3})?(?=[\s/:"')\]]|$)/;

/** Проверяется синтаксис, а не комментарии: у документации и строк другие задачи. */
export function validateDesignSource(
  file: string,
  source: string,
  tokens: ReadonlySet<string>,
): DesignViolation[] {
  const found: DesignViolation[] = [];
  const product = /^src\/(features|app)\//.test(file);
  const controlClasses: { start: number; end: number }[] = [];
  const report = (position: number, rule: string, message: string) => {
    found.push({ line: source.slice(0, position).split("\n").length, rule, message });
  };
  const checkValue = (value: string, position: number) => {
    if (controlClasses.some(({ start, end }) => position >= start && position < end)) {
      if (/\b(?:h|min-h|size)-(?:\d|\[)/.test(value))
        report(
          position,
          "control-size",
          "Размер контрола задаётся через size/controlSize или control-* токен.",
        );
      if (/\btext-(?:xs|sm|base|lg|caption|micro|\[)/.test(value))
        report(
          position,
          "control-type",
          "Типографика контрола задаётся его вариантом в shared/ui.",
        );
      const colors = [...value.matchAll(/\b(bg|text|border|ring)-([a-z][\w-]*)/g)];
      if (colors.some(([, kind, token]) => kind === "bg" || tokens.has(`--color-${token}`)))
        report(
          position,
          "control-color",
          "Цвет контрола задаётся через variant в shared/ui, не через className страницы.",
        );
    }
    if (product && /\b(?:bg|text|border|ring)-[a-z][\w-]*\/\d+\b/.test(value)) {
      report(
        position,
        "token-shades",
        "Используйте готовый семантический цвет; оттенки и прозрачность определяются в дизайн-ките.",
      );
    }
    if (literalColor.test(value) && file !== "src/shared/design-system/tokens.css") {
      report(
        position,
        "semantic-colors",
        "Цвет задаётся в shared/design-system/tokens.css, не в компоненте.",
      );
    }
    if (paletteClass.test(value))
      report(
        position,
        "semantic-utilities",
        "Используйте bg-brand/text-risk и другие семантические токены вместо стандартной палитры.",
      );
    for (const match of value.matchAll(
      /\b(?:bg|text|border|ring|outline|fill|stroke|accent|decoration|from|via|to)(?:-(?:t|r|b|l|x|y|offset))?-([\w-]+)(?=[\s/;"')\]]|$)(?!\s*:)/g,
    )) {
      const name = match[1]!;
      if (tokens.has(`--${name}`) && !tokens.has(`--color-${name}`))
        report(
          position,
          "mapped-color-utility",
          `Токен --${name} не подключён к Tailwind через --color-${name}. Используйте доступный цветовой алиас.`,
        );
    }
    for (const match of value.matchAll(/var\((--[\w-]+)(?=[,)])/g)) {
      const token = match[1]!;
      if (!tokens.has(token) && !token.startsWith("--radix-"))
        report(position, "known-token", `Не определён токен ${token}.`);
    }
  };
  if (file.endsWith(".css")) {
    if (file === "src/shared/design-system/tokens.css") {
      for (const match of source.matchAll(/(--[\w-]+):\s*([^;]+);/g)) {
        if (literalColor.test(match[2]!) && !match[1]!.startsWith("--palette-")) {
          report(
            match.index,
            "palette-derived",
            "Семантический цвет должен ссылаться на общую palette-* основу.",
          );
        }
      }
    }
    checkValue(
      source.replace(/\/\*[^]*?\*\//g, (comment) => " ".repeat(comment.length)),
      0,
    );
    return found;
  }
  const parsed = parseSync(file, source);
  for (const error of parsed.errors)
    report(error.labels[0]?.start ?? 0, "valid-syntax", error.message);
  new Visitor({
    JSXOpeningElement(node) {
      if (!product || node.name.type !== "JSXIdentifier") return;
      const component = node.name.name;
      if (component === "h1")
        report(node.start, "page-title", "Заголовок страницы берётся из PageHeader или PageTitle.");
      if (
        ![
          "Button",
          "BackButton",
          "Input",
          "SelectTrigger",
          "Textarea",
          "SearchField",
          "Chip",
        ].includes(component)
      )
        return;
      const classes = node.attributes.find(
        (attribute) =>
          attribute.type === "JSXAttribute" &&
          attribute.name.type === "JSXIdentifier" &&
          attribute.name.name === "className",
      );
      if (classes?.type !== "JSXAttribute" || !classes.value) return;
      controlClasses.push({ start: classes.value.start, end: classes.value.end });
    },
    Literal(node) {
      if (typeof node.value === "string") checkValue(node.value, node.start);
    },
    TemplateElement(node) {
      checkValue(node.value.cooked ?? node.value.raw, node.start);
    },
  }).visit(parsed.program);
  return found;
}

export function tokenCycles(source: string): string[] {
  const values = new Map(
    [...source.matchAll(/(--[\w-]+):\s*([^;]+);/g)].map((m) => [m[1]!, m[2]!]),
  );
  const done = new Set<string>();
  const cycles = new Set<string>();
  const visit = (name: string, chain: string[]) => {
    if (chain.includes(name)) {
      cycles.add([...chain, name].join(" → "));
      return;
    }
    if (done.has(name)) return;
    for (const match of (values.get(name) ?? "").matchAll(/var\((--[\w-]+)/g))
      visit(match[1]!, [...chain, name]);
    done.add(name);
  };
  for (const name of values.keys()) visit(name, []);
  return [...cycles];
}
