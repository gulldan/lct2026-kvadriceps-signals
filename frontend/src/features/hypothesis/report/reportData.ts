import { listClusters, type ApiCluster } from "@/features/cluster";
import {
  getHypothesis,
  getHypothesisBoard,
  type ApiHypothesis,
  type Itc,
  type ItcComponent,
} from "@/features/hypothesis/api";
import { isClosed, primaryQueue, verdictMeta } from "@/features/hypothesis/board/model";
import {
  cleanInternalTitle,
  displayHypothesisTitle,
  hasProjectedTheme,
  hypothesisClusterId,
  isFallbackDraft,
  isResearchDirection,
  priorityScore,
  statusMeta,
  successCriteriaLines,
} from "@/features/hypothesis/model";
import { parseGeneration } from "@/features/hypothesis/provenance";
import { listKPIs, type ApiKPI } from "@/features/kpi";
import type { ReportBlock, ReportDoc, ReportFact, ReportMetric } from "@/shared/report/blocks";
import { i18n } from "@/shared/i18n";
import { formatDate } from "@/shared/lib/format";

const REPORT_PAGE_SIZE = 200;
const REPORT_ROWS_MAX = 40;
const PILOTS_MAX = 5;

const WORK_QUEUES = ["ready", "risk", "needs_verify", "insufficient"] as const;
type WorkQueue = (typeof WORK_QUEUES)[number];
const QUEUE_LABEL = {
  ready: "queues.ready.label",
  risk: "queues.risk.label",
  needs_verify: "queues.needsVerify.label",
  insufficient: "queues.insufficient.label",
} as const satisfies Record<WorkQueue, string>;

interface KpiRow {
  kpi: ApiKPI;
  total: number;
  ready: number;
  best: number | null;
}

// Закрытые гипотезы (архив и отказ) только считаются: иначе они забивали топ по приоритету и
// попадали в кандидаты на пилот.
export interface ReportData {
  ranked: ApiHypothesis[];
  shown: ApiHypothesis[];
  closedTotal: number;
  queues: Record<WorkQueue, number>;
  kpis: ApiKPI[];
  kpiRows: KpiRow[];
  pilots: ApiHypothesis[];
  constraints: Record<string, string>;
  themes: Record<string, string>;
}

export function kpiTargetLabel(k: ApiKPI): string {
  if (k.baseline === null && k.target === null) return "—";
  const unit = k.unit !== "" ? ` ${k.unit}` : "";
  if (k.baseline !== null && k.target !== null) return `${k.baseline} → ${k.target}${unit}`;
  return `${k.baseline ?? k.target}${unit}`;
}

// ИТЦ с бэка может прийти без компонент/полосы — отчёт не должен падать.
export function itcComponents(itc: Itc): ItcComponent[] {
  const c = itc.components;
  if (!c) return [];
  return [c.SM, c.NV, c.IP, c.HR].filter((x): x is ItcComponent => Boolean(x));
}

async function loadConstraints(pilots: ApiHypothesis[]): Promise<Record<string, string>> {
  const pairs = await Promise.all(
    pilots.map(async (h) => {
      try {
        return [h.id, parseGeneration(await getHypothesis(h.id)).constraints ?? ""] as const;
      } catch {
        return [h.id, ""] as const;
      }
    }),
  );
  return Object.fromEntries(pairs.filter(([, text]) => text !== ""));
}

