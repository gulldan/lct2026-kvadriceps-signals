import { printColors } from "@/shared/design-system/colors";
// Шрифты — из любой поставки Office: у Fontsource Onest только в woff2, встроить нельзя. Модуль
// тяжёлый (библиотека docx) — подключать только динамическим import().
import {
  BorderStyle,
  Document,
  ExternalHyperlink,
  Footer,
  HeadingLevel,
  LineRuleType,
  Packer,
  PageNumber,
  Paragraph,
  Tab,
  Table,
  TableCell,
  TableLayoutType,
  TableRow,
  TabStopType,
  TextRun,
  WidthType,
} from "docx";

import { i18n } from "@/shared/i18n";
import { saveFile } from "@/shared/lib/download";

import {
  metricLength,
  type ReportBlock,
  type ReportDoc,
  type ReportFact,
  type ReportMetric,
} from "./blocks";

const INK = printColors.ink;
const MUTED = printColors.muted;
const HAIRLINE = printColors.border;
const RULE = printColors.strongBorder;
const SANS = "Arial";
const MONO = "Consolas";
// A4 с полями 20 мм, в twips.
const MARGIN = 1134;
const WIDTH = 11906 - 2 * MARGIN;
const HALF = WIDTH / 2;
const GUTTER = 280;
const LINE = 312;

type Run = {
  size?: number;
  bold?: boolean;
  italic?: boolean;
  color?: string;
  mono?: boolean;
  tracking?: number;
};

function run(value: string, o: Run = {}): TextRun {
  return new TextRun({
    text: value,
    font: o.mono ? MONO : SANS,
    size: o.size ?? 21,
    bold: o.bold,
    italics: o.italic,
    color: o.color ?? INK,
    characterSpacing: o.tracking,
  });
}

// Голый w:tab в абзаце Word пропускает, и номер слипается с пунктом.
const tab = () => new TextRun({ children: [new Tab()] });

function label(value: string, o: { color?: string; before?: number; after?: number } = {}) {
  return new Paragraph({
    spacing: { before: o.before ?? 0, after: o.after ?? 60 },
    children: [
      run(value.toUpperCase(), { mono: true, size: 15, tracking: 9, color: o.color ?? INK }),
    ],
  });
}

function text(value: string, o: Run & { after?: number; keepNext?: boolean; line?: number } = {}) {
  return new Paragraph({
    spacing: { after: o.after ?? 120, line: o.line ?? LINE },
    keepNext: o.keepNext,
    children: [run(value, o)],
  });
}

const NONE = { style: BorderStyle.NONE, size: 0, color: "FFFFFF" } as const;
const line = (color: string) => ({ style: BorderStyle.SINGLE, size: 4, color }) as const;
const noBorders = {
  top: NONE,
  bottom: NONE,
  left: NONE,
  right: NONE,
  insideHorizontal: NONE,
  insideVertical: NONE,
};

type Cell = (Paragraph | Table)[];

/** В Word таблица — единственная сетка, которая не разъезжается при правке текста. */
function columns(cells: Cell[], o: { top?: string; bottom?: string; pad?: number } = {}): Table {
  const width = WIDTH / cells.length;
  const pad = o.pad ?? 0;
  return new Table({
    width: { size: WIDTH, type: WidthType.DXA },
    columnWidths: cells.map(() => width),
    layout: TableLayoutType.FIXED,
    borders: {
      ...noBorders,
      top: o.top ? line(o.top) : NONE,
      bottom: o.bottom ? line(o.bottom) : NONE,
    },
    rows: [
      new TableRow({
        cantSplit: true,
        children: cells.map(
          (children, i) =>
            new TableCell({
              width: { size: width, type: WidthType.DXA },
              margins: {
                top: pad,
                bottom: pad,
                left: i === 0 ? 0 : GUTTER,
                right: i === cells.length - 1 ? 0 : GUTTER,
              },
              // Ячейка Word обязана кончаться абзацем, иначе файл открывается с ошибкой.
              children:
                children.at(-1) instanceof Paragraph ? children : [...children, new Paragraph({})],
            }),
        ),
      }),
    ],
  });
}