export async function loadReportData(): Promise<ReportData> {
  const [board, kpis, clusters] = await Promise.all([
    getHypothesisBoard({ limit: REPORT_PAGE_SIZE, offset: 0 }),
    listKPIs(),
    listClusters().catch(() => [] as ApiCluster[]),
  ]);
  // Шаг — фактический размер первой страницы: сервер может урезать limit. Страницы независимы,
  // поэтому идут разом, а цикл ниже лишь добирает недостачу, если какая-то пришла короче.
  const step = board.items.length;
  const offsets: number[] = [];
  if (step > 0) for (let offset = step; offset < board.total; offset += step) offsets.push(offset);
  const pages = await Promise.all(
    offsets.map((offset) => getHypothesisBoard({ limit: REPORT_PAGE_SIZE, offset })),
  );
  for (const page of pages) board.items.push(...page.items);
  while (board.items.length < board.total) {
    const page = await getHypothesisBoard({ limit: REPORT_PAGE_SIZE, offset: board.items.length });
    if (page.items.length === 0) throw new Error("Incomplete hypothesis portfolio");
    board.items.push(...page.items);
  }

  const portfolio = board.items.filter((h) => !isFallbackDraft(h) && !isResearchDirection(h));
  const ranked = portfolio
    .filter((h) => !isClosed(h))
    .toSorted((a, b) => (priorityScore(b) ?? -1) - (priorityScore(a) ?? -1));
  const ready = ranked.filter((h) => primaryQueue(h) === "ready");

  const kpiRows: KpiRow[] = kpis.map((k) => {
    const linked = ranked.filter((h) => h.kpi_id === k.id);
    const top = linked[0];
    return {
      kpi: k,
      total: linked.length,
      ready: linked.filter((h) => primaryQueue(h) === "ready").length,
      best: top ? priorityScore(top) : null,
    };
  });

  const pilots = ready.slice(0, PILOTS_MAX);
  return {
    ranked,
    shown: ranked.slice(0, REPORT_ROWS_MAX),
    closedTotal: portfolio.length - ranked.length,
    queues: Object.fromEntries(
      WORK_QUEUES.map((queue) => [queue, ranked.filter((h) => primaryQueue(h) === queue).length]),
    ) as Record<WorkQueue, number>,
    kpis,
    kpiRows,
    pilots,
    constraints: await loadConstraints(pilots),
    themes: Object.fromEntries(
      pilots.flatMap((h) => {
        const cluster = clusters.find((c) => c.id === hypothesisClusterId(h));
        return cluster ? [[cluster.id, cleanInternalTitle(cluster.label)]] : [];
      }),
    ),
  };
}

const translate = i18n.t.bind(i18n) as (key: string, options?: Record<string, unknown>) => string;
const t = (key: string, options?: Record<string, unknown>) =>
  translate(`hypothesis:${key}`, options);