function metricSize(m: ReportMetric): number {
  const length = metricLength(m);
  if (length <= 3) return 72;
  if (length <= 5) return 56;
  return length <= 8 ? 44 : 30;
}

function metricsGrid(metrics: ReportMetric[]): Table {
  const size = (HALF - GUTTER) / 2;
  const pairs = Array.from({ length: Math.ceil(metrics.length / 2) }, (_, row) => row * 2);
  return new Table({
    width: { size: size * 2, type: WidthType.DXA },
    columnWidths: [size, size],
    layout: TableLayoutType.FIXED,
    borders: noBorders,
    rows: pairs.map(
      (first) =>
        new TableRow({
          cantSplit: true,
          children: [first, first + 1].map(
            (i) =>
              new TableCell({
                width: { size, type: WidthType.DXA },
                margins: { top: first ? 240 : 0, bottom: 0, left: 0, right: GUTTER },
                children: metrics[i]
                  ? [
                      new Paragraph({
                        spacing: { after: 160 },
                        children: [
                          run(metrics[i].value, { size: metricSize(metrics[i]), tracking: -4 }),
                          ...(metrics[i].unit
                            ? [
                                run(
                                  /^\p{L}/u.test(metrics[i].unit)
                                    ? `\u00a0${metrics[i].unit}`
                                    : metrics[i].unit,
                                  { size: metricSize(metrics[i]) / 2 },
                                ),
                              ]
                            : []),
                        ],
                      }),
                      label(metrics[i].label),
                    ]
                  : [new Paragraph({})],
              }),
          ),
        }),
    ),
  });
}

function facts(rows: ReportFact[]): Paragraph[] {
  return rows.flatMap((f, i) => [
    label(f.label, { before: i === 0 ? 360 : 200, after: 40 }),
    text(f.value, { size: 17, color: MUTED, after: 0 }),
  ]);
}

function hero(o: {
  label: string;
  title: string;
  titleSize: number;
  heading: (typeof HeadingLevel)[keyof typeof HeadingLevel];
  facts?: ReportFact[];
  lead?: string;
  aside?: Paragraph[];
  about?: string;
  text?: string;
  metrics?: ReportMetric[];
}): (Paragraph | Table)[] {
  const metrics = o.metrics ?? [];
  const left: Cell = [
    label(o.label, { after: 200 }),
    new Paragraph({
      heading: o.heading,
      spacing: { after: 0, line: 252 },
      children: [run(o.title, { size: o.titleSize, tracking: -Math.round(o.titleSize / 3) })],
    }),
    ...facts(o.facts ?? []),
  ];
  const right: Cell = [...(o.lead ? [text(o.lead, { color: MUTED })] : []), ...(o.aside ?? [])];
  const card: Cell = [
    columns(right.length > 0 ? [left, right] : [left], { top: RULE, bottom: RULE, pad: 280 }),
  ];
  if (o.about || o.text || metrics.length > 0) {
    // Две таблицы подряд Word склеивает в одну, и линейки шапки пропадают.
    card.push(
      new Paragraph({
        spacing: { before: o.about ? 360 : 0, after: o.about ? 200 : 0 },
        children: o.about ? [run(o.about, { size: 28, tracking: -6 })] : [],
      }),
      columns(
        [
          o.text ? [text(o.text, { color: MUTED })] : [],
          metrics.length > 0 ? [metricsGrid(metrics)] : [],
        ],
        { pad: o.about ? 0 : 280 },
      ),
    );
  }
  return [
    // Заголовок раздела не должен остаться внизу листа без карточки.
    new Paragraph({ spacing: { before: 240, after: 0 }, keepNext: true, children: [] }),
    // Одна неразрывная строка: LibreOffice не соблюдает «не отрывать от следующего» в таблицах.
    columns([card]),
    // Иначе Word склеит карточку со следующей таблицей.
    new Paragraph({ spacing: { after: 120 }, children: [] }),
  ];
}

function kvTable(rows: ReportFact[], o: { pad: number; line: number }): Table {
  const labelWidth = Math.round(WIDTH / 3);
  const cell = (width: number, children: Paragraph[]) =>
    new TableCell({
      width: { size: width, type: WidthType.DXA },
      margins: { top: o.pad, bottom: o.pad, left: 0, right: GUTTER },
      children,
    });
  return new Table({
    width: { size: WIDTH, type: WidthType.DXA },
    columnWidths: [labelWidth, WIDTH - labelWidth],
    layout: TableLayoutType.FIXED,
    borders: {
      ...noBorders,
      top: line(HAIRLINE),
      bottom: line(HAIRLINE),
      insideHorizontal: line(HAIRLINE),
    },
    rows: rows.map(
      (r) =>
        new TableRow({
          cantSplit: true,
          children: [
            cell(labelWidth, [label(r.label, { after: 0 })]),
            cell(WIDTH - labelWidth, [text(r.value, { size: 19, after: 0, line: o.line })]),
          ],
        }),
    ),
  });
}

// Ширина знака 9 pt с запасом и поля ячейки, в twips.
const CHAR = 110;
const CELL_PAD = 220;
const sum = (values: number[]) => values.reduce((total, v) => total + v, 0);

/** Без сетки LibreOffice и часть Word делят ширину поровну и ломают слова. */
function columnWidths(head: string[], rows: string[][]): number[] {
  const cells = head.map((hd, i) => [hd, ...rows.map((r) => r[i] ?? "")]);
  // Подпись колонки набрана капителью с разрядкой — её знак шире на четверть.
  const min = cells.map(
    (c) =>
      CELL_PAD +
      CHAR *
        Math.max(c[0]!.length * 1.25, ...c.flatMap((v) => v.split(/\s+/)).map((w) => w.length)),
  );
  const extra = cells.map((c, i) =>
    Math.max(0, CELL_PAD + CHAR * Math.min(60, Math.max(...c.map((v) => v.length))) - min[i]!),
  );
  const spare = WIDTH - sum(min);
  if (spare <= 0 || sum(extra) === 0) return min.map((m) => Math.floor((m * WIDTH) / sum(min)));
  return min.map((m, i) => Math.floor(m + (spare * extra[i]!) / sum(extra)));
}

function dataTable(head: string[], rows: string[][]): Table {
  const widths = columnWidths(head, rows);
  const cell = (children: Paragraph[], i: number, o: { head?: boolean } = {}) =>
    new TableCell({
      width: { size: widths[i] ?? 0, type: WidthType.DXA },
      margins: { top: 80, bottom: 80, left: i === 0 ? 0 : 100, right: 100 },
      borders: o.head ? { bottom: line(RULE) } : undefined,
      children,
    });
  return new Table({
    width: { size: WIDTH, type: WidthType.DXA },
    columnWidths: widths,
    layout: TableLayoutType.FIXED,
    borders: { ...noBorders, bottom: line(HAIRLINE), insideHorizontal: line(HAIRLINE) },
    rows: [
      new TableRow({
        tableHeader: true,
        cantSplit: true,
        children: head.map((hd, i) => cell([label(hd, { after: 0 })], i, { head: true })),
      }),
      ...rows.map(
        (r) =>
          new TableRow({
            cantSplit: true,
            children: r.map((c, i) => cell([text(c, { size: 18, after: 0 })], i)),
          }),
      ),
    ],
  });
}