export function plainText(s: string): string {
  return s
    .replace(/\*\*([^*]+)\*\*/g, "$1")
    .replace(/\*([^*]+)\*/g, "$1")
    .replace(/^#+\s*/gm, "")
    .replace(/^\s*[-•]\s+/gm, "— ")
    .trim();
}

export function trimSnippet(s: string): string {
  const flat = s.replace(/\s+/g, " ").trim();
  if (flat.length <= 400) return flat;
  const cut = flat.slice(0, 400);
  const end = Math.max(cut.lastIndexOf(". "), cut.lastIndexOf("! "), cut.lastIndexOf("? "));
  if (end > 200) return cut.slice(0, end + 1);
  const wordEnd = cut.lastIndexOf(" ");
  return `${wordEnd > 0 ? cut.slice(0, wordEnd) : cut}…`;
}

// В аннотации важен законченный вывод: предел символов мог отрезать само значение измерения.
export function firstSentence(s: string): string {
  const flat = plainText(s).replace(/\s+/g, " ");
  const sentences = new Intl.Segmenter(i18n.resolvedLanguage, { granularity: "sentence" });
  return sentences.segment(flat)[Symbol.iterator]().next().value?.segment.trim() ?? "";
}

export function hypothesisCard(h: ApiHypothesis, kpi?: ApiKPI, theme?: string) {
  const target = (h.detail?.quantitative_parameters ?? []).find(
    (p) => (p.role ?? "").toLowerCase() === "target" && p.name && p.value !== undefined,
  );
  const priority = priorityScore(h);
  const metrics: ReportMetric[] = [];
  if (priority !== null)
    metrics.push({ value: String(priority), unit: "/100", label: t("hreport.p.priority") });
  if (target?.name && target.value !== undefined)
    metrics.push({ value: String(target.value), unit: target.unit, label: target.name });
  else if (h.trl !== null)
    metrics.push({ value: String(h.trl), unit: "/9", label: t("hreport.p.trl") });
  const facts: ReportFact[] = [];
  if (kpi)
    facts.push({ label: t("hreport.p.goal"), value: `${kpi.title} (${kpiTargetLabel(kpi)})` });
  if (theme && !hasProjectedTheme(h)) facts.push({ label: t("hreport.p.theme"), value: theme });
  return {
    label: [h.trl === null ? "" : `TRL ${h.trl}`, statusMeta(h.status).label]
      .filter(Boolean)
      .join(" — "),
    title: displayHypothesisTitle(h),
    facts,
    lead: trimSnippet(plainText(h.detail?.problem_addressed || h.rationale)) || undefined,
    metrics,
  };
}

export function portfolioReportDoc(data: ReportData): ReportDoc {
  const blocks: ReportBlock[] = [
    {
      kind: "doctitle",
      org: null,
      doctype: t("report.kicker", { date: formatDate(new Date().toISOString()) }),
      title: t("report.title"),
      meta: [t("report.summary", { goals: data.kpis.length, closed: data.closedTotal })],
      lines: [],
      about: t("report.aboutTitle"),
      text: t("report.aboutText"),
      metrics: [
        { value: String(data.ranked.length), label: t("report.openLabel") },
        ...(["ready", "risk", "needs_verify"] as const).map((queue) => ({
          value: String(data.queues[queue]),
          label: t(QUEUE_LABEL[queue]),
        })),
      ],
    },
  ];
  let n = 0;
  const section = (title: string, note?: string) => {
    blocks.push({ kind: "heading", text: `${++n}. ${t(title)}` });
    if (note) blocks.push({ kind: "paragraph", muted: true, text: t(note) });
  };

  section("report.pilotsTitle", data.pilots.length > 0 ? "report.pilotsNote" : "report.pilotsNone");
  for (const [i, h] of data.pilots.entries()) {
    const criteria = successCriteriaLines(h.detail?.experiment_plan?.success_criteria);
    const card = hypothesisCard(
      h,
      data.kpis.find((k) => k.id === h.kpi_id),
      data.themes[hypothesisClusterId(h) ?? ""],
    );
    blocks.push({
      kind: "card",
      ...card,
      label: [`${n}.${i + 1}`, h.trl === null ? "" : `TRL ${h.trl}`, verdictMeta(h)?.label]
        .filter(Boolean)
        .join(" — "),
      about: t("report.pilotAbout"),
      text: h.statement,
    });
    if (h.detail?.problem_addressed && h.rationale !== "")
      blocks.push({ kind: "paragraph", text: `${t("report.pilotRationale")}. ${h.rationale}` });
    if (data.constraints[h.id])
      blocks.push({
        kind: "paragraph",
        muted: true,
        text: `${t("report.pilotConstraints")}: ${data.constraints[h.id]}`,
      });
    if (criteria.length > 0)
      blocks.push({
        kind: "paragraph",
        muted: true,
        text: `${t("report.pilotCriteria")}: ${criteria.join("; ")}`,
      });
  }

  if (data.ranked.length > 0) {
    section("report.rankingTitle", "report.rankingNote");
    blocks.push({
      kind: "table",
      head: (["num", "hypothesis", "priority", "check", "trl", "queue"] as const).map((key) =>
        t(`report.rankHead.${key}`),
      ),
      rows: data.shown.map((h, i) => {
        const queue = primaryQueue(h);
        return [
          String(i + 1),
          displayHypothesisTitle(h),
          String(priorityScore(h) ?? "—"),
          verdictMeta(h)?.label ?? "—",
          h.trl === null ? "—" : String(h.trl),
          queue && queue !== "all" && queue !== "needs_trl" ? t(QUEUE_LABEL[queue]) : "—",
        ];
      }),
    });
    if (data.ranked.length > data.shown.length)
      blocks.push({
        kind: "paragraph",
        muted: true,
        text: t("report.truncated", { shown: data.shown.length, total: data.ranked.length }),
      });
  }

  if (data.kpiRows.length > 0) {
    section("report.goalsTitle");
    blocks.push({
      kind: "table",
      head: (["goal", "target", "hypotheses", "ready", "best"] as const).map((key) =>
        t(`report.goalsHead.${key}`),
      ),
      rows: data.kpiRows.map(({ kpi, total, ready, best }) => [
        kpi.metric === "" ? kpi.title : `${kpi.title} (${kpi.metric})`,
        kpiTargetLabel(kpi),
        String(total),
        String(ready),
        best === null ? "—" : `${best}/100`,
      ]),
    });
  }

  blocks.push({ kind: "paragraph", muted: true, text: t("report.footer") });
  return { fileName: t("report.docxFileName"), title: t("report.title"), blocks };
}