function doctitle(b: Extract<ReportBlock, { kind: "doctitle" }>) {
  return hero({
    label: [b.doctype, b.org].filter(Boolean).join(" — "),
    title: b.title,
    titleSize: 52,
    heading: HeadingLevel.TITLE,
    facts: b.facts,
    lead: b.lead,
    about: b.about,
    text: b.text,
    metrics: b.metrics,
    aside: [
      ...(b.meta.length > 0 ? [label(b.meta.join(" · "), { color: MUTED, after: 120 })] : []),
      ...b.lines.map((l) => text(l, { size: 17, color: MUTED, after: 60 })),
    ],
  });
}

function renderBlocks(doc: ReportDoc): (Paragraph | Table)[] {
  const out: (Paragraph | Table)[] = [];
  let tableNo = 0;
  const gap = (twips: number) => (doc.dense ? Math.round(twips / 2) : twips);
  const lineHeight = doc.dense ? 264 : LINE;
  // Без абзаца Word склеит таблицу со следующей; в плотном документе он в полстроки.
  const spacer = () =>
    new Paragraph({
      spacing: doc.dense ? { after: 0, line: 120, lineRule: LineRuleType.EXACT } : { after: 120 },
      children: [],
    });
  for (const [i, b] of doc.blocks.entries()) {
    // Иначе заголовок с пояснением остаётся внизу листа, а таблица уходит на следующий.
    const previous = doc.blocks[i - 1]?.kind;
    const afterHeading = previous === "heading" || previous === "subheading";
    switch (b.kind) {
      case "doctitle":
        out.push(...doctitle(b));
        break;
      case "card":
        out.push(
          ...hero({
            ...b,
            titleSize: 40,
            heading: HeadingLevel.HEADING_2,
          }),
        );
        break;
      case "heading":
        out.push(
          new Paragraph({
            heading: HeadingLevel.HEADING_1,
            keepNext: true,
            spacing: { before: gap(480), after: gap(200) },
            border: { top: { ...line(RULE), space: 10 } },
            children: [run(b.text, { size: 32, tracking: -8 })],
          }),
        );
        break;
      case "subheading":
        out.push(
          new Paragraph({
            heading: HeadingLevel.HEADING_2,
            keepNext: true,
            spacing: { before: gap(280), after: gap(120) },
            children: [run(b.text, { size: 24, tracking: -4 })],
          }),
        );
        break;
      case "paragraph":
        out.push(
          b.muted
            ? text(b.text, {
                size: 17,
                color: MUTED,
                italic: b.italic,
                bold: b.bold,
                keepNext: afterHeading,
                after: gap(120),
                line: lineHeight,
              })
            : text(b.text, {
                italic: b.italic,
                bold: b.bold,
                keepNext: afterHeading,
                after: gap(120),
                line: lineHeight,
              }),
        );
        break;
      case "kv":
        out.push(kvTable(b.rows, { pad: gap(90), line: lineHeight }), spacer());
        break;
      case "table":
        if (b.title) {
          // Словарь общий: Word собирается и для технологии, где словарь гипотез не загружен.
          out.push(
            label(i18n.t("ui.export.tableCaption", { n: ++tableNo, title: b.title }), {
              color: MUTED,
              before: 240,
              after: 100,
            }),
          );
        }
        out.push(
          dataTable(b.head, b.rows),
          new Paragraph({ spacing: { after: 120 }, children: [] }),
        );
        break;
      case "list":
        b.items.forEach((item: string, n: number) => {
          out.push(
            new Paragraph({
              spacing: { after: 60, line: lineHeight },
              indent: { left: 360, hanging: 360 },
              tabStops: [{ type: TabStopType.LEFT, position: 360 }],
              bullet: b.ordered ? undefined : { level: 0 },
              children: b.ordered
                ? [run(`${n + 1}.`, { mono: true, size: 17, color: MUTED }), tab(), run(item)]
                : [run(item)],
            }),
          );
        });
        break;
      case "quote":
        if (doc.dense) {
          out.push(
            new Paragraph({
              spacing: { before: gap(200), after: gap(120), line: lineHeight },
              indent: { left: 240 },
              border: { left: { style: BorderStyle.SINGLE, size: 12, color: RULE, space: 10 } },
              children: [
                run(`${b.source.toUpperCase()}  `, {
                  mono: true,
                  size: 15,
                  tracking: 9,
                  color: MUTED,
                }),
                run(`«${b.text}»`, { size: 19 }),
                ...(b.note ? [run(`  ${b.note}`, { size: 15, color: MUTED })] : []),
              ],
            }),
          );
          break;
        }
        out.push(
          new Paragraph({
            spacing: { before: gap(200), after: 60 },
            keepNext: true,
            indent: { left: 240 },
            border: { left: { style: BorderStyle.SINGLE, size: 12, color: RULE, space: 10 } },
            children: [
              run(b.source.toUpperCase(), { mono: true, size: 15, tracking: 9, color: MUTED }),
            ],
          }),
          new Paragraph({
            spacing: { after: b.note ? 40 : gap(120), line: lineHeight },
            indent: { left: 240 },
            border: { left: { style: BorderStyle.SINGLE, size: 12, color: RULE, space: 10 } },
            children: [run(`«${b.text}»`, { size: 19 })],
          }),
        );
        if (b.note) {
          out.push(
            new Paragraph({
              spacing: { after: 120 },
              indent: { left: 240 },
              border: { left: { style: BorderStyle.SINGLE, size: 12, color: RULE, space: 10 } },
              children: [run(b.note, { size: 15, color: MUTED })],
            }),
          );
        }
        break;
      case "references":
        for (const item of b.items) {
          const children: (TextRun | ExternalHyperlink)[] = [
            run(String(item.n).padStart(2, "0"), { mono: true, size: 15, color: MUTED }),
            tab(),
            run(item.text, { size: 18 }),
          ];
          if (item.url) {
            children.push(
              new ExternalHyperlink({
                link: item.url,
                children: [run(` ${item.url}`, { size: 16, color: MUTED })],
              }),
            );
          }
          out.push(
            new Paragraph({
              spacing: { after: item.note ? 40 : 120, line: 276 },
              indent: { left: 567, hanging: 567 },
              tabStops: [{ type: TabStopType.LEFT, position: 567 }],
              keepNext: !!item.note,
              children,
            }),
          );
          if (item.note) {
            out.push(
              new Paragraph({
                spacing: { after: 160, line: 276 },
                indent: { left: 567 },
                children: [run(item.note, { size: 17, color: MUTED })],
              }),
            );
          }
        }
        break;
    }
  }
  return out;
}

function footerTitle(title: string): string {
  return title.length > 70 ? `${title.slice(0, 69)}…` : title;
}

export async function downloadBlocksDocx(report: ReportDoc): Promise<void> {
  const doc = new Document({
    creator: "Технологический радар",
    title: report.title,
    styles: { default: { document: { run: { font: SANS, size: 21, color: INK } } } },
    sections: [
      {
        properties: {
          titlePage: true,
          page: { margin: { top: MARGIN, bottom: MARGIN, left: MARGIN, right: MARGIN } },
        },
        footers: {
          default: new Footer({
            children: [
              new Paragraph({
                tabStops: [{ type: TabStopType.RIGHT, position: WIDTH }],
                children: [
                  run(footerTitle(report.title).toUpperCase(), {
                    mono: true,
                    size: 14,
                    tracking: 9,
                    color: MUTED,
                  }),
                  new TextRun({
                    children: [new Tab(), PageNumber.CURRENT],
                    font: MONO,
                    size: 14,
                    color: MUTED,
                  }),
                ],
              }),
            ],
          }),
          first: new Footer({ children: [] }),
        },
        children: renderBlocks(report),
      },
    ],
  });
  saveFile(report.fileName, await Packer.toBlob(doc));
}
